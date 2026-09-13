package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

// 快照以字符串数组保存，Lua 不对 int64 金额做浮点转换；所有源读取成功后才修改目标。
var channelModelUsageAdjustScript = redis.NewScript(`
local snapshot = {ARGV[2]}
local kind = redis.call('TYPE', KEYS[#KEYS]).ok
if kind ~= 'none' and kind ~= 'hash' then return redis.error_reply('invalid adjustment key type') end
for i = 1, #KEYS - 1 do
 local raw = redis.call('HGET', KEYS[i], ARGV[1]) or '0'
 if not string.match(raw, '^%d+$') or (#raw > 1 and string.sub(raw, 1, 1) == '0') or #raw > 19 or (#raw == 19 and raw > '9223372036854775807') then
  return redis.error_reply('invalid model usage')
 end
 snapshot[#snapshot + 1] = raw
end
redis.call('HSET', KEYS[#KEYS], ARGV[1], cjson.encode(snapshot))
redis.call('EXPIREAT', KEYS[#KEYS], ARGV[3])
return 1
`)

func setChannelModelBudgetUsage(ctx context.Context, counter channelBudgetCounter, scope string, userID int, used int64, now time.Time) error {
	field := "__pool"
	if scope == channelBudgetScopeUser {
		field = strconv.Itoa(userID)
	}
	if common.RedisEnabled {
		if common.RDB == nil {
			return errors.New("周期额度 Redis 未初始化")
		}
		keys := append(append([]string{}, counter.modelKeys...), counter.poolKey)
		return channelModelUsageAdjustScript.Run(ctx, common.RDB, keys, field, strconv.FormatInt(used, 10), counter.end+86400).Err()
	}
	channelPeriodUsageMemory.Lock()
	defer channelPeriodUsageMemory.Unlock()
	snapshot := []string{strconv.FormatInt(used, 10)}
	for _, key := range counter.modelKeys {
		value := int64(0)
		if bucket := channelPeriodUsageMemory.values[key]; bucket != nil {
			value = bucket.values[field]
		}
		if value < 0 {
			return errors.New("模型用量为负数")
		}
		snapshot = append(snapshot, strconv.FormatInt(value, 10))
	}
	// 与事实桶共用过期容器，避免只做人工调整的身份留下永久内存。
	for key, bucket := range channelPeriodUsageMemory.values {
		if bucket.expiresAt <= now.Unix() {
			delete(channelPeriodUsageMemory.values, key)
		}
	}
	bucket := channelPeriodUsageMemory.values[counter.poolKey]
	if bucket == nil {
		bucket = &channelPeriodUsageBucket{expiresAt: counter.end + 86400, since: channelBudgetTrackingStart(counter, now)}
		channelPeriodUsageMemory.values[counter.poolKey] = bucket
	}
	if bucket.adjustments == nil {
		bucket.adjustments = make(map[string][]string)
	}
	bucket.adjustments[field] = snapshot
	return nil
}
