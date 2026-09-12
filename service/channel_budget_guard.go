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
	row    channelBudgetRow
}

// Error 返回安全的领域描述。
// @return 不包含存储或凭据的错误文本。
func (block *ChannelPeriodBlock) Error() string { return "渠道周期额度已达到上限" }

// channelBudgetOverride 是用户对某行的个人覆盖。
type channelBudgetOverride struct {
	limit     int64
	expiresAt int64
}

// GetChannelPeriodStatus 统一解析规则优先级、个人覆盖和周期用量。
// @param ctx 请求上下文。
// @param channel 渠道基础配置。
// @param userID 当前用户。
// @return 完整状态及错误。
func GetChannelPeriodStatus(ctx context.Context, channel *model.Channel, userID int) (dto.ChannelPeriodStatus, error) {
	status, _, err := evaluateChannelBudgets(ctx, channel, userID, "")
	return status, err
}

// evaluateChannelBudgets 按预算行解析当前用户的全部指标。
// @param ctx 请求上下文。
// @param channel 渠道基础配置。
// @param userID 当前用户。
// @param modelName 请求原始模型名，空表示展示全部行。
// @return 状态、与指标一一对应的预算行，以及错误。
func evaluateChannelBudgets(ctx context.Context, channel *model.Channel, userID int, modelName string) (dto.ChannelPeriodStatus, []channelBudgetRow, error) {
	status := dto.ChannelPeriodStatus{SchemaVersion: 1, Timezone: time.Local.String(), StorageMode: channelUserDailyQuotaStorageMode(), Metrics: []dto.ChannelPeriodMetric{}}
	if channel == nil || channel.Id <= 0 || userID <= 0 {
		return status, nil, errors.New("周期额度用户或渠道无效")
	}
	policy, err := GetChannelPeriodPolicy(ctx, channel.Id)
	if err != nil {
		return status, nil, err
	}
	gap, err := getChannelQuotaGap(ctx, channel.Id)
	if err != nil {
		return status, nil, err
	}
	// 同一状态和同一次结算都使用单个时间快照，跨午夜时不混用两天的计数。
	now := channelPeriodNow().In(time.Local)
	plan := buildChannelBudgetPlan(policy, int64(channel.GetUserDailyQuotaLimit()), int64(channel.GetUserWeeklyQuotaLimit()))
	res, err := resolveChannelBudgetRows(plan, now, modelName)
	if err != nil {
		return status, nil, err
	}
	status.Revision, status.NextChangeAt, status.FallbackEnabled = policy.Revision, res.next, policy.Config.Fallback.Enabled
	overrides, err := loadChannelBudgetOverrides(ctx, plan, res, channel.Id, userID, now, &status.NextChangeAt)
	if err != nil {
		return status, nil, err
	}
	type counterValue struct {
		counter           channelBudgetCounter
		user, pool, since int64
	}
	cache := make(map[string]counterValue)
	read := func(row channelBudgetRow) (counterValue, error) {
		if value, ok := cache[row.identityKey()]; ok {
			return value, nil
		}
		counter := newChannelBudgetCounter(channel.Id, row, res)
		user, pool, since, readErr := readChannelBudgetCounter(ctx, counter, userID, now)
		value := counterValue{counter: counter, user: user, pool: pool, since: since}
		cache[row.identityKey()] = value
		return value, readErr
	}
	var rows []channelBudgetRow
	applyPersonal := func(metric *dto.ChannelPeriodMetric, row channelBudgetRow, override channelBudgetOverride) {
		metric.Limit = override.limit
		metric.Source = dto.ChannelPeriodSource{Kind: channelBudgetSourcePersonal, ExpiresAt: override.expiresAt}
		// 旧个人日/周指标只改 limit 与来源，不输出 override_limit；其余行沿用整段特批的形状。
		if !row.legacyUser {
			metric.OverrideLimit = &override.limit
		}
		if override.expiresAt > now.Unix() && (status.NextChangeAt == 0 || override.expiresAt < status.NextChangeAt) {
			status.NextChangeAt = override.expiresAt
		}
	}
	// 日/周分组：每组一条指标，位置取分组首行，内容取生效行。
	emittedGroups := make(map[string]bool)
	for _, row := range plan.Rows {
		group := row.groupKey()
		if row.Window == channelBudgetWindowOccurrence || emittedGroups[group] {
			continue
		}
		effective, ok := res.effectiveRow(plan, group)
		if !ok {
			continue
		}
		emittedGroups[group] = true
		value, readErr := read(row)
		if readErr != nil {
			return status, nil, readErr
		}
		metric := dto.ChannelPeriodMetric{Scope: row.Scope, Period: row.Window, Limit: effective.Limit, BaseLimit: effective.Limit, ResetAt: value.counter.end, Source: res.source(effective), Enforced: true}
		if row.Scope == channelBudgetScopeUser {
			metric.Used = value.user
		} else {
			metric.Used = value.pool
		}
		if row.legacyUser {
			// 旧个人日/周指标：基础额度是渠道列，覆盖范围按 reset 窗口与缺口判断。
			metric.BaseLimit, metric.Coverage = row.Limit, "existing"
			if start := time.Unix(metric.ResetAt, 0).In(time.Local); row.Window == channelBudgetWindowDaily && gap >= start.AddDate(0, 0, -1).Unix() && gap < metric.ResetAt || row.Window == channelBudgetWindowWeekly && gap >= start.AddDate(0, 0, -7).Unix() && gap < metric.ResetAt {
				metric.Coverage = "incomplete"
			}
		} else {
			metric.TrackingSince, metric.Coverage = value.since, channelBudgetCoverage(gap, value.since, value.counter.end)
		}
		for _, candidate := range plan.Rows {
			if override, exists := overrides[candidate.ID]; exists && candidate.groupKey() == group {
				applyPersonal(&metric, row, override)
				break
			}
		}
		status.Metrics = append(status.Metrics, metric)
		rows = append(rows, effective)
	}
	// 整段行：每个当前生效时段按个人、池子各输出一条，只有分组生效行才拦截。
	for _, state := range res.activeSchedules {
		for _, scope := range []string{channelBudgetScopeUser, channelBudgetScopePool} {
			for _, row := range plan.Rows {
				if row.Window != channelBudgetWindowOccurrence || row.ScheduleID != state.schedule.ID || row.Scope != scope || !row.matchesModel(modelName) {
					continue
				}
				value, readErr := read(row)
				if readErr != nil {
					return status, nil, readErr
				}
				effective, ok := res.effectiveRow(plan, row.groupKey())
				metric := dto.ChannelPeriodMetric{Scope: scope, Period: "custom", Limit: row.Limit, BaseLimit: row.Limit, Used: value.pool, ResetAt: value.counter.end, TrackingSince: value.since, Coverage: channelBudgetCoverage(gap, value.since, value.counter.end), Enforced: ok && effective.ID == row.ID, Source: res.source(row)}
				if scope == channelBudgetScopeUser {
					metric.Used = value.user
					if override, exists := overrides[row.ID]; exists {
						applyPersonal(&metric, row, override)
					}
				}
				status.Metrics = append(status.Metrics, metric)
				rows = append(rows, row)
			}
		}
	}
	for i := range status.Metrics {
		metric := &status.Metrics[i]
		if metric.Limit > 0 {
			remaining := max(int64(0), metric.Limit-metric.Used)
			metric.Remaining = &remaining
			if metric.Enforced && metric.Used >= metric.Limit {
				status.Blocked = true
			}
		}
	}
	return status, rows, nil
}

func channelBudgetCoverage(gap, since, end int64) string {
	if gap >= since && gap < end {
		return "incomplete"
	}
	return "since_tracking_start"
}

// loadChannelBudgetOverrides 读取用户的旧个人日/周覆盖与规则整段特批，并映射到预算行。
func loadChannelBudgetOverrides(ctx context.Context, plan channelBudgetPlan, res channelBudgetResolution, channelID, userID int, now time.Time, next *int64) (map[string]channelBudgetOverride, error) {
	overrides := make(map[string]channelBudgetOverride)
	var override *model.ChannelUserLimitOverride
	var err error
	if plan.Revision > 0 {
		// 个人明确覆盖可能比临时规则更严格，故障时不能静默回落到较宽的基础额度。
		override, err = model.GetActiveChannelUserLimitOverrideStrict(ctx, channelID, userID, channelPeriodNow().Unix())
	} else {
		override, err = getCachedChannelUserLimitOverride(ctx, channelID, userID)
	}
	if err != nil {
		return nil, err
	}
	if override != nil && (override.ExpiresAt == 0 || override.ExpiresAt > now.Unix()) {
		for id, value := range map[string]*int{legacyUserDailyBudgetID: override.UserDailyQuotaLimit, legacyUserWeeklyBudgetID: override.UserWeeklyQuotaLimit} {
			if value == nil || *value <= 0 || *value > common.MaxQuota {
				continue
			}
			// 无策略时沿用旧 max 语义：基础不限或覆盖不高于基础时不生效；基础额度来自 32 位渠道列，转 int 无损。
			base := int64(0)
			for _, row := range plan.Rows {
				if row.ID == id {
					if effective, ok := res.effectiveRow(plan, row.groupKey()); ok {
						base = effective.Limit
					}
				}
			}
			if plan.Revision == 0 && int64(effectiveChannelUserLimit(int(base), value)) == base {
				continue
			}
			overrides[id] = channelBudgetOverride{limit: int64(*value), expiresAt: override.ExpiresAt}
		}
		if override.ExpiresAt > now.Unix() && (*next == 0 || override.ExpiresAt < *next) {
			*next = override.ExpiresAt
		}
	}
	if len(res.activeSchedules) == 0 {
		return overrides, nil
	}
	periodOverrides, err := model.ListChannelUserPeriodOverrides(ctx, channelID, userID, now.Unix())
	if err != nil {
		return nil, err
	}
	for _, personal := range periodOverrides {
		if personal.UserPeriodQuotaLimit <= 0 || personal.UserPeriodQuotaLimit > common.MaxPeriodQuota {
			continue
		}
		overrides["rule-"+personal.RuleId+"-user-occurrence"] = channelBudgetOverride{limit: personal.UserPeriodQuotaLimit, expiresAt: personal.ExpiresAt}
	}
	return overrides, nil
}

// CheckChannelPeriodLimits 检查完整有效限制，保持软上限、不预占。
// @param ctx 请求上下文。
// @param channel 渠道配置。
// @param userID 用户 ID。
// @return 额度耗尽返回 ChannelPeriodBlock；存储故障返回普通错误。
func CheckChannelPeriodLimits(ctx context.Context, channel *model.Channel, userID int) error {
	status, rows, err := evaluateChannelBudgets(ctx, channel, userID, "")
	if err != nil {
		return err
	}
	return firstChannelBudgetBlock(status, rows)
}

func firstChannelBudgetBlock(status dto.ChannelPeriodStatus, rows []channelBudgetRow) error {
	for i, metric := range status.Metrics {
		if metric.Enforced && metric.Limit > 0 && metric.Used >= metric.Limit {
			return &ChannelPeriodBlock{Metric: metric, row: rows[i]}
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
		// 只有渠道级（不限模型）的个人日/周行沿用旧错误码，按模型的行属于周期额度。
		if block.Metric.Scope == channelBudgetScopeUser && len(block.row.Models) == 0 {
			if block.Metric.Period == channelBudgetWindowDaily {
				code = types.ErrorCodeChannelUserDailyQuotaExceeded
			}
			if block.Metric.Period == channelBudgetWindowWeekly {
				code = types.ErrorCodeChannelUserWeeklyQuotaExceeded
			}
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
	status, rows, err := evaluateChannelBudgets(c, base, userID, common.GetContextKeyString(c, constant.ContextKeyOriginalModel))
	if err != nil {
		return ChannelPeriodAPIError(err)
	}
	// gin 的 GetInt 只识别 int：若写入 int64，旧个人日周检查会读到 0 并误判为不限。
	for i, metric := range status.Metrics {
		if metric.Scope == channelBudgetScopeUser && len(rows[i].Models) == 0 && metric.Period == channelBudgetWindowDaily {
			common.SetContextKey(c, constant.ContextKeyChannelUserDailyQuotaLimit, int(metric.Limit))
		}
		if metric.Scope == channelBudgetScopeUser && len(rows[i].Models) == 0 && metric.Period == channelBudgetWindowWeekly {
			common.SetContextKey(c, constant.ContextKeyChannelUserWeeklyQuotaLimit, int(metric.Limit))
		}
	}
	if err := firstChannelBudgetBlock(status, rows); err != nil {
		c.Set("channel_period_block", err)
		return ChannelPeriodAPIError(err)
	}
	return nil
}
