package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// exerciseChannelStatusCleanupRace 在候选查询后重新启用一个渠道，验证只删除仍禁用的渠道。
func exerciseChannelStatusCleanupRace(t *testing.T, db *gorm.DB, cleanup func() (int64, error)) {
	t.Helper()
	const preservedID, deletedID = 98151, 98152
	for _, id := range []int{preservedID, deletedID} {
		require.NoError(t, db.Create(&Channel{Id: id, Name: "状态清理回归", Status: common.ChannelStatusManuallyDisabled}).Error)
		require.NoError(t, db.Create(&Ability{ChannelId: id, Group: "default", Model: "status-cleanup-model", Enabled: true}).Error)
	}
	var once sync.Once
	var changed bool
	callbackName := "test:channel_status_cleanup_reenable"
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*[]int); !ok {
			return
		}
		once.Do(func() {
			// 此时最初的候选查询已结束，模拟另一管理员在删除事务前重新启用。
			result := db.Model(&Channel{}).Where("id = ?", preservedID).Update("status", common.ChannelStatusEnabled)
			tx.AddError(result.Error)
			changed = result.Error == nil && result.RowsAffected == 1
		})
	}))
	t.Cleanup(func() { require.NoError(t, db.Callback().Query().Remove(callbackName)) })
	rows, err := cleanup()
	require.NoError(t, err)
	require.True(t, changed, "未触发重新启用场景")
	assert.EqualValues(t, 1, rows)
	var preserved Channel
	require.NoError(t, db.First(&preserved, preservedID).Error)
	assert.Equal(t, common.ChannelStatusEnabled, preserved.Status)
	var removed, preservedAbilities, deletedAbilities int64
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", deletedID).Count(&removed).Error)
	require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", preservedID).Count(&preservedAbilities).Error)
	require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", deletedID).Count(&deletedAbilities).Error)
	assert.Zero(t, removed)
	assert.EqualValues(t, 1, preservedAbilities)
	assert.Zero(t, deletedAbilities)
}

// TestChannelStatusCleanupPreservesReenabledChannels 覆盖单状态与全部禁用清理的状态竞争。
func TestChannelStatusCleanupPreservesReenabledChannels(t *testing.T) {
	for _, mode := range []string{"status", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			truncateTables(t)
			cleanup := DeleteDisabledChannel
			if mode == "status" {
				cleanup = func() (int64, error) { return DeleteChannelByStatus(int64(common.ChannelStatusManuallyDisabled)) }
			}
			exerciseChannelStatusCleanupRace(t, DB, cleanup)
		})
	}
}
