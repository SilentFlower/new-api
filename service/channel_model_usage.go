package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/go-redis/redis/v8"
)

const channelModelCounterSource = "continuous_model"

// normalizeChannelModelTracking 保留旧客户端省略的开关，并只由服务端推进覆盖时间。
func normalizeChannelModelTracking(config *dto.ChannelPeriodPolicyConfig, previous dto.ChannelPeriodPolicyConfig, now time.Time) error {
	if previous.ModelUsageTracking != nil {
		old := *previous.ModelUsageTracking
		if old.FirstEnabledAt <= 0 || old.EnabledAt < old.FirstEnabledAt || old.DisabledAt < 0 {
			return errors.New("模型累计历史无效")
		}
		config.ModelUsageTracking = &old
	} else {
		config.ModelUsageTracking = nil
	}
	if config.ModelUsageTrackingEnabled == nil {
		config.ModelUsageTrackingEnabled = previous.ModelUsageTrackingEnabled
	}
	enabled := config.ModelUsageTrackingEnabled != nil && *config.ModelUsageTrackingEnabled
	wasEnabled := previous.ModelUsageTrackingEnabled != nil && *previous.ModelUsageTrackingEnabled
	if enabled && !wasEnabled {
		if config.ModelUsageTracking == nil {
			config.ModelUsageTracking = &dto.ChannelModelUsageTracking{FirstEnabledAt: now.Unix()}
		}
		config.ModelUsageTracking.EnabledAt = now.Unix()
	}
	if !enabled && wasEnabled && config.ModelUsageTracking != nil {
		config.ModelUsageTracking.DisabledAt = now.Unix()
	}
	if enabled && config.ModelUsageTracking == nil {
		return errors.New("模型累计缺少开启时间")
	}
	return nil
}

// normalizeChannelModelCounterSources 按计数身份复用旧来源，避免个人/池子或时段行意外读到不同数据。
func normalizeChannelModelCounterSources(rows, previous []dto.ChannelBudgetRow, trackingStarted bool) error {
	sources := make(map[string]string, len(previous))
	for _, row := range previous {
		if row.CounterSource != "" && (row.CounterSource != channelModelCounterSource || !trackingStarted || len(row.Models) == 0 || row.Window == channelBudgetWindowOccurrence) {
			return errors.New("模型预算计数来源无效")
		}
		identity := (channelBudgetRow{ChannelBudgetRow: row}).identityKey()
		if source, ok := sources[identity]; ok && source != row.CounterSource {
			return errors.New("相同预算身份的计数来源不一致")
		}
		sources[identity] = row.CounterSource
	}
	for i := range rows {
		row := &rows[i]
		if row.CounterSource != "" && row.CounterSource != channelModelCounterSource {
			return errors.New("模型预算计数来源无效")
		}
		identity := (channelBudgetRow{ChannelBudgetRow: *row}).identityKey()
		source, exists := sources[identity]
		if !exists && trackingStarted && len(row.Models) > 0 && row.Window != channelBudgetWindowOccurrence {
			source = channelModelCounterSource
		}
		row.CounterSource = source
		sources[identity] = source
	}
	return nil
}

func channelModelUsageKey(channelID int, window, modelName string, start int64) string {
	digest := sha256.Sum256([]byte(modelName))
	return fmt.Sprintf("channel_model_usage:{%d}:%s:%x:%d", channelID, window, digest, start)
}

func configureChannelModelCounter(counter *channelBudgetCounter, row channelBudgetRow, res channelBudgetResolution) {
	counter.poolKey = "channel_model_adjustment" + strings.TrimPrefix(counter.poolKey, "channel_budget")
	counter.userKey = counter.poolKey
	counter.createdAt = row.CreatedAt
	if res.tracking != nil {
		counter.createdAt = res.tracking.FirstEnabledAt
		// 只保存有界切换历史；当前窗口内曾关闭时保守提示缺口，不声称补齐关闭区段。
		counter.trackingGap = res.tracking.DisabledAt > 0 && res.tracking.DisabledAt < counter.end &&
			(res.tracking.EnabledAt <= res.tracking.DisabledAt || res.tracking.EnabledAt > counter.start)
	}
	for _, modelName := range row.Models {
		counter.modelKeys = append(counter.modelKeys, channelModelUsageKey(counter.channelID, row.Window, modelName, counter.start))
	}
}

// channelModelUsageCounters 每次结算至多增加两个模型事实；关闭后仅维持既有新来源预算所需的周期。
func channelModelUsageCounters(channelID int, config dto.ChannelPeriodPolicyConfig, res channelBudgetResolution) []channelBudgetCounter {
	if res.modelName == "" || config.ModelUsageTracking == nil {
		return nil
	}
	enabled := config.ModelUsageTrackingEnabled != nil && *config.ModelUsageTrackingEnabled
	windows := map[string]bool{channelBudgetWindowDaily: enabled, channelBudgetWindowWeekly: enabled}
	if !enabled {
		for _, row := range config.Budgets {
			if row.CounterSource == channelModelCounterSource && (channelBudgetRow{ChannelBudgetRow: row}).matchesModel(res.modelName) {
				windows[row.Window] = true
			}
		}
	}
	var counters []channelBudgetCounter
	for _, window := range []string{channelBudgetWindowDaily, channelBudgetWindowWeekly} {
		if !windows[window] {
			continue
		}
		row := channelBudgetRow{ChannelBudgetRow: dto.ChannelBudgetRow{Window: window, Models: []string{res.modelName}, CreatedAt: config.ModelUsageTracking.FirstEnabledAt}}
		counter := newChannelBudgetCounter(channelID, row, res)
		counter.poolKey = channelModelUsageKey(channelID, window, res.modelName, counter.start)
		counter.userKey = counter.poolKey
		counters = append(counters, counter)
	}
	return counters
}

// 一次读取全部所选模型与调整快照，避免并发结算/调整造成撕裂；仅管理列表使用 HGETALL。
var channelModelUsageReadScript = redis.NewScript(`
local result = {}
for i, key in ipairs(KEYS) do
 if ARGV[1] == 'all' then
  result[i] = redis.call('HGETALL', key)
 else
  local fields = {}
  for j = 2, #ARGV do
   fields[#fields + 1] = ARGV[j]
   fields[#fields + 1] = redis.call('HGET', key, ARGV[j]) or ''
  end
  result[i] = fields
 end
end
return result
`)

// readChannelModelUsageSnapshot 返回同一原子快照下的模型事实和人工目标，供热路径与管理列表共用解释逻辑。
func readChannelModelUsageSnapshot(ctx context.Context, counter channelBudgetCounter, fields []string) ([]map[string]string, map[string]string, error) {
	keys := append(append([]string{}, counter.modelKeys...), counter.poolKey)
	maps := make([]map[string]string, len(keys))
	if common.RedisEnabled {
		if common.RDB == nil {
			return nil, nil, errors.New("周期额度 Redis 未初始化")
		}
		args := []interface{}{"fields"}
		if fields == nil {
			args[0] = "all"
		}
		for _, field := range fields {
			args = append(args, field)
		}
		values, err := channelModelUsageReadScript.Run(ctx, common.RDB, keys, args...).Slice()
		if err != nil {
			return nil, nil, err
		}
		if len(values) != len(keys) {
			return nil, nil, errors.New("模型计数快照长度无效")
		}
		for i, value := range values {
			pairs, ok := value.([]interface{})
			if !ok || len(pairs)%2 != 0 {
				return nil, nil, errors.New("模型计数快照类型无效")
			}
			maps[i] = make(map[string]string, len(pairs)/2)
			for j := 0; j < len(pairs); j += 2 {
				field, ok := pairs[j].(string)
				raw, valid := pairs[j+1].(string)
				if !ok || !valid {
					return nil, nil, errors.New("模型计数快照字段无效")
				}
				if raw != "" {
					maps[i][field] = raw
				}
			}
		}
	} else {
		channelPeriodUsageMemory.Lock()
		defer channelPeriodUsageMemory.Unlock()
		for i, key := range keys {
			maps[i] = make(map[string]string)
			bucket := channelPeriodUsageMemory.values[key]
			if bucket == nil {
				continue
			}
			if fields == nil {
				for field, used := range bucket.values {
					maps[i][field] = strconv.FormatInt(used, 10)
				}
				for field, snapshot := range bucket.adjustments {
					raw, err := common.Marshal(snapshot)
					if err != nil {
						return nil, nil, err
					}
					maps[i][field] = string(raw)
				}
			} else {
				for _, field := range fields {
					if used, exists := bucket.values[field]; exists {
						maps[i][field] = strconv.FormatInt(used, 10)
					}
					if snapshot, exists := bucket.adjustments[field]; exists {
						raw, err := common.Marshal(snapshot)
						if err != nil {
							return nil, nil, err
						}
						maps[i][field] = string(raw)
					}
				}
			}
			maps[i]["__since"] = strconv.FormatInt(bucket.since, 10)
		}
	}
	return maps[:len(maps)-1], maps[len(maps)-1], nil
}

func parseChannelModelUsage(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 || strconv.FormatInt(value, 10) != raw {
		return 0, errors.New("模型用量不是合法非负整数")
	}
	return value, nil
}

// channelModelBudgetValue 使用目标值加快照之后的事实增量，所有求和均检查 int64 上界。
func channelModelBudgetValue(field string, facts []map[string]string, adjustments map[string]string) (int64, error) {
	var snapshot []string
	total := int64(0)
	if raw, exists := adjustments[field]; exists {
		if err := common.UnmarshalJsonStr(raw, &snapshot); err != nil || len(snapshot) != len(facts)+1 {
			return 0, errors.New("模型预算调整快照无效")
		}
		var err error
		if snapshot[0] == "" {
			return 0, errors.New("模型预算调整目标缺失")
		}
		total, err = parseChannelModelUsage(snapshot[0])
		if err != nil {
			return 0, err
		}
	}
	for i, fact := range facts {
		value, err := parseChannelModelUsage(fact[field])
		if err != nil {
			return 0, err
		}
		if snapshot != nil {
			if snapshot[i+1] == "" {
				return 0, errors.New("模型预算调整源值缺失")
			}
			before, err := parseChannelModelUsage(snapshot[i+1])
			if err != nil || value < before {
				return 0, errors.New("模型预算调整源计数回退")
			}
			value -= before
		}
		if total > math.MaxInt64-value {
			return 0, errors.New("模型预算汇总溢出")
		}
		total += value
	}
	return total, nil
}

func readChannelModelBudget(ctx context.Context, counter channelBudgetCounter, userID int, now time.Time) (int64, int64, int64, error) {
	field := strconv.Itoa(userID)
	facts, adjustments, err := readChannelModelUsageSnapshot(ctx, counter, []string{field, "__pool", "__since"})
	if err != nil {
		return 0, 0, 0, err
	}
	user, err := channelModelBudgetValue(field, facts, adjustments)
	if err != nil {
		return 0, 0, 0, err
	}
	pool, err := channelModelBudgetValue("__pool", facts, adjustments)
	if err != nil {
		return 0, 0, 0, err
	}
	since := max(counter.start, counter.createdAt)
	for _, fact := range facts {
		start, err := parseChannelModelUsage(fact["__since"])
		if err != nil {
			return 0, 0, 0, err
		}
		if start == 0 {
			start = channelBudgetTrackingStart(counter, now)
		}
		since = max(since, start)
	}
	return user, pool, since, nil
}

func listChannelModelBudgetUsage(ctx context.Context, counter channelBudgetCounter) (map[int]int64, error) {
	facts, adjustments, err := readChannelModelUsageSnapshot(ctx, counter, nil)
	if err != nil {
		return nil, err
	}
	users := make(map[int]int64)
	for _, fields := range append(facts, adjustments) {
		for field := range fields {
			if strings.HasPrefix(field, "__") {
				continue
			}
			userID, err := strconv.Atoi(field)
			if err != nil || userID <= 0 {
				return nil, errors.New("模型用量用户无效")
			}
			users[userID] = 0
		}
	}
	for userID := range users {
		used, err := channelModelBudgetValue(strconv.Itoa(userID), facts, adjustments)
		if err != nil {
			return nil, err
		}
		if used == 0 {
			delete(users, userID)
		} else {
			users[userID] = used
		}
	}
	return users, nil
}
