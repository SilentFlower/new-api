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
	"unicode/utf8"

	"gorm.io/gorm"
)

// 令牌迁移到独立账号相关的 model 层工具函数

// MaxMigratedUsernameRunes 限制新用户用户名最大 rune 数（与 User.Username 的 validate:"max=20" 对齐）。
const MaxMigratedUsernameRunes = 20

// MaxMigratedUsernameRetry 单个令牌生成 username 时最多允许的冲突重试次数。
// 超过该次数仍冲突则该令牌迁移失败上报，由超管修改令牌名后重试。
const MaxMigratedUsernameRetry = 100

// ErrUsernameConflictRetryExceeded 当 BuildMigratedUsername 在 MaxMigratedUsernameRetry 次内
// 仍无法找到可用用户名时返回的 sentinel 错误。controller 层可用 errors.Is 识别并替换为
// 国际化文案（见 i18n.MsgTokenMigrateUsernameConflict），其余底层错误照原样透传。
var ErrUsernameConflictRetryExceeded = errors.New("用户名冲突重试次数过多")

// GetTokensForMigration 拉取指定用户名下、指定 ID 列表的令牌，包含迁移所需的全部字段。
// 与 GetTokenKeysByIds 不同，这里需要 name / group / remain_quota / unlimited_quota 等字段，
// 因此不限制 Select 列。结果按传入 ids 顺序无关，调用方应自己根据 token.Id 关联回原始请求项。
func GetTokensForMigration(ids []int, userId int) ([]Token, error) {
	if len(ids) == 0 {
		return nil, errors.New("ids 不能为空")
	}
	if userId == 0 {
		return nil, errors.New("userId 不能为空")
	}
	var tokens []Token
	err := DB.Where("user_id = ? AND id IN (?)", userId, ids).Find(&tokens).Error
	return tokens, err
}

// RefreshTokenCache 暴露给 controller 层，用于在迁移事务提交后刷新 Redis 缓存。
// 内部仍然走 cacheSetToken，保持与项目其他写入路径一致。
// 注意：传入的 token 必须是「已经更新过 UserId 的副本」。
func RefreshTokenCache(token Token) error {
	return cacheSetToken(token)
}

// MigrateTokenOwnership 在事务内将指定令牌的 user_id 由 srcUserId 切换到 newUserId。
//   - 仅修改 user_id 字段，其他字段（key/group/remain_quota/used_quota/status 等）保持不变；
//   - 通过 WHERE id = ? AND user_id = ? 双条件防止误改他人令牌；
//   - 校验 RowsAffected == 1，否则返回错误（令牌不存在 / 已被他人迁移 / user_id 已被改）。
//
// 调用方应在更外层把「校验源用户、构造新用户、写入新用户、调用本函数」打成同一个事务，
// 任一环节失败即整体回滚，保证「新用户存在但令牌未切」或「令牌已切但新用户写入失败」不会发生。
func MigrateTokenOwnership(tx *gorm.DB, tokenId, srcUserId, newUserId int) error {
	if tx == nil {
		return errors.New("tx 不能为空")
	}
	if tokenId == 0 {
		return errors.New("tokenId 不能为空")
	}
	if srcUserId == 0 {
		return errors.New("srcUserId 不能为空")
	}
	if newUserId == 0 {
		return errors.New("newUserId 不能为空")
	}
	if srcUserId == newUserId {
		return errors.New("srcUserId 与 newUserId 相同，无需迁移")
	}

	result := tx.Model(&Token{}).
		Where("id = ? AND user_id = ?", tokenId, srcUserId).
		Update("user_id", newUserId)
	if result.Error != nil {
		return result.Error
	}
	// 期望恰好命中 1 行。0 行 => 令牌不存在或已被他人迁移；>1 行 => 数据异常。
	if result.RowsAffected != 1 {
		return fmt.Errorf("token 归属切换失败，预期影响 1 行，实际 %d 行", result.RowsAffected)
	}
	return nil
}

// runeTruncate 按 rune（即 Unicode 码点）截断字符串到最多 maxRunes 个 rune。
// 使用 rune 而非 byte，避免把中文等多字节字符切坏。
func runeTruncate(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes])
}

// runeCount 返回字符串的 rune 数，等价于 utf8.RuneCountInString，仅作语义封装。
func runeCount(s string) int {
	return utf8.RuneCountInString(s)
}

// usernameExistsInDB 检查给定用户名是否已被任何用户（含已软删除）占用。
// 与 CheckUserExistOrDeleted 功能等价，但接受事务句柄 tx，可在迁移事务内复用同一连接。
func usernameExistsInDB(tx *gorm.DB, username string) (bool, error) {
	if tx == nil {
		return false, errors.New("tx 不能为空")
	}
	if username == "" {
		return false, errors.New("username 不能为空")
	}
	var user User
	err := tx.Unscoped().Where("username = ?", username).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// composeCandidate 按基础名 base + 后缀 suffix 拼接候选用户名。
//   - 若 suffix 为空，结果即为 runeTruncate(base, MaxMigratedUsernameRunes)；
//   - 若 suffix 非空，则在保证最终 rune 数 ≤ MaxMigratedUsernameRunes 的前提下，
//     对 base 部分进一步压缩（截短主体），suffix 永远完整保留。
func composeCandidate(base, suffix string) string {
	suffixRunes := runeCount(suffix)
	if suffixRunes >= MaxMigratedUsernameRunes {
		// 极端情况：suffix 自己就超过最大长度，直接截断 suffix 兜底（理论不会发生，
		// 因为我们的 suffix 形如 "_2".."_100"，最长 4 个 rune）。
		return runeTruncate(suffix, MaxMigratedUsernameRunes)
	}
	maxBaseRunes := MaxMigratedUsernameRunes - suffixRunes
	truncatedBase := runeTruncate(base, maxBaseRunes)
	return truncatedBase + suffix
}

// BuildMigratedUsername 为单个待迁移令牌生成一个全局唯一的新用户名。
//
// 处理顺序（与 PRD 一致）：
//  1. tokenName -> TrimSpace；为空时使用 token_<tokenId> 作为兜底主体；
//  2. 否则把主体按 rune 截断到 MaxMigratedUsernameRunes；
//  3. 与 DB 中已存在用户名（含软删除）以及本批次 assigned 中的用户名冲突时，
//     依次尝试追加 "_2", "_3", ..., 直到第 MaxMigratedUsernameRetry 次仍冲突即失败；
//  4. 加后缀时若总长度超出 MaxMigratedUsernameRunes，会进一步压缩主体保证 ≤ 上限。
//
// 成功返回的 username 不会写回 assigned，调用方需在「事务提交成功」后自行
// 把返回值塞进 assigned，以避免因事务回滚导致名字被永久占用却没人用。
func BuildMigratedUsername(tx *gorm.DB, tokenName string, tokenId int, assigned map[string]bool) (string, error) {
	if tx == nil {
		return "", errors.New("tx 不能为空")
	}
	if assigned == nil {
		return "", errors.New("assigned 不能为 nil")
	}

	// 1) 计算主体（base）
	base := strings.TrimSpace(tokenName)
	if base == "" {
		base = fmt.Sprintf("token_%d", tokenId)
	}
	// 2) 主体首先按 rune 截断
	base = runeTruncate(base, MaxMigratedUsernameRunes)
	if base == "" {
		// 极端兜底：主体被截到空（理论不会发生，因为 token_<id> 至少一个字符）
		base = fmt.Sprintf("token_%d", tokenId)
		base = runeTruncate(base, MaxMigratedUsernameRunes)
	}

	// 3) 冲突检测 + 重试加后缀
	// attempt 从 0 起：0 = 不带后缀；1.. = 带后缀 _<n+1>
	for attempt := 0; attempt < MaxMigratedUsernameRetry; attempt++ {
		var candidate string
		if attempt == 0 {
			candidate = base
		} else {
			suffix := fmt.Sprintf("_%d", attempt+1)
			candidate = composeCandidate(base, suffix)
		}

		// 候选名不能为空（理论已被前面兜底覆盖）
		if candidate == "" {
			continue
		}

		// 本批次内冲突（同一次请求里其他令牌已被分配到这个名）
		if assigned[candidate] {
			continue
		}

		// DB 内冲突（含软删除用户）
		exists, err := usernameExistsInDB(tx, candidate)
		if err != nil {
			return "", err
		}
		if exists {
			continue
		}

		// 找到可用候选
		return candidate, nil
	}

	// 用 %w 包装 sentinel 错误，便于 controller 层 errors.Is 识别后替换为 i18n 文案。
	return "", fmt.Errorf("%w (base=%q)", ErrUsernameConflictRetryExceeded, base)
}
