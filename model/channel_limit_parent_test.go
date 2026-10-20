package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupChannelLimitParentFixture 建立三个独立渠道，父渠道处于禁用状态以覆盖批量清理。
func setupChannelLimitParentFixture(t *testing.T) {
	t.Helper()
	truncateTables(t)
	for _, channel := range []Channel{
		{Id: 98101, Name: "父渠道", Key: "parent-secret", Status: common.ChannelStatusManuallyDisabled, UserConcurrencyLimit: common.GetPointer(4)},
		{Id: 98102, Name: "子渠道", Key: "child-secret", Status: common.ChannelStatusEnabled, UserConcurrencyLimit: common.GetPointer(1)},
		{Id: 98103, Name: "其他渠道", Key: "other-secret", Status: common.ChannelStatusEnabled},
	} {
		require.NoError(t, DB.Create(&channel).Error)
	}
}

func TestChannelLimitParentRelationRejectsCyclesAndStaleWrites(t *testing.T) {
	setupChannelLimitParentFixture(t)
	ctx := t.Context()
	relation := &ChannelLimitParent{ChannelId: 98102, ParentChannelId: 98101, UpdatedBy: 1}
	require.NoError(t, ReplaceChannelLimitParent(ctx, relation))
	assert.Equal(t, 1, relation.Revision)
	for _, input := range []ChannelLimitParent{
		{ChannelId: 98102, ParentChannelId: 98102, Revision: 1, UpdatedBy: 1},
		{ChannelId: 98102, ParentChannelId: 999999, Revision: 1, UpdatedBy: 1},
		{ChannelId: 98101, ParentChannelId: 98103, UpdatedBy: 1},
		{ChannelId: 98103, ParentChannelId: 98102, UpdatedBy: 1},
	} {
		require.ErrorIs(t, ReplaceChannelLimitParent(ctx, &input), ErrInvalidChannelLimitParent)
	}
	require.ErrorIs(t, ReplaceChannelLimitParent(ctx, &ChannelLimitParent{ChannelId: 98102, UpdatedBy: 1}), ErrChannelLimitParentConflict)
	options, total, err := ListChannelLimitParentOptions(ctx, 98103, "", 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, options, 1)
	assert.Equal(t, 98101, options[0].ID)
	encoded, err := common.Marshal(options)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "secret")
	relation.ParentChannelId = 0
	require.NoError(t, ReplaceChannelLimitParent(ctx, relation))
	assert.Equal(t, 2, relation.Revision)
	stored, err := GetChannelLimitParent(ctx, 98102)
	require.NoError(t, err)
	assert.Zero(t, stored.ParentChannelId)
	assert.Equal(t, 2, stored.Revision)
	child, err := GetChannelById(98102, false)
	require.NoError(t, err)
	assert.Equal(t, 1, child.GetUserConcurrencyLimit())
}

func TestChannelLimitParentProtectsEveryDeletionEntry(t *testing.T) {
	for _, method := range []string{"single", "batch", "status", "disabled"} {
		t.Run(method, func(t *testing.T) {
			setupChannelLimitParentFixture(t)
			relation := &ChannelLimitParent{ChannelId: 98102, ParentChannelId: 98101, UpdatedBy: 1}
			require.NoError(t, ReplaceChannelLimitParent(t.Context(), relation))
			var err error
			switch method {
			case "single":
				err = (&Channel{Id: 98101}).Delete()
			case "batch":
				_, err = BatchDeleteChannels([]int{98101})
			case "status":
				_, err = DeleteChannelByStatus(int64(common.ChannelStatusManuallyDisabled))
			case "disabled":
				_, err = DeleteDisabledChannel()
			}
			require.ErrorIs(t, err, ErrChannelLimitParentInUse)
			_, err = GetChannelById(98101, false)
			require.NoError(t, err)
			relation.ParentChannelId = 0
			require.NoError(t, ReplaceChannelLimitParent(t.Context(), relation))
			require.NoError(t, (&Channel{Id: 98101}).Delete())
		})
	}
}
