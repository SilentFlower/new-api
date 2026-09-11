package model

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrChannelPeriodPolicyConflict 表示配置已经被另一位管理员修改。
var ErrChannelPeriodPolicyConflict = errors.New("渠道周期策略版本已变化，请刷新后重试")

// ChannelPeriodPolicy 保存唯一渠道的版本化策略，Config 使用三库均支持的 TEXT。
type ChannelPeriodPolicy struct {
	Id        int    `json:"id"`
	ChannelId int    `json:"channel_id" gorm:"uniqueIndex"`
	Revision  int    `json:"revision"`
	Config    string `json:"config" gorm:"type:text"`
	CreatedAt int64  `json:"created_at" gorm:"type:bigint"`
	UpdatedAt int64  `json:"updated_at" gorm:"type:bigint"`
	UpdatedBy int    `json:"updated_by"`
}

// GetChannelPeriodPolicy 查询权威策略；仅记录不存在代表未配置。
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @return 策略及数据库错误，不存在时策略为 nil。
func GetChannelPeriodPolicy(ctx context.Context, channelID int) (*ChannelPeriodPolicy, error) {
	if DB == nil {
		return nil, errors.New("渠道周期策略数据库未初始化")
	}
	var policy ChannelPeriodPolicy
	err := DB.WithContext(ctx).Where("channel_id = ?", channelID).First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &policy, err
}

// ReplaceChannelPeriodPolicy 按旧版本原子替换策略，不允许覆盖并发编辑。
// @param ctx 请求上下文。
// @param policy 待保存策略，Revision 表示预期旧版本。
// @return 保存失败或版本冲突错误；成功后更新 policy 的版本。
func ReplaceChannelPeriodPolicy(ctx context.Context, policy *ChannelPeriodPolicy) error {
	if DB == nil || policy == nil || policy.ChannelId <= 0 || policy.Revision < 0 {
		return gorm.ErrInvalidData
	}
	now := time.Now().Unix()
	if policy.Revision == 0 {
		policy.Revision = 1
		policy.CreatedAt, policy.UpdatedAt = now, now
		result := DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(policy)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrChannelPeriodPolicyConflict
		}
		return nil
	}
	result := DB.WithContext(ctx).Model(&ChannelPeriodPolicy{}).
		Where("channel_id = ? AND revision = ?", policy.ChannelId, policy.Revision).
		Updates(map[string]interface{}{"config": policy.Config, "revision": policy.Revision + 1, "updated_at": now, "updated_by": policy.UpdatedBy})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrChannelPeriodPolicyConflict
	}
	policy.Revision++
	policy.UpdatedAt = now
	return nil
}
