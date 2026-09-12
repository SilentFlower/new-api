package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func channelBudgetTestConfig(now time.Time) dto.ChannelPeriodPolicyConfig {
	five, fifty, fifteen, hundredFifty, forty := int64(5), int64(50), int64(15), int64(150), int64(40)
	holidayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	return dto.ChannelPeriodPolicyConfig{SchemaVersion: 1, PoolDailyQuotaLimit: 1000, PoolWeeklyQuotaLimit: 5000, Rules: []dto.ChannelPeriodRule{
		{ID: "r2", Name: "整周", Enabled: true, Kind: "weekly", StartWeekday: 0, StartTime: "00:00", EndWeekday: 6, EndTime: "23:59", CreatedAt: 1, UserPeriodQuotaLimit: &forty},
		{ID: "r1", Name: "假期", Enabled: true, Kind: "date_range", StartAt: holidayStart.Unix(), EndAt: holidayStart.AddDate(0, 0, 4).Unix(), CreatedAt: 2, UserDailyQuotaLimit: &five, PoolDailyQuotaLimit: &fifty, UserPeriodQuotaLimit: &fifteen, PoolPeriodQuotaLimit: &hundredFifty},
	}}
}

// TestChannelBudgetPlanKeepsLegacyCounterKeys 保证 v1 派生身份沿用旧 Redis key，规则内日限与无时段日限共用计数。
func TestChannelBudgetPlanKeepsLegacyCounterKeys(t *testing.T) {
	now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
	config := channelBudgetTestConfig(now)
	plan := buildChannelBudgetPlan(dto.ChannelPeriodPolicyView{Revision: 3, Config: config}, 100, 500)
	res, err := resolveChannelBudgetRows(plan, now, "")
	require.NoError(t, err)
	day := time.Date(2031, 9, 16, 0, 0, 0, 0, time.Local)
	monday := time.Date(2031, 9, 15, 0, 0, 0, 0, time.Local)
	rows := make(map[string]channelBudgetRow, len(plan.Rows))
	for _, row := range plan.Rows {
		rows[row.ID] = row
	}
	cases := []struct {
		id, userKey, poolKey string
		start, end           int64
	}{
		{legacyUserDailyBudgetID, "channel_user_daily_quota:{80}:2031-09-16", "channel_pool_daily_quota:{80}:2031-09-16", day.Unix(), day.AddDate(0, 0, 1).Unix()},
		{legacyPoolDailyBudgetID, "channel_user_daily_quota:{80}:2031-09-16", "channel_pool_daily_quota:{80}:2031-09-16", day.Unix(), day.AddDate(0, 0, 1).Unix()},
		{legacyUserWeeklyBudgetID, "channel_user_weekly_quota:{80}:2031-09-15", "channel_pool_weekly_quota:{80}:2031-09-15", monday.Unix(), monday.AddDate(0, 0, 7).Unix()},
		{"rule-r1-user-daily", "channel_user_daily_quota:{80}:2031-09-16", "channel_pool_daily_quota:{80}:2031-09-16", day.Unix(), day.AddDate(0, 0, 1).Unix()},
		{"rule-r1-user-occurrence", fmt.Sprintf("channel_period_quota:{80}:r1:%d", day.Unix()), fmt.Sprintf("channel_period_quota:{80}:r1:%d", day.Unix()), day.Unix(), day.AddDate(0, 0, 4).Unix()},
		{"rule-r2-pool-occurrence", fmt.Sprintf("channel_period_quota:{80}:r2:%d", monday.Unix()), fmt.Sprintf("channel_period_quota:{80}:r2:%d", monday.Unix()), monday.Unix(), monday.AddDate(0, 0, 6).Add(23*time.Hour + 59*time.Minute).Unix()},
	}
	for _, item := range cases {
		row, ok := rows[item.id]
		require.True(t, ok, item.id)
		counter := newChannelBudgetCounter(80, row, res)
		assert.Equal(t, item.userKey, counter.userKey, item.id)
		assert.Equal(t, item.poolKey, counter.poolKey, item.id)
		assert.Equal(t, item.start, counter.start, item.id)
		assert.Equal(t, item.end, counter.end, item.id)
	}
	// 规则内日限只改上限不换计数：与无时段日限同身份。
	assert.Equal(t, rows[legacyUserDailyBudgetID].identityKey(), rows["rule-r1-user-daily"].identityKey())
	assert.Equal(t, rows["rule-r1-user-occurrence"].identityKey(), rows["rule-r1-pool-occurrence"].identityKey())
	// 规则未填写的整段额度只保留形状，不参与生效。
	assert.True(t, rows["rule-r2-pool-occurrence"].implicit)
	assert.False(t, res.eligible(rows["rule-r2-pool-occurrence"]))
	// 同组优先级：date_range > weekly > 无时段。
	effective, ok := res.effectiveRow(plan, rows[legacyUserDailyBudgetID].groupKey())
	require.True(t, ok)
	assert.Equal(t, "rule-r1-user-daily", effective.ID)
	effective, ok = res.effectiveRow(plan, rows["rule-r2-user-occurrence"].groupKey())
	require.True(t, ok)
	assert.Equal(t, "rule-r1-user-occurrence", effective.ID)
	effective, ok = res.effectiveRow(plan, rows[legacyUserWeeklyBudgetID].groupKey())
	require.True(t, ok)
	assert.Equal(t, legacyUserWeeklyBudgetID, effective.ID)
	assert.Equal(t, day.AddDate(0, 0, 4).Unix(), res.next)
	counters := channelBudgetCounters(80, plan, res, res.counting)
	require.Len(t, counters, 4)
	assert.Equal(t, []string{rows[legacyUserDailyBudgetID].identityKey(), rows[legacyUserWeeklyBudgetID].identityKey(), rows["rule-r2-user-occurrence"].identityKey(), rows["rule-r1-user-occurrence"].identityKey()}, []string{counters[0].identity, counters[1].identity, counters[2].identity, counters[3].identity})
}

// TestChannelBudgetModelRowsMatchExactly 保证按模型的行只对原始模型名精确命中，并与渠道级行并行约束。
func TestChannelBudgetModelRowsMatchExactly(t *testing.T) {
	now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
	plan := buildChannelBudgetPlan(dto.ChannelPeriodPolicyView{Revision: 1, Config: dto.ChannelPeriodPolicyConfig{SchemaVersion: 1, PoolDailyQuotaLimit: 600}}, 0, 0)
	modelRow := channelBudgetRow{ID: "m1", Name: "gpt-6-astra", Enabled: true, Scope: channelBudgetScopePool, Window: channelBudgetWindowDaily, Models: []string{"gpt-6-astra"}, Limit: 500, OnExceed: channelBudgetAction{Mode: channelBudgetActionFallback, Model: "gpt-6-mini"}, CreatedAt: now.Unix()}
	plan.Rows = append(plan.Rows, modelRow)
	for _, item := range []struct {
		modelName string
		matched   bool
	}{{"gpt-6-astra", true}, {"gpt-6-Astra", false}, {"gpt-6-astra-2", false}, {"", true}} {
		res, err := resolveChannelBudgetRows(plan, now, item.modelName)
		require.NoError(t, err)
		_, ok := res.effectiveRow(plan, modelRow.groupKey())
		assert.Equal(t, item.matched, ok, item.modelName)
		// 渠道级池子行不因模型行存在而失效。
		overall, ok := res.effectiveRow(plan, plan.Rows[2].groupKey())
		require.True(t, ok, item.modelName)
		assert.Equal(t, legacyPoolDailyBudgetID, overall.ID)
	}
	res, err := resolveChannelBudgetRows(plan, now, "gpt-6-astra")
	require.NoError(t, err)
	counter := newChannelBudgetCounter(80, modelRow, res)
	day := time.Date(2031, 9, 16, 0, 0, 0, 0, time.Local)
	assert.Equal(t, fmt.Sprintf("channel_budget:{80}:%s:%d", modelRow.identityHash(), day.Unix()), counter.poolKey)
	assert.Equal(t, counter.poolKey, counter.userKey)
	assert.Len(t, modelRow.identityHash(), 12)
	// 身份只由周期、时段与模型集合决定：个人行与池子行共用计数。
	userRow := modelRow
	userRow.ID, userRow.Scope = "m2", channelBudgetScopeUser
	assert.Equal(t, modelRow.identityKey(), userRow.identityKey())
	assert.NotEqual(t, modelRow.groupKey(), userRow.groupKey())
	counters := channelBudgetCounters(80, plan, res, res.counting)
	require.Len(t, counters, 3)
	assert.Equal(t, counter.poolKey, counters[2].poolKey)
}

// TestSelectChannelLimitFallbackRowFirst 保证行级降级优先、策略级兜底，同渠道换模型时目标渠道为来源渠道。
func TestSelectChannelLimitFallbackRowFirst(t *testing.T) {
	policyLevel := dto.ChannelPeriodPolicyConfig{Fallback: dto.ChannelLimitFallback{Enabled: true, ChannelID: 9, Model: "gpt-4o-mini"}}
	cases := []struct {
		name    string
		config  dto.ChannelPeriodPolicyConfig
		block   *ChannelPeriodBlock
		target  dto.ChannelLimitFallback
		allowed bool
	}{
		{"nil block inherits policy", policyLevel, nil, policyLevel.Fallback, true},
		{"inherit without policy fallback", dto.ChannelPeriodPolicyConfig{}, &ChannelPeriodBlock{row: channelBudgetRow{OnExceed: channelBudgetAction{Mode: channelBudgetActionInherit}}}, dto.ChannelLimitFallback{}, false},
		{"row reject beats policy", policyLevel, &ChannelPeriodBlock{row: channelBudgetRow{OnExceed: channelBudgetAction{Mode: channelBudgetActionReject}}}, dto.ChannelLimitFallback{}, false},
		{"row same-channel model switch", dto.ChannelPeriodPolicyConfig{}, &ChannelPeriodBlock{row: channelBudgetRow{Models: []string{"gpt-6-astra"}, OnExceed: channelBudgetAction{Mode: channelBudgetActionFallback, Model: "gpt-6-mini"}}}, dto.ChannelLimitFallback{Enabled: true, ChannelID: 80, Model: "gpt-6-mini"}, true},
		{"row explicit channel", dto.ChannelPeriodPolicyConfig{}, &ChannelPeriodBlock{row: channelBudgetRow{OnExceed: channelBudgetAction{Mode: channelBudgetActionFallback, ChannelID: 12, Model: "gpt-6-mini"}}}, dto.ChannelLimitFallback{Enabled: true, ChannelID: 12, Model: "gpt-6-mini"}, true},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			target, allowed := SelectChannelLimitFallback(item.config, 80, item.block)
			assert.Equal(t, item.allowed, allowed)
			assert.Equal(t, item.target, target)
		})
	}
}

// TestChannelBudgetUsageMemoryRedisEquivalence 保证同一结算序列在内存与 Redis 两种存储下产生相同指标。
func TestChannelBudgetUsageMemoryRedisEquivalence(t *testing.T) {
	now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
	// 保存路径由服务端建立规则身份，因此只给出时间与额度。
	config := channelBudgetTestConfig(now)
	for i := range config.Rules {
		config.Rules[i].ID, config.Rules[i].CreatedAt = "", 0
		if config.Rules[i].Kind == "date_range" {
			config.Rules[i].StartLocal, config.Rules[i].EndLocal = "2031-09-16T00:00", "2031-09-20T00:00"
		}
	}
	var results [][]dto.ChannelPeriodMetric
	for _, useRedis := range []bool{false, true} {
		channel := setupChannelPeriodTest(t, &now, useRedis)
		ctx := context.Background()
		_, err := SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
		require.NoError(t, err)
		require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 12, "gpt-6-astra"))
		require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 8, 30, ""))
		status, err := GetChannelPeriodStatus(ctx, channel, 7)
		require.NoError(t, err)
		// 规则身份由服务端随机生成，两次保存不同，比较时抹去。
		for i := range status.Metrics {
			status.Metrics[i].Source.RuleID = ""
		}
		results = append(results, status.Metrics)
	}
	require.Len(t, results[0], 8)
	assert.Equal(t, results[0], results[1])
	assert.Equal(t, int64(12), results[0][0].Used)
	assert.Equal(t, int64(42), results[0][2].Used)
	assert.Equal(t, "custom", results[0][4].Period)
	assert.False(t, results[0][4].Enforced)
	assert.True(t, results[0][6].Enforced)
	assert.Equal(t, int64(12), results[0][6].Used)
}
