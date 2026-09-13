package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// GetChannelBudgetUsageSummary 一次读取渠道全部已保存预算行的当前窗口用量。
// 池子行返回池子汇总；个人行返回用量最高的用户及其在该行上的生效上限（含提额），供管理端预算表单次加载。
// @param ctx 请求上下文。
// @param channel 渠道；生效上限需要按用户评估整份策略，因此传渠道而非 ID。
// @return 摘要视图（TopUser 只含用户 ID 与额度，用户摘要由调用方补齐）及错误。
func GetChannelBudgetUsageSummary(ctx context.Context, channel *model.Channel) (dto.ChannelBudgetUsageSummaryView, error) {
	view, err := GetChannelPeriodPolicy(ctx, channel.Id)
	if err != nil {
		return dto.ChannelBudgetUsageSummaryView{}, err
	}
	now := channelPeriodNow().In(time.Local)
	res, err := resolveChannelBudgetRows(buildChannelBudgetPlan(view), now, "")
	if err != nil {
		return dto.ChannelBudgetUsageSummaryView{}, err
	}
	result := dto.ChannelBudgetUsageSummaryView{ChannelID: channel.Id, Revision: view.Revision, StorageMode: channelUserDailyQuotaStorageMode(), Now: now.Unix(), Items: make([]dto.ChannelBudgetUsageSummaryItem, 0, len(view.Config.Budgets))}
	topUsers := make(map[int][]int, 0)
	for _, row := range view.Config.Budgets {
		counter := newChannelBudgetCounter(channel.Id, channelBudgetRow{ChannelBudgetRow: row}, res)
		_, poolUsed, since, err := readChannelBudgetCounter(ctx, counter, 0, now)
		if err != nil {
			return dto.ChannelBudgetUsageSummaryView{}, err
		}
		item := dto.ChannelBudgetUsageSummaryItem{BudgetID: row.ID, Scope: row.Scope, WindowStart: counter.start, WindowEnd: counter.end, TrackingSince: since, UsedQuota: poolUsed, PoolUsedQuota: poolUsed}
		if row.Scope == channelBudgetScopeUser {
			values, err := listChannelBudgetUsage(ctx, counter)
			if err != nil {
				return dto.ChannelBudgetUsageSummaryView{}, err
			}
			item.UsedQuota = 0
			if top, ok := channelBudgetTopUser(values); ok {
				item.UsedQuota = top.UsedQuota
				item.TopUser = &top
				topUsers[top.UserID] = append(topUsers[top.UserID], len(result.Items))
			}
		}
		result.Items = append(result.Items, item)
	}
	// 生效上限要考虑同分组其他行的提额，按用户评估一次整份策略即可覆盖该用户的全部行。
	for userID, indexes := range topUsers {
		status, err := GetChannelPeriodStatus(ctx, channel, userID)
		if err != nil {
			return dto.ChannelBudgetUsageSummaryView{}, err
		}
		metrics := make(map[string]dto.ChannelPeriodMetric, len(status.Metrics))
		for _, metric := range status.Metrics {
			metrics[metric.BudgetID] = metric
		}
		for _, index := range indexes {
			top := result.Items[index].TopUser
			if metric, ok := metrics[result.Items[index].BudgetID]; ok {
				top.EffectiveLimit, top.Override = metric.Limit, metric.OverrideLimit != nil
			}
		}
	}
	return result, nil
}

// channelBudgetTopUser 取用量最高的用户；同值按用户 ID 升序，与分页列表的排序保持一致。
func channelBudgetTopUser(values map[int]int64) (dto.ChannelBudgetUsageTopUser, bool) {
	top, found := dto.ChannelBudgetUsageTopUser{}, false
	for userID, used := range values {
		if !found || used > top.UsedQuota || (used == top.UsedQuota && userID < top.UserID) {
			top, found = dto.ChannelBudgetUsageTopUser{UserID: userID, UsedQuota: used}, true
		}
	}
	return top, found
}
