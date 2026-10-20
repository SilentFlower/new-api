package model

import (
	"sort"

	"github.com/samber/lo"
	"gorm.io/gorm"
)

// deleteChannelsMatchingStatus 仅清理候选中在事务内仍匹配原状态的渠道。
// 分块删除与 BatchDeleteChannels 保持一致；单独保留状态复核，避免改写原有批删协议。
func deleteChannelsMatchingStatus(ids []int, statuses []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	sort.Ints(ids)
	var deleted int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		for _, chunk := range lo.Chunk(ids, 200) {
			var channels []Channel
			// 候选可能已重新启用；状态复核与后续删除持有同一事务的行锁。
			if err := lockForUpdate(tx).Select("id").Where("id IN ? AND status IN ?", chunk, statuses).Order("id ASC").Find(&channels).Error; err != nil {
				return err
			}
			if len(channels) == 0 {
				continue
			}
			eligible := make([]int, len(channels))
			for i, channel := range channels {
				eligible[i] = channel.Id
			}
			if err := protectChannelLimitParentDeletion(tx, eligible); err != nil {
				return err
			}
			result := tx.Where("id IN ?", eligible).Delete(&Channel{})
			if result.Error != nil {
				return result.Error
			}
			deleted += result.RowsAffected
			if err := tx.Where("channel_id IN ?", eligible).Delete(&Ability{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}
