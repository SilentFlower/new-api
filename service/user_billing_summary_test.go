package service

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeAdminUserBillingSummaryRequest(t *testing.T) {
	tests := []struct {
		name      string
		request   dto.AdminUserBillingSummaryRequest
		wantIDs   []int
		wantSort  string
		wantOrder string
		wantError error
	}{
		{
			name:    "默认排序并去重",
			request: dto.AdminUserBillingSummaryRequest{UserIDs: []int{3, 1, 3, 2}},
			wantIDs: []int{3, 1, 2}, wantSort: "user_id", wantOrder: "desc",
		},
		{
			name:    "归一化显式排序",
			request: dto.AdminUserBillingSummaryRequest{UserIDs: []int{1}, SortBy: " Subscription_Remaining ", SortOrder: " ASC "},
			wantIDs: []int{1}, wantSort: "subscription_remaining", wantOrder: "asc",
		},
		{
			name:      "拒绝非正用户",
			request:   dto.AdminUserBillingSummaryRequest{UserIDs: []int{1, 0}},
			wantError: ErrAdminUserBillingSummaryInvalidRequest,
		},
		{
			name:      "拒绝未知排序字段",
			request:   dto.AdminUserBillingSummaryRequest{UserIDs: []int{1}, SortBy: "quota"},
			wantError: ErrAdminUserBillingSummaryInvalidRequest,
		},
		{
			name:      "拒绝未知排序方向",
			request:   dto.AdminUserBillingSummaryRequest{UserIDs: []int{1}, SortOrder: "sideways"},
			wantError: ErrAdminUserBillingSummaryInvalidRequest,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalized, err := normalizeAdminUserBillingSummaryRequest(test.request)
			if test.wantError != nil {
				assert.ErrorIs(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantIDs, normalized.userIDs)
			assert.Equal(t, test.wantSort, normalized.sortBy)
			assert.Equal(t, test.wantOrder, normalized.sortOrder)
		})
	}

	tooMany := make([]int, AdminUserBillingSummaryMaxUsers+1)
	for index := range tooMany {
		tooMany[index] = index + 1
	}
	_, err := normalizeAdminUserBillingSummaryRequest(dto.AdminUserBillingSummaryRequest{UserIDs: tooMany})
	assert.ErrorIs(t, err, ErrAdminUserBillingSummaryBatchTooLarge)

	duplicates := make([]int, AdminUserBillingSummaryMaxUsers+100)
	for index := range duplicates {
		duplicates[index] = 1
	}
	normalized, err := normalizeAdminUserBillingSummaryRequest(dto.AdminUserBillingSummaryRequest{UserIDs: duplicates})
	require.NoError(t, err)
	assert.Equal(t, []int{1}, normalized.userIDs)
}

func TestBuildAdminUserBillingSummaryResponseAggregatesFactsAndSortsGlobally(t *testing.T) {
	now := int64(10000)
	users := []model.UserBillingSummaryRow{
		{ID: 1, Quota: 100, UsedQuota: 30, Group: "vip", Status: common.UserStatusDisabled, Role: common.RoleGuestUser},
		{ID: 2, Quota: 50, UsedQuota: 10, Group: "default", Status: common.UserStatusEnabled, Role: common.RoleAdminUser},
		{ID: 3, Quota: 50, Group: "default", Status: common.UserStatusEnabled, Role: common.RoleCommonUser},
		{ID: 5, Quota: 300, Group: "default", Status: common.UserStatusEnabled, Role: common.RoleCommonUser},
		{ID: 6, Quota: 200, Group: "default", Status: common.UserStatusEnabled, Role: common.RoleCommonUser},
	}
	plans := []model.SubscriptionPlan{
		{Id: 11, Title: "有限一", QuotaResetPeriod: model.SubscriptionResetNever},
		{Id: 12, Title: "有限二", QuotaResetPeriod: model.SubscriptionResetNever},
		{Id: 13, Title: "无限", QuotaResetPeriod: model.SubscriptionResetNever},
		{Id: 14, Title: "周期重置", QuotaResetPeriod: model.SubscriptionResetCustom, QuotaResetCustomSeconds: 500},
	}
	subscriptions := []model.UserSubscription{
		{Id: 101, UserId: 1, PlanId: 11, AmountTotal: 100, AmountUsed: 40, StartTime: 9000, EndTime: 20000, Status: "active"},
		{Id: 102, UserId: 1, PlanId: 12, AmountTotal: 200, AmountUsed: 50, StartTime: 9000, EndTime: 20000, Status: "active"},
		{Id: 201, UserId: 2, PlanId: 13, AmountUsed: 25, StartTime: 9000, EndTime: 20000, Status: "active"},
		{Id: 202, UserId: 2, PlanId: 11, AmountTotal: 100, AmountUsed: 40, StartTime: 9000, EndTime: 20000, Status: "active"},
		{Id: 501, UserId: 5, PlanId: 99, AmountTotal: 100, AmountUsed: 10, StartTime: 9000, EndTime: 20000, Status: "active"},
		{Id: 601, UserId: 6, PlanId: 14, AmountTotal: 500, AmountUsed: 200, StartTime: 9000, EndTime: 20000, Status: "active", LastResetTime: 9000, NextResetTime: 9500},
	}
	request := normalizedAdminUserBillingSummaryRequest{
		userIDs: []int{1, 2, 3, 4, 5, 6}, sortBy: "subscription_remaining", sortOrder: "desc",
	}

	response := buildAdminUserBillingSummaryResponse(request, users, subscriptions, plans, now)

	require.Len(t, response.Items, 6)
	assert.Equal(t, []int{2, 6, 1, 3, 5, 4}, billingSummaryUserIDs(response.Items))
	byID := billingSummaryItemsByUserID(response.Items)
	assert.Equal(t, userBillingSummaryStatusOK, byID[1].Status)
	require.NotNil(t, byID[1].RemoteStatus)
	require.NotNil(t, byID[1].RemoteRole)
	assert.Equal(t, common.UserStatusDisabled, *byID[1].RemoteStatus)
	assert.Equal(t, common.RoleGuestUser, *byID[1].RemoteRole)
	assert.EqualValues(t, 300, byID[1].Subscription.FiniteTotal)
	assert.EqualValues(t, 90, byID[1].Subscription.FiniteUsed)
	assert.EqualValues(t, 210, byID[1].Subscription.FiniteRemaining)
	assert.Equal(t, userBillingSortKindInfinite, byID[2].SortKey.Kind)
	assert.True(t, byID[2].Subscription.Unlimited)
	assert.EqualValues(t, 60, byID[2].Subscription.FiniteRemaining)
	assert.EqualValues(t, 500, byID[6].Subscription.FiniteRemaining)
	assert.Zero(t, byID[6].Subscription.Items[0].AmountUsed)
	assert.EqualValues(t, 10000, byID[6].Subscription.Items[0].LastResetTime)
	assert.Equal(t, userBillingSummaryStatusError, byID[5].Status)
	assert.Equal(t, userBillingErrorPlanNotFound, byID[5].ErrorCode)
	assert.Equal(t, userBillingSortKindUnknown, byID[5].SortKey.Kind)
	assert.Equal(t, userBillingSummaryStatusNotFound, byID[4].Status)
	assert.Nil(t, byID[4].RemoteStatus)
	assert.Equal(t, 0, byID[3].Subscription.ActiveCount)
	assert.Empty(t, byID[3].Subscription.Items)

	request.sortOrder = "asc"
	ascending := buildAdminUserBillingSummaryResponse(request, users, subscriptions, plans, now)
	assert.Equal(t, []int{3, 1, 6, 2, 5, 4}, billingSummaryUserIDs(ascending.Items))

	request.sortBy = "wallet_quota"
	walletAscending := buildAdminUserBillingSummaryResponse(request, users, subscriptions, plans, now)
	assert.Equal(t, []int{3, 2, 1, 6, 5, 4}, billingSummaryUserIDs(walletAscending.Items))
}

func TestBuildUserBillingSubscriptionSummaryReturnsStablePerUserErrors(t *testing.T) {
	now := int64(10000)
	validPlan := model.SubscriptionPlan{Id: 11, Title: "有效套餐", QuotaResetPeriod: model.SubscriptionResetNever}
	base := model.UserSubscription{
		Id: 1, UserId: 1, PlanId: 11, AmountTotal: 100, AmountUsed: 10,
		StartTime: 9000, EndTime: 20000, Status: "active",
	}
	tests := []struct {
		name          string
		subscriptions []model.UserSubscription
		plans         map[int]model.SubscriptionPlan
		wantCode      string
	}{
		{
			name: "套餐缺失", subscriptions: []model.UserSubscription{base},
			plans: map[int]model.SubscriptionPlan{}, wantCode: userBillingErrorPlanNotFound,
		},
		{
			name: "订阅数据无效", subscriptions: []model.UserSubscription{{
				Id: 1, UserId: 1, PlanId: 11, AmountTotal: 100, AmountUsed: 101,
				StartTime: 9000, EndTime: 20000, Status: "active",
			}}, plans: map[int]model.SubscriptionPlan{11: validPlan}, wantCode: userBillingErrorInvalidSubscription,
		},
		{
			name: "重置配置无效", subscriptions: []model.UserSubscription{base},
			plans:    map[int]model.SubscriptionPlan{11: {Id: 11, QuotaResetPeriod: "hourly"}},
			wantCode: userBillingErrorInvalidReset,
		},
		{
			name: "聚合溢出",
			subscriptions: []model.UserSubscription{
				{Id: 1, UserId: 1, PlanId: 11, AmountTotal: math.MaxInt64, StartTime: 9000, EndTime: 20000, Status: "active"},
				{Id: 2, UserId: 1, PlanId: 11, AmountTotal: 1, StartTime: 9000, EndTime: 20000, Status: "active"},
			},
			plans: map[int]model.SubscriptionPlan{11: validPlan}, wantCode: userBillingErrorAggregationOverflow,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			summary, code := buildUserBillingSubscriptionSummary(1, test.subscriptions, test.plans, now)
			assert.Nil(t, summary)
			assert.Equal(t, test.wantCode, code)
		})
	}
}

func billingSummaryUserIDs(items []dto.AdminUserBillingSummaryItem) []int {
	userIDs := make([]int, 0, len(items))
	for _, item := range items {
		userIDs = append(userIDs, item.UserID)
	}
	return userIDs
}

func billingSummaryItemsByUserID(items []dto.AdminUserBillingSummaryItem) map[int]dto.AdminUserBillingSummaryItem {
	byID := make(map[int]dto.AdminUserBillingSummaryItem, len(items))
	for _, item := range items {
		byID[item.UserID] = item
	}
	return byID
}
