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
	channelUserWeeklyQuotaOperationTimeout = 2 * time.Second
	channelUserWeeklyQuotaRetention        = 7 * 24 * time.Hour
)

var (
	// ErrChannelUserWeeklyQuotaExceeded 表示用户在当前渠道的本周额度已经达到上限。
	ErrChannelUserWeeklyQuotaExceeded = errors.New("渠道单用户每周额度已达到上限")
	// ErrChannelUserWeeklyQuotaUnavailable 表示每周额度状态存储不可用。
	ErrChannelUserWeeklyQuotaUnavailable = errors.New("渠道单用户每周额度服务不可用")

	channelUserWeeklyQuotaAddScript = redis.NewScript(`
local value = redis.call('HINCRBY', KEYS[1], ARGV[1], ARGV[2])
redis.call('EXPIRE', KEYS[1], ARGV[3])
return value
`)
	channelUserWeeklyQuotaSetScript = redis.NewScript(`
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
	channelUserWeeklyQuotaNow    = time.Now
	channelUserWeeklyQuotaMemory = newChannelUserWeeklyQuotaMemoryStore()
)

type channelUserWeeklyQuotaPeriod struct {
	channelID int
	date      string
	redisKey  string
	resetAt   time.Time
	ttl       time.Duration
}

type channelUserWeeklyQuotaStore interface {
	get(ctx context.Context, period channelUserWeeklyQuotaPeriod, userID int) (int64, error)
	add(ctx context.Context, period channelUserWeeklyQuotaPeriod, userID int, quota int) error
	list(ctx context.Context, period channelUserWeeklyQuotaPeriod) (map[int]int64, error)
	set(ctx context.Context, period channelUserWeeklyQuotaPeriod, userID int, usedQuota int) error
}

type channelUserWeeklyQuotaRedisStore struct {
	client *redis.Client
}

type channelUserWeeklyQuotaMemoryKey struct {
	channelID int
	date      string
}

type channelUserWeeklyQuotaMemoryStore struct {
	mu     sync.Mutex
	values map[channelUserWeeklyQuotaMemoryKey]map[int]int64
}

// ChannelUserWeeklyQuotaUsage 描述一个用户在指定渠道当前自然周的已用额度。
type ChannelUserWeeklyQuotaUsage struct {
	UserID    int   `json:"user_id"`
	UsedQuota int64 `json:"used_quota"`
}

// CheckChannelUserWeeklyQuota 检查用户在当前渠道是否仍可发起新的计费请求。
//
// @param ctx 请求上下文。
// @param channelID 已选定的渠道 ID。
// @param userID 当前认证用户 ID。
// @param limit 渠道配置的单用户每周额度上限，零或负数表示不限制。
// @return int64 当前自然周已经记录的正向额度。
// @return error 达到上限或状态存储不可用时返回领域错误。
func CheckChannelUserWeeklyQuota(ctx context.Context, channelID int, userID int, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}
	if channelID <= 0 || userID <= 0 {
		return 0, fmt.Errorf("%w: channel_id 和 user_id 必须为正数", ErrChannelUserWeeklyQuotaUnavailable)
	}
	store, err := currentChannelUserWeeklyQuotaStore()
	if err != nil {
		return 0, err
	}
	period := currentChannelUserWeeklyQuotaPeriod(channelID)
	opCtx, cancel := channelUserWeeklyQuotaOperationContext(ctx)
	defer cancel()
	usedQuota, err := store.get(opCtx, period, userID)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrChannelUserWeeklyQuotaUnavailable, err)
	}
	if usedQuota >= int64(limit) {
		return usedQuota, ErrChannelUserWeeklyQuotaExceeded
	}
	return usedQuota, nil
}

// RecordChannelUserWeeklyQuota 记录一次已经完成的渠道正向额度。
//
// @param ctx 请求上下文。
// @param channelID 实际记账的渠道 ID。
// @param userID 实际消费的用户 ID。
// @param quota 本次新增的正向额度。
// @return error 状态写入失败时返回错误。
func RecordChannelUserWeeklyQuota(ctx context.Context, channelID int, userID int, quota int) error {
	if quota <= 0 {
		return nil
	}
	if channelID <= 0 || userID <= 0 || quota > common.MaxQuota {
		return fmt.Errorf("%w: 非法的渠道、用户或额度", ErrChannelUserWeeklyQuotaUnavailable)
	}
	store, err := currentChannelUserWeeklyQuotaStore()
	if err != nil {
		return err
	}
	period := currentChannelUserWeeklyQuotaPeriod(channelID)
	opCtx, cancel := channelUserWeeklyQuotaOperationContext(ctx)
	defer cancel()
	if err := store.add(opCtx, period, userID, quota); err != nil {
		return fmt.Errorf("%w: %v", ErrChannelUserWeeklyQuotaUnavailable, err)
	}
	return nil
}

// RecordRelayChannelUserWeeklyQuota 记录 Relay 已完成的渠道正向额度，失败时只写安全告警。
//
// @param ctx 请求上下文。
// @param relayInfo 包含最终渠道和用户的 Relay 信息。
// @param quota 本次新增的正向额度。
// @return 无。
func RecordRelayChannelUserWeeklyQuota(ctx context.Context, relayInfo *relaycommon.RelayInfo, quota int) {
	if relayInfo == nil || relayInfo.ChannelMeta == nil || quota <= 0 {
		return
	}
	if err := RecordChannelUserWeeklyQuota(ctx, relayInfo.ChannelId, relayInfo.UserId, quota); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf(
			"记录渠道单用户每周额度失败: channel_id=%d user_id=%d quota=%d error=%s",
			relayInfo.ChannelId,
			relayInfo.UserId,
			quota,
			common.LocalLogPreview(err.Error()),
		))
	}
}

// ListChannelUserWeeklyQuota 返回指定渠道当前自然周出现过的用户额度。
//
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @return []ChannelUserWeeklyQuotaUsage 按用户 ID 排序的本周额度列表。
// @return int64 当前周期结束时间的 Unix 时间戳。
// @return string 状态存储模式，取值为 redis 或 memory。
// @return error 状态读取失败时返回错误。
func ListChannelUserWeeklyQuota(ctx context.Context, channelID int) ([]ChannelUserWeeklyQuotaUsage, int64, string, error) {
	if channelID <= 0 {
		return nil, 0, "", fmt.Errorf("%w: channel_id 必须为正数", ErrChannelUserWeeklyQuotaUnavailable)
	}
	store, err := currentChannelUserWeeklyQuotaStore()
	if err != nil {
		return nil, 0, "", err
	}
	period := currentChannelUserWeeklyQuotaPeriod(channelID)
	opCtx, cancel := channelUserWeeklyQuotaOperationContext(ctx)
	defer cancel()
	values, err := store.list(opCtx, period)
	if err != nil {
		return nil, 0, "", fmt.Errorf("%w: %v", ErrChannelUserWeeklyQuotaUnavailable, err)
	}
	items := make([]ChannelUserWeeklyQuotaUsage, 0, len(values))
	for userID, usedQuota := range values {
		items = append(items, ChannelUserWeeklyQuotaUsage{UserID: userID, UsedQuota: usedQuota})
	}
	sort.Slice(items, func(i int, j int) bool {
		return items[i].UserID < items[j].UserID
	})
	return items, period.resetAt.Unix(), channelUserWeeklyQuotaStorageMode(), nil
}

// SetChannelUserWeeklyQuota 把用户当前自然周的已用额度设置为目标值。
//
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param userID 用户 ID。
// @param usedQuota 调整后的已用额度，零表示删除当前累计。
// @return error 状态写入失败时返回错误。
func SetChannelUserWeeklyQuota(ctx context.Context, channelID int, userID int, usedQuota int) error {
	if channelID <= 0 || userID <= 0 || usedQuota < 0 || usedQuota > common.MaxQuota {
		return fmt.Errorf("%w: 非法的渠道、用户或目标额度", ErrChannelUserWeeklyQuotaUnavailable)
	}
	store, err := currentChannelUserWeeklyQuotaStore()
	if err != nil {
		return err
	}
	period := currentChannelUserWeeklyQuotaPeriod(channelID)
	opCtx, cancel := channelUserWeeklyQuotaOperationContext(ctx)
	defer cancel()
	if err := store.set(opCtx, period, userID, usedQuota); err != nil {
		return fmt.Errorf("%w: %v", ErrChannelUserWeeklyQuotaUnavailable, err)
	}
	return nil
}

// GetChannelUserWeeklyQuotaUsage 返回指定用户当前自然周的已用额度和刷新时间。
//
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param userID 用户 ID。
// @return int64 当前自然周已用额度。
// @return int64 下次刷新时间的 Unix 秒。
// @return string 状态存储模式。
// @return error 状态读取失败时返回错误。
func GetChannelUserWeeklyQuotaUsage(ctx context.Context, channelID int, userID int) (int64, int64, string, error) {
	if channelID <= 0 || userID <= 0 {
		return 0, 0, "", fmt.Errorf("%w: channel_id 和 user_id 必须为正数", ErrChannelUserWeeklyQuotaUnavailable)
	}
	store, err := currentChannelUserWeeklyQuotaStore()
	if err != nil {
		return 0, 0, "", err
	}
	period := currentChannelUserWeeklyQuotaPeriod(channelID)
	opCtx, cancel := channelUserWeeklyQuotaOperationContext(ctx)
	defer cancel()
	usedQuota, err := store.get(opCtx, period, userID)
	if err != nil {
		return 0, 0, "", fmt.Errorf("%w: %v", ErrChannelUserWeeklyQuotaUnavailable, err)
	}
	return usedQuota, period.resetAt.Unix(), channelUserWeeklyQuotaStorageMode(), nil
}

func currentChannelUserWeeklyQuotaStore() (channelUserWeeklyQuotaStore, error) {
	if !common.RedisEnabled {
		return channelUserWeeklyQuotaMemory, nil
	}
	if common.RDB == nil {
		return nil, fmt.Errorf("%w: Redis 客户端未初始化", ErrChannelUserWeeklyQuotaUnavailable)
	}
	return &channelUserWeeklyQuotaRedisStore{client: common.RDB}, nil
}

func currentChannelUserWeeklyQuotaPeriod(channelID int) channelUserWeeklyQuotaPeriod {
	now := channelUserWeeklyQuotaNow().In(time.Local)
	daysSinceMonday := (int(now.Weekday()) + 6) % 7
	weekStart := time.Date(now.Year(), now.Month(), now.Day()-daysSinceMonday, 0, 0, 0, 0, time.Local)
	resetAt := weekStart.AddDate(0, 0, 7)
	return channelUserWeeklyQuotaPeriod{
		channelID: channelID,
		date:      weekStart.Format("2006-01-02"),
		redisKey:  fmt.Sprintf("channel_user_weekly_quota:{%d}:%s", channelID, weekStart.Format("2006-01-02")),
		resetAt:   resetAt,
		ttl:       resetAt.Sub(now) + channelUserWeeklyQuotaRetention,
	}
}

func channelUserWeeklyQuotaStorageMode() string {
	if common.RedisEnabled {
		return "redis"
	}
	return "memory"
}

func channelUserWeeklyQuotaOperationContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, channelUserWeeklyQuotaOperationTimeout)
}

func channelUserWeeklyQuotaTTLSeconds(period channelUserWeeklyQuotaPeriod) int64 {
	seconds := int64(period.ttl / time.Second)
	if period.ttl%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		return 1
	}
	return seconds
}

func (store *channelUserWeeklyQuotaRedisStore) get(ctx context.Context, period channelUserWeeklyQuotaPeriod, userID int) (int64, error) {
	value, err := store.client.HGet(ctx, period.redisKey, strconv.Itoa(userID)).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	return value, err
}

func (store *channelUserWeeklyQuotaRedisStore) add(ctx context.Context, period channelUserWeeklyQuotaPeriod, userID int, quota int) error {
	return channelUserWeeklyQuotaAddScript.Run(
		ctx,
		store.client,
		[]string{period.redisKey},
		strconv.Itoa(userID),
		quota,
		channelUserWeeklyQuotaTTLSeconds(period),
	).Err()
}

func (store *channelUserWeeklyQuotaRedisStore) list(ctx context.Context, period channelUserWeeklyQuotaPeriod) (map[int]int64, error) {
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

func (store *channelUserWeeklyQuotaRedisStore) set(ctx context.Context, period channelUserWeeklyQuotaPeriod, userID int, usedQuota int) error {
	return channelUserWeeklyQuotaSetScript.Run(
		ctx,
		store.client,
		[]string{period.redisKey},
		strconv.Itoa(userID),
		usedQuota,
		channelUserWeeklyQuotaTTLSeconds(period),
	).Err()
}

func newChannelUserWeeklyQuotaMemoryStore() *channelUserWeeklyQuotaMemoryStore {
	return &channelUserWeeklyQuotaMemoryStore{values: make(map[channelUserWeeklyQuotaMemoryKey]map[int]int64)}
}

func (store *channelUserWeeklyQuotaMemoryStore) get(_ context.Context, period channelUserWeeklyQuotaPeriod, userID int) (int64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := store.preparePeriod(period)
	return store.values[key][userID], nil
}

func (store *channelUserWeeklyQuotaMemoryStore) add(_ context.Context, period channelUserWeeklyQuotaPeriod, userID int, quota int) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := store.preparePeriod(period)
	if store.values[key][userID] > math.MaxInt64-int64(quota) {
		return errors.New("渠道单用户每周额度累计溢出")
	}
	store.values[key][userID] += int64(quota)
	return nil
}

func (store *channelUserWeeklyQuotaMemoryStore) list(_ context.Context, period channelUserWeeklyQuotaPeriod) (map[int]int64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := store.preparePeriod(period)
	result := make(map[int]int64, len(store.values[key]))
	for userID, usedQuota := range store.values[key] {
		result[userID] = usedQuota
	}
	return result, nil
}

func (store *channelUserWeeklyQuotaMemoryStore) set(_ context.Context, period channelUserWeeklyQuotaPeriod, userID int, usedQuota int) error {
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

func (store *channelUserWeeklyQuotaMemoryStore) preparePeriod(period channelUserWeeklyQuotaPeriod) channelUserWeeklyQuotaMemoryKey {
	key := channelUserWeeklyQuotaMemoryKey{channelID: period.channelID, date: period.date}
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
