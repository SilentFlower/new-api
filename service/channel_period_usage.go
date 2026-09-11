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

type channelPeriodCounter struct {
	key        string
	start, end int64
	createdAt  int64
}

// 一个脚本更新全部限额计数；先检查所有字段，避免 Redis 脚本错误留下部分写入。
// 累加由 HINCRBY 执行。溢出预检使用十进制逐位进位，避免 Lua double 丢失 int64 精度。
var channelPeriodUsageAddScript = redis.NewScript(`
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
  if i > 2 then
    raw = redis.call('HGET', key, '__pool') or '0'
    if not check_add(raw, ARGV[2]) then return redis.error_reply('pool integer overflow') end
  end
end
for i, key in ipairs(KEYS) do
  redis.call('HINCRBY', key, ARGV[1], ARGV[2])
  if i > 2 then
    redis.call('HINCRBY', key, '__pool', ARGV[2])
    redis.call('HSETNX', key, '__since', ARGV[3 + i*2])
  end
  redis.call('EXPIREAT', key, ARGV[2 + i*2])
end
return 1
`)

func channelPeriodCounters(channelID int, now time.Time, active []channelRuleOccurrence) []channelPeriodCounter {
	now = now.In(time.Local)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	week := day.AddDate(0, 0, -(int(now.Weekday())+6)%7)
	items := []channelPeriodCounter{
		{key: fmt.Sprintf("channel_pool_daily_quota:{%d}:%s", channelID, day.Format("2006-01-02")), start: day.Unix(), end: day.AddDate(0, 0, 1).Unix()},
		{key: fmt.Sprintf("channel_pool_weekly_quota:{%d}:%s", channelID, week.Format("2006-01-02")), start: week.Unix(), end: week.AddDate(0, 0, 7).Unix()},
	}
	for _, occ := range active {
		items = append(items, channelPeriodCounter{key: fmt.Sprintf("channel_period_quota:{%d}:%s:%d", channelID, occ.rule.ID, occ.start.Unix()), start: occ.start.Unix(), end: occ.end.Unix(), createdAt: occ.rule.CreatedAt})
	}
	return items
}

func channelPeriodTrackingStart(counter channelPeriodCounter, now time.Time) int64 {
	return min(now.Unix(), max(counter.start, counter.createdAt, common.StartTime))
}

func recordChannelPeriodUsage(ctx context.Context, channelID, userID, quota int) error {
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
	// 停用只关闭拦截，当前 occurrence 仍累计，恢复后不能获得额外额度。
	trackingConfig := policy.Config
	trackingConfig.Rules = append(trackingConfig.Rules[:0:0], trackingConfig.Rules...)
	for i := range trackingConfig.Rules {
		trackingConfig.Rules[i].Enabled = true
	}
	_, _, active, _, err := resolveChannelPeriodSources(trackingConfig, 0, now)
	if err != nil {
		return err
	}
	counters := channelPeriodCounters(channelID, now, active)
	daily, weekly := channelUserDailyQuotaPeriodAt(channelID, now), channelUserWeeklyQuotaPeriodAt(channelID, now)
	if common.RedisEnabled {
		if common.RDB == nil {
			return errors.New("周期额度 Redis 未初始化")
		}
		keys := []string{daily.redisKey, weekly.redisKey}
		// ARGV 前三项固定，后续按 key 顺序传入过期时间和统计起点。
		args := []interface{}{strconv.Itoa(userID), quota, now.Unix(), daily.resetAt.Add(channelUserDailyQuotaRetention).Unix(), 0, weekly.resetAt.Add(channelUserWeeklyQuotaRetention).Unix(), 0}
		for _, counter := range counters {
			keys = append(keys, counter.key)
			args = append(args, counter.end+86400, channelPeriodTrackingStart(counter, now))
		}
		return channelPeriodUsageAddScript.Run(opCtx, common.RDB, keys, args...).Err()
	}
	// 保留旧管理接口读写的 map；统一持锁顺序保证并发调整不会撕裂一次累计。
	channelPeriodUsageMemory.Lock()
	defer channelPeriodUsageMemory.Unlock()
	channelUserDailyQuotaMemory.mu.Lock()
	defer channelUserDailyQuotaMemory.mu.Unlock()
	channelUserWeeklyQuotaMemory.mu.Lock()
	defer channelUserWeeklyQuotaMemory.mu.Unlock()
	dailyKey, weeklyKey := channelUserDailyQuotaMemory.preparePeriod(daily), channelUserWeeklyQuotaMemory.preparePeriod(weekly)
	dailyValues, weeklyValues := channelUserDailyQuotaMemory.values[dailyKey], channelUserWeeklyQuotaMemory.values[weeklyKey]
	if dailyValues[userID] > math.MaxInt64-int64(quota) || weeklyValues[userID] > math.MaxInt64-int64(quota) {
		return errors.New("个人周期额度累计溢出")
	}
	for key, bucket := range channelPeriodUsageMemory.values {
		if bucket.expiresAt <= now.Unix() {
			delete(channelPeriodUsageMemory.values, key)
		}
	}
	field := strconv.Itoa(userID)
	for _, counter := range counters {
		bucket := channelPeriodUsageMemory.values[counter.key]
		if bucket == nil {
			bucket = &channelPeriodUsageBucket{values: make(map[string]int64), since: channelPeriodTrackingStart(counter, now), expiresAt: counter.end + 86400}
			channelPeriodUsageMemory.values[counter.key] = bucket
		}
		if bucket.values[field] > math.MaxInt64-int64(quota) || bucket.values["__pool"] > math.MaxInt64-int64(quota) {
			return errors.New("池子或时段额度累计溢出")
		}
	}
	dailyValues[userID] += int64(quota)
	weeklyValues[userID] += int64(quota)
	for _, counter := range counters {
		bucket := channelPeriodUsageMemory.values[counter.key]
		bucket.values[field] += int64(quota)
		bucket.values["__pool"] += int64(quota)
	}
	return nil
}

func getChannelPeriodUsage(ctx context.Context, counter channelPeriodCounter, userID int, now time.Time) (int64, int64, int64, error) {
	if common.RedisEnabled {
		if common.RDB == nil {
			return 0, 0, 0, errors.New("周期额度 Redis 未初始化")
		}
		values, err := common.RDB.HMGet(ctx, counter.key, strconv.Itoa(userID), "__pool", "__since").Result()
		if err != nil {
			return 0, 0, 0, err
		}
		parsed := [3]int64{0, 0, channelPeriodTrackingStart(counter, now)}
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
	bucket := channelPeriodUsageMemory.values[counter.key]
	if bucket == nil {
		return 0, 0, channelPeriodTrackingStart(counter, now), nil
	}
	return bucket.values[strconv.Itoa(userID)], bucket.values["__pool"], bucket.since, nil
}
