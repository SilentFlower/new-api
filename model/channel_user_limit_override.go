package model

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// ChannelUserLimitOverride 描述指定用户在指定渠道上的个人上限覆盖。
type ChannelUserLimitOverride struct {
	Id                   int  `json:"id"`
	ChannelId            int  `json:"channel_id" gorm:"uniqueIndex:idx_channel_user_limit_override"`
	UserId               int  `json:"user_id" gorm:"uniqueIndex:idx_channel_user_limit_override"`
	UserConcurrencyLimit *int `json:"user_concurrency_limit"`
	// 旧日/周提额已迁移到行级提额表，仅为兼容历史表结构保留，不再读写。
	UserDailyQuotaLimit  *int  `json:"-"`
	UserWeeklyQuotaLimit *int  `json:"-"`
	ExpiresAt            int64 `json:"expires_at" gorm:"bigint;index"`
	UpdatedBy            int   `json:"updated_by"`
	CreatedAt            int64 `json:"created_at" gorm:"bigint"`
	UpdatedAt            int64 `json:"updated_at" gorm:"bigint"`
}

// GetActiveChannelUserLimitOverride 查询当前仍有效的个人覆盖记录。
//
// @param channelID 渠道 ID。
// @param userID 用户 ID。
// @param now 当前 Unix 秒。
// @return *ChannelUserLimitOverride 有效覆盖；不存在或已过期时返回 nil。
// @return error 数据库查询失败时返回错误。
func GetActiveChannelUserLimitOverride(channelID int, userID int, now int64) (*ChannelUserLimitOverride, error) {
	if DB == nil {
		return nil, nil
	}
	var override ChannelUserLimitOverride
	err := DB.Where("channel_id = ? AND user_id = ? AND (expires_at = 0 OR expires_at > ?)", channelID, userID, now).
		First(&override).Error
	if err == nil {
		return &override, nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if !DB.Migrator().HasTable(&ChannelUserLimitOverride{}) {
		return nil, nil
	}
	return nil, err
}

// ListActiveChannelUserLimitOverrides 分页返回指定渠道当前有效的个人覆盖。
//
// @param channelID 渠道 ID。
// @param now 当前 Unix 秒。
// @param offset 分页偏移量。
// @param limit 分页大小。
// @return []ChannelUserLimitOverride 按用户 ID 升序排列的覆盖列表。
// @return int64 当前有效覆盖总数。
// @return error 数据库查询失败时返回错误。
func ListActiveChannelUserLimitOverrides(channelID int, now int64, offset int, limit int) ([]ChannelUserLimitOverride, int64, error) {
	query := DB.Model(&ChannelUserLimitOverride{}).
		Where("channel_id = ? AND (expires_at = 0 OR expires_at > ?)", channelID, now)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var overrides []ChannelUserLimitOverride
	err := query.Order("user_id ASC").Offset(offset).Limit(limit).Find(&overrides).Error
	return overrides, total, err
}

// ReplaceChannelUserLimitOverride 整条替换指定用户的个人覆盖记录。
//
// @param override 待保存的覆盖记录。
// @return error 数据库写入失败时返回错误。
func ReplaceChannelUserLimitOverride(override *ChannelUserLimitOverride) error {
	if override == nil {
		return gorm.ErrInvalidData
	}
	now := time.Now().Unix()
	return DB.Transaction(func(tx *gorm.DB) error {
		var existing ChannelUserLimitOverride
		err := tx.Where("channel_id = ? AND user_id = ?", override.ChannelId, override.UserId).First(&existing).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		if err == nil {
			return tx.Model(&existing).Updates(map[string]interface{}{
				"user_concurrency_limit": override.UserConcurrencyLimit,
				"expires_at":             override.ExpiresAt,
				"updated_by":             override.UpdatedBy,
				"updated_at":             now,
			}).Error
		}
		override.CreatedAt = now
		override.UpdatedAt = now
		return tx.Create(override).Error
	})
}

// DeleteChannelUserLimitOverride 删除指定用户的个人覆盖记录。
//
// @param channelID 渠道 ID。
// @param userID 用户 ID。
// @return error 数据库删除失败时返回错误。
func DeleteChannelUserLimitOverride(channelID int, userID int) error {
	return DB.Where("channel_id = ? AND user_id = ?", channelID, userID).
		Delete(&ChannelUserLimitOverride{}).Error
}

// GetActiveChannelUserLimitOverrideStrict 严格查询个人覆盖，周期策略不得把缺表当成无覆盖。
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param userID 用户 ID。
// @param now 当前秒级时间。
// @return 当前覆盖或真实数据库错误。
func GetActiveChannelUserLimitOverrideStrict(ctx context.Context, channelID, userID int, now int64) (*ChannelUserLimitOverride, error) {
	if DB == nil {
		return nil, errors.New("个人覆盖数据库未初始化")
	}
	var override ChannelUserLimitOverride
	err := DB.WithContext(ctx).Where("channel_id = ? AND user_id = ? AND (expires_at = 0 OR expires_at > ?)", channelID, userID, now).First(&override).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &override, nil
}
