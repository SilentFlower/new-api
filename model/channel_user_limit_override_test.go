package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChannelUserLimitOverrideReplaceAndExpiry 验证并发覆盖整条替换、唯一性与到期回落。
func TestChannelUserLimitOverrideReplaceAndExpiry(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	concurrency := 3
	first := &ChannelUserLimitOverride{ChannelId: 81, UserId: 123, UserConcurrencyLimit: &concurrency, ExpiresAt: now + 3600, UpdatedBy: 1}
	require.NoError(t, ReplaceChannelUserLimitOverride(first))
	saved, err := GetActiveChannelUserLimitOverride(81, 123, now)
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, 3, *saved.UserConcurrencyLimit)

	higher := 5
	require.NoError(t, ReplaceChannelUserLimitOverride(&ChannelUserLimitOverride{ChannelId: 81, UserId: 123, UserConcurrencyLimit: &higher, ExpiresAt: now + 7200, UpdatedBy: 2}))
	saved, err = GetActiveChannelUserLimitOverride(81, 123, now)
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, 5, *saved.UserConcurrencyLimit)
	assert.Equal(t, now+7200, saved.ExpiresAt)

	expired, err := GetActiveChannelUserLimitOverride(81, 123, now+7201)
	require.NoError(t, err)
	assert.Nil(t, expired)
}
