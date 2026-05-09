/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

package model

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 通过 model 包的 TestMain（已经迁移 User/Token 表，使用 SQLite in-memory）共享测试 DB。
// 每个测试自己注册 t.Cleanup，避免污染其它用例。

// truncateUsers 在测试结束时清空 users 表，避免相互污染。
func truncateUsers(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		DB.Exec("DELETE FROM users")
	})
}

// seedUserForUsername 直接插入一条占位用户（仅占用 username，密码非真实哈希），
// 用于测试 BuildMigratedUsername 在 DB 冲突时的退避行为。
// users.aff_code 字段是 uniqueIndex，因此每条种子用户必须分配唯一 aff_code，
// 否则第二条插入会触发 UNIQUE 约束失败。
func seedUserForUsername(t *testing.T, username string) {
	t.Helper()
	user := &User{
		Username:    username,
		Password:    "placeholder", // 测试用，跳过密码哈希校验
		DisplayName: username,
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		// 使用 username 作为 aff_code 的本地化前缀，确保跨种子用户不冲突。
		AffCode: "aff_" + username,
	}
	require.NoError(t, DB.Create(user).Error, "failed to seed user: %s", username)
}

// ---------------------------------------------------------------------------
// BuildMigratedUsername — 关键分支测试
// ---------------------------------------------------------------------------

// 1) 普通名：不冲突时直接返回 trim 后的原名。
func TestBuildMigratedUsername_PlainName(t *testing.T) {
	truncateUsers(t)

	assigned := map[string]bool{}
	got, err := BuildMigratedUsername(DB, "alice", 1, assigned)
	require.NoError(t, err)
	assert.Equal(t, "alice", got)
}

// 2) 首尾空白被 TrimSpace 去掉。
func TestBuildMigratedUsername_TrimsWhitespace(t *testing.T) {
	truncateUsers(t)

	assigned := map[string]bool{}
	got, err := BuildMigratedUsername(DB, "   bob  ", 1, assigned)
	require.NoError(t, err)
	assert.Equal(t, "bob", got)
}

// 3) 空名兜底为 token_<id>。
func TestBuildMigratedUsername_EmptyNameFallback(t *testing.T) {
	truncateUsers(t)

	assigned := map[string]bool{}
	got, err := BuildMigratedUsername(DB, "", 42, assigned)
	require.NoError(t, err)
	assert.Equal(t, "token_42", got)
}

// 4) 全空白名同样兜底（trim 后为空）。
func TestBuildMigratedUsername_AllWhitespaceFallback(t *testing.T) {
	truncateUsers(t)

	assigned := map[string]bool{}
	got, err := BuildMigratedUsername(DB, "   \t  \n", 7, assigned)
	require.NoError(t, err)
	assert.Equal(t, "token_7", got)
}

// 5) 超长 ASCII 主体按 rune 截断到 ≤ 20。
func TestBuildMigratedUsername_TruncatesLongAsciiByRune(t *testing.T) {
	truncateUsers(t)

	long := strings.Repeat("a", 50)
	assigned := map[string]bool{}
	got, err := BuildMigratedUsername(DB, long, 1, assigned)
	require.NoError(t, err)
	// 主体被截到 20 个 rune
	assert.Equal(t, 20, utf8.RuneCountInString(got))
	assert.Equal(t, strings.Repeat("a", 20), got)
}

// 6) 中文名按 rune 截断（不会把多字节字符切坏）。
//    生成 25 个中文字符（每个 3 字节），截断后应剩 20 个 rune（60 字节），
//    而不是按 byte 截到 20 字节（约 6.67 个字符，会切坏中文）。
func TestBuildMigratedUsername_TruncatesChineseByRune(t *testing.T) {
	truncateUsers(t)

	// 用「测」字（U+6D4B，UTF-8 占 3 字节）拼出 25 个 rune
	long := strings.Repeat("测", 25)
	assigned := map[string]bool{}
	got, err := BuildMigratedUsername(DB, long, 1, assigned)
	require.NoError(t, err)
	assert.Equal(t, 20, utf8.RuneCountInString(got))
	assert.Equal(t, strings.Repeat("测", 20), got)
	// 验证字节数也符合 rune 截断（20 个 3 字节字符 = 60 字节）
	assert.Equal(t, 60, len(got))
	// 验证 UTF-8 仍然有效（无切坏）
	assert.True(t, utf8.ValidString(got))
}

// 7) 与本批次 assigned 集合冲突 -> 自动加 _2。
func TestBuildMigratedUsername_BatchConflictAddsSuffix(t *testing.T) {
	truncateUsers(t)

	assigned := map[string]bool{"charlie": true}
	got, err := BuildMigratedUsername(DB, "charlie", 1, assigned)
	require.NoError(t, err)
	assert.Equal(t, "charlie_2", got)
}

// 8) 与 DB 中已存在的用户冲突 -> 自动加 _2。
func TestBuildMigratedUsername_DBConflictAddsSuffix(t *testing.T) {
	truncateUsers(t)
	seedUserForUsername(t, "dave")

	assigned := map[string]bool{}
	got, err := BuildMigratedUsername(DB, "dave", 1, assigned)
	require.NoError(t, err)
	assert.Equal(t, "dave_2", got)
}

// 9) DB 已占 dave / dave_2，第三次冲突 -> dave_3。
func TestBuildMigratedUsername_MultipleConflictsIncrement(t *testing.T) {
	truncateUsers(t)
	seedUserForUsername(t, "eve")
	seedUserForUsername(t, "eve_2")

	assigned := map[string]bool{}
	got, err := BuildMigratedUsername(DB, "eve", 1, assigned)
	require.NoError(t, err)
	assert.Equal(t, "eve_3", got)
}

// 10) 主体已经接近 20 rune，加后缀时主体被进一步压缩，保证最终 ≤ 20。
func TestBuildMigratedUsername_TruncateBaseWhenAddingSuffix(t *testing.T) {
	truncateUsers(t)

	// 19 个 'a' 主体，DB 已占用整名，需要加 _2 -> 总长 21 rune > 20
	// 算法应压缩主体到 18 rune，最终 "aaaa..._2" 共 20 rune
	base := strings.Repeat("a", 19)
	seedUserForUsername(t, base)

	assigned := map[string]bool{}
	got, err := BuildMigratedUsername(DB, base, 1, assigned)
	require.NoError(t, err)
	assert.LessOrEqual(t, utf8.RuneCountInString(got), MaxMigratedUsernameRunes)
	// 后缀必须保留完整
	assert.True(t, strings.HasSuffix(got, "_2"))
}

// 11) 跨「DB + assigned」混合冲突也能正确推进。
func TestBuildMigratedUsername_MixedConflict(t *testing.T) {
	truncateUsers(t)
	seedUserForUsername(t, "frank")

	// frank（DB 占用） + frank_2（assigned 占用） -> 预期 frank_3
	assigned := map[string]bool{"frank_2": true}
	got, err := BuildMigratedUsername(DB, "frank", 1, assigned)
	require.NoError(t, err)
	assert.Equal(t, "frank_3", got)
}

// 12) BuildMigratedUsername 不应自动把成功的 username 写回 assigned，
//     调用方需要在事务提交后自行加入。
func TestBuildMigratedUsername_DoesNotMutateAssigned(t *testing.T) {
	truncateUsers(t)

	assigned := map[string]bool{}
	_, err := BuildMigratedUsername(DB, "george", 1, assigned)
	require.NoError(t, err)
	assert.Empty(t, assigned, "BuildMigratedUsername 不应自动写回 assigned")
}

// 13) 重试次数耗尽时返回的错误必须可以被 errors.Is 识别为
//     ErrUsernameConflictRetryExceeded（controller 层据此映射 i18n 文案）。
func TestBuildMigratedUsername_ErrorIsSentinel(t *testing.T) {
	truncateUsers(t)

	// 制造无法逃逸的冲突：把 base + base_2..base_<MaxRetry+1> 全部塞进 assigned。
	// 用一个短名字（"x"）保证加序号也不会超长。
	base := "x"
	assigned := map[string]bool{base: true}
	for i := 2; i <= MaxMigratedUsernameRetry+1; i++ {
		assigned[fmt.Sprintf("%s_%d", base, i)] = true
	}

	_, err := BuildMigratedUsername(DB, base, 1, assigned)
	require.Error(t, err)
	assert.True(t,
		errors.Is(err, ErrUsernameConflictRetryExceeded),
		"期望错误能被 errors.Is 识别为 ErrUsernameConflictRetryExceeded，但得到: %v", err)
}

// ---------------------------------------------------------------------------
// MigrateTokenOwnership — 关键分支测试
// ---------------------------------------------------------------------------

// truncateTokens 在测试结束时清空 tokens 表，避免相互污染。
func truncateTokens(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		DB.Exec("DELETE FROM tokens")
	})
}

// seedTokenForOwnership 插入一条令牌占位用于测试 MigrateTokenOwnership。
// tokens.key 是 uniqueIndex，因此每条种子令牌的 key 必须唯一。
func seedTokenForOwnership(t *testing.T, userId int, name, key string) *Token {
	t.Helper()
	tk := &Token{
		UserId:         userId,
		Name:           name,
		Key:            key,
		Status:         common.TokenStatusEnabled,
		CreatedTime:    1,
		AccessedTime:   1,
		ExpiredTime:    -1,
		RemainQuota:    100,
		UnlimitedQuota: false,
		Group:          "default",
	}
	require.NoError(t, DB.Create(tk).Error, "failed to seed token: %s", name)
	return tk
}

// 1) 正常切换 user_id：影响 1 行，无错误。
func TestMigrateTokenOwnership_Success(t *testing.T) {
	truncateTokens(t)

	tk := seedTokenForOwnership(t, 100, "tk1", "ownerkey1")
	err := MigrateTokenOwnership(DB, tk.Id, 100, 200)
	require.NoError(t, err)

	// 验证 user_id 真的被改了
	var reloaded Token
	require.NoError(t, DB.First(&reloaded, tk.Id).Error)
	assert.Equal(t, 200, reloaded.UserId)
	// 其他字段保持不变
	assert.Equal(t, "tk1", reloaded.Name)
	assert.Equal(t, "ownerkey1", reloaded.Key)
	assert.Equal(t, 100, reloaded.RemainQuota)
}

// 2) 源用户 ID 不匹配：不应影响任何行，并返回错误。
func TestMigrateTokenOwnership_SrcMismatch(t *testing.T) {
	truncateTokens(t)

	tk := seedTokenForOwnership(t, 100, "tk2", "ownerkey2")
	// 用错误的 srcUserId
	err := MigrateTokenOwnership(DB, tk.Id, 999, 200)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "归属切换失败")

	// 验证 user_id 没被改
	var reloaded Token
	require.NoError(t, DB.First(&reloaded, tk.Id).Error)
	assert.Equal(t, 100, reloaded.UserId)
}

// 3) tokenId 不存在：返回错误。
func TestMigrateTokenOwnership_TokenNotFound(t *testing.T) {
	truncateTokens(t)

	err := MigrateTokenOwnership(DB, 99999, 100, 200)
	require.Error(t, err)
}

// 4) src == new：业务逻辑不应允许，提前拦截返回错误。
func TestMigrateTokenOwnership_SrcEqualsNew(t *testing.T) {
	truncateTokens(t)

	tk := seedTokenForOwnership(t, 100, "tk3", "ownerkey3")
	err := MigrateTokenOwnership(DB, tk.Id, 100, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "相同")
}

// 5) 边界参数校验：0 值参数应当返回错误。
func TestMigrateTokenOwnership_ZeroArgs(t *testing.T) {
	truncateTokens(t)

	require.Error(t, MigrateTokenOwnership(DB, 0, 100, 200))
	require.Error(t, MigrateTokenOwnership(DB, 1, 0, 200))
	require.Error(t, MigrateTokenOwnership(DB, 1, 100, 0))
	require.Error(t, MigrateTokenOwnership(nil, 1, 100, 200))
}
