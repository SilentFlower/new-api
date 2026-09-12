package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
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
	view := dto.ChannelPeriodPolicyView{Timezone: time.Local.String(), Now: time.Now().Unix(), Config: dto.ChannelPeriodPolicyConfig{SchemaVersion: 1, Rules: []dto.ChannelPeriodRule{}}}
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
		if err := validateChannelPeriodStoredPolicy(view); err != nil {
			return view, err
		}
		view.Now, view.Timezone = time.Now().Unix(), time.Local.String()
		return view, nil
	}
	item, err := model.GetChannelPeriodPolicy(ctx, channelID)
	if err != nil {
		return view, err
	}
	if item != nil {
		view.Revision = item.Revision
		if err = common.UnmarshalJsonStr(item.Config, &view.Config); err != nil {
			return view, err
		}
		if err := validateChannelPeriodStoredPolicy(view); err != nil {
			return view, err
		}
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
			err = validateChannelPeriodStoredPolicy(view)
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
	config, err := NormalizeChannelPeriodConfig(input.Config, old.Config, channelPeriodNow().In(time.Local))
	if err != nil {
		return result, err
	}
	if config.Fallback.Enabled {
		if config.Fallback.ChannelID == channelID {
			return result, fmt.Errorf("%w: 降级不能指向自身", ErrInvalidChannelPeriodPolicy)
		}
		target, targetErr := model.GetChannelById(config.Fallback.ChannelID, false)
		if targetErr != nil || target == nil {
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

// ReplaceChannelUserPeriodOverride 校验并保存指定规则的个人整段提额。
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param userID 用户 ID。
// @param ruleID 稳定规则 ID。
// @param input 额度和到期时间。
// @param updatedBy 管理员 ID。
// @return 校验或持久化错误。
func ReplaceChannelUserPeriodOverride(ctx context.Context, channelID, userID int, ruleID string, input dto.ChannelUserPeriodOverrideInput, updatedBy int) error {
	view, err := GetChannelPeriodPolicy(ctx, channelID)
	if err != nil {
		return err
	}
	if input.ExpiresAt < 0 || input.ExpiresAt > 0 && input.ExpiresAt <= time.Now().Unix() {
		return fmt.Errorf("%w: 特批到期时间须在未来", ErrInvalidChannelPeriodPolicy)
	}
	if userID <= 0 || updatedBy <= 0 {
		return fmt.Errorf("%w: 用户或管理员无效", ErrInvalidChannelPeriodPolicy)
	}
	for _, rule := range view.Config.Rules {
		if rule.ID != ruleID {
			continue
		}
		if rule.UserPeriodQuotaLimit == nil || *rule.UserPeriodQuotaLimit <= 0 || input.UserPeriodQuotaLimit <= *rule.UserPeriodQuotaLimit || input.UserPeriodQuotaLimit > common.MaxPeriodQuota {
			return fmt.Errorf("%w: 整段特批须高于规则基础额度且不超过最大值", ErrInvalidChannelPeriodPolicy)
		}
		return model.ReplaceChannelUserPeriodOverride(ctx, &model.ChannelUserPeriodOverride{ChannelId: channelID, UserId: userID, RuleId: ruleID, UserPeriodQuotaLimit: input.UserPeriodQuotaLimit, ExpiresAt: input.ExpiresAt, UpdatedBy: updatedBy})
	}
	return fmt.Errorf("%w: 规则不存在", ErrInvalidChannelPeriodPolicy)
}

// PreviewChannelPeriodPolicy 解析服务端时间、生效指标与下一切换点，不写配置。
// @param ctx 请求上下文。
// @param channel 渠道配置。
// @param config 待预览的配置。
// @param now 预览时刻。
// @return 配置及生效结果，或校验错误。
func PreviewChannelPeriodPolicy(ctx context.Context, channel *model.Channel, config dto.ChannelPeriodPolicyConfig, now time.Time) (map[string]interface{}, error) {
	previous, err := GetChannelPeriodPolicy(ctx, channel.Id)
	if err != nil {
		return nil, err
	}
	normalized, err := NormalizeChannelPeriodConfig(config, previous.Config, now)
	if err != nil {
		return nil, err
	}
	limits, sources, _, next, err := resolveChannelPeriodSources(normalized, int64(channel.GetUserDailyQuotaLimit()), now)
	if err != nil {
		return nil, err
	}
	// 预览不会建立规则身份；保留新规则的空 ID，避免预览结果无法随后保存。
	for i := range config.Rules {
		if config.Rules[i].ID != "" {
			continue
		}
		previewID := normalized.Rules[i].ID
		normalized.Rules[i].ID = ""
		for key, source := range sources {
			if source.RuleID == previewID {
				source.RuleID = ""
				sources[key] = source
			}
		}
	}
	return map[string]interface{}{"config": normalized, "revision": previous.Revision, "timezone": time.Local.String(), "now": now.Unix(), "limits": limits, "sources": sources, "next_change_at": next}, nil
}

func validateChannelPeriodStoredPolicy(view dto.ChannelPeriodPolicyView) error {
	if view.Revision < 0 || (view.Revision == 0 && (view.Config.PoolDailyQuotaLimit != 0 || view.Config.PoolWeeklyQuotaLimit != 0 || len(view.Config.Rules) > 0 || view.Config.Fallback.Enabled)) {
		return errors.New("周期策略版本无效")
	}
	for _, rule := range view.Config.Rules {
		if len(rule.ID) != 32 || rule.CreatedAt <= 0 {
			return errors.New("已存储的规则身份或统计起点无效")
		}
	}
	normalized, err := NormalizeChannelPeriodConfig(view.Config, view.Config, channelPeriodNow().In(time.Local))
	if err != nil || !reflect.DeepEqual(normalized, view.Config) {
		return errors.New("已存储的周期策略无效")
	}
	return nil
}
