package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

// ErrInvalidChannelPeriodPolicy 表示周期策略输入非法。
var ErrInvalidChannelPeriodPolicy = errors.New("渠道周期策略参数无效")

var channelPeriodPolicyCache = struct {
	sync.Mutex
	values map[string]channelPeriodPolicyCacheEntry
}{values: make(map[string]channelPeriodPolicyCacheEntry)}

type channelPeriodPolicyCacheEntry struct {
	data      []byte
	revision  int
	expiresAt time.Time
}

// 发布缓存只接受不低于当前版本的策略；跨实例迟到的写入不能覆盖新配置。
var channelPeriodPolicyPublishScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if current then
  local decoded = cjson.decode(current)
  if type(decoded.revision) ~= 'number' then return redis.error_reply('invalid policy revision') end
  if decoded.revision > tonumber(ARGV[2]) then return current end
end
redis.call('SET', KEYS[1], ARGV[1], 'EX', 5)
return ARGV[1]
`)

// GetChannelPeriodPolicy 读取短缓存中的配置，缺失和读取失败严格区分。
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @return 权威策略视图及错误；无记录时返回版本 0。
func GetChannelPeriodPolicy(ctx context.Context, channelID int) (dto.ChannelPeriodPolicyView, error) {
	view := dto.ChannelPeriodPolicyView{Timezone: time.Local.String(), Now: time.Now().Unix(), Config: defaultChannelBudgetConfig()}
	if channelID <= 0 {
		return view, fmt.Errorf("%w: 渠道无效", ErrInvalidChannelPeriodPolicy)
	}
	key := fmt.Sprintf("channel_period_policy:{%d}", channelID)
	localKey := fmt.Sprintf("%p:%s", model.DB, key)
	var cached []byte
	if common.RedisEnabled {
		if common.RDB == nil {
			return view, errors.New("周期策略 Redis 未初始化")
		}
		var err error
		cached, err = common.RDB.Get(ctx, key).Bytes()
		if err != nil && !errors.Is(err, redis.Nil) {
			return view, err
		}
	} else {
		channelPeriodPolicyCache.Lock()
		if entry, ok := channelPeriodPolicyCache.values[localKey]; ok && time.Now().Before(entry.expiresAt) {
			cached = entry.data
		}
		channelPeriodPolicyCache.Unlock()
	}
	if len(cached) > 0 {
		if err := common.Unmarshal(cached, &view); err != nil {
			return view, err
		}
		if err := validateChannelBudgetStoredPolicy(view); err != nil {
			return view, err
		}
		view.Now, view.Timezone = time.Now().Unix(), time.Local.String()
		return view, nil
	}
	item, err := model.GetChannelPeriodPolicy(ctx, channelID)
	if err != nil {
		return view, err
	}
	raw := `{"schema_version":1}`
	if item != nil {
		view.Revision = item.Revision
		raw = item.Config
	}
	// 从节点先启动或迁移写入失败时，尚无策略记录的渠道也必须继续受旧日/周列约束。
	if view.Config, err = decodeChannelBudgetPolicyConfig(raw, channelID); err != nil {
		return view, err
	}
	if err := validateChannelBudgetStoredPolicy(view); err != nil {
		return view, err
	}
	data, err := common.Marshal(view)
	if err != nil {
		return view, err
	}
	if common.RedisEnabled {
		// 缓存未命中期间可能已有另一实例发布新版，返回脚本选出的当前版本。
		var published string
		published, err = channelPeriodPolicyPublishScript.Run(ctx, common.RDB, []string{key}, data, view.Revision).Text()
		cached = []byte(published)
	} else {
		cached = cacheChannelPeriodPolicy(localKey, data, view.Revision)
	}
	if err == nil {
		err = common.Unmarshal(cached, &view)
		if err == nil {
			err = validateChannelBudgetStoredPolicy(view)
		}
		view.Now, view.Timezone = time.Now().Unix(), time.Local.String()
	}
	return view, err
}

func cacheChannelPeriodPolicy(key string, data []byte, revision int) []byte {
	channelPeriodPolicyCache.Lock()
	defer channelPeriodPolicyCache.Unlock()
	// 数据库查询在锁外执行，迟到的读取和保存都不能把本地缓存降回旧版本。
	if current, ok := channelPeriodPolicyCache.values[key]; ok && current.revision > revision {
		return current.data
	}
	now := time.Now()
	for k, entry := range channelPeriodPolicyCache.values {
		if !now.Before(entry.expiresAt) {
			delete(channelPeriodPolicyCache.values, k)
		}
	}
	if len(channelPeriodPolicyCache.values) >= 4096 {
		for k := range channelPeriodPolicyCache.values {
			delete(channelPeriodPolicyCache.values, k)
			break
		}
	}
	channelPeriodPolicyCache.values[key] = channelPeriodPolicyCacheEntry{data: data, revision: revision, expiresAt: now.Add(5 * time.Second)}
	return data
}

// SaveChannelPeriodPolicy 校验并按版本保存策略，返回保存后的权威视图。
// @param ctx 请求上下文。
// @param channelID 来源渠道 ID。
// @param input 配置及预期版本。
// @param updatedBy 管理员 ID。
// @return 保存结果及错误；返回正版本时可能已保存但缓存刷新失败。
func SaveChannelPeriodPolicy(ctx context.Context, channelID int, input dto.ChannelPeriodPolicyInput, updatedBy int) (dto.ChannelPeriodPolicyView, error) {
	var result dto.ChannelPeriodPolicyView
	old, err := GetChannelPeriodPolicy(ctx, channelID)
	if err != nil {
		return result, err
	}
	if old.Revision != input.ExpectedRevision {
		return result, model.ErrChannelPeriodPolicyConflict
	}
	// 指向本渠道的行级降级归一为同渠道换模型；策略级仍禁止自指。
	config, err := NormalizeChannelBudgetConfig(channelBudgetSameChannelToZero(input.Config, channelID), old.Config, channelPeriodNow().In(time.Local))
	if err != nil {
		return result, err
	}
	targets := []dto.ChannelBudgetAction{config.DefaultOnExceed}
	for _, row := range config.Budgets {
		targets = append(targets, row.OnExceed)
	}
	for i, target := range targets {
		if target.Mode != channelBudgetActionFallback || target.ChannelID == 0 {
			continue
		}
		if i == 0 && target.ChannelID == channelID {
			return result, fmt.Errorf("%w: 降级不能指向自身", ErrInvalidChannelPeriodPolicy)
		}
		candidate, targetErr := model.GetChannelById(target.ChannelID, false)
		if targetErr != nil || candidate == nil {
			return result, fmt.Errorf("%w: 降级目标不存在", ErrInvalidChannelPeriodPolicy)
		}
	}
	data, err := common.Marshal(config)
	if err != nil {
		return result, err
	}
	item := &model.ChannelPeriodPolicy{ChannelId: channelID, Revision: input.ExpectedRevision, Config: string(data), UpdatedBy: updatedBy}
	if err = model.ReplaceChannelPeriodPolicy(ctx, item); err != nil {
		return result, err
	}
	result = dto.ChannelPeriodPolicyView{Revision: item.Revision, Config: config, Timezone: time.Local.String(), Now: time.Now().Unix()}
	data, err = common.Marshal(result)
	if err != nil {
		return result, err
	}
	key := fmt.Sprintf("channel_period_policy:{%d}", channelID)
	if common.RedisEnabled {
		if common.RDB == nil {
			return result, errors.New("策略已保存，但缓存刷新失败")
		}
		err = channelPeriodPolicyPublishScript.Run(ctx, common.RDB, []string{key}, data, result.Revision).Err()
	} else {
		cacheChannelPeriodPolicy(fmt.Sprintf("%p:%s", model.DB, key), data, result.Revision)
	}
	return result, err
}

// decodeChannelBudgetPolicyConfig 解析已存储的策略正文；遇到 v1 形状时按渠道列兜底转换，不改写存储。
func decodeChannelBudgetPolicyConfig(raw string, channelID int) (dto.ChannelPeriodPolicyConfig, error) {
	var probe struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := common.UnmarshalJsonStr(raw, &probe); err != nil {
		return dto.ChannelPeriodPolicyConfig{}, err
	}
	if probe.SchemaVersion == channelBudgetSchemaVersion {
		var config dto.ChannelPeriodPolicyConfig
		err := common.UnmarshalJsonStr(raw, &config)
		return config, err
	}
	if probe.SchemaVersion != 1 {
		return dto.ChannelPeriodPolicyConfig{}, fmt.Errorf("不支持的已存储预算策略版本: %d", probe.SchemaVersion)
	}
	var v1 dto.ChannelPeriodPolicyConfigV1
	if err := common.UnmarshalJsonStr(raw, &v1); err != nil {
		return dto.ChannelPeriodPolicyConfig{}, err
	}
	channel, err := model.GetChannelById(channelID, false)
	if err != nil {
		return dto.ChannelPeriodPolicyConfig{}, err
	}
	var userDaily, userWeekly int64
	if channel.UserDailyQuotaLimit != nil && *channel.UserDailyQuotaLimit > 0 {
		userDaily = int64(*channel.UserDailyQuotaLimit)
	}
	if channel.UserWeeklyQuotaLimit != nil && *channel.UserWeeklyQuotaLimit > 0 {
		userWeekly = int64(*channel.UserWeeklyQuotaLimit)
	}
	return convertChannelBudgetPolicyV1(v1, userDaily, userWeekly, channelPeriodNow().Unix()), nil
}

// invalidateChannelPeriodPolicyCache 让本地与 Redis 的策略缓存失效，下一次读取回源数据库。
func invalidateChannelPeriodPolicyCache(ctx context.Context, channelID int) {
	key := fmt.Sprintf("channel_period_policy:{%d}", channelID)
	channelPeriodPolicyCache.Lock()
	delete(channelPeriodPolicyCache.values, fmt.Sprintf("%p:%s", model.DB, key))
	channelPeriodPolicyCache.Unlock()
	if common.RedisEnabled && common.RDB != nil {
		_ = common.RDB.Del(ctx, key).Err()
	}
}
