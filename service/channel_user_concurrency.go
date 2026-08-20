package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

const (
	channelUserConcurrencyLeaseTTL          = 120 * time.Second
	channelUserConcurrencyHeartbeatInterval = 30 * time.Second
	channelUserConcurrencyOperationTimeout  = 2 * time.Second
)

var (
	// ErrChannelUserConcurrencyExceeded 表示当前渠道的用户并发名额已满。
	ErrChannelUserConcurrencyExceeded = errors.New("渠道单用户并发已达到上限")
	// ErrChannelUserConcurrencyUnavailable 表示并发租约存储不可用或租约已经丢失。
	ErrChannelUserConcurrencyUnavailable = errors.New("渠道单用户并发服务不可用")

	channelUserConcurrencyAcquireScript = redis.NewScript(`
local key = KEYS[1]
local user_index_key = KEYS[2]
local now = tonumber(ARGV[1])
local expires_at = tonumber(ARGV[2])
local lease_id = ARGV[3]
local limit = tonumber(ARGV[4])
local ttl = tonumber(ARGV[5])
local user_id = ARGV[6]

redis.call('ZREMRANGEBYSCORE', key, '-inf', now)
if redis.call('ZCARD', key) >= limit then
  redis.call('PEXPIRE', key, ttl)
  return 0
end

redis.call('ZADD', key, expires_at, lease_id)
redis.call('PEXPIRE', key, ttl)
redis.call('SADD', user_index_key, user_id)
redis.call('PEXPIRE', user_index_key, ttl)
return 1
`)
	channelUserConcurrencyRenewScript = redis.NewScript(`
local key = KEYS[1]
local user_index_key = KEYS[2]
local expires_at = tonumber(ARGV[1])
local lease_id = ARGV[2]
local ttl = tonumber(ARGV[3])
local user_id = ARGV[4]

if redis.call('ZSCORE', key, lease_id) == false then
  return 0
end

redis.call('ZADD', key, expires_at, lease_id)
redis.call('PEXPIRE', key, ttl)
redis.call('SADD', user_index_key, user_id)
redis.call('PEXPIRE', user_index_key, ttl)
return 1
`)
	channelUserConcurrencyReleaseScript = redis.NewScript(`
local key = KEYS[1]
local user_index_key = KEYS[2]
local lease_id = ARGV[1]
local user_id = ARGV[2]

redis.call('ZREM', key, lease_id)
if redis.call('ZCARD', key) == 0 then
  redis.call('DEL', key)
  redis.call('SREM', user_index_key, user_id)
end
return 1
`)

	channelUserConcurrencyMemory = newChannelUserConcurrencyMemoryStore()
)

type channelUserConcurrencyStore interface {
	acquire(ctx context.Context, key string, userIndexKey string, channelID int, userID int, leaseID string, limit int, now time.Time, expiresAt time.Time, ttl time.Duration) (bool, error)
	renew(ctx context.Context, key string, userIndexKey string, userID int, leaseID string, expiresAt time.Time, ttl time.Duration) (bool, error)
	release(ctx context.Context, key string, userIndexKey string, channelID int, userID int, leaseID string) error
	list(ctx context.Context, channelID int, now time.Time, ttl time.Duration) (map[int]int, error)
}

type channelUserConcurrencyRedisStore struct {
	client *redis.Client
}

type channelUserConcurrencyMemoryStore struct {
	mu       sync.Mutex
	leases   map[string]map[string]time.Time
	userKeys map[int]map[int]string
}

// ChannelUserConcurrencyUsage 描述一个用户在指定渠道当前持有的有效并发租约数量。
type ChannelUserConcurrencyUsage struct {
	UserID      int `json:"user_id"`
	Concurrency int `json:"concurrency"`
}

// ChannelUserConcurrencyLease 表示一个渠道与用户组合持有的并发租约。
//
// Release 可以被重复调用；只有第一次调用会执行实际释放。
type ChannelUserConcurrencyLease struct {
	store        channelUserConcurrencyStore
	key          string
	userIndexKey string
	channelID    int
	userID       int
	leaseID      string
	onLost       func(error)
	stop         chan struct{}
	done         chan struct{}
	releaseOnce  sync.Once
	lostOnce     sync.Once
	releaseErr   error
	lost         atomic.Bool
	lostSignal   chan struct{}
}

// AcquireChannelUserConcurrency 获取指定渠道和用户的并发租约。
//
// @param ctx 请求上下文。
// @param channelID 已选定的渠道 ID。
// @param userID 当前认证用户 ID。
// @param limit 渠道配置的单用户最大并发数，零或负数表示不限制。
// @param onLost 长请求续租失败或租约丢失时的回调。
// @return *ChannelUserConcurrencyLease 获取成功的租约，调用方必须释放。
// @return error 达到上限或租约存储不可用时返回领域错误。
func AcquireChannelUserConcurrency(ctx context.Context, channelID int, userID int, limit int, onLost func(error)) (*ChannelUserConcurrencyLease, error) {
	if limit <= 0 {
		return &ChannelUserConcurrencyLease{}, nil
	}
	if channelID <= 0 || userID <= 0 {
		return nil, fmt.Errorf("%w: channel_id 和 user_id 必须为正数", ErrChannelUserConcurrencyUnavailable)
	}

	store, err := currentChannelUserConcurrencyStore()
	if err != nil {
		return nil, err
	}
	lease := &ChannelUserConcurrencyLease{
		store:        store,
		key:          channelUserConcurrencyKey(channelID, userID),
		userIndexKey: channelUserConcurrencyUserIndexKey(channelID),
		channelID:    channelID,
		userID:       userID,
		leaseID:      uuid.NewString(),
		onLost:       onLost,
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
		lostSignal:   make(chan struct{}),
	}
	now := time.Now()
	opCtx, cancel := channelUserConcurrencyOperationContext(ctx)
	allowed, acquireErr := store.acquire(opCtx, lease.key, lease.userIndexKey, channelID, userID, lease.leaseID, limit, now, now.Add(channelUserConcurrencyLeaseTTL), channelUserConcurrencyLeaseTTL)
	cancel()
	if acquireErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrChannelUserConcurrencyUnavailable, acquireErr)
	}
	if !allowed {
		return nil, ErrChannelUserConcurrencyExceeded
	}

	go lease.heartbeat()
	return lease, nil
}

// IsLost 判断租约是否因续租失败或存储记录丢失而失效。
//
// @return bool 租约已经失效时返回 true。
func (lease *ChannelUserConcurrencyLease) IsLost() bool {
	return lease != nil && lease.lost.Load()
}

// LostSignal 返回租约失效时关闭的通知通道。
//
// @return <-chan struct{} 未启用限制时返回 nil；租约失效时关闭有效通道。
func (lease *ChannelUserConcurrencyLease) LostSignal() <-chan struct{} {
	if lease == nil {
		return nil
	}
	return lease.lostSignal
}

// Release 释放当前并发租约并停止续租。
//
// @param ctx 用于释放存储名额的上下文。
// @return error Redis 释放失败时返回错误；重复释放返回第一次调用的结果。
func (lease *ChannelUserConcurrencyLease) Release(ctx context.Context) error {
	if lease == nil || lease.store == nil {
		return nil
	}
	lease.releaseOnce.Do(func() {
		close(lease.stop)
		<-lease.done
		releaseCtx := context.Background()
		if ctx != nil {
			releaseCtx = context.WithoutCancel(ctx)
		}
		opCtx, cancel := channelUserConcurrencyOperationContext(releaseCtx)
		defer cancel()
		lease.releaseErr = lease.store.release(opCtx, lease.key, lease.userIndexKey, lease.channelID, lease.userID, lease.leaseID)
	})
	return lease.releaseErr
}

func (lease *ChannelUserConcurrencyLease) heartbeat() {
	defer close(lease.done)
	ticker := time.NewTicker(channelUserConcurrencyHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-lease.stop:
			return
		case <-ticker.C:
			if err := lease.renew(); err != nil {
				lease.lost.Store(true)
				lease.lostOnce.Do(func() {
					close(lease.lostSignal)
					if lease.onLost != nil {
						lease.onLost(err)
					}
				})
				return
			}
		}
	}
}

func (lease *ChannelUserConcurrencyLease) renew() error {
	now := time.Now()
	opCtx, cancel := channelUserConcurrencyOperationContext(context.Background())
	defer cancel()
	renewed, err := lease.store.renew(opCtx, lease.key, lease.userIndexKey, lease.userID, lease.leaseID, now.Add(channelUserConcurrencyLeaseTTL), channelUserConcurrencyLeaseTTL)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrChannelUserConcurrencyUnavailable, err)
	}
	if !renewed {
		return fmt.Errorf("%w: 租约已丢失", ErrChannelUserConcurrencyUnavailable)
	}
	return nil
}

// ListChannelUserConcurrency 返回指定渠道当前仍有效的用户并发数量。
//
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @return []ChannelUserConcurrencyUsage 按用户 ID 排序的当前并发列表。
// @return string 状态存储模式，取值为 redis 或 memory。
// @return error 状态读取失败时返回错误。
func ListChannelUserConcurrency(ctx context.Context, channelID int) ([]ChannelUserConcurrencyUsage, string, error) {
	if channelID <= 0 {
		return nil, "", fmt.Errorf("%w: channel_id 必须为正数", ErrChannelUserConcurrencyUnavailable)
	}
	store, err := currentChannelUserConcurrencyStore()
	if err != nil {
		return nil, "", err
	}
	opCtx, cancel := channelUserConcurrencyOperationContext(ctx)
	defer cancel()
	values, err := store.list(opCtx, channelID, time.Now(), channelUserConcurrencyLeaseTTL)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrChannelUserConcurrencyUnavailable, err)
	}
	items := make([]ChannelUserConcurrencyUsage, 0, len(values))
	for userID, concurrency := range values {
		if concurrency > 0 {
			items = append(items, ChannelUserConcurrencyUsage{UserID: userID, Concurrency: concurrency})
		}
	}
	sort.Slice(items, func(i int, j int) bool {
		return items[i].UserID < items[j].UserID
	})
	return items, channelUserConcurrencyStorageMode(), nil
}

// GetChannelUserConcurrencyUsage 返回指定用户当前持有的有效并发数量。
//
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param userID 用户 ID。
// @return int 当前有效并发数量。
// @return string 状态存储模式。
// @return error 状态读取失败时返回错误。
func GetChannelUserConcurrencyUsage(ctx context.Context, channelID int, userID int) (int, string, error) {
	if channelID <= 0 || userID <= 0 {
		return 0, "", fmt.Errorf("%w: channel_id 和 user_id 必须为正数", ErrChannelUserConcurrencyUnavailable)
	}
	store, err := currentChannelUserConcurrencyStore()
	if err != nil {
		return 0, "", err
	}
	opCtx, cancel := channelUserConcurrencyOperationContext(ctx)
	defer cancel()
	values, err := store.list(opCtx, channelID, time.Now(), channelUserConcurrencyLeaseTTL)
	if err != nil {
		return 0, "", fmt.Errorf("%w: %v", ErrChannelUserConcurrencyUnavailable, err)
	}
	return values[userID], channelUserConcurrencyStorageMode(), nil
}

func currentChannelUserConcurrencyStore() (channelUserConcurrencyStore, error) {
	if !common.RedisEnabled {
		return channelUserConcurrencyMemory, nil
	}
	if common.RDB == nil {
		return nil, fmt.Errorf("%w: Redis 客户端未初始化", ErrChannelUserConcurrencyUnavailable)
	}
	return &channelUserConcurrencyRedisStore{client: common.RDB}, nil
}

func channelUserConcurrencyOperationContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, channelUserConcurrencyOperationTimeout)
}

func (store *channelUserConcurrencyRedisStore) acquire(ctx context.Context, key string, userIndexKey string, _ int, userID int, leaseID string, limit int, now time.Time, expiresAt time.Time, ttl time.Duration) (bool, error) {
	result, err := channelUserConcurrencyAcquireScript.Run(ctx, store.client, []string{key, userIndexKey}, now.UnixMilli(), expiresAt.UnixMilli(), leaseID, limit, ttl.Milliseconds(), userID).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func (store *channelUserConcurrencyRedisStore) renew(ctx context.Context, key string, userIndexKey string, userID int, leaseID string, expiresAt time.Time, ttl time.Duration) (bool, error) {
	result, err := channelUserConcurrencyRenewScript.Run(ctx, store.client, []string{key, userIndexKey}, expiresAt.UnixMilli(), leaseID, ttl.Milliseconds(), userID).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func (store *channelUserConcurrencyRedisStore) release(ctx context.Context, key string, userIndexKey string, _ int, userID int, leaseID string) error {
	return channelUserConcurrencyReleaseScript.Run(ctx, store.client, []string{key, userIndexKey}, leaseID, userID).Err()
}

func (store *channelUserConcurrencyRedisStore) list(ctx context.Context, channelID int, now time.Time, ttl time.Duration) (map[int]int, error) {
	indexKey := channelUserConcurrencyUserIndexKey(channelID)
	members, err := store.client.SMembers(ctx, indexKey).Result()
	if err != nil {
		return nil, err
	}
	userIDs := make([]int, 0, len(members))
	invalidMembers := make([]interface{}, 0)
	for _, member := range members {
		userID, parseErr := strconv.Atoi(member)
		if parseErr == nil && userID > 0 {
			userIDs = append(userIDs, userID)
		} else {
			invalidMembers = append(invalidMembers, member)
		}
	}
	if len(userIDs) == 0 {
		if len(invalidMembers) > 0 {
			if err := store.client.SRem(ctx, indexKey, invalidMembers...).Err(); err != nil {
				return nil, err
			}
		}
		return map[int]int{}, nil
	}
	counts := make([]*redis.IntCmd, len(userIDs))
	_, err = store.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for index, userID := range userIDs {
			key := channelUserConcurrencyKey(channelID, userID)
			pipe.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(now.UnixMilli(), 10))
			counts[index] = pipe.ZCard(ctx, key)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	values := make(map[int]int, len(userIDs))
	emptyUsers := invalidMembers
	for index, userID := range userIDs {
		count := int(counts[index].Val())
		if count <= 0 {
			emptyUsers = append(emptyUsers, userID)
			continue
		}
		values[userID] = count
	}
	_, err = store.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		if len(emptyUsers) > 0 {
			pipe.SRem(ctx, indexKey, emptyUsers...)
		}
		if len(values) > 0 {
			pipe.PExpire(ctx, indexKey, ttl)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return values, nil
}

func newChannelUserConcurrencyMemoryStore() *channelUserConcurrencyMemoryStore {
	return &channelUserConcurrencyMemoryStore{
		leases:   make(map[string]map[string]time.Time),
		userKeys: make(map[int]map[int]string),
	}
}

func (store *channelUserConcurrencyMemoryStore) acquire(_ context.Context, key string, _ string, channelID int, userID int, leaseID string, limit int, now time.Time, expiresAt time.Time, _ time.Duration) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	leases := store.leases[key]
	if leases == nil {
		leases = make(map[string]time.Time)
		store.leases[key] = leases
	}
	for id, expiration := range leases {
		if !expiration.After(now) {
			delete(leases, id)
		}
	}
	if len(leases) >= limit {
		return false, nil
	}
	leases[leaseID] = expiresAt
	if store.userKeys[channelID] == nil {
		store.userKeys[channelID] = make(map[int]string)
	}
	store.userKeys[channelID][userID] = key
	return true, nil
}

func (store *channelUserConcurrencyMemoryStore) renew(_ context.Context, key string, _ string, _ int, leaseID string, expiresAt time.Time, _ time.Duration) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	leases := store.leases[key]
	if leases == nil {
		return false, nil
	}
	if _, ok := leases[leaseID]; !ok {
		return false, nil
	}
	leases[leaseID] = expiresAt
	return true, nil
}

func (store *channelUserConcurrencyMemoryStore) release(_ context.Context, key string, _ string, channelID int, userID int, leaseID string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	leases := store.leases[key]
	if leases == nil {
		return nil
	}
	delete(leases, leaseID)
	if len(leases) == 0 {
		delete(store.leases, key)
		delete(store.userKeys[channelID], userID)
		if len(store.userKeys[channelID]) == 0 {
			delete(store.userKeys, channelID)
		}
	}
	return nil
}

func (store *channelUserConcurrencyMemoryStore) list(_ context.Context, channelID int, now time.Time, _ time.Duration) (map[int]int, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	values := make(map[int]int)
	for userID, key := range store.userKeys[channelID] {
		leases := store.leases[key]
		for leaseID, expiration := range leases {
			if !expiration.After(now) {
				delete(leases, leaseID)
			}
		}
		if len(leases) == 0 {
			delete(store.leases, key)
			delete(store.userKeys[channelID], userID)
			continue
		}
		values[userID] = len(leases)
	}
	if len(store.userKeys[channelID]) == 0 {
		delete(store.userKeys, channelID)
	}
	return values, nil
}

func channelUserConcurrencyKey(channelID int, userID int) string {
	return fmt.Sprintf("channel_user_concurrency:{%d}:{%d}", channelID, userID)
}

func channelUserConcurrencyUserIndexKey(channelID int) string {
	return fmt.Sprintf("channel_user_concurrency_users:{%d}", channelID)
}

func channelUserConcurrencyStorageMode() string {
	if common.RedisEnabled {
		return "redis"
	}
	return "memory"
}
