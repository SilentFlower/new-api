package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelUserConcurrencyLostStore struct{}

func (channelUserConcurrencyLostStore) acquire(context.Context, string, string, int, int, string, int, time.Time, time.Time, time.Duration) (bool, error) {
	return true, nil
}

func (channelUserConcurrencyLostStore) renew(context.Context, string, string, int, string, time.Time, time.Duration) (bool, error) {
	return false, nil
}

func (channelUserConcurrencyLostStore) release(context.Context, string, string, int, int, string) error {
	return nil
}

func (channelUserConcurrencyLostStore) list(context.Context, int, time.Time, time.Duration) (map[int]int, error) {
	return map[int]int{}, nil
}

func useChannelUserConcurrencyMemoryStore(t *testing.T) {
	t.Helper()
	originalRedisEnabled := common.RedisEnabled
	originalMemoryStore := channelUserConcurrencyMemory
	common.RedisEnabled = false
	channelUserConcurrencyMemory = newChannelUserConcurrencyMemoryStore()
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		channelUserConcurrencyMemory = originalMemoryStore
	})
}

func TestAcquireChannelUserConcurrencyMemoryLimitAndRelease(t *testing.T) {
	useChannelUserConcurrencyMemoryStore(t)
	ctx := context.Background()

	first, err := AcquireChannelUserConcurrency(ctx, 80, 33, 2, nil)
	require.NoError(t, err)
	second, err := AcquireChannelUserConcurrency(ctx, 80, 33, 2, nil)
	require.NoError(t, err)

	_, err = AcquireChannelUserConcurrency(ctx, 80, 33, 2, nil)
	require.ErrorIs(t, err, ErrChannelUserConcurrencyExceeded)

	require.NoError(t, first.Release(ctx))
	require.NoError(t, first.Release(ctx))
	replacement, err := AcquireChannelUserConcurrency(ctx, 80, 33, 2, nil)
	require.NoError(t, err)

	require.NoError(t, second.Release(ctx))
	require.NoError(t, replacement.Release(ctx))
}

func TestAcquireChannelUserConcurrencyMemoryIsolation(t *testing.T) {
	useChannelUserConcurrencyMemoryStore(t)
	ctx := context.Background()

	first, err := AcquireChannelUserConcurrency(ctx, 80, 33, 1, nil)
	require.NoError(t, err)
	otherUser, err := AcquireChannelUserConcurrency(ctx, 80, 34, 1, nil)
	require.NoError(t, err)
	otherChannel, err := AcquireChannelUserConcurrency(ctx, 81, 33, 1, nil)
	require.NoError(t, err)

	_, err = AcquireChannelUserConcurrency(ctx, 80, 33, 1, nil)
	require.ErrorIs(t, err, ErrChannelUserConcurrencyExceeded)

	require.NoError(t, first.Release(ctx))
	require.NoError(t, otherUser.Release(ctx))
	require.NoError(t, otherChannel.Release(ctx))
}

func TestAcquireChannelUserConcurrencyUnlimitedSkipsUnavailableRedis(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	originalRedis := common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedis
	})

	lease, err := AcquireChannelUserConcurrency(context.Background(), 80, 33, 0, nil)
	require.NoError(t, err)
	require.NoError(t, lease.Release(context.Background()))
}

func TestAcquireChannelUserConcurrencyConfiguredRedisFailsClosed(t *testing.T) {
	originalRedisEnabled := common.RedisEnabled
	originalRedis := common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedis
	})

	_, err := AcquireChannelUserConcurrency(context.Background(), 80, 33, 4, nil)
	require.ErrorIs(t, err, ErrChannelUserConcurrencyUnavailable)
}

func TestChannelUserConcurrencyLeaseRenewDetectsLostLease(t *testing.T) {
	lease := &ChannelUserConcurrencyLease{
		store:   channelUserConcurrencyLostStore{},
		key:     "test-key",
		leaseID: "test-lease",
	}

	err := lease.renew()
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrChannelUserConcurrencyUnavailable))
}

func TestChannelUserConcurrencyMemoryStoreRemovesExpiredLeases(t *testing.T) {
	store := newChannelUserConcurrencyMemoryStore()
	now := time.Unix(100, 0)
	allowed, err := store.acquire(context.Background(), "key", "index", 80, 33, "expired", 1, now, now.Add(-time.Second), time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = store.acquire(context.Background(), "key", "index", 80, 33, "replacement", 1, now, now.Add(time.Minute), time.Minute)
	require.NoError(t, err)
	assert.True(t, allowed)
}

func TestListChannelUserConcurrencyMemoryCountsOnlyActiveLeases(t *testing.T) {
	store := newChannelUserConcurrencyMemoryStore()
	now := time.Unix(100, 0)
	ctx := context.Background()

	allowed, err := store.acquire(ctx, "key-33", "index", 80, 33, "active-a", 3, now, now.Add(time.Minute), time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = store.acquire(ctx, "key-33", "index", 80, 33, "active-b", 3, now, now.Add(time.Minute), time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = store.acquire(ctx, "key-34", "index", 80, 34, "expired", 3, now, now.Add(-time.Second), time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)

	values, err := store.list(ctx, 80, now, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, map[int]int{33: 2}, values)
}

func TestChannelUserConcurrencyRedisStoreScripts(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})
	store := &channelUserConcurrencyRedisStore{client: client}
	ctx := context.Background()
	key := "channel_user_concurrency:{80}:{33}"
	indexKey := "channel_user_concurrency_users:{80}"
	now := time.Unix(100, 0)

	for i := 0; i < 4; i++ {
		allowed, err := store.acquire(ctx, key, indexKey, 80, 33, "lease-"+string(rune('a'+i)), 4, now, now.Add(time.Minute), time.Minute)
		require.NoError(t, err)
		require.True(t, allowed)
	}
	allowed, err := store.acquire(ctx, key, indexKey, 80, 33, "lease-e", 4, now, now.Add(time.Minute), time.Minute)
	require.NoError(t, err)
	assert.False(t, allowed)

	renewed, err := store.renew(ctx, key, indexKey, 33, "lease-a", now.Add(2*time.Minute), time.Minute)
	require.NoError(t, err)
	assert.True(t, renewed)
	renewed, err = store.renew(ctx, key, indexKey, 33, "missing", now.Add(2*time.Minute), time.Minute)
	require.NoError(t, err)
	assert.False(t, renewed)

	require.NoError(t, store.release(ctx, key, indexKey, 80, 33, "lease-a"))
	allowed, err = store.acquire(ctx, key, indexKey, 80, 33, "lease-e", 4, now, now.Add(time.Minute), time.Minute)
	require.NoError(t, err)
	assert.True(t, allowed)

	count, err := client.ZCard(ctx, key).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(4), count)
	ttl, err := client.PTTL(ctx, key).Result()
	require.NoError(t, err)
	assert.Positive(t, ttl)
	members, err := client.SMembers(ctx, indexKey).Result()
	require.NoError(t, err)
	assert.Equal(t, []string{"33"}, members)
	values, err := store.list(ctx, 80, now, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, map[int]int{33: 4}, values)
}

func TestChannelUserConcurrencyRedisStoreConcurrentAcquireHonorsLimit(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})
	store := &channelUserConcurrencyRedisStore{client: client}
	ctx := context.Background()
	key := "channel_user_concurrency:{80}:{33}"
	indexKey := "channel_user_concurrency_users:{80}"
	now := time.Unix(100, 0)
	type acquireResult struct {
		allowed bool
		err     error
	}
	results := make(chan acquireResult, 8)
	var waitGroup sync.WaitGroup

	for i := 0; i < 8; i++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			allowed, err := store.acquire(ctx, key, indexKey, 80, 33, fmt.Sprintf("lease-%d", index), 4, now, now.Add(time.Minute), time.Minute)
			results <- acquireResult{allowed: allowed, err: err}
		}(i)
	}
	waitGroup.Wait()
	close(results)

	allowedCount := 0
	for result := range results {
		require.NoError(t, result.err)
		if result.allowed {
			allowedCount++
		}
	}
	assert.Equal(t, 4, allowedCount)
}
