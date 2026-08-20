package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const (
	channelUserLimitOverrideCacheTTL        = 30 * time.Second
	channelUserLimitOverrideCacheMaxEntries = 4096
	maxChannelUserOverrideConcurrency       = 1000
)

// ErrInvalidChannelUserLimitOverride 表示管理员提交的个人限制覆盖不符合业务约束。
var ErrInvalidChannelUserLimitOverride = errors.New("个人限制覆盖参数无效")

var channelUserLimitOverrideMemoryCache = struct {
	sync.Mutex
	values     map[string]channelUserLimitOverrideCacheEntry
	maxEntries int
}{
	values:     make(map[string]channelUserLimitOverrideCacheEntry),
	maxEntries: channelUserLimitOverrideCacheMaxEntries,
}

type channelUserLimitOverrideCacheValue struct {
	Exists   bool                           `json:"exists"`
	Override model.ChannelUserLimitOverride `json:"override"`
}

type channelUserLimitOverrideCacheEntry struct {
	value     channelUserLimitOverrideCacheValue
	expiresAt time.Time
}

// ChannelUserEffectiveLimits 描述渠道默认限制、个人覆盖和最终生效限制。
type ChannelUserEffectiveLimits struct {
	BaseConcurrency      int   `json:"base_concurrency"`
	BaseDailyQuota       int   `json:"base_daily_quota"`
	BaseWeeklyQuota      int   `json:"base_weekly_quota"`
	OverrideConcurrency  *int  `json:"override_concurrency,omitempty"`
	OverrideDailyQuota   *int  `json:"override_daily_quota,omitempty"`
	OverrideWeeklyQuota  *int  `json:"override_weekly_quota,omitempty"`
	EffectiveConcurrency int   `json:"effective_concurrency"`
	EffectiveDailyQuota  int   `json:"effective_daily_quota"`
	EffectiveWeeklyQuota int   `json:"effective_weekly_quota"`
	ExpiresAt            int64 `json:"expires_at"`
	Active               bool  `json:"active"`
}

// ChannelUserLimitOverrideInput 描述管理员提交的整条个人覆盖。
type ChannelUserLimitOverrideInput struct {
	UserConcurrencyLimit *int  `json:"user_concurrency_limit"`
	UserDailyQuotaLimit  *int  `json:"user_daily_quota_limit"`
	UserWeeklyQuotaLimit *int  `json:"user_weekly_quota_limit"`
	ExpiresAt            int64 `json:"expires_at"`
}

// ResolveChannelUserEffectiveLimits 解析指定渠道和用户当前实际生效的限制。
//
// @param ctx 请求上下文。
// @param channel 渠道模型。
// @param userID 用户 ID。
// @return ChannelUserEffectiveLimits 默认、覆盖与有效限制。
// @return error 缓存和数据库读取失败时返回错误；返回值仍包含渠道默认限制。
func ResolveChannelUserEffectiveLimits(ctx context.Context, channel *model.Channel, userID int) (ChannelUserEffectiveLimits, error) {
	limits := newBaseChannelUserEffectiveLimits(channel)
	if channel == nil || channel.Id <= 0 || userID <= 0 {
		return limits, nil
	}
	override, err := getCachedChannelUserLimitOverride(ctx, channel.Id, userID)
	if err != nil || override == nil {
		return limits, err
	}
	limits.Active = true
	limits.ExpiresAt = override.ExpiresAt
	limits.OverrideConcurrency = override.UserConcurrencyLimit
	limits.OverrideDailyQuota = override.UserDailyQuotaLimit
	limits.OverrideWeeklyQuota = override.UserWeeklyQuotaLimit
	limits.EffectiveConcurrency = effectiveChannelUserLimit(limits.BaseConcurrency, override.UserConcurrencyLimit)
	limits.EffectiveDailyQuota = effectiveChannelUserLimit(limits.BaseDailyQuota, override.UserDailyQuotaLimit)
	limits.EffectiveWeeklyQuota = effectiveChannelUserLimit(limits.BaseWeeklyQuota, override.UserWeeklyQuotaLimit)
	return limits, nil
}

// ApplyChannelUserEffectiveLimits 解析并把当前用户的有效限制写入 Gin 上下文。
//
// @param c Gin 请求上下文。
// @param channel 已选定的渠道。
// @return ChannelUserEffectiveLimits 本次请求使用的限制快照。
func ApplyChannelUserEffectiveLimits(c *gin.Context, channel *model.Channel) ChannelUserEffectiveLimits {
	userID := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	requestContext := context.Background()
	if c.Request != nil {
		requestContext = c.Request.Context()
	}
	limits, err := ResolveChannelUserEffectiveLimits(requestContext, channel, userID)
	if err != nil {
		logger.LogWarn(requestContext, fmt.Sprintf(
			"解析渠道用户个人限制失败，回落渠道默认值: channel_id=%d user_id=%d error=%s",
			channel.Id,
			userID,
			common.LocalLogPreview(err.Error()),
		))
	}
	common.SetContextKey(c, constant.ContextKeyChannelUserConcurrencyLimit, limits.EffectiveConcurrency)
	common.SetContextKey(c, constant.ContextKeyChannelUserDailyQuotaLimit, limits.EffectiveDailyQuota)
	common.SetContextKey(c, constant.ContextKeyChannelUserWeeklyQuotaLimit, limits.EffectiveWeeklyQuota)
	return limits
}

// ReplaceChannelUserLimitOverride 校验并整条替换指定用户的个人覆盖。
//
// @param ctx 请求上下文。
// @param channel 渠道模型。
// @param userID 用户 ID。
// @param input 覆盖输入。
// @param updatedBy 操作管理员用户 ID。
// @return error 输入非法或数据库写入失败时返回错误。
func ReplaceChannelUserLimitOverride(ctx context.Context, channel *model.Channel, userID int, input ChannelUserLimitOverrideInput, updatedBy int) error {
	if channel == nil || channel.Id <= 0 || userID <= 0 || updatedBy <= 0 {
		return fmt.Errorf("%w: 渠道、用户或操作管理员无效", ErrInvalidChannelUserLimitOverride)
	}
	if input.UserConcurrencyLimit == nil && input.UserDailyQuotaLimit == nil && input.UserWeeklyQuotaLimit == nil {
		if err := model.DeleteChannelUserLimitOverride(channel.Id, userID); err != nil {
			return err
		}
		invalidateChannelUserLimitOverrideCache(ctx, channel.Id, userID)
		return nil
	}
	if input.ExpiresAt < 0 || (input.ExpiresAt > 0 && input.ExpiresAt <= time.Now().Unix()) {
		return fmt.Errorf("%w: 个人覆盖到期时间必须为未来时间", ErrInvalidChannelUserLimitOverride)
	}
	if err := validateChannelUserLimitOverrideValue("并发", channel.GetUserConcurrencyLimit(), input.UserConcurrencyLimit, maxChannelUserOverrideConcurrency); err != nil {
		return err
	}
	if err := validateChannelUserLimitOverrideValue("每日额度", channel.GetUserDailyQuotaLimit(), input.UserDailyQuotaLimit, common.MaxQuota); err != nil {
		return err
	}
	if err := validateChannelUserLimitOverrideValue("每周额度", channel.GetUserWeeklyQuotaLimit(), input.UserWeeklyQuotaLimit, common.MaxQuota); err != nil {
		return err
	}
	override := &model.ChannelUserLimitOverride{
		ChannelId:            channel.Id,
		UserId:               userID,
		UserConcurrencyLimit: input.UserConcurrencyLimit,
		UserDailyQuotaLimit:  input.UserDailyQuotaLimit,
		UserWeeklyQuotaLimit: input.UserWeeklyQuotaLimit,
		ExpiresAt:            input.ExpiresAt,
		UpdatedBy:            updatedBy,
	}
	if err := model.ReplaceChannelUserLimitOverride(override); err != nil {
		return err
	}
	invalidateChannelUserLimitOverrideCache(ctx, channel.Id, userID)
	return nil
}

// DeleteChannelUserLimitOverride 删除指定用户的个人覆盖并立即失效缓存。
//
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param userID 用户 ID。
// @return error 数据库删除失败时返回错误。
func DeleteChannelUserLimitOverride(ctx context.Context, channelID int, userID int) error {
	if err := model.DeleteChannelUserLimitOverride(channelID, userID); err != nil {
		return err
	}
	invalidateChannelUserLimitOverrideCache(ctx, channelID, userID)
	return nil
}

func newBaseChannelUserEffectiveLimits(channel *model.Channel) ChannelUserEffectiveLimits {
	limits := ChannelUserEffectiveLimits{}
	if channel == nil {
		return limits
	}
	limits.BaseConcurrency = channel.GetUserConcurrencyLimit()
	limits.BaseDailyQuota = channel.GetUserDailyQuotaLimit()
	limits.BaseWeeklyQuota = channel.GetUserWeeklyQuotaLimit()
	limits.EffectiveConcurrency = limits.BaseConcurrency
	limits.EffectiveDailyQuota = limits.BaseDailyQuota
	limits.EffectiveWeeklyQuota = limits.BaseWeeklyQuota
	return limits
}

func effectiveChannelUserLimit(base int, override *int) int {
	if base <= 0 || override == nil || *override <= base {
		return base
	}
	return *override
}

func validateChannelUserLimitOverrideValue(name string, base int, override *int, maximum int) error {
	if override == nil {
		return nil
	}
	if base <= 0 {
		return fmt.Errorf("%w: 渠道%s不限时不能设置个人提额", ErrInvalidChannelUserLimitOverride, name)
	}
	if *override <= base || *override > maximum {
		return fmt.Errorf("%w: 个人%s必须大于渠道默认值且不超过 %d", ErrInvalidChannelUserLimitOverride, name, maximum)
	}
	return nil
}

func getCachedChannelUserLimitOverride(ctx context.Context, channelID int, userID int) (*model.ChannelUserLimitOverride, error) {
	key := fmt.Sprintf("channel_user_limit_override:{%d}:{%d}", channelID, userID)
	if common.RedisEnabled && common.RDB != nil {
		value, err := common.RDB.Get(ctx, key).Bytes()
		if err == nil {
			var cached channelUserLimitOverrideCacheValue
			if common.Unmarshal(value, &cached) == nil {
				if !cached.Exists {
					return nil, nil
				}
				return &cached.Override, nil
			}
		} else if !errors.Is(err, redis.Nil) {
			// Redis 读取失败时回源数据库，个人覆盖只放宽限制，故障回落更保守。
		}
	} else if !common.RedisEnabled {
		channelUserLimitOverrideMemoryCache.Lock()
		entry, ok := channelUserLimitOverrideMemoryCache.values[key]
		if ok && time.Now().Before(entry.expiresAt) {
			channelUserLimitOverrideMemoryCache.Unlock()
			if !entry.value.Exists {
				return nil, nil
			}
			result := entry.value.Override
			return &result, nil
		}
		delete(channelUserLimitOverrideMemoryCache.values, key)
		channelUserLimitOverrideMemoryCache.Unlock()
	}
	override, err := model.GetActiveChannelUserLimitOverride(channelID, userID, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	cached := channelUserLimitOverrideCacheValue{Exists: override != nil}
	if override != nil {
		cached.Override = *override
	}
	cacheChannelUserLimitOverride(ctx, key, cached)
	return override, nil
}

func cacheChannelUserLimitOverride(ctx context.Context, key string, value channelUserLimitOverrideCacheValue) {
	ttl := channelUserLimitOverrideCacheTTL
	if value.Exists && value.Override.ExpiresAt > 0 {
		untilExpiry := time.Until(time.Unix(value.Override.ExpiresAt, 0))
		if untilExpiry <= 0 {
			return
		}
		if untilExpiry < ttl {
			ttl = untilExpiry
		}
	}
	if common.RedisEnabled && common.RDB != nil {
		data, err := common.Marshal(value)
		if err == nil {
			_ = common.RDB.Set(ctx, key, data, ttl).Err()
		}
		return
	}
	if !common.RedisEnabled {
		channelUserLimitOverrideMemoryCache.Lock()
		now := time.Now()
		for cachedKey, entry := range channelUserLimitOverrideMemoryCache.values {
			if !now.Before(entry.expiresAt) {
				delete(channelUserLimitOverrideMemoryCache.values, cachedKey)
			}
		}
		if _, exists := channelUserLimitOverrideMemoryCache.values[key]; !exists &&
			len(channelUserLimitOverrideMemoryCache.values) >= channelUserLimitOverrideMemoryCache.maxEntries {
			// 覆盖缓存只有 30 秒寿命，任意淘汰即可限制内存，无需引入额外 LRU 状态。
			for cachedKey := range channelUserLimitOverrideMemoryCache.values {
				delete(channelUserLimitOverrideMemoryCache.values, cachedKey)
				break
			}
		}
		channelUserLimitOverrideMemoryCache.values[key] = channelUserLimitOverrideCacheEntry{
			value:     value,
			expiresAt: now.Add(ttl),
		}
		channelUserLimitOverrideMemoryCache.Unlock()
	}
}

func invalidateChannelUserLimitOverrideCache(ctx context.Context, channelID int, userID int) {
	key := fmt.Sprintf("channel_user_limit_override:{%d}:{%d}", channelID, userID)
	channelUserLimitOverrideMemoryCache.Lock()
	delete(channelUserLimitOverrideMemoryCache.values, key)
	channelUserLimitOverrideMemoryCache.Unlock()
	if common.RedisEnabled && common.RDB != nil {
		_ = common.RDB.Del(ctx, key).Err()
	}
}
