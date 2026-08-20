package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserBillingSummaryRowsReturnsOnlyExistingMinimalFacts(t *testing.T) {
	truncateTables(t)

	accessToken := "billing-summary-secret-token"
	users := []User{
		{
			Id: 6101, Username: "billing-summary-one", Password: "secret-password-one",
			Email: "one@example.com", AccessToken: &accessToken, Quota: 800, UsedQuota: 300,
			Group: "vip", Status: common.UserStatusDisabled, Role: common.RoleAdminUser,
			AffCode: "billing-summary-aff-one",
		},
		{
			Id: 6102, Username: "billing-summary-guest", Password: "secret-password-two",
			Quota: 100, UsedQuota: 20, Group: "default", Status: common.UserStatusEnabled,
			AffCode: "billing-summary-aff-two",
		},
		{
			Id: 6103, Username: "billing-summary-deleted", Password: "secret-password-three",
			Quota: 900, Group: "default", Status: common.UserStatusEnabled, Role: common.RoleCommonUser,
			AffCode: "billing-summary-aff-three",
		},
	}
	for index := range users {
		require.NoError(t, DB.Create(&users[index]).Error)
	}
	require.NoError(t, DB.Model(&User{}).Where("id = ?", users[1].Id).Update("role", common.RoleGuestUser).Error)
	require.NoError(t, DB.Delete(&users[2]).Error)

	rows, err := GetUserBillingSummaryRows(context.Background(), []int{6103, 6102, 6101, 6999})

	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, 6101, rows[0].ID)
	assert.Equal(t, 800, rows[0].Quota)
	assert.Equal(t, 300, rows[0].UsedQuota)
	assert.Equal(t, "vip", rows[0].Group)
	assert.Equal(t, common.UserStatusDisabled, rows[0].Status)
	assert.Equal(t, common.RoleAdminUser, rows[0].Role)
	assert.Equal(t, common.RoleGuestUser, rows[1].Role)

	payload, err := common.Marshal(rows)
	require.NoError(t, err)
	assert.NotContains(t, string(payload), "secret-password")
	assert.NotContains(t, string(payload), "billing-summary-secret-token")
	assert.NotContains(t, string(payload), "one@example.com")
}

func TestGetBillingSummarySubscriptionsAndPlansUseBatchFilters(t *testing.T) {
	truncateTables(t)

	now := GetDBTimestamp()
	plans := []SubscriptionPlan{
		{Id: 6201, Title: "有限套餐", QuotaResetPeriod: SubscriptionResetDaily},
		{Id: 6202, Title: "无限套餐", QuotaResetPeriod: SubscriptionResetNever},
	}
	require.NoError(t, DB.Create(&plans).Error)
	subscriptions := []UserSubscription{
		{Id: 6301, UserId: 6101, PlanId: 6201, AmountTotal: 1000, AmountUsed: 400, StartTime: now - 100, EndTime: now + 1000, Status: "active"},
		{Id: 6302, UserId: 6101, PlanId: 6202, StartTime: now - 100, EndTime: now + 2000, Status: "cancelled"},
		{Id: 6303, UserId: 6101, PlanId: 6202, StartTime: now - 2000, EndTime: now - 1, Status: "active"},
		{Id: 6304, UserId: 6102, PlanId: 6202, StartTime: now - 100, EndTime: now + 3000, Status: "active"},
	}
	require.NoError(t, DB.Create(&subscriptions).Error)

	active, err := GetActiveUserSubscriptionsByUserIDs(context.Background(), []int{6102, 6101}, now)
	require.NoError(t, err)
	require.Len(t, active, 2)
	assert.Equal(t, []int{6301, 6304}, []int{active[0].Id, active[1].Id})

	loadedPlans, err := GetSubscriptionPlansForBillingSummary(context.Background(), []int{6202, 6201, 6202})
	require.NoError(t, err)
	require.Len(t, loadedPlans, 2)
	assert.Equal(t, 6201, loadedPlans[0].Id)
	assert.Equal(t, "有限套餐", loadedPlans[0].Title)
	assert.Equal(t, SubscriptionResetDaily, loadedPlans[0].QuotaResetPeriod)
	assert.Equal(t, 6202, loadedPlans[1].Id)
}
