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

package controller

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// MigrateTokensBatchMaxSize 单次迁移接口能处理的最大令牌数量。
// 与 GetTokenKeysBatch 等批量接口保持一致，避免被刷接口创建大量用户。
const MigrateTokensBatchMaxSize = 100

// migrateTokensRequest 迁移接口请求体。
// 仅接受 token_ids，新用户 role / status / quota / group 等敏感字段一律由后端推导，
// 不接受前端传入，防止越权构造。
type migrateTokensRequest struct {
	TokenIds []int `json:"token_ids"`
}

// migrateTokenResult 单个令牌迁移完成后的结果项，用于响应给前端。
// 出于安全考虑，永远不会包含 password 字段。
type migrateTokenResult struct {
	TokenId     int    `json:"token_id"`
	TokenName   string `json:"token_name"`
	NewUsername string `json:"new_username,omitempty"`
	NewUserId   int    `json:"new_user_id,omitempty"`
	Status      string `json:"status"` // "success" | "failed"
	Error       string `json:"error,omitempty"`
}

// MigrateTokensToAccounts 处理批量「令牌迁移到独立账号」请求。
//
// 接口语义：
//   - 仅 RoleRootUser 可调用（已在路由处用 RootAuth() 强制）。
//     handler 内再做一层 role 校验，作为越权防御纵深。
//   - 源账号固定为「当前登录用户」，按 c.GetInt("id") 取，不接受前端覆盖。
//   - 每个令牌的「创建新 user + 切换 token.user_id」打成独立 GORM 事务，
//     单令牌失败仅回滚自己，其他令牌继续尝试，最终响应里逐项返回成功 / 失败。
//   - 不返回密码、不写日志、不暴露密码相关字段。
func MigrateTokensToAccounts(c *gin.Context) {
	// 1) 双重保险：handler 内再校验 role，避免中间件配置失误时静默放行。
	if c.GetInt("role") != common.RoleRootUser {
		common.ApiErrorI18n(c, i18n.MsgTokenMigrateForbidden)
		return
	}

	// 2) 解析并校验请求体。
	var req migrateTokensRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgTokenMigrateInvalidIds)
		return
	}
	if len(req.TokenIds) == 0 {
		common.ApiErrorI18n(c, i18n.MsgTokenMigrateInvalidIds)
		return
	}
	if len(req.TokenIds) > MigrateTokensBatchMaxSize {
		common.ApiErrorI18n(c, i18n.MsgTokenMigrateBatchTooMany, map[string]any{"Max": MigrateTokensBatchMaxSize})
		return
	}

	srcUserId := c.GetInt("id")
	if srcUserId == 0 {
		common.ApiErrorI18n(c, i18n.MsgTokenMigrateForbidden)
		return
	}

	// 3) 拉取源用户信息，用于计算新用户 group 的回退值。
	srcUser, err := model.GetUserById(srcUserId, false)
	if err != nil {
		common.ApiError(c, fmt.Errorf("加载源用户失败: %w", err))
		return
	}

	// 4) 拉取待迁移令牌（带 user_id 过滤，确保只能迁移自己名下的令牌）。
	tokens, err := model.GetTokensForMigration(req.TokenIds, srcUserId)
	if err != nil {
		common.ApiError(c, fmt.Errorf("加载令牌失败: %w", err))
		return
	}

	// 5) 把拿到的令牌按 id 建索引，方便后面识别哪些 id 实际不存在。
	tokenById := make(map[int]*model.Token, len(tokens))
	for i := range tokens {
		tokenById[tokens[i].Id] = &tokens[i]
	}

	// 6) assigned 跨令牌共享，避免本批次内不同令牌生成出相同的新用户名。
	assigned := make(map[string]bool, len(req.TokenIds))

	results := make([]migrateTokenResult, 0, len(req.TokenIds))

	// 按客户端传入顺序处理，方便前端把结果与请求列表对齐。
	for _, tokenId := range req.TokenIds {
		tk, ok := tokenById[tokenId]
		if !ok {
			// 令牌不存在或不属于当前 root 账号 —— GetTokensForMigration 里
			// 已经按 user_id 过滤掉了，这里给前端清晰的失败反馈。
			results = append(results, migrateTokenResult{
				TokenId: tokenId,
				Status:  "failed",
				Error:   common.TranslateMessage(c, i18n.MsgTokenMigrateNotFound),
			})
			continue
		}

		newUsername, newUserId, migrateErr := migrateSingleTokenInTx(c, srcUser, tk, assigned)
		if migrateErr != nil {
			// 单令牌失败：默认把底层错误原样透传给超管（多为中文）；
			// 对已知的 sentinel 错误（如用户名冲突）替换为 i18n 翻译，照顾英文用户。
			errMsg := migrateErr.Error()
			if errors.Is(migrateErr, model.ErrUsernameConflictRetryExceeded) {
				errMsg = common.TranslateMessage(c, i18n.MsgTokenMigrateUsernameConflict)
			}
			results = append(results, migrateTokenResult{
				TokenId:   tk.Id,
				TokenName: tk.Name,
				Status:    "failed",
				Error:     errMsg,
			})
			continue
		}

		// 单令牌成功：把新用户名锁进 assigned，避免后续令牌重名。
		assigned[newUsername] = true

		// 异步刷新令牌 Redis 缓存，注意此时 token 的 UserId 已是 newUserId。
		// 复制一份 Token 值再传进 goroutine，避免后续循环修改原指针造成竞态。
		updatedToken := *tk
		updatedToken.UserId = newUserId
		gopool.Go(func() {
			if err := refreshMigratedTokenCache(updatedToken); err != nil {
				common.SysLog(fmt.Sprintf("failed to refresh token cache after migration: %v", err))
			}
		})

		// 写审计日志：仅记录令牌名 / id / 新用户名 / 新用户 id，不含密码。
		model.RecordLog(srcUserId, model.LogTypeManage,
			fmt.Sprintf("迁移令牌 %s (id=%d) 到新用户 %s (id=%d)", tk.Name, tk.Id, newUsername, newUserId))
		logger.LogInfo(c.Request.Context(),
			fmt.Sprintf("token migrate success: src_user=%d, token_id=%d, new_user_id=%d, new_username=%s",
				srcUserId, tk.Id, newUserId, newUsername))

		results = append(results, migrateTokenResult{
			TokenId:     tk.Id,
			TokenName:   tk.Name,
			NewUsername: newUsername,
			NewUserId:   newUserId,
			Status:      "success",
		})
	}

	common.ApiSuccess(c, gin.H{"results": results})
}

// migrateSingleTokenInTx 在独立事务里完成「为单个令牌创建新用户并切换归属」。
//
// 关键约束：
//   - 整个流程原子：新用户写入失败 -> token 不切；token 切换失败 -> 新用户回滚；
//     避免出现「新用户存在但 token 没切」或「token 切了但 user 还没建」的不一致状态。
//   - 不修改令牌 key / group / remain_quota / used_quota / status 等任何字段。
//   - 不返回 password；password 仅入库（bcrypt 哈希），由超管事后通过用户管理 → 编辑用户重置。
func migrateSingleTokenInTx(c *gin.Context, srcUser *model.User, tk *model.Token, assigned map[string]bool) (newUsername string, newUserId int, err error) {
	// 计算新用户的 group：token.Group 为空或 "auto" 时回退为源用户的 group，
	// 因为 "auto" 是渠道路由用语义，不应当作普通组直接落到 user.group。
	newGroup := tk.Group
	if newGroup == "" || newGroup == "auto" {
		newGroup = srcUser.Group
	}

	// 计算新用户的 quota：
	// - 普通令牌：直接继承令牌剩余额度 RemainQuota；
	// - 无限令牌：使用与 controller/token.go AddToken 处一致的 maxQuotaValue（事实无限）。
	var newQuota int
	if tk.UnlimitedQuota {
		newQuota = int(1000000000 * common.QuotaPerUnit)
	} else {
		newQuota = tk.RemainQuota
	}

	// 生成 16 字节随机密码。仅入库 bcrypt 哈希，绝不返回 / 写日志。
	rawPassword := common.GetRandomString(16)

	txErr := model.DB.Transaction(func(tx *gorm.DB) error {
		// (a) 在事务内生成 username（DB 查重 + 本批次 assigned 查重）
		uname, buildErr := model.BuildMigratedUsername(tx, tk.Name, tk.Id, assigned)
		if buildErr != nil {
			return buildErr
		}

		// (b) 创建新用户。强制写死敏感字段，不接受请求体覆盖。
		newUser := &model.User{
			Username:    uname,
			Password:    rawPassword, // InsertWithTx 内部会 bcrypt 哈希
			DisplayName: uname,
			Email:       "",
			Role:        common.RoleCommonUser,
			Status:      common.UserStatusEnabled,
			Group:       newGroup,
		}
		if insertErr := newUser.InsertWithTx(tx, 0); insertErr != nil {
			return insertErr
		}

		// (c) 把 quota 强制改成期望值。
		// 注意 InsertWithTx 内部会把 user.Quota 覆写成 common.QuotaForNewUser，
		// 这里必须显式 Update 把目标 quota 写回去。
		if updateQuotaErr := tx.Model(&model.User{}).
			Where("id = ?", newUser.Id).
			Update("quota", newQuota).Error; updateQuotaErr != nil {
			return updateQuotaErr
		}

		// (d) 切换 token.user_id：仅修改 user_id，其余字段全部保留。
		if migrateErr := model.MigrateTokenOwnership(tx, tk.Id, srcUser.Id, newUser.Id); migrateErr != nil {
			return migrateErr
		}

		// (e) 把新用户名 / 新用户 ID 通过闭包外的指针返回。
		newUsername = uname
		newUserId = newUser.Id
		return nil
	})

	if txErr != nil {
		return "", 0, txErr
	}
	return newUsername, newUserId, nil
}

// refreshMigratedTokenCache 在事务提交后异步刷新 Redis 令牌缓存。
//
// 缓存刷新通过 model 包导出的 RefreshTokenCache 完成（model 包内部仍然走
// 原 cacheSetToken 实现）。如果缓存层失败，仅记 SysLog，不影响业务结果，
// 因为 DB 已经是最新状态，缓存最终会因 TTL 自然过期。
func refreshMigratedTokenCache(token model.Token) error {
	return model.RefreshTokenCache(token)
}
