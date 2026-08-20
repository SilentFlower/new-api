package model

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestProjectUserSubscriptionCycleAdvancesEveryDuePeriodWithoutWriting(t *testing.T) {
	plan := SubscriptionPlan{
		Id:                      6401,
		QuotaResetPeriod:        SubscriptionResetCustom,
		QuotaResetCustomSeconds: 100,
	}
	subscription := UserSubscription{
		Id: 6501, UserId: 6101, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 600,
		StartTime: 1000, EndTime: 2000, Status: "active", LastResetTime: 1000, NextResetTime: 1100,
	}

	projected, changed, err := ProjectUserSubscriptionCycle(subscription, plan, 1350)

	require.NoError(t, err)
	assert.True(t, changed)
	assert.Zero(t, projected.AmountUsed)
	assert.EqualValues(t, 1300, projected.LastResetTime)
	assert.EqualValues(t, 1400, projected.NextResetTime)
	assert.EqualValues(t, 600, subscription.AmountUsed)
	assert.EqualValues(t, 1100, subscription.NextResetTime)
}

func TestProjectUserSubscriptionCycleInitializesFutureResetTime(t *testing.T) {
	plan := SubscriptionPlan{Id: 6402, QuotaResetPeriod: SubscriptionResetCustom, QuotaResetCustomSeconds: 100}
	subscription := UserSubscription{
		Id: 6502, UserId: 6101, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 200,
		StartTime: 1000, EndTime: 2000, Status: "active",
	}

	projected, changed, err := ProjectUserSubscriptionCycle(subscription, plan, 1050)

	require.NoError(t, err)
	assert.True(t, changed)
	assert.EqualValues(t, 200, projected.AmountUsed)
	assert.EqualValues(t, 1000, projected.LastResetTime)
	assert.EqualValues(t, 1100, projected.NextResetTime)
}

func TestProjectUserSubscriptionCycleRejectsInvalidResetConfiguration(t *testing.T) {
	subscription := UserSubscription{Id: 6503, StartTime: 1000, EndTime: 2000}

	_, _, err := ProjectUserSubscriptionCycle(subscription, SubscriptionPlan{QuotaResetPeriod: "hourly"}, 1100)
	assert.Error(t, err)

	_, _, err = ProjectUserSubscriptionCycle(subscription, SubscriptionPlan{QuotaResetPeriod: SubscriptionResetCustom}, 1100)
	assert.Error(t, err)

	_, _, err = ProjectUserSubscriptionCycle(subscription, SubscriptionPlan{
		QuotaResetPeriod:        SubscriptionResetCustom,
		QuotaResetCustomSeconds: MaxSubscriptionResetCustomSeconds + 1,
	}, 1100)
	assert.Error(t, err)
}

func TestProjectUserSubscriptionCycleJumpsAcrossLongOverdueCustomPeriods(t *testing.T) {
	plan := SubscriptionPlan{
		Id:                      6404,
		QuotaResetPeriod:        SubscriptionResetCustom,
		QuotaResetCustomSeconds: 1,
	}
	subscription := UserSubscription{
		Id: 6505, UserId: 6101, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 900,
		StartTime: 1000, EndTime: 2100000000, Status: "active", LastResetTime: 1000, NextResetTime: 1001,
	}

	projected, changed, err := ProjectUserSubscriptionCycle(subscription, plan, 2000000000)

	require.NoError(t, err)
	assert.True(t, changed)
	assert.Zero(t, projected.AmountUsed)
	assert.EqualValues(t, 2000000000, projected.LastResetTime)
	assert.EqualValues(t, 2000000001, projected.NextResetTime)
}

func TestCalcNextResetTimeRejectsOverflowingCustomCycle(t *testing.T) {
	plan := &SubscriptionPlan{
		QuotaResetPeriod:        SubscriptionResetCustom,
		QuotaResetCustomSeconds: 10,
	}

	assert.Zero(t, calcNextResetTime(time.Unix(math.MaxInt64-5, 0), plan, 0))
}

func TestMaybeResetUserSubscriptionPersistsSharedProjection(t *testing.T) {
	truncateTables(t)

	plan := SubscriptionPlan{
		Id: 6403, Title: "共享投影套餐", QuotaResetPeriod: SubscriptionResetCustom, QuotaResetCustomSeconds: 100,
	}
	require.NoError(t, DB.Create(&plan).Error)
	subscription := UserSubscription{
		Id: 6504, UserId: 6101, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 700,
		StartTime: 1000, EndTime: 2000, Status: "active", LastResetTime: 1000, NextResetTime: 1100,
	}
	require.NoError(t, DB.Create(&subscription).Error)
	expected, changed, err := ProjectUserSubscriptionCycle(subscription, plan, 1250)
	require.NoError(t, err)
	require.True(t, changed)

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return maybeResetUserSubscriptionWithPlanTx(tx, &subscription, &plan, 1250)
	}))

	var stored UserSubscription
	require.NoError(t, DB.First(&stored, subscription.Id).Error)
	assert.Equal(t, expected.AmountUsed, stored.AmountUsed)
	assert.Equal(t, expected.LastResetTime, stored.LastResetTime)
	assert.Equal(t, expected.NextResetTime, stored.NextResetTime)
}
