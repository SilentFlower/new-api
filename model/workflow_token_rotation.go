package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// IssueWorkflowReplacementToken 为现有工作流用户签发同身份的替代令牌。
// @param userID 工作流用户 ID。
// @param oldTokenID 当前生效令牌 ID。
// @param workflowID 工作流原始 ID，必须与旧令牌名称一致。
// @return 新令牌及其原始 Key；失败时返回错误。
func IssueWorkflowReplacementToken(userID, oldTokenID int, workflowID string) (*Token, error) {
	if userID <= 0 || oldTokenID <= 0 || workflowID == "" {
		return nil, errors.New("工作流用户、旧令牌和工作流 ID 均不能为空")
	}
	if len([]rune(workflowID)) > TokenNameMaxLength {
		return nil, errors.New("工作流 ID 超过令牌名称长度限制")
	}
	key, err := common.GenerateKey()
	if err != nil {
		return nil, err
	}
	var replacement Token
	err = DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.First(&user, "id = ?", userID).Error; err != nil {
			return err
		}
		if user.Role != common.RoleCommonUser || user.Status != common.UserStatusEnabled || !user.GetSetting().RecordIpLog {
			return errors.New("目标账号不是已启用的工作流普通用户")
		}
		var old Token
		if err := lockForUpdate(tx).First(&old, "id = ? AND user_id = ?", oldTokenID, userID).Error; err != nil {
			return err
		}
		if old.Name != workflowID || old.Status != common.TokenStatusEnabled || !old.UnlimitedQuota {
			return errors.New("旧令牌的名称、状态或额度模式不符合工作流约束")
		}
		var pending []Token
		if err := tx.Where("user_id = ? AND name = ? AND status = ? AND id <> ?",
			userID, workflowID, common.TokenStatusEnabled, oldTokenID).Limit(2).Find(&pending).Error; err != nil {
			return err
		}
		if len(pending) > 1 {
			return errors.New("工作流存在多枚待切换令牌，需先人工核对")
		}
		if len(pending) == 1 {
			if !sameWorkflowTokenPolicy(old, pending[0]) {
				return errors.New("已有同名令牌与工作流约束不一致")
			}
			replacement = pending[0]
			return nil
		}
		replacement = Token{
			UserId:             userID,
			Key:                key,
			Name:               old.Name,
			Status:             common.TokenStatusEnabled,
			CreatedTime:        common.GetTimestamp(),
			AccessedTime:       common.GetTimestamp(),
			ExpiredTime:        old.ExpiredTime,
			RemainQuota:        old.RemainQuota,
			UnlimitedQuota:     true,
			ModelLimitsEnabled: old.ModelLimitsEnabled,
			ModelLimits:        old.ModelLimits,
			AllowIps:           old.AllowIps,
			Group:              old.Group,
			CrossGroupRetry:    old.CrossGroupRetry,
			AutoGroups:         old.AutoGroups,
		}
		return tx.Create(&replacement).Error
	})
	if err != nil {
		return nil, err
	}
	return &replacement, nil
}

// RetireWorkflowToken 在确认同用户替代令牌已启用后停用旧令牌。
// @param userID 工作流用户 ID。
// @param oldTokenID 待停用令牌 ID。
// @param replacementTokenID 已切换至 Gateway 的替代令牌 ID。
// @return 校验、数据库写入或缓存刷新失败时返回错误。
func RetireWorkflowToken(userID, oldTokenID, replacementTokenID int) error {
	if userID <= 0 || oldTokenID <= 0 || replacementTokenID <= 0 || oldTokenID == replacementTokenID {
		return errors.New("工作流用户或替代令牌 ID 无效")
	}
	var old Token
	err := DB.Transaction(func(tx *gorm.DB) error {
		var replacement Token
		if err := tx.First(&old, "id = ? AND user_id = ?", oldTokenID, userID).Error; err != nil {
			return err
		}
		if err := tx.First(&replacement, "id = ? AND user_id = ?", replacementTokenID, userID).Error; err != nil {
			return err
		}
		if replacement.Status != common.TokenStatusEnabled || replacement.Name != old.Name || !sameWorkflowTokenPolicy(old, replacement) {
			return errors.New("替代令牌未启用或不属于同一工作流")
		}
		if old.Status == common.TokenStatusDisabled {
			return nil
		}
		result := tx.Model(&Token{}).Where("id = ? AND user_id = ? AND status = ?", oldTokenID, userID, common.TokenStatusEnabled).
			Update("status", common.TokenStatusDisabled)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("旧令牌已不处于生效状态")
		}
		old.Status = common.TokenStatusDisabled
		return nil
	})
	if err != nil {
		return err
	}
	if !common.RedisEnabled {
		return nil
	}
	if err := cacheSetToken(old); err != nil {
		if deleteErr := cacheDeleteToken(old.Key); deleteErr != nil {
			return fmt.Errorf("旧令牌已停用但缓存刷新失败: %w", err)
		}
	}
	return nil
}

func sameWorkflowTokenPolicy(old, replacement Token) bool {
	// 轮换只允许替换 Key，不允许借已存在的同名令牌放宽访问策略。
	return old.UnlimitedQuota && replacement.UnlimitedQuota &&
		old.Group == replacement.Group && old.ExpiredTime == replacement.ExpiredTime &&
		old.ModelLimitsEnabled == replacement.ModelLimitsEnabled && old.ModelLimits == replacement.ModelLimits &&
		sameOptionalString(old.AllowIps, replacement.AllowIps) && old.CrossGroupRetry == replacement.CrossGroupRetry &&
		old.AutoGroups == replacement.AutoGroups
}

func sameOptionalString(left, right *string) bool {
	if left == nil {
		return right == nil || *right == ""
	}
	if right == nil {
		return *left == ""
	}
	return *left == *right
}
