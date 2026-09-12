package service

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// ChannelPeriodBlock 描述导致拒绝的一项有效金额限制。
type ChannelPeriodBlock struct {
	Metric dto.ChannelPeriodMetric `json:"metric"`
}

// Error 返回安全的领域描述。
// @return 不包含存储或凭据的错误文本。
func (block *ChannelPeriodBlock) Error() string { return "渠道周期额度已达到上限" }

// GetChannelPeriodStatus 统一解析规则优先级、个人覆盖和周期用量。
// @param ctx 请求上下文。
// @param channel 渠道基础配置。
// @param userID 当前用户。
// @return 完整状态及错误。
func GetChannelPeriodStatus(ctx context.Context, channel *model.Channel, userID int) (dto.ChannelPeriodStatus, error) {
	status := dto.ChannelPeriodStatus{SchemaVersion: 1, Timezone: time.Local.String(), StorageMode: channelUserDailyQuotaStorageMode(), Metrics: []dto.ChannelPeriodMetric{}}
	if channel == nil || channel.Id <= 0 || userID <= 0 {
		return status, errors.New("周期额度用户或渠道无效")
	}
	policy, err := GetChannelPeriodPolicy(ctx, channel.Id)
	if err != nil {
		return status, err
	}
	gap, err := getChannelQuotaGap(ctx, channel.Id)
	if err != nil {
		return status, err
	}
	now := channelPeriodNow().In(time.Local)
	limits, sources, active, next, err := resolveChannelPeriodSources(policy.Config, int64(channel.GetUserDailyQuotaLimit()), now)
	if err != nil {
		return status, err
	}
	status.Revision, status.NextChangeAt, status.FallbackEnabled = policy.Revision, next, policy.Config.Fallback.Enabled
	baseDaily, baseWeekly := limits["user_daily"], int64(channel.GetUserWeeklyQuotaLimit())
	limits["user_weekly"] = baseWeekly
	sources["user_weekly"] = dto.ChannelPeriodSource{Kind: "default"}
	var override *model.ChannelUserLimitOverride
	if policy.Revision > 0 {
		// 个人明确覆盖可能比临时规则更严格，故障时不能静默回落到较宽的基础额度。
		override, err = model.GetActiveChannelUserLimitOverrideStrict(ctx, channel.Id, userID, channelPeriodNow().Unix())
	} else {
		override, err = getCachedChannelUserLimitOverride(ctx, channel.Id, userID)
	}
	if err != nil {
		return status, err
	}
	if override != nil && (override.ExpiresAt == 0 || override.ExpiresAt > now.Unix()) {
		for key, value := range map[string]*int{"user_daily": override.UserDailyQuotaLimit, "user_weekly": override.UserWeeklyQuotaLimit} {
			if value == nil || *value <= 0 || *value > common.MaxQuota {
				continue
			}
			// revision 为 0 时基础额度来自渠道的 32 位列，转回 int 比较无损。
			if policy.Revision == 0 && int64(effectiveChannelUserLimit(int(limits[key]), value)) == limits[key] {
				continue
			}
			limits[key] = int64(*value)
			sources[key] = dto.ChannelPeriodSource{Kind: "personal", ExpiresAt: override.ExpiresAt}
		}
		if override.ExpiresAt > now.Unix() && (status.NextChangeAt == 0 || override.ExpiresAt < status.NextChangeAt) {
			status.NextChangeAt = override.ExpiresAt
		}
	}
	dailyStore, err := currentChannelUserDailyQuotaStore()
	if err != nil {
		return status, err
	}
	weeklyStore, err := currentChannelUserWeeklyQuotaStore()
	if err != nil {
		return status, err
	}
	// 同一状态和同一次结算都使用单个时间快照，跨午夜时不混用两天的计数。
	dailyPeriod, weeklyPeriod := channelUserDailyQuotaPeriodAt(channel.Id, now), channelUserWeeklyQuotaPeriodAt(channel.Id, now)
	daily, err := dailyStore.get(ctx, dailyPeriod, userID)
	if err != nil {
		return status, err
	}
	weekly, err := weeklyStore.get(ctx, weeklyPeriod, userID)
	if err != nil {
		return status, err
	}
	dailyReset, weeklyReset := dailyPeriod.resetAt.Unix(), weeklyPeriod.resetAt.Unix()
	status.Metrics = append(status.Metrics,
		dto.ChannelPeriodMetric{Scope: "user", Period: "daily", Limit: limits["user_daily"], BaseLimit: baseDaily, Used: daily, ResetAt: dailyReset, Source: sources["user_daily"], Coverage: "existing", Enforced: true},
		dto.ChannelPeriodMetric{Scope: "user", Period: "weekly", Limit: limits["user_weekly"], BaseLimit: baseWeekly, Used: weekly, ResetAt: weeklyReset, Source: sources["user_weekly"], Coverage: "existing", Enforced: true},
	)
	var periodOverrides []model.ChannelUserPeriodOverride
	if len(active) > 0 {
		periodOverrides, err = model.ListChannelUserPeriodOverrides(ctx, channel.Id, userID, now.Unix())
		if err != nil {
			return status, err
		}
	}
	for i, counter := range channelPeriodCounters(channel.Id, now, active) {
		userUsed, poolUsed, since, readErr := getChannelPeriodUsage(ctx, counter, userID, now)
		if readErr != nil {
			return status, readErr
		}
		coverage := "since_tracking_start"
		if gap >= since && gap < counter.end {
			coverage = "incomplete"
		}
		if i < 2 {
			period := "daily"
			if i == 1 {
				period = "weekly"
			}
			key := "pool_" + period
			status.Metrics = append(status.Metrics, dto.ChannelPeriodMetric{Scope: "pool", Period: period, Limit: limits[key], BaseLimit: limits[key], Used: poolUsed, ResetAt: counter.end, Source: sources[key], TrackingSince: since, Coverage: coverage, Enforced: true})
			continue
		}
		occ := active[i-2]
		for _, scope := range []string{"user", "pool"} {
			key := scope + "_custom"
			value, used := occ.rule.UserPeriodQuotaLimit, userUsed
			if scope == "pool" {
				value, used = occ.rule.PoolPeriodQuotaLimit, poolUsed
			}
			limit := int64(0)
			if value != nil {
				limit = *value
			}
			metric := dto.ChannelPeriodMetric{Scope: scope, Period: "custom", Limit: limit, BaseLimit: limit, Used: used, ResetAt: counter.end, TrackingSince: since, Coverage: coverage, Enforced: sources[key].RuleID == occ.rule.ID, Source: dto.ChannelPeriodSource{Kind: occ.rule.Kind, RuleID: occ.rule.ID, RuleName: occ.rule.Name, StartAt: occ.start.Unix(), EndAt: occ.end.Unix()}}
			if scope == "user" {
				for _, personal := range periodOverrides {
					if personal.RuleId != occ.rule.ID || personal.UserPeriodQuotaLimit <= 0 || personal.UserPeriodQuotaLimit > common.MaxPeriodQuota {
						continue
					}
					metric.Limit = personal.UserPeriodQuotaLimit
					metric.OverrideLimit = &personal.UserPeriodQuotaLimit
					metric.Source.Kind, metric.Source.ExpiresAt = "personal", personal.ExpiresAt
					if personal.ExpiresAt > now.Unix() && (status.NextChangeAt == 0 || personal.ExpiresAt < status.NextChangeAt) {
						status.NextChangeAt = personal.ExpiresAt
					}
				}
			}
			status.Metrics = append(status.Metrics, metric)
		}
	}
	for i := range status.Metrics {
		metric := &status.Metrics[i]
		if metric.Scope == "user" && metric.Period != "custom" {
			start := dailyPeriod.resetAt.AddDate(0, 0, -1).Unix()
			if metric.Period == "weekly" {
				start = weeklyPeriod.resetAt.AddDate(0, 0, -7).Unix()
			}
			if gap >= start && gap < metric.ResetAt {
				metric.Coverage = "incomplete"
			}
		}
		if metric.Limit > 0 {
			remaining := max(int64(0), metric.Limit-metric.Used)
			metric.Remaining = &remaining
			if metric.Enforced && metric.Used >= metric.Limit {
				status.Blocked = true
			}
		}
	}
	return status, nil
}

// CheckChannelPeriodLimits 检查完整有效限制，保持软上限、不预占。
// @param ctx 请求上下文。
// @param channel 渠道配置。
// @param userID 用户 ID。
// @return 额度耗尽返回 ChannelPeriodBlock；存储故障返回普通错误。
func CheckChannelPeriodLimits(ctx context.Context, channel *model.Channel, userID int) error {
	status, err := GetChannelPeriodStatus(ctx, channel, userID)
	if err != nil {
		return err
	}
	for _, metric := range status.Metrics {
		if metric.Enforced && metric.Limit > 0 && metric.Used >= metric.Limit {
			return &ChannelPeriodBlock{Metric: metric}
		}
	}
	return nil
}

// ChannelPeriodAPIError 把领域错误转换为稳定 Relay 错误，保留旧个人日周错误码。
// @param err 检查错误。
// @return 带禁止普通重试标记的 429 或 503 错误。
func ChannelPeriodAPIError(err error) *types.NewAPIError {
	if err == nil {
		return nil
	}
	code, status := types.ErrorCodeChannelPeriodQuotaUnavailable, http.StatusServiceUnavailable
	message := "channel period quota service unavailable"
	var block *ChannelPeriodBlock
	if errors.As(err, &block) {
		code, status, message = types.ErrorCodeChannelPeriodQuotaExceeded, http.StatusTooManyRequests, "channel period quota limit exceeded"
		if block.Metric.Scope == "user" && block.Metric.Period == "daily" {
			code = types.ErrorCodeChannelUserDailyQuotaExceeded
		}
		if block.Metric.Scope == "user" && block.Metric.Period == "weekly" {
			code = types.ErrorCodeChannelUserWeeklyQuotaExceeded
		}
	}
	return types.NewOpenAIError(errors.New(message), code, status, types.ErrOptionWithSkipRetry())
}

// CheckSelectedChannelPeriodLimits 在既有限额入口检查所选渠道策略。
// @param c 已完成选渠的 Gin 上下文。
// @return 本地限额或存储错误，不访问供应商。
func CheckSelectedChannelPeriodLimits(c *gin.Context) *types.NewAPIError {
	channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	userID := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	policy, err := GetChannelPeriodPolicy(c, channelID)
	if err != nil {
		return ChannelPeriodAPIError(err)
	}
	// 无新配置时由既有个人日周入口执行，保持旧错误语义及不限时快速返回。
	if policy.Revision == 0 {
		return nil
	}
	channel, ok := c.Get("channel_period_base_channel")
	base, valid := channel.(*model.Channel)
	if !ok || !valid || base.Id != channelID {
		base, err = model.CacheGetChannel(channelID)
		if err != nil {
			return ChannelPeriodAPIError(err)
		}
	}
	status, err := GetChannelPeriodStatus(c, base, userID)
	if err != nil {
		return ChannelPeriodAPIError(err)
	}
	// gin 的 GetInt 只识别 int：若写入 int64，旧个人日周检查会读到 0 并误判为不限。
	for _, metric := range status.Metrics {
		if metric.Scope == "user" && metric.Period == "daily" {
			common.SetContextKey(c, constant.ContextKeyChannelUserDailyQuotaLimit, int(metric.Limit))
		}
		if metric.Scope == "user" && metric.Period == "weekly" {
			common.SetContextKey(c, constant.ContextKeyChannelUserWeeklyQuotaLimit, int(metric.Limit))
		}
	}
	for _, metric := range status.Metrics {
		if metric.Enforced && metric.Limit > 0 && metric.Used >= metric.Limit {
			block := &ChannelPeriodBlock{Metric: metric}
			c.Set("channel_period_block", block)
			return ChannelPeriodAPIError(block)
		}
	}
	return nil
}
