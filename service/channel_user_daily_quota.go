package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/go-redis/redis/v8"
)

const (
	channelUserDailyQuotaOperationTimeout = 2 * time.Second
	channelUserDailyQuotaRetention        = 24 * time.Hour
)

var (
	// ErrChannelUserDailyQuotaExceeded 表示用户在当前渠道的当日额度已经达到上限。
	ErrChannelUserDailyQuotaExceeded = errors.New("渠道单用户每日额度已达到上限")
	// ErrChannelUserDailyQuotaUnavailable 表示每日额度状态存储不可用。
	ErrChannelUserDailyQuotaUnavailable = errors.New("渠道单用户每日额度服务不可用")

	channelUserDailyQuotaAddScript = redis.NewScript(`
local value = redis.call('HINCRBY', KEYS[1], ARGV[1], ARGV[2])
redis.call('EXPIRE', KEYS[1], ARGV[3])
return value
`)
	channelUserDailyQuotaSetScript = redis.NewScript(`
local field = ARGV[1]
local value = tonumber(ARGV[2])
local ttl = tonumber(ARGV[3])

if value == 0 then
  redis.call('HDEL', KEYS[1], field)
  if redis.call('HLEN', KEYS[1]) == 0 then
    redis.call('DEL', KEYS[1])
  end
  return 0
end

redis.call('HSET', KEYS[1], field, value)
redis.call('EXPIRE', KEYS[1], ttl)
return value
`)
	channelUserDailyQuotaNow    = time.Now
	channelUserDailyQuotaMemory = newChannelUserDailyQuotaMemoryStore()
)

type channelUserDailyQuotaPeriod struct {
	channelID int
	date      string
	redisKey  string
	resetAt   time.Time
	ttl       time.Duration
}

type channelUserDailyQuotaStore interface {
	get(ctx context.Context, period channelUserDailyQuotaPeriod, userID int) (int64, error)
	add(ctx context.Context, period channelUserDailyQuotaPeriod, userID int, quota int) error
	list(ctx context.Context, period channelUserDailyQuotaPeriod) (map[int]int64, error)
	set(ctx context.Context, period channelUserDailyQuotaPeriod, userID int, usedQuota int) error
}

type channelUserDailyQuotaRedisStore struct {
	client *redis.Client
}

type channelUserDailyQuotaMemoryKey struct {
	channelID int
	date      string
}

type channelUserDailyQuotaMemoryStore struct {
	mu     sync.Mutex
	values map[channelUserDailyQuotaMemoryKey]map[int]int64
}

// ChannelUserDailyQuotaUsage 描述一个用户在指定渠道当前自然日的已用额度。
type ChannelUserDailyQuotaUsage struct {
	UserID    int   `json:"user_id"`
	UsedQuota int64 `json:"used_quota"`
}

// CheckChannelUserDailyQuota 检查用户在当前渠道是否仍可发起新的计费请求。
//
// @param ctx 请求上下文。
// @param channelID 已选定的渠道 ID。
// @param userID 当前认证用户 ID。
// @param limit 渠道配置的单用户每日额度上限，零或负数表示不限制。
// @return int64 当前自然日已经记录的正向额度。
// @return error 达到上限或状态存储不可用时返回领域错误。
func CheckChannelUserDailyQuota(ctx context.Context, channelID int, userID int, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}
	if channelID <= 0 || userID <= 0 {
		return 0, fmt.Errorf("%w: channel_id 和 user_id 必须为正数", ErrChannelUserDailyQuotaUnavailable)
	}
	store, err := currentChannelUserDailyQuotaStore()
	if err != nil {
		return 0, err
	}
	period := currentChannelUserDailyQuotaPeriod(channelID)
	opCtx, cancel := channelUserDailyQuotaOperationContext(ctx)
	defer cancel()
	usedQuota, err := store.get(opCtx, period, userID)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrChannelUserDailyQuotaUnavailable, err)
	}
	if usedQuota >= int64(limit) {
		return usedQuota, ErrChannelUserDailyQuotaExceeded
	}
	return usedQuota, nil
}

// RecordChannelUserDailyQuota 记录一次已经完成的渠道正向额度。
//
// @param ctx 请求上下文。
// @param channelID 实际记账的渠道 ID。
// @param userID 实际消费的用户 ID。
// @param quota 本次新增的正向额度。
// @return error 状态写入失败时返回错误。
func RecordChannelUserDailyQuota(ctx context.Context, channelID int, userID int, quota int) error {
	if quota <= 0 {
		return nil
	}
	if channelID <= 0 || userID <= 0 || quota > common.MaxQuota {
		return fmt.Errorf("%w: 非法的渠道、用户或额度", ErrChannelUserDailyQuotaUnavailable)
	}
	store, err := currentChannelUserDailyQuotaStore()
	if err != nil {
		return err
	}
	period := currentChannelUserDailyQuotaPeriod(channelID)
	opCtx, cancel := channelUserDailyQuotaOperationContext(ctx)
	defer cancel()
	if err := store.add(opCtx, period, userID, quota); err != nil {
		return fmt.Errorf("%w: %v", ErrChannelUserDailyQuotaUnavailable, err)
	}
	return nil
}

// RecordRelayChannelUserDailyQuota 记录 Relay 已完成的渠道正向额度，失败时只写安全告警。
//
// @param ctx 请求上下文。
// @param relayInfo 包含最终渠道、用户和每日额度配置快照的 Relay 信息。
// @param quota 本次新增的正向额度。
// @return 无。
func RecordRelayChannelUserDailyQuota(ctx context.Context, relayInfo *relaycommon.RelayInfo, quota int) {
	if relayInfo == nil || relayInfo.ChannelMeta == nil || relayInfo.ChannelUserDailyQuotaLimit <= 0 || quota <= 0 {
		return
	}
	if err := RecordChannelUserDailyQuota(ctx, relayInfo.ChannelId, relayInfo.UserId, quota); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf(
			"记录渠道单用户每日额度失败: channel_id=%d user_id=%d quota=%d error=%s",
			relayInfo.ChannelId,
			relayInfo.UserId,
			quota,
			common.LocalLogPreview(err.Error()),
		))
	}
}

// ListChannelUserDailyQuota 返回指定渠道当前自然日出现过的用户额度。
//
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @return []ChannelUserDailyQuotaUsage 按用户 ID 排序的当日额度列表。
// @return int64 当前周期结束时间的 Unix 时间戳。
// @return string 状态存储模式，取值为 redis 或 memory。
// @return error 状态读取失败时返回错误。
func ListChannelUserDailyQuota(ctx context.Context, channelID int) ([]ChannelUserDailyQuotaUsage, int64, string, error) {
	if channelID <= 0 {
		return nil, 0, "", fmt.Errorf("%w: channel_id 必须为正数", ErrChannelUserDailyQuotaUnavailable)
	}
	store, err := currentChannelUserDailyQuotaStore()
	if err != nil {
		return nil, 0, "", err
	}
	period := currentChannelUserDailyQuotaPeriod(channelID)
	opCtx, cancel := channelUserDailyQuotaOperationContext(ctx)
	defer cancel()
	values, err := store.list(opCtx, period)
	if err != nil {
		return nil, 0, "", fmt.Errorf("%w: %v", ErrChannelUserDailyQuotaUnavailable, err)
	}
	items := make([]ChannelUserDailyQuotaUsage, 0, len(values))
	for userID, usedQuota := range values {
		items = append(items, ChannelUserDailyQuotaUsage{UserID: userID, UsedQuota: usedQuota})
	}
	sort.Slice(items, func(i int, j int) bool {
		return items[i].UserID < items[j].UserID
	})
	return items, period.resetAt.Unix(), channelUserDailyQuotaStorageMode(), nil
}

// SetChannelUserDailyQuota 把用户当前自然日的已用额度设置为目标值。
//
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param userID 用户 ID。
// @param usedQuota 调整后的已用额度，零表示删除当前累计。
// @return error 状态写入失败时返回错误。
func SetChannelUserDailyQuota(ctx context.Context, channelID int, userID int, usedQuota int) error {
	if channelID <= 0 || userID <= 0 || usedQuota < 0 || usedQuota > common.MaxQuota {
		return fmt.Errorf("%w: 非法的渠道、用户或目标额度", ErrChannelUserDailyQuotaUnavailable)
	}
	store, err := currentChannelUserDailyQuotaStore()
	if err != nil {
		return err
	}
	period := currentChannelUserDailyQuotaPeriod(channelID)
	opCtx, cancel := channelUserDailyQuotaOperationContext(ctx)
	defer cancel()
	if err := store.set(opCtx, period, userID, usedQuota); err != nil {
		return fmt.Errorf("%w: %v", ErrChannelUserDailyQuotaUnavailable, err)
	}
	return nil
}

func currentChannelUserDailyQuotaStore() (channelUserDailyQuotaStore, error) {
	if !common.RedisEnabled {
		return channelUserDailyQuotaMemory, nil
	}
	if common.RDB == nil {
		return nil, fmt.Errorf("%w: Redis 客户端未初始化", ErrChannelUserDailyQuotaUnavailable)
	}
	return &channelUserDailyQuotaRedisStore{client: common.RDB}, nil
}

func currentChannelUserDailyQuotaPeriod(channelID int) channelUserDailyQuotaPeriod {
	now := channelUserDailyQuotaNow().In(time.Local)
	resetAt := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.Local)
	return channelUserDailyQuotaPeriod{
		channelID: channelID,
		date:      now.Format("2006-01-02"),
		redisKey:  fmt.Sprintf("channel_user_daily_quota:{%d}:%s", channelID, now.Format("2006-01-02")),
		resetAt:   resetAt,
		ttl:       resetAt.Sub(now) + channelUserDailyQuotaRetention,
	}
}

func channelUserDailyQuotaStorageMode() string {
	if common.RedisEnabled {
		return "redis"
	}
	return "memory"
}

func channelUserDailyQuotaOperationContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, channelUserDailyQuotaOperationTimeout)
}

func channelUserDailyQuotaTTLSeconds(period channelUserDailyQuotaPeriod) int64 {
	seconds := int64(period.ttl / time.Second)
	if period.ttl%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		return 1
	}
	return seconds
}

func (store *channelUserDailyQuotaRedisStore) get(ctx context.Context, period channelUserDailyQuotaPeriod, userID int) (int64, error) {
	value, err := store.client.HGet(ctx, period.redisKey, strconv.Itoa(userID)).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	return value, err
}

func (store *channelUserDailyQuotaRedisStore) add(ctx context.Context, period channelUserDailyQuotaPeriod, userID int, quota int) error {
	return channelUserDailyQuotaAddScript.Run(
		ctx,
		store.client,
		[]string{period.redisKey},
		strconv.Itoa(userID),
		quota,
		channelUserDailyQuotaTTLSeconds(period),
	).Err()
}

func (store *channelUserDailyQuotaRedisStore) list(ctx context.Context, period channelUserDailyQuotaPeriod) (map[int]int64, error) {
	values, err := store.client.HGetAll(ctx, period.redisKey).Result()
	if err != nil {
		return nil, err
	}
	result := make(map[int]int64, len(values))
	for rawUserID, rawQuota := range values {
		userID, userErr := strconv.Atoi(rawUserID)
		quota, quotaErr := strconv.ParseInt(rawQuota, 10, 64)
		if userErr != nil || quotaErr != nil || userID <= 0 || quota < 0 {
			continue
		}
		result[userID] = quota
	}
	return result, nil
}

func (store *channelUserDailyQuotaRedisStore) set(ctx context.Context, period channelUserDailyQuotaPeriod, userID int, usedQuota int) error {
	return channelUserDailyQuotaSetScript.Run(
		ctx,
		store.client,
		[]string{period.redisKey},
		strconv.Itoa(userID),
		usedQuota,
		channelUserDailyQuotaTTLSeconds(period),
	).Err()
}

func newChannelUserDailyQuotaMemoryStore() *channelUserDailyQuotaMemoryStore {
	return &channelUserDailyQuotaMemoryStore{values: make(map[channelUserDailyQuotaMemoryKey]map[int]int64)}
}

func (store *channelUserDailyQuotaMemoryStore) get(_ context.Context, period channelUserDailyQuotaPeriod, userID int) (int64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := store.preparePeriod(period)
	return store.values[key][userID], nil
}

func (store *channelUserDailyQuotaMemoryStore) add(_ context.Context, period channelUserDailyQuotaPeriod, userID int, quota int) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := store.preparePeriod(period)
	if store.values[key][userID] > math.MaxInt64-int64(quota) {
		return errors.New("渠道单用户每日额度累计溢出")
	}
	store.values[key][userID] += int64(quota)
	return nil
}

func (store *channelUserDailyQuotaMemoryStore) list(_ context.Context, period channelUserDailyQuotaPeriod) (map[int]int64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := store.preparePeriod(period)
	result := make(map[int]int64, len(store.values[key]))
	for userID, usedQuota := range store.values[key] {
		result[userID] = usedQuota
	}
	return result, nil
}

func (store *channelUserDailyQuotaMemoryStore) set(_ context.Context, period channelUserDailyQuotaPeriod, userID int, usedQuota int) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := store.preparePeriod(period)
	if usedQuota == 0 {
		delete(store.values[key], userID)
		return nil
	}
	store.values[key][userID] = int64(usedQuota)
	return nil
}

func (store *channelUserDailyQuotaMemoryStore) preparePeriod(period channelUserDailyQuotaPeriod) channelUserDailyQuotaMemoryKey {
	key := channelUserDailyQuotaMemoryKey{channelID: period.channelID, date: period.date}
	for existingKey := range store.values {
		if existingKey.date != period.date {
			delete(store.values, existingKey)
		}
	}
	if store.values[key] == nil {
		store.values[key] = make(map[int]int64)
	}
	return key
}
