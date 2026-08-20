package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

const (
	userBillingSummaryStatusOK       = "ok"
	userBillingSummaryStatusNotFound = "not_found"
	userBillingSummaryStatusError    = "error"

	userBillingSortKindFinite   = "finite"
	userBillingSortKindInfinite = "infinite"
	userBillingSortKindUnknown  = "unknown"

	userBillingErrorInvalidSubscription = "invalid_subscription_data"
	userBillingErrorPlanNotFound        = "subscription_plan_not_found"
	userBillingErrorInvalidReset        = "invalid_subscription_reset"
	userBillingErrorAggregationOverflow = "subscription_aggregation_overflow"
)

// AdminUserBillingSummaryMaxUsers 是单次批量账务摘要去重后的用户上限。
const AdminUserBillingSummaryMaxUsers = 500

var (
	// ErrAdminUserBillingSummaryInvalidRequest 表示批量账务摘要请求参数不符合契约。
	ErrAdminUserBillingSummaryInvalidRequest = errors.New("管理员用户账务摘要请求参数无效")
	// ErrAdminUserBillingSummaryBatchTooLarge 表示去重后的用户数量超过批量上限。
	ErrAdminUserBillingSummaryBatchTooLarge = errors.New("管理员用户账务摘要批量数量超过上限")
)

type normalizedAdminUserBillingSummaryRequest struct {
	userIDs   []int
	sortBy    string
	sortOrder string
}

// GetAdminUserBillingSummaries 批量读取、聚合并排序管理员用户账务摘要。
//
// @param ctx 请求上下文。
// @param request 批量用户与排序参数。
// @return *dto.AdminUserBillingSummaryResponse 统一排序后的账务摘要。
// @return error 参数或批量数据库查询失败时返回错误。
func GetAdminUserBillingSummaries(ctx context.Context, request dto.AdminUserBillingSummaryRequest) (*dto.AdminUserBillingSummaryResponse, error) {
	normalized, err := normalizeAdminUserBillingSummaryRequest(request)
	if err != nil {
		return nil, err
	}
	now := model.GetDBTimestamp()
	users, err := model.GetUserBillingSummaryRows(ctx, normalized.userIDs)
	if err != nil {
		return nil, fmt.Errorf("批量查询用户账务字段失败: %w", err)
	}
	subscriptions, err := model.GetActiveUserSubscriptionsByUserIDs(ctx, normalized.userIDs, now)
	if err != nil {
		return nil, fmt.Errorf("批量查询用户有效订阅失败: %w", err)
	}
	planIDs := make([]int, 0, len(subscriptions))
	seenPlanIDs := make(map[int]struct{}, len(subscriptions))
	for _, subscription := range subscriptions {
		if _, exists := seenPlanIDs[subscription.PlanId]; exists {
			continue
		}
		seenPlanIDs[subscription.PlanId] = struct{}{}
		planIDs = append(planIDs, subscription.PlanId)
	}
	plans, err := model.GetSubscriptionPlansForBillingSummary(ctx, planIDs)
	if err != nil {
		return nil, fmt.Errorf("批量查询订阅套餐失败: %w", err)
	}
	return buildAdminUserBillingSummaryResponse(normalized, users, subscriptions, plans, now), nil
}

func normalizeAdminUserBillingSummaryRequest(request dto.AdminUserBillingSummaryRequest) (normalizedAdminUserBillingSummaryRequest, error) {
	if len(request.UserIDs) == 0 {
		return normalizedAdminUserBillingSummaryRequest{}, ErrAdminUserBillingSummaryInvalidRequest
	}
	userIDs := make([]int, 0, len(request.UserIDs))
	seenUserIDs := make(map[int]struct{}, len(request.UserIDs))
	for _, userID := range request.UserIDs {
		if userID <= 0 {
			return normalizedAdminUserBillingSummaryRequest{}, ErrAdminUserBillingSummaryInvalidRequest
		}
		if _, exists := seenUserIDs[userID]; exists {
			continue
		}
		seenUserIDs[userID] = struct{}{}
		userIDs = append(userIDs, userID)
	}
	if len(userIDs) > AdminUserBillingSummaryMaxUsers {
		return normalizedAdminUserBillingSummaryRequest{}, ErrAdminUserBillingSummaryBatchTooLarge
	}

	sortBy := strings.ToLower(strings.TrimSpace(request.SortBy))
	if sortBy == "" {
		sortBy = "user_id"
	}
	switch sortBy {
	case "user_id", "wallet_quota", "subscription_remaining":
	default:
		return normalizedAdminUserBillingSummaryRequest{}, ErrAdminUserBillingSummaryInvalidRequest
	}

	sortOrder := strings.ToLower(strings.TrimSpace(request.SortOrder))
	if sortOrder == "" {
		sortOrder = "desc"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		return normalizedAdminUserBillingSummaryRequest{}, ErrAdminUserBillingSummaryInvalidRequest
	}
	return normalizedAdminUserBillingSummaryRequest{userIDs: userIDs, sortBy: sortBy, sortOrder: sortOrder}, nil
}

func buildAdminUserBillingSummaryResponse(
	request normalizedAdminUserBillingSummaryRequest,
	users []model.UserBillingSummaryRow,
	subscriptions []model.UserSubscription,
	plans []model.SubscriptionPlan,
	now int64,
) *dto.AdminUserBillingSummaryResponse {
	usersByID := make(map[int]model.UserBillingSummaryRow, len(users))
	for _, user := range users {
		usersByID[user.ID] = user
	}
	subscriptionsByUserID := make(map[int][]model.UserSubscription)
	for _, subscription := range subscriptions {
		subscriptionsByUserID[subscription.UserId] = append(subscriptionsByUserID[subscription.UserId], subscription)
	}
	plansByID := make(map[int]model.SubscriptionPlan, len(plans))
	for _, plan := range plans {
		plansByID[plan.Id] = plan
	}

	items := make([]dto.AdminUserBillingSummaryItem, 0, len(request.userIDs))
	for _, userID := range request.userIDs {
		user, exists := usersByID[userID]
		if !exists {
			items = append(items, dto.AdminUserBillingSummaryItem{
				UserID:  userID,
				Status:  userBillingSummaryStatusNotFound,
				SortKey: dto.UserBillingSortKey{Kind: userBillingSortKindUnknown},
			})
			continue
		}

		remoteStatus := user.Status
		remoteRole := user.Role
		item := dto.AdminUserBillingSummaryItem{
			UserID:       userID,
			Status:       userBillingSummaryStatusOK,
			RemoteStatus: &remoteStatus,
			RemoteRole:   &remoteRole,
			Wallet: &dto.UserBillingWalletSummary{
				Quota:     user.Quota,
				UsedQuota: user.UsedQuota,
				Group:     user.Group,
			},
		}
		subscriptionSummary, errorCode := buildUserBillingSubscriptionSummary(
			userID,
			subscriptionsByUserID[userID],
			plansByID,
			now,
		)
		if errorCode != "" {
			item.Status = userBillingSummaryStatusError
			item.ErrorCode = errorCode
			item.SortKey = dto.UserBillingSortKey{Kind: userBillingSortKindUnknown}
			items = append(items, item)
			continue
		}
		item.Subscription = subscriptionSummary
		item.SortKey = userBillingSortKey(request.sortBy, item)
		items = append(items, item)
	}
	sortAdminUserBillingSummaryItems(items, request.sortBy, request.sortOrder)
	return &dto.AdminUserBillingSummaryResponse{
		SortBy:    request.sortBy,
		SortOrder: request.sortOrder,
		Items:     items,
	}
}

func buildUserBillingSubscriptionSummary(
	userID int,
	subscriptions []model.UserSubscription,
	plansByID map[int]model.SubscriptionPlan,
	now int64,
) (*dto.UserBillingSubscriptionSummary, string) {
	summary := &dto.UserBillingSubscriptionSummary{
		ActiveCount: len(subscriptions),
		Items:       make([]dto.UserBillingSubscriptionItem, 0, len(subscriptions)),
	}
	for _, subscription := range subscriptions {
		if subscription.Id <= 0 || subscription.UserId != userID || subscription.PlanId <= 0 ||
			subscription.Status != "active" || subscription.StartTime <= 0 || subscription.EndTime <= now ||
			subscription.LastResetTime < 0 || subscription.NextResetTime < 0 ||
			subscription.AmountTotal < 0 || subscription.AmountUsed < 0 ||
			(subscription.AmountTotal > 0 && subscription.AmountUsed > subscription.AmountTotal) {
			return nil, userBillingErrorInvalidSubscription
		}
		plan, exists := plansByID[subscription.PlanId]
		if !exists {
			return nil, userBillingErrorPlanNotFound
		}
		projected, _, err := model.ProjectUserSubscriptionCycle(subscription, plan, now)
		if err != nil {
			return nil, userBillingErrorInvalidReset
		}
		item := dto.UserBillingSubscriptionItem{
			SubscriptionID: projected.Id,
			PlanID:         projected.PlanId,
			PlanTitle:      plan.Title,
			Unlimited:      projected.AmountTotal == 0,
			AmountTotal:    projected.AmountTotal,
			AmountUsed:     projected.AmountUsed,
			StartTime:      projected.StartTime,
			EndTime:        projected.EndTime,
			LastResetTime:  projected.LastResetTime,
			NextResetTime:  projected.NextResetTime,
		}
		if item.Unlimited {
			summary.Unlimited = true
			summary.Items = append(summary.Items, item)
			continue
		}
		remaining := projected.AmountTotal - projected.AmountUsed
		item.AmountRemaining = &remaining
		var overflow bool
		if summary.FiniteTotal, overflow = addUserBillingAmount(summary.FiniteTotal, projected.AmountTotal); overflow {
			return nil, userBillingErrorAggregationOverflow
		}
		if summary.FiniteUsed, overflow = addUserBillingAmount(summary.FiniteUsed, projected.AmountUsed); overflow {
			return nil, userBillingErrorAggregationOverflow
		}
		if summary.FiniteRemaining, overflow = addUserBillingAmount(summary.FiniteRemaining, remaining); overflow {
			return nil, userBillingErrorAggregationOverflow
		}
		summary.Items = append(summary.Items, item)
	}
	return summary, ""
}

func addUserBillingAmount(current int64, value int64) (int64, bool) {
	if current < 0 || value < 0 || current > math.MaxInt64-value {
		return 0, true
	}
	return current + value, false
}

func userBillingSortKey(sortBy string, item dto.AdminUserBillingSummaryItem) dto.UserBillingSortKey {
	if sortBy == "subscription_remaining" && item.Subscription.Unlimited {
		return dto.UserBillingSortKey{Kind: userBillingSortKindInfinite}
	}
	value := int64(item.UserID)
	switch sortBy {
	case "wallet_quota":
		value = int64(item.Wallet.Quota)
	case "subscription_remaining":
		value = item.Subscription.FiniteRemaining
	}
	return dto.UserBillingSortKey{Kind: userBillingSortKindFinite, Value: &value}
}

func sortAdminUserBillingSummaryItems(items []dto.AdminUserBillingSummaryItem, sortBy string, sortOrder string) {
	sort.SliceStable(items, func(leftIndex int, rightIndex int) bool {
		left := items[leftIndex]
		right := items[rightIndex]
		if left.SortKey.Kind == userBillingSortKindUnknown || right.SortKey.Kind == userBillingSortKindUnknown {
			if left.SortKey.Kind != right.SortKey.Kind {
				return right.SortKey.Kind == userBillingSortKindUnknown
			}
			return left.UserID > right.UserID
		}
		if sortBy == "subscription_remaining" && left.SortKey.Kind != right.SortKey.Kind {
			if sortOrder == "desc" {
				return left.SortKey.Kind == userBillingSortKindInfinite
			}
			return left.SortKey.Kind == userBillingSortKindFinite
		}
		if left.SortKey.Kind == userBillingSortKindFinite && right.SortKey.Kind == userBillingSortKindFinite {
			leftValue := *left.SortKey.Value
			rightValue := *right.SortKey.Value
			if leftValue != rightValue {
				if sortOrder == "asc" {
					return leftValue < rightValue
				}
				return leftValue > rightValue
			}
		}
		return left.UserID > right.UserID
	})
}
