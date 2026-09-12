package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// findChannelBudgetRow 在当前策略里查找一条预算行。
func findChannelBudgetRow(ctx context.Context, channelID int, budgetID string) (dto.ChannelPeriodPolicyView, dto.ChannelBudgetRow, error) {
	view, err := GetChannelPeriodPolicy(ctx, channelID)
	if err != nil {
		return view, dto.ChannelBudgetRow{}, err
	}
	for _, row := range view.Config.Budgets {
		if row.ID == budgetID {
			return view, row, nil
		}
	}
	return view, dto.ChannelBudgetRow{}, fmt.Errorf("%w: 预算行不存在", ErrInvalidChannelPeriodPolicy)
}

// ReplaceChannelUserBudgetOverride 校验并保存用户对某条个人预算行的提额。
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param userID 用户 ID。
// @param budgetID 预算行 id。
// @param input 提额与到期时间。
// @param updatedBy 管理员 ID。
// @return 校验或持久化错误。
func ReplaceChannelUserBudgetOverride(ctx context.Context, channelID, userID int, budgetID string, input dto.ChannelBudgetUserOverrideInput, updatedBy int) error {
	if userID <= 0 || updatedBy <= 0 {
		return fmt.Errorf("%w: 用户或管理员无效", ErrInvalidChannelPeriodPolicy)
	}
	if input.ExpiresAt < 0 || input.ExpiresAt > 0 && input.ExpiresAt <= time.Now().Unix() {
		return fmt.Errorf("%w: 提额到期时间须在未来", ErrInvalidChannelPeriodPolicy)
	}
	_, row, err := findChannelBudgetRow(ctx, channelID, budgetID)
	if err != nil {
		return err
	}
	if row.Scope != channelBudgetScopeUser {
		return fmt.Errorf("%w: 只能对个人预算行提额", ErrInvalidChannelPeriodPolicy)
	}
	// 提额只允许放宽：基础不限时无需提额，目标必须高于该行基础额度且不超过上界。
	if row.Limit <= 0 || input.Limit <= row.Limit || input.Limit > common.MaxPeriodQuota {
		return fmt.Errorf("%w: 提额须高于该行基础额度且不超过最大值", ErrInvalidChannelPeriodPolicy)
	}
	return model.ReplaceChannelUserBudgetOverride(ctx, &model.ChannelUserBudgetOverride{ChannelId: channelID, UserId: userID, BudgetId: budgetID, QuotaLimit: input.Limit, ExpiresAt: input.ExpiresAt, UpdatedBy: updatedBy})
}

// DeleteChannelUserBudgetOverride 撤销用户对某条预算行的提额，其他行不受影响。
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param userID 用户 ID。
// @param budgetID 预算行 id。
// @return 行不存在或数据库错误。
func DeleteChannelUserBudgetOverride(ctx context.Context, channelID, userID int, budgetID string) error {
	_, row, err := findChannelBudgetRow(ctx, channelID, budgetID)
	if err != nil {
		return err
	}
	if row.Scope != channelBudgetScopeUser {
		return fmt.Errorf("%w: 只能撤销个人预算行的提额", ErrInvalidChannelPeriodPolicy)
	}
	return model.DeleteChannelUserBudgetOverride(ctx, channelID, userID, budgetID)
}

// GetChannelBudgetUsage 返回某行当前窗口的用量：池子汇总或分页用户列表。
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param budgetID 预算行 id。
// @param scope pool 或 user。
// @param offset 分页偏移。
// @param limit 分页大小。
// @return 用量视图（Items 只含用户 ID 与额度，用户摘要由调用方补齐）及错误。
func GetChannelBudgetUsage(ctx context.Context, channelID int, budgetID, scope string, offset, limit int) (dto.ChannelBudgetUsageView, error) {
	view, row, err := findChannelBudgetRow(ctx, channelID, budgetID)
	if err != nil {
		return dto.ChannelBudgetUsageView{}, err
	}
	if scope != channelBudgetScopeUser && scope != channelBudgetScopePool {
		return dto.ChannelBudgetUsageView{}, fmt.Errorf("%w: 用量范围无效", ErrInvalidChannelPeriodPolicy)
	}
	now := channelPeriodNow().In(time.Local)
	res, err := resolveChannelBudgetRows(buildChannelBudgetPlan(view), now, "")
	if err != nil {
		return dto.ChannelBudgetUsageView{}, err
	}
	counter := newChannelBudgetCounter(channelID, channelBudgetRow{ChannelBudgetRow: row}, res)
	result := dto.ChannelBudgetUsageView{ChannelID: channelID, BudgetID: budgetID, Scope: scope, WindowStart: counter.start, WindowEnd: counter.end, StorageMode: channelUserDailyQuotaStorageMode(), Items: []dto.ChannelBudgetUsageItem{}}
	_, poolUsed, since, err := readChannelBudgetCounter(ctx, counter, 0, now)
	if err != nil {
		return result, err
	}
	result.TrackingSince = since
	if scope == channelBudgetScopePool {
		result.UsedQuota = poolUsed
		return result, nil
	}
	values, err := listChannelBudgetUsage(ctx, counter)
	if err != nil {
		return result, err
	}
	items := make([]dto.ChannelBudgetUsageItem, 0, len(values))
	for userID, used := range values {
		items = append(items, dto.ChannelBudgetUsageItem{UserID: userID, UsedQuota: used})
	}
	sortChannelBudgetUsageItems(items)
	result.Total = len(items)
	if offset > len(items) {
		offset = len(items)
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	result.Items = items[offset:end]
	return result, nil
}

// SetChannelBudgetUsage 把某行当前窗口的池子汇总或指定用户已用额度设为目标值，不改策略与其他行。
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param budgetID 预算行 id。
// @param input 范围、用户与目标值。
// @return 校验或存储错误。
func SetChannelBudgetUsage(ctx context.Context, channelID int, budgetID string, input dto.ChannelBudgetUsageInput) error {
	view, row, err := findChannelBudgetRow(ctx, channelID, budgetID)
	if err != nil {
		return err
	}
	if input.Scope != channelBudgetScopeUser && input.Scope != channelBudgetScopePool {
		return fmt.Errorf("%w: 用量范围无效", ErrInvalidChannelPeriodPolicy)
	}
	if input.Scope == channelBudgetScopeUser && input.UserID <= 0 {
		return fmt.Errorf("%w: 用户无效", ErrInvalidChannelPeriodPolicy)
	}
	if input.UsedQuota < 0 || input.UsedQuota > common.MaxPeriodQuota {
		return fmt.Errorf("%w: 已用额度须为合法非负整数", ErrInvalidChannelPeriodPolicy)
	}
	now := channelPeriodNow().In(time.Local)
	res, err := resolveChannelBudgetRows(buildChannelBudgetPlan(view), now, "")
	if err != nil {
		return err
	}
	counter := newChannelBudgetCounter(channelID, channelBudgetRow{ChannelBudgetRow: row}, res)
	opCtx, cancel := channelUserDailyQuotaOperationContext(ctx)
	defer cancel()
	return setChannelBudgetUsage(opCtx, counter, input.Scope, input.UserID, input.UsedQuota, now)
}

func sortChannelBudgetUsageItems(items []dto.ChannelBudgetUsageItem) {
	// 用量最多的用户排在首位，预算表可直接展示该用户的进度；同用量按 ID 保持分页稳定。
	sort.Slice(items, func(i, j int) bool {
		if items[i].UsedQuota == items[j].UsedQuota {
			return items[i].UserID < items[j].UserID
		}
		return items[i].UsedQuota > items[j].UsedQuota
	})
}
