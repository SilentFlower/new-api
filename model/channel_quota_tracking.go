package model

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelQuotaTracking 保存最近一次计数缺口，不存储或推算消费金额。
type ChannelQuotaTracking struct {
	ChannelId int   `gorm:"primaryKey;autoIncrement:false"`
	LastGapAt int64 `gorm:"type:bigint"`
}

// RecordChannelQuotaGap 单调更新渠道最近一次未成功累计的结算时间。
// @param ctx 限时操作上下文。
// @param channelID 实际结算渠道。
// @param at 结算 Unix 秒。
// @return 持久化错误。
func RecordChannelQuotaGap(ctx context.Context, channelID int, at int64) error {
	if DB == nil {
		return gorm.ErrInvalidDB
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item := ChannelQuotaTracking{ChannelId: channelID, LastGapAt: at}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&item).Error; err != nil {
			return err
		}
		return tx.Model(&ChannelQuotaTracking{}).Where("channel_id = ? AND last_gap_at < ?", channelID, at).Update("last_gap_at", at).Error
	})
}

// GetChannelQuotaGap 严格读取最近一次计数缺口。
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @return 最近缺口时间，未发生为 0；读取失败返回错误。
func GetChannelQuotaGap(ctx context.Context, channelID int) (int64, error) {
	if DB == nil {
		return 0, gorm.ErrInvalidDB
	}
	var item ChannelQuotaTracking
	err := DB.WithContext(ctx).Where("channel_id = ?", channelID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	return item.LastGapAt, err
}
