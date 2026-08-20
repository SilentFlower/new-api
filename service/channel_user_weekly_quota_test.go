package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupChannelUserWeeklyQuotaMemoryTest(t *testing.T, now *time.Time) {
	t.Helper()
	originalRedisEnabled := common.RedisEnabled
	originalNow := channelUserWeeklyQuotaNow
	originalMemory := channelUserWeeklyQuotaMemory
	common.RedisEnabled = false
	channelUserWeeklyQuotaMemory = newChannelUserWeeklyQuotaMemoryStore()
	channelUserWeeklyQuotaNow = func() time.Time { return *now }
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		channelUserWeeklyQuotaNow = originalNow
		channelUserWeeklyQuotaMemory = originalMemory
	})
}

// TestChannelUserWeeklyQuotaMemorySoftLimitAndMondayReset 验证周限软上限及周一刷新。
func TestChannelUserWeeklyQuotaMemorySoftLimitAndMondayReset(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.Local)
	setupChannelUserWeeklyQuotaMemoryTest(t, &now)
	ctx := context.Background()

	require.NoError(t, RecordChannelUserWeeklyQuota(ctx, 80, 123, 60))
	usedQuota, err := CheckChannelUserWeeklyQuota(ctx, 80, 123, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(60), usedQuota)
	require.NoError(t, RecordChannelUserWeeklyQuota(ctx, 80, 123, 50))
	usedQuota, err = CheckChannelUserWeeklyQuota(ctx, 80, 123, 100)
	assert.ErrorIs(t, err, ErrChannelUserWeeklyQuotaExceeded)
	assert.Equal(t, int64(110), usedQuota)

	items, resetAt, storageMode, err := ListChannelUserWeeklyQuota(ctx, 80)
	require.NoError(t, err)
	assert.Equal(t, "memory", storageMode)
	assert.Equal(t, time.Date(2026, 8, 24, 0, 0, 0, 0, time.Local).Unix(), resetAt)
	assert.Equal(t, []ChannelUserWeeklyQuotaUsage{{UserID: 123, UsedQuota: 110}}, items)

	now = time.Date(2026, 8, 24, 0, 1, 0, 0, time.Local)
	usedQuota, err = CheckChannelUserWeeklyQuota(ctx, 80, 123, 100)
	require.NoError(t, err)
	assert.Zero(t, usedQuota)
}

// TestChannelUserWeeklyQuotaDisabledSkipsUnavailableRedis 验证不限时不因 Redis 故障阻止请求。
func TestChannelUserWeeklyQuotaDisabledSkipsUnavailableRedis(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	originalRedisClient := common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedisClient
	})

	usedQuota, err := CheckChannelUserWeeklyQuota(context.Background(), 80, 123, 0)
	require.NoError(t, err)
	assert.Zero(t, usedQuota)
	_, err = CheckChannelUserWeeklyQuota(context.Background(), 80, 123, 100)
	assert.True(t, errors.Is(err, ErrChannelUserWeeklyQuotaUnavailable))
}

// TestRecordChannelUserQuotaUsageTracksDailyAndWeekly 验证一次正向结算同时进入日、周状态。
func TestRecordChannelUserQuotaUsageTracksDailyAndWeekly(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.Local)
	setupChannelUserDailyQuotaMemoryTest(t, &now)
	setupChannelUserWeeklyQuotaMemoryTest(t, &now)

	require.NoError(t, RecordChannelUserQuotaUsage(context.Background(), 80, 123, 75))
	daily, _, _, err := GetChannelUserDailyQuotaUsage(context.Background(), 80, 123)
	require.NoError(t, err)
	weekly, _, _, err := GetChannelUserWeeklyQuotaUsage(context.Background(), 80, 123)
	require.NoError(t, err)
	assert.Equal(t, int64(75), daily)
	assert.Equal(t, int64(75), weekly)
}
