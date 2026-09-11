package model

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelUserPeriodOverride 保存个人对稳定规则身份的整段提额。
type ChannelUserPeriodOverride struct {
	Id                   int    `json:"id"`
	ChannelId            int    `json:"channel_id" gorm:"uniqueIndex:idx_channel_user_period_override"`
	UserId               int    `json:"user_id" gorm:"uniqueIndex:idx_channel_user_period_override"`
	RuleId               string `json:"rule_id" gorm:"type:varchar(32);uniqueIndex:idx_channel_user_period_override"`
	UserPeriodQuotaLimit int    `json:"user_period_quota_limit"`
	ExpiresAt            int64  `json:"expires_at" gorm:"type:bigint"`
	UpdatedAt            int64  `json:"updated_at" gorm:"type:bigint"`
	UpdatedBy            int    `json:"updated_by"`
}

// ListChannelUserPeriodOverrides 查询用户当前有效的所有规则特批。
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param userID 用户 ID。
// @param now 当前 Unix 秒。
// @return 有效特批及数据库错误。
func ListChannelUserPeriodOverrides(ctx context.Context, channelID, userID int, now int64) ([]ChannelUserPeriodOverride, error) {
	var items []ChannelUserPeriodOverride
	if DB == nil {
		return nil, gorm.ErrInvalidDB
	}
	err := DB.WithContext(ctx).Where("channel_id = ? AND user_id = ? AND (expires_at = 0 OR expires_at > ?)", channelID, userID, now).Find(&items).Error
	return items, err
}

// ReplaceChannelUserPeriodOverride 原子保存指定规则的个人特批。
// @param ctx 请求上下文。
// @param item 已校验的特批记录。
// @return 数据库错误。
func ReplaceChannelUserPeriodOverride(ctx context.Context, item *ChannelUserPeriodOverride) error {
	item.UpdatedAt = time.Now().Unix()
	return DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}, {Name: "user_id"}, {Name: "rule_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"user_period_quota_limit", "expires_at", "updated_at", "updated_by"}),
	}).Create(item).Error
}

// DeleteChannelUserPeriodOverride 撤销指定用户对指定规则的个人提额。
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param userID 用户 ID。
// @param ruleID 规则 ID。
// @return 数据库错误。
func DeleteChannelUserPeriodOverride(ctx context.Context, channelID, userID int, ruleID string) error {
	return DB.WithContext(ctx).Where("channel_id = ? AND user_id = ? AND rule_id = ?", channelID, userID, ruleID).Delete(&ChannelUserPeriodOverride{}).Error
}
