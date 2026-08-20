package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReplaceChannelUserLimitOverrideResolvesEffectiveLimits 验证覆盖放宽、持久化和撤销回落。
func TestReplaceChannelUserLimitOverrideResolvesEffectiveLimits(t *testing.T) {
	require.NoError(t, model.DB.Exec("DELETE FROM channel_user_limit_overrides").Error)
	baseConcurrency := 2
	baseDaily := 1000
	baseWeekly := 5000
	channel := &model.Channel{
		Id:                   91,
		UserConcurrencyLimit: &baseConcurrency,
		UserDailyQuotaLimit:  &baseDaily,
		UserWeeklyQuotaLimit: &baseWeekly,
	}
	overrideConcurrency := 4
	overrideDaily := 2000
	overrideWeekly := 9000
	input := ChannelUserLimitOverrideInput{
		UserConcurrencyLimit: &overrideConcurrency,
		UserDailyQuotaLimit:  &overrideDaily,
		UserWeeklyQuotaLimit: &overrideWeekly,
		ExpiresAt:            time.Now().Add(time.Hour).Unix(),
	}

	require.NoError(t, ReplaceChannelUserLimitOverride(context.Background(), channel, 123, input, 1))
	limits, err := ResolveChannelUserEffectiveLimits(context.Background(), channel, 123)
	require.NoError(t, err)
	assert.True(t, limits.Active)
	assert.Equal(t, 4, limits.EffectiveConcurrency)
	assert.Equal(t, 2000, limits.EffectiveDailyQuota)
	assert.Equal(t, 9000, limits.EffectiveWeeklyQuota)

	require.NoError(t, DeleteChannelUserLimitOverride(context.Background(), channel.Id, 123))
	limits, err = ResolveChannelUserEffectiveLimits(context.Background(), channel, 123)
	require.NoError(t, err)
	assert.False(t, limits.Active)
	assert.Equal(t, baseDaily, limits.EffectiveDailyQuota)
}

// TestReplaceChannelUserLimitOverrideRejectsUnlimitedOrNonIncrease 验证不限渠道和非提额输入被拒绝。
func TestReplaceChannelUserLimitOverrideRejectsUnlimitedOrNonIncrease(t *testing.T) {
	baseDaily := 1000
	channel := &model.Channel{Id: 92, UserDailyQuotaLimit: &baseDaily}
	equalDaily := 1000
	err := ReplaceChannelUserLimitOverride(context.Background(), channel, 123, ChannelUserLimitOverrideInput{UserDailyQuotaLimit: &equalDaily}, 1)
	assert.ErrorIs(t, err, ErrInvalidChannelUserLimitOverride)

	unlimitedConcurrency := 4
	err = ReplaceChannelUserLimitOverride(context.Background(), channel, 123, ChannelUserLimitOverrideInput{UserConcurrencyLimit: &unlimitedConcurrency}, 1)
	assert.ErrorIs(t, err, ErrInvalidChannelUserLimitOverride)
}

// TestChannelUserLimitOverrideMemoryCacheIsBounded 验证无 Redis 模式会清理过期项并限制缓存容量。
func TestChannelUserLimitOverrideMemoryCacheIsBounded(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	channelUserLimitOverrideMemoryCache.Lock()
	originalValues := channelUserLimitOverrideMemoryCache.values
	originalMaxEntries := channelUserLimitOverrideMemoryCache.maxEntries
	channelUserLimitOverrideMemoryCache.values = map[string]channelUserLimitOverrideCacheEntry{
		"expired": {
			expiresAt: time.Now().Add(-time.Second),
		},
	}
	channelUserLimitOverrideMemoryCache.maxEntries = 2
	channelUserLimitOverrideMemoryCache.Unlock()
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		channelUserLimitOverrideMemoryCache.Lock()
		channelUserLimitOverrideMemoryCache.values = originalValues
		channelUserLimitOverrideMemoryCache.maxEntries = originalMaxEntries
		channelUserLimitOverrideMemoryCache.Unlock()
	})

	cacheChannelUserLimitOverride(context.Background(), "first", channelUserLimitOverrideCacheValue{})
	cacheChannelUserLimitOverride(context.Background(), "second", channelUserLimitOverrideCacheValue{})
	cacheChannelUserLimitOverride(context.Background(), "latest", channelUserLimitOverrideCacheValue{})

	channelUserLimitOverrideMemoryCache.Lock()
	defer channelUserLimitOverrideMemoryCache.Unlock()
	assert.NotContains(t, channelUserLimitOverrideMemoryCache.values, "expired")
	assert.Contains(t, channelUserLimitOverrideMemoryCache.values, "latest")
	assert.Len(t, channelUserLimitOverrideMemoryCache.values, 2)
}
