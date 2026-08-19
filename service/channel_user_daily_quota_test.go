package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupChannelUserDailyQuotaMemoryTest(t *testing.T, now *time.Time) {
	t.Helper()
	originalRedisEnabled := common.RedisEnabled
	originalNow := channelUserDailyQuotaNow
	originalMemory := channelUserDailyQuotaMemory
	if originalRedisEnabled {
		common.RedisEnabled = false
	}
	channelUserDailyQuotaMemory = newChannelUserDailyQuotaMemoryStore()
	channelUserDailyQuotaNow = func() time.Time {
		return *now
	}
	t.Cleanup(func() {
		if originalRedisEnabled {
			common.RedisEnabled = true
		}
		channelUserDailyQuotaNow = originalNow
		channelUserDailyQuotaMemory = originalMemory
	})
}

func TestChannelUserDailyQuotaMemorySoftLimitAndIsolation(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.Local)
	setupChannelUserDailyQuotaMemoryTest(t, &now)
	ctx := context.Background()

	usedQuota, err := CheckChannelUserDailyQuota(ctx, 80, 123, 100)
	require.NoError(t, err)
	assert.Zero(t, usedQuota)

	require.NoError(t, RecordChannelUserDailyQuota(ctx, 80, 123, 60))
	usedQuota, err = CheckChannelUserDailyQuota(ctx, 80, 123, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(60), usedQuota)

	// 软上限不预占额度，因此检查通过后的请求可以把累计推到上限之上。
	require.NoError(t, RecordChannelUserDailyQuota(ctx, 80, 123, 50))
	usedQuota, err = CheckChannelUserDailyQuota(ctx, 80, 123, 100)
	assert.ErrorIs(t, err, ErrChannelUserDailyQuotaExceeded)
	assert.Equal(t, int64(110), usedQuota)

	otherUserQuota, err := CheckChannelUserDailyQuota(ctx, 80, 456, 100)
	require.NoError(t, err)
	assert.Zero(t, otherUserQuota)
	otherChannelQuota, err := CheckChannelUserDailyQuota(ctx, 81, 123, 100)
	require.NoError(t, err)
	assert.Zero(t, otherChannelQuota)
}

func TestChannelUserDailyQuotaMemorySetListAndNextDayReset(t *testing.T) {
	now := time.Date(2026, 8, 20, 23, 30, 0, 0, time.Local)
	setupChannelUserDailyQuotaMemoryTest(t, &now)
	ctx := context.Background()

	require.NoError(t, SetChannelUserDailyQuota(ctx, 80, 123, 25))
	items, resetAt, storageMode, err := ListChannelUserDailyQuota(ctx, 80)
	require.NoError(t, err)
	assert.Equal(t, "memory", storageMode)
	assert.Equal(t, time.Date(2026, 8, 21, 0, 0, 0, 0, time.Local).Unix(), resetAt)
	assert.Equal(t, []ChannelUserDailyQuotaUsage{{UserID: 123, UsedQuota: 25}}, items)

	require.NoError(t, SetChannelUserDailyQuota(ctx, 80, 123, 0))
	items, _, _, err = ListChannelUserDailyQuota(ctx, 80)
	require.NoError(t, err)
	assert.Empty(t, items)

	require.NoError(t, RecordChannelUserDailyQuota(ctx, 80, 123, 40))
	now = time.Date(2026, 8, 21, 0, 1, 0, 0, time.Local)
	usedQuota, err := CheckChannelUserDailyQuota(ctx, 80, 123, 100)
	require.NoError(t, err)
	assert.Zero(t, usedQuota)
}

func TestChannelUserDailyQuotaDisabledSkipsUnavailableRedis(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	originalRedisClient := common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedisClient
	})

	usedQuota, err := CheckChannelUserDailyQuota(context.Background(), 80, 123, 0)
	require.NoError(t, err)
	assert.Zero(t, usedQuota)
	_, err = CheckChannelUserDailyQuota(context.Background(), 80, 123, 100)
	assert.True(t, errors.Is(err, ErrChannelUserDailyQuotaUnavailable))
}

func TestSetChannelUserDailyQuotaRejectsInvalidTarget(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.Local)
	setupChannelUserDailyQuotaMemoryTest(t, &now)

	assert.Error(t, SetChannelUserDailyQuota(context.Background(), 80, 123, -1))
	assert.Error(t, SetChannelUserDailyQuota(context.Background(), 80, 123, common.MaxQuota+1))
}

func TestChannelUserDailyQuotaRedisStoreAddSetListAndTTL(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})
	store := &channelUserDailyQuotaRedisStore{client: client}
	period := channelUserDailyQuotaPeriod{
		channelID: 80,
		date:      "2026-08-20",
		redisKey:  "channel_user_daily_quota:{80}:2026-08-20",
		resetAt:   time.Date(2026, 8, 21, 0, 0, 0, 0, time.Local),
		ttl:       14 * time.Hour,
	}
	ctx := context.Background()

	require.NoError(t, store.add(ctx, period, 123, 60))
	require.NoError(t, store.add(ctx, period, 123, 50))
	usedQuota, err := store.get(ctx, period, 123)
	require.NoError(t, err)
	assert.Equal(t, int64(110), usedQuota)
	values, err := store.list(ctx, period)
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{123: 110}, values)
	ttl, err := client.TTL(ctx, period.redisKey).Result()
	require.NoError(t, err)
	assert.Positive(t, ttl)

	require.NoError(t, store.set(ctx, period, 123, 25))
	usedQuota, err = store.get(ctx, period, 123)
	require.NoError(t, err)
	assert.Equal(t, int64(25), usedQuota)
	require.NoError(t, store.set(ctx, period, 123, 0))
	exists, err := client.Exists(ctx, period.redisKey).Result()
	require.NoError(t, err)
	assert.Zero(t, exists)
}
