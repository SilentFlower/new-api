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

// GetChannelPeriodStatus 解析当前用户在渠道内全部预算行的额度状态。
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
	status := dto.ChannelPeriodStatus{SchemaVersion: channelBudgetSchemaVersion, Timezone: time.Local.String(), StorageMode: channelUserDailyQuotaStorageMode(), Metrics: []dto.ChannelPeriodMetric{}}
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
	plan := buildChannelBudgetPlan(policy)
	res, err := resolveChannelBudgetRows(plan, now, modelName)
	if err != nil {
		return status, nil, err
	}
	status.Revision, status.NextChangeAt, status.FallbackEnabled = policy.Revision, res.next, policy.Config.DefaultOnExceed.Mode == channelBudgetActionFallback
	overrides := make(map[string]model.ChannelUserBudgetOverride)
	if len(plan.Rows) > 0 {
		// 个人提额可能比时段行更严格或更宽，读取失败不能静默回落到基础额度。
		items, listErr := model.ListActiveChannelUserBudgetOverrides(ctx, channel.Id, userID, now.Unix())
		if listErr != nil {
			return status, nil, listErr
		}
		for _, item := range items {
			if item.QuotaLimit > 0 && item.QuotaLimit <= common.MaxPeriodQuota {
				overrides[item.BudgetId] = item
			}
		}
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
	emit := func(row, effective channelBudgetRow, value counterValue, enforced bool) {
		metric := dto.ChannelPeriodMetric{BudgetID: effective.ID, BudgetName: effective.Name, Scope: row.Scope, Period: row.Window, Models: append([]string{}, effective.Models...), Limit: effective.Limit, BaseLimit: effective.Limit, ResetAt: value.counter.end, Source: res.source(effective), Enforced: enforced}
		if row.Scope == channelBudgetScopeUser {
			metric.Used = value.user
		} else {
			metric.Used = value.pool
		}
		if value.counter.legacy != "" && row.Scope == channelBudgetScopeUser {
			// 旧个人日/周 Hash 没有统计起点，覆盖范围按 reset 窗口与缺口判断。
			metric.Coverage = "existing"
			start := time.Unix(metric.ResetAt, 0).In(time.Local).AddDate(0, 0, -1).Unix()
			if row.Window == channelBudgetWindowWeekly {
				start = time.Unix(metric.ResetAt, 0).In(time.Local).AddDate(0, 0, -7).Unix()
			}
			if gap >= start && gap < metric.ResetAt {
				metric.Coverage = "incomplete"
			}
		} else {
			metric.TrackingSince, metric.Coverage = value.since, channelBudgetCoverage(gap, value.since, value.counter.end)
		}
		if row.Scope == channelBudgetScopeUser {
			// 个人提额作用于整个分组：任一行的提额都覆盖当前生效行。
			for _, candidate := range plan.Rows {
				override, exists := overrides[candidate.ID]
				if !exists || candidate.groupKey() != row.groupKey() || (row.Window == channelBudgetWindowOccurrence && candidate.ID != row.ID) {
					continue
				}
				metric.Limit, metric.OverrideLimit = override.QuotaLimit, &override.QuotaLimit
				metric.Source = dto.ChannelPeriodSource{Kind: channelBudgetSourcePersonal, ExpiresAt: override.ExpiresAt}
				if override.ExpiresAt > now.Unix() && (status.NextChangeAt == 0 || override.ExpiresAt < status.NextChangeAt) {
					status.NextChangeAt = override.ExpiresAt
				}
				break
			}
		}
		status.Metrics = append(status.Metrics, metric)
		rows = append(rows, effective)
	}
	// 日/周分组：每组一条指标，位置取分组首行，内容取生效行。
	emitted := make(map[string]bool)
	for _, row := range plan.Rows {
		group := row.groupKey()
		if row.Window == channelBudgetWindowOccurrence || emitted[group] {
			continue
		}
		effective, ok := res.effectiveRow(plan, group)
		if !ok {
			continue
		}
		emitted[group] = true
		value, readErr := read(row)
		if readErr != nil {
			return status, nil, readErr
		}
		emit(row, effective, value, true)
	}
	// 整段行：每个当前生效时段内命中的行各输出一条，只有分组生效行才拦截。
	for _, state := range res.activeSchedules {
		for _, row := range plan.Rows {
			if row.Window != channelBudgetWindowOccurrence || row.ScheduleID != state.schedule.ID || !row.Enabled || !row.matchesModel(modelName) {
				continue
			}
			value, readErr := read(row)
			if readErr != nil {
				return status, nil, readErr
			}
			effective, ok := res.effectiveRow(plan, row.groupKey())
			emit(row, row, value, ok && effective.ID == row.ID)
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

// ChannelPeriodAPIError 把领域错误转换为稳定 Relay 错误，渠道级个人日/周行保留旧错误码。
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
		if block.row.isChannelUserRow() && block.Metric.Period == channelBudgetWindowDaily {
			code = types.ErrorCodeChannelUserDailyQuotaExceeded
		}
		if block.row.isChannelUserRow() && block.Metric.Period == channelBudgetWindowWeekly {
			code = types.ErrorCodeChannelUserWeeklyQuotaExceeded
		}
	}
	return types.NewOpenAIError(errors.New(message), code, status, types.ErrOptionWithSkipRetry())
}

// CheckSelectedChannelPeriodLimits 在既有限额入口按请求原始模型名检查所选渠道的全部预算。
// @param c 已完成选渠的 Gin 上下文。
// @return 本地限额或存储错误，不访问供应商。
func CheckSelectedChannelPeriodLimits(c *gin.Context) *types.NewAPIError {
	channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	userID := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	policy, err := GetChannelPeriodPolicy(c, channelID)
	if err != nil {
		return ChannelPeriodAPIError(err)
	}
	// 没有预算行就没有可拦截的指标，不读计数也不查渠道。
	if len(policy.Config.Budgets) == 0 {
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
	if err := firstChannelBudgetBlock(status, rows); err != nil {
		c.Set("channel_period_block", err)
		return ChannelPeriodAPIError(err)
	}
	return nil
}
