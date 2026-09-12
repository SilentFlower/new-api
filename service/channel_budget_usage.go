package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

var channelPeriodNow = func() time.Time { return channelUserDailyQuotaNow() }

var channelPeriodUsageMemory = struct {
	sync.Mutex
	values map[string]*channelPeriodUsageBucket
}{values: make(map[string]*channelPeriodUsageBucket)}

type channelPeriodUsageBucket struct {
	values    map[string]int64
	since     int64
	expiresAt int64
}

// channelBudgetCounter 是一个计数身份在当前窗口的存储位置。
// 遗留身份的个人字段与池子汇总位于不同 key，其余身份两者同 key。
type channelBudgetCounter struct {
	channelID  int
	identity   string
	userKey    string
	poolKey    string
	start, end int64
	createdAt  int64
	// legacy 表示 userKey 是旧个人日/周 Hash：无 __pool，读写走旧存储，过期沿用旧规则。
	legacy         string
	legacyExpireAt int64
}

// 一个脚本更新全部计数；ARGV 前三项固定为 (userID, delta, now)，之后每个 key 依次携带 (expireAt, since, poolFlag)。
// 先检查所有字段再写入，避免脚本错误留下部分写入；溢出预检用十进制逐位进位以保留 int64 精度。
var channelBudgetUsageAddScript = redis.NewScript(`
local function check_add(raw, delta)
  if not string.match(raw, '^%d+$') or (#raw > 1 and string.sub(raw, 1, 1) == '0') then return false end
  local carry = tonumber(delta)
  local out = ''
  for i = #raw, 1, -1 do
    local n = tonumber(string.sub(raw, i, i)) + carry
    out = tostring(n % 10) .. out
    carry = math.floor(n / 10)
  end
  while carry > 0 do
    out = tostring(carry % 10) .. out
    carry = math.floor(carry / 10)
  end
  return #out < 19 or (#out == 19 and out <= '9223372036854775807')
end
for i, key in ipairs(KEYS) do
  local kind = redis.call('TYPE', key).ok
  if kind ~= 'none' and kind ~= 'hash' then return redis.error_reply('invalid quota key type') end
  local raw = redis.call('HGET', key, ARGV[1]) or '0'
  if not check_add(raw, ARGV[2]) then return redis.error_reply('quota integer overflow') end
  if ARGV[6 + (i - 1) * 3] == '1' then
    raw = redis.call('HGET', key, '__pool') or '0'
    if not check_add(raw, ARGV[2]) then return redis.error_reply('pool integer overflow') end
  end
end
for i, key in ipairs(KEYS) do
  redis.call('HINCRBY', key, ARGV[1], ARGV[2])
  if ARGV[6 + (i - 1) * 3] == '1' then
    redis.call('HINCRBY', key, '__pool', ARGV[2])
    redis.call('HSETNX', key, '__since', ARGV[5 + (i - 1) * 3])
  end
  redis.call('EXPIREAT', key, ARGV[4 + (i - 1) * 3])
end
return 1
`)

// newChannelBudgetCounter 把预算行映射到当前窗口的计数 key。
// @param channelID 渠道 ID。
// @param row 预算行。
// @param res 当前解析结果，提供时刻与时段 occurrence。
// @return 计数位置。
func newChannelBudgetCounter(channelID int, row channelBudgetRow, res channelBudgetResolution) channelBudgetCounter {
	now := res.now.In(time.Local)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	week := day.AddDate(0, 0, -(int(now.Weekday())+6)%7)
	counter := channelBudgetCounter{channelID: channelID, identity: row.identityKey(), createdAt: row.CreatedAt}
	legacy := len(row.Models) == 0
	switch row.Window {
	case channelBudgetWindowDaily:
		counter.start, counter.end = day.Unix(), day.AddDate(0, 0, 1).Unix()
		if legacy {
			period := channelUserDailyQuotaPeriodAt(channelID, now)
			counter.userKey, counter.poolKey = period.redisKey, fmt.Sprintf("channel_pool_daily_quota:{%d}:%s", channelID, day.Format("2006-01-02"))
			counter.legacy, counter.legacyExpireAt, counter.createdAt = channelBudgetWindowDaily, period.resetAt.Add(channelUserDailyQuotaRetention).Unix(), 0
			return counter
		}
	case channelBudgetWindowWeekly:
		counter.start, counter.end = week.Unix(), week.AddDate(0, 0, 7).Unix()
		if legacy {
			period := channelUserWeeklyQuotaPeriodAt(channelID, now)
			counter.userKey, counter.poolKey = period.redisKey, fmt.Sprintf("channel_pool_weekly_quota:{%d}:%s", channelID, week.Format("2006-01-02"))
			counter.legacy, counter.legacyExpireAt, counter.createdAt = channelBudgetWindowWeekly, period.resetAt.Add(channelUserWeeklyQuotaRetention).Unix(), 0
			return counter
		}
	default:
		state := res.schedules[row.ScheduleID]
		counter.start, counter.end, counter.createdAt = state.occ.start.Unix(), state.occ.end.Unix(), state.schedule.rule.CreatedAt
		if legacy {
			counter.userKey = fmt.Sprintf("channel_period_quota:{%d}:%s:%d", channelID, row.ScheduleID, counter.start)
			counter.poolKey = counter.userKey
			return counter
		}
		counter.createdAt = max(counter.createdAt, row.CreatedAt)
	}
	counter.userKey = fmt.Sprintf("channel_budget:{%d}:%s:%d", channelID, row.identityHash(), counter.start)
	counter.poolKey = counter.userKey
	return counter
}

// channelBudgetCounters 按身份去重收集需要处理的计数，保持行顺序。
func channelBudgetCounters(channelID int, plan channelBudgetPlan, res channelBudgetResolution, include func(channelBudgetRow) bool) []channelBudgetCounter {
	seen := make(map[string]bool)
	var counters []channelBudgetCounter
	for _, row := range plan.Rows {
		if !include(row) || seen[row.identityKey()] {
			continue
		}
		seen[row.identityKey()] = true
		counters = append(counters, newChannelBudgetCounter(channelID, row, res))
	}
	return counters
}

func channelBudgetTrackingStart(counter channelBudgetCounter, now time.Time) int64 {
	return min(now.Unix(), max(counter.start, counter.createdAt, common.StartTime))
}

// recordChannelBudgetUsage 把一次正向结算原子累计进所有命中的计数身份。
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param userID 用户 ID。
// @param quota 正向额度。
// @param modelName 客户端原始模型名，空表示不按模型匹配。
// @return 参数、策略或存储错误。
func recordChannelBudgetUsage(ctx context.Context, channelID, userID, quota int, modelName string) error {
	if quota <= 0 {
		return nil
	}
	if channelID <= 0 || userID <= 0 || quota > common.MaxQuota {
		return errors.New("周期额度记录参数无效")
	}
	opCtx, cancel := channelUserDailyQuotaOperationContext(ctx)
	defer cancel()
	policy, err := GetChannelPeriodPolicy(opCtx, channelID)
	if err != nil {
		return err
	}
	now := channelPeriodNow().In(time.Local)
	plan := buildChannelBudgetPlan(policy)
	res, err := resolveChannelBudgetRows(plan, now, modelName)
	if err != nil {
		return err
	}
	// 渠道级日/周身份始终累计，没有对应行时也保留历史，便于以后加行。
	plan.Rows = append(channelBudgetBaseRows(), plan.Rows...)
	counters := channelBudgetCounters(channelID, plan, res, res.counting)
	if common.RedisEnabled {
		if common.RDB == nil {
			return errors.New("周期额度 Redis 未初始化")
		}
		var keys []string
		args := []interface{}{strconv.Itoa(userID), quota, now.Unix()}
		for _, counter := range counters {
			if counter.legacy != "" {
				keys = append(keys, counter.userKey)
				args = append(args, counter.legacyExpireAt, 0, 0)
			}
			keys = append(keys, counter.poolKey)
			args = append(args, counter.end+86400, channelBudgetTrackingStart(counter, now), 1)
		}
		return channelBudgetUsageAddScript.Run(opCtx, common.RDB, keys, args...).Err()
	}
	// 统一持锁顺序保证并发调整不会撕裂一次累计：新增计数 → 旧个人日 → 旧个人周。
	channelPeriodUsageMemory.Lock()
	defer channelPeriodUsageMemory.Unlock()
	channelUserDailyQuotaMemory.mu.Lock()
	defer channelUserDailyQuotaMemory.mu.Unlock()
	channelUserWeeklyQuotaMemory.mu.Lock()
	defer channelUserWeeklyQuotaMemory.mu.Unlock()
	for key, bucket := range channelPeriodUsageMemory.values {
		if bucket.expiresAt <= now.Unix() {
			delete(channelPeriodUsageMemory.values, key)
		}
	}
	field := strconv.Itoa(userID)
	var legacyMaps []map[int]int64
	for _, counter := range counters {
		switch counter.legacy {
		case channelBudgetWindowDaily:
			values := channelUserDailyQuotaMemory.values[channelUserDailyQuotaMemory.preparePeriod(channelUserDailyQuotaPeriodAt(channelID, now))]
			legacyMaps = append(legacyMaps, values)
		case channelBudgetWindowWeekly:
			values := channelUserWeeklyQuotaMemory.values[channelUserWeeklyQuotaMemory.preparePeriod(channelUserWeeklyQuotaPeriodAt(channelID, now))]
			legacyMaps = append(legacyMaps, values)
		}
		bucket := channelPeriodUsageMemory.values[counter.poolKey]
		if bucket == nil {
			bucket = &channelPeriodUsageBucket{values: make(map[string]int64), since: channelBudgetTrackingStart(counter, now), expiresAt: counter.end + 86400}
			channelPeriodUsageMemory.values[counter.poolKey] = bucket
		}
		if bucket.values[field] > math.MaxInt64-int64(quota) || bucket.values["__pool"] > math.MaxInt64-int64(quota) {
			return errors.New("池子或时段额度累计溢出")
		}
	}
	for _, values := range legacyMaps {
		if values[userID] > math.MaxInt64-int64(quota) {
			return errors.New("个人周期额度累计溢出")
		}
	}
	for _, values := range legacyMaps {
		values[userID] += int64(quota)
	}
	for _, counter := range counters {
		bucket := channelPeriodUsageMemory.values[counter.poolKey]
		bucket.values[field] += int64(quota)
		bucket.values["__pool"] += int64(quota)
	}
	return nil
}

// readChannelBudgetCounter 读取某身份当前窗口的个人已用、池子已用与统计起点。
// @param ctx 请求上下文。
// @param counter 计数位置。
// @param userID 用户 ID。
// @param now 当前时刻。
// @return 个人已用、池子已用、统计起点及存储错误。
func readChannelBudgetCounter(ctx context.Context, counter channelBudgetCounter, userID int, now time.Time) (int64, int64, int64, error) {
	userUsed, poolUsed, since, err := readChannelBudgetHash(ctx, counter, userID, now)
	if err != nil || counter.legacy == "" {
		return userUsed, poolUsed, since, err
	}
	// 遗留身份的个人已用来自旧个人 Hash，池子 key 上的用户字段仅自策略建立起累计。
	channelID := counter.channelID
	if counter.legacy == channelBudgetWindowDaily {
		store, storeErr := currentChannelUserDailyQuotaStore()
		if storeErr != nil {
			return 0, 0, 0, storeErr
		}
		userUsed, err = store.get(ctx, channelUserDailyQuotaPeriodAt(channelID, now), userID)
	} else {
		store, storeErr := currentChannelUserWeeklyQuotaStore()
		if storeErr != nil {
			return 0, 0, 0, storeErr
		}
		userUsed, err = store.get(ctx, channelUserWeeklyQuotaPeriodAt(channelID, now), userID)
	}
	return userUsed, poolUsed, since, err
}

func readChannelBudgetHash(ctx context.Context, counter channelBudgetCounter, userID int, now time.Time) (int64, int64, int64, error) {
	if common.RedisEnabled {
		if common.RDB == nil {
			return 0, 0, 0, errors.New("周期额度 Redis 未初始化")
		}
		values, err := common.RDB.HMGet(ctx, counter.poolKey, strconv.Itoa(userID), "__pool", "__since").Result()
		if err != nil {
			return 0, 0, 0, err
		}
		parsed := [3]int64{0, 0, channelBudgetTrackingStart(counter, now)}
		for i, value := range values {
			if value == nil {
				continue
			}
			raw, ok := value.(string)
			if !ok {
				return 0, 0, 0, errors.New("周期额度状态类型无效")
			}
			parsed[i], err = strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed[i] < 0 {
				return 0, 0, 0, errors.New("周期额度状态无效")
			}
		}
		return parsed[0], parsed[1], parsed[2], nil
	}
	channelPeriodUsageMemory.Lock()
	defer channelPeriodUsageMemory.Unlock()
	bucket := channelPeriodUsageMemory.values[counter.poolKey]
	if bucket == nil {
		return 0, 0, channelBudgetTrackingStart(counter, now), nil
	}
	return bucket.values[strconv.Itoa(userID)], bucket.values["__pool"], bucket.since, nil
}

// listChannelBudgetUsage 返回某身份当前窗口内全部用户的已用额度。
// @param ctx 请求上下文。
// @param counter 计数位置。
// @return 用户 ID 到已用额度的映射及存储错误。
func listChannelBudgetUsage(ctx context.Context, counter channelBudgetCounter) (map[int]int64, error) {
	switch counter.legacy {
	case channelBudgetWindowDaily:
		store, err := currentChannelUserDailyQuotaStore()
		if err != nil {
			return nil, err
		}
		return store.list(ctx, channelUserDailyQuotaPeriodAt(counter.channelID, time.Unix(counter.start, 0)))
	case channelBudgetWindowWeekly:
		store, err := currentChannelUserWeeklyQuotaStore()
		if err != nil {
			return nil, err
		}
		return store.list(ctx, channelUserWeeklyQuotaPeriodAt(counter.channelID, time.Unix(counter.start, 0)))
	}
	values := make(map[int]int64)
	if common.RedisEnabled {
		if common.RDB == nil {
			return nil, errors.New("周期额度 Redis 未初始化")
		}
		fields, err := common.RDB.HGetAll(ctx, counter.poolKey).Result()
		if err != nil {
			return nil, err
		}
		for field, raw := range fields {
			userID, parseErr := strconv.Atoi(field)
			used, usedErr := strconv.ParseInt(raw, 10, 64)
			if parseErr != nil || usedErr != nil || userID <= 0 {
				continue
			}
			values[userID] = used
		}
		return values, nil
	}
	channelPeriodUsageMemory.Lock()
	defer channelPeriodUsageMemory.Unlock()
	if bucket := channelPeriodUsageMemory.values[counter.poolKey]; bucket != nil {
		for field, used := range bucket.values {
			if userID, err := strconv.Atoi(field); err == nil && userID > 0 {
				values[userID] = used
			}
		}
	}
	return values, nil
}

// 直接设置池子汇总或某个用户字段；0 删除用户字段，统计起点只在缺失时建立。
var channelBudgetUsageSetScript = redis.NewScript(`
local kind = redis.call('TYPE', KEYS[1]).ok
if kind ~= 'none' and kind ~= 'hash' then return redis.error_reply('invalid quota key type') end
if ARGV[2] == '0' and ARGV[1] ~= '__pool' then
  redis.call('HDEL', KEYS[1], ARGV[1])
else
  redis.call('HSET', KEYS[1], ARGV[1], ARGV[2])
end
if ARGV[4] == '1' then redis.call('HSETNX', KEYS[1], '__since', ARGV[5]) end
redis.call('EXPIREAT', KEYS[1], ARGV[3])
return 1
`)

// setChannelBudgetUsage 把某身份当前窗口的池子汇总或指定用户已用额度设为目标值。
// @param ctx 请求上下文。
// @param counter 计数位置。
// @param scope pool 或 user。
// @param userID scope=user 时的用户 ID。
// @param used 目标已用额度。
// @param now 当前时刻。
// @return 存储错误。
func setChannelBudgetUsage(ctx context.Context, counter channelBudgetCounter, scope string, userID int, used int64, now time.Time) error {
	if scope == channelBudgetScopeUser && counter.legacy != "" {
		// 渠道级个人日/周用量仍存放在旧个人 Hash，沿用其设置语义。
		if used > common.MaxQuota {
			return fmt.Errorf("%w: 旧个人日/周计数不支持超过 32 位的目标值", ErrInvalidChannelPeriodPolicy)
		}
		if counter.legacy == channelBudgetWindowDaily {
			return SetChannelUserDailyQuota(ctx, counter.channelID, userID, int(used))
		}
		return SetChannelUserWeeklyQuota(ctx, counter.channelID, userID, int(used))
	}
	field := "__pool"
	if scope == channelBudgetScopeUser {
		field = strconv.Itoa(userID)
	}
	if common.RedisEnabled {
		if common.RDB == nil {
			return errors.New("周期额度 Redis 未初始化")
		}
		return channelBudgetUsageSetScript.Run(ctx, common.RDB, []string{counter.poolKey}, field, used, counter.end+86400, 1, channelBudgetTrackingStart(counter, now)).Err()
	}
	channelPeriodUsageMemory.Lock()
	defer channelPeriodUsageMemory.Unlock()
	bucket := channelPeriodUsageMemory.values[counter.poolKey]
	if bucket == nil {
		bucket = &channelPeriodUsageBucket{values: make(map[string]int64), since: channelBudgetTrackingStart(counter, now), expiresAt: counter.end + 86400}
		channelPeriodUsageMemory.values[counter.poolKey] = bucket
	}
	if used == 0 && field != "__pool" {
		delete(bucket.values, field)
		return nil
	}
	bucket.values[field] = used
	return nil
}
