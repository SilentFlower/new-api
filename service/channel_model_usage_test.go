package service

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// modelUsageRow 构造具有明确模型选择器的预算夹具。
func modelUsageRow(scope, window string, models ...string) dto.ChannelBudgetRow {
	row := budgetRow("模型预算", scope, window, "", 100)
	row.Models = models
	return row
}

// TestRecordRelayChannelUserQuotaUsageUsesRoutingModel 验证降级按目标路由模型累计，普通模型映射仍按原始路由模型累计。
// @param t 测试上下文。
func TestRecordRelayChannelUserQuotaUsageUsesRoutingModel(t *testing.T) {
	for index, test := range []struct {
		name         string
		routingModel string
		expected     map[string]int64
	}{
		{name: "额度降级", routingModel: "B", expected: map[string]int64{"A": 0, "B": 7, "provider-alias": 0}},
		{name: "普通模型映射", routingModel: "", expected: map[string]int64{"A": 7, "B": 0, "provider-alias": 0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2031, 9, 16+index, 10, 0, 0, 0, time.Local)
			channel := setupChannelPeriodTest(t, &now, false)
			config := budgetConfig(nil,
				modelUsageRow("pool", "daily", "A"),
				modelUsageRow("pool", "daily", "B"),
				modelUsageRow("pool", "daily", "provider-alias"),
			)
			view, err := SaveChannelPeriodPolicy(t.Context(), channel.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{
				UserId:           7,
				OriginModelName:  "A",
				RoutingModelName: test.routingModel,
				ChannelMeta:      &relaycommon.ChannelMeta{ChannelId: channel.Id, UpstreamModelName: "provider-alias", IsModelMapped: true},
			}
			RecordRelayChannelUserQuotaUsage(t.Context(), info, 7)
			actual := make(map[string]int64)
			for _, row := range view.Config.Budgets {
				if len(row.Models) == 0 {
					continue
				}
				require.Len(t, row.Models, 1)
				usage, err := GetChannelBudgetUsage(t.Context(), channel.Id, row.ID, "pool", 0, 20)
				require.NoError(t, err)
				actual[row.Models[0]] = usage.UsedQuota
			}
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestChannelModelUsageBeforeBudgetAndAdjustmentIsolation(t *testing.T) {
	for _, useRedis := range []bool{false, true} {
		t.Run(fmt.Sprint(useRedis), func(t *testing.T) {
			now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
			channel := setupChannelPeriodTest(t, &now, useRedis)
			ctx := context.Background()
			enabled := true
			config := budgetConfig(nil)
			config.ModelUsageTrackingEnabled = &enabled
			view, err := SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
			require.NoError(t, err)
			started := now.Unix()
			require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 40, "A"))
			require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 20, "B"))
			require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 8, 10, "A"))
			now = now.Add(time.Hour)
			view.Config.Budgets = append(view.Config.Budgets,
				modelUsageRow("user", "daily", "A", "B"), modelUsageRow("pool", "daily", "A", "B"),
				modelUsageRow("user", "daily", "A"), modelUsageRow("user", "weekly", "A", "B"))
			view, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
			require.NoError(t, err)
			for _, row := range view.Config.Budgets {
				if len(row.Models) > 0 {
					assert.Equal(t, channelModelCounterSource, row.CounterSource)
				}
			}
			ids := view.Config.Budgets
			// setup 的旧渠道日周行由首次保存保留为停用，使用末尾新建行。
			ids = ids[len(ids)-4:]
			status, err := GetChannelPeriodStatus(ctx, channel, 7)
			require.NoError(t, err)
			require.Len(t, status.Metrics, 4)
			assert.Equal(t, []int64{60, 70, 40, 60}, []int64{status.Metrics[0].Used, status.Metrics[1].Used, status.Metrics[2].Used, status.Metrics[3].Used})
			assert.Equal(t, started, status.Metrics[0].TrackingSince)
			assert.Equal(t, int64(40), *status.Metrics[0].Remaining)
			require.NoError(t, SetChannelBudgetUsage(ctx, channel.Id, ids[0].ID, dto.ChannelBudgetUsageInput{Scope: "user", UserID: 7, UsedQuota: 0}))
			list, err := GetChannelBudgetUsage(ctx, channel.Id, ids[0].ID, "user", 0, 100)
			require.NoError(t, err)
			require.Len(t, list.Items, 1)
			assert.Equal(t, 8, list.Items[0].UserID)
			require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 5, "B"))
			status, err = GetChannelPeriodStatus(ctx, channel, 7)
			require.NoError(t, err)
			assert.Equal(t, []int64{5, 75, 40, 65}, []int64{status.Metrics[0].Used, status.Metrics[1].Used, status.Metrics[2].Used, status.Metrics[3].Used})
			require.NoError(t, SetChannelBudgetUsage(ctx, channel.Id, ids[1].ID, dto.ChannelBudgetUsageInput{Scope: "pool", UsedQuota: 2}))
			require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 3, "A"))
			status, err = GetChannelPeriodStatus(ctx, channel, 7)
			require.NoError(t, err)
			assert.Equal(t, []int64{8, 5, 43, 68}, []int64{status.Metrics[0].Used, status.Metrics[1].Used, status.Metrics[2].Used, status.Metrics[3].Used})
			list, err = GetChannelBudgetUsage(ctx, channel.Id, ids[0].ID, "user", 0, 100)
			require.NoError(t, err)
			require.Len(t, list.Items, 2)
			assert.Equal(t, int64(8), list.Items[1].UsedQuota)
			// 下个日窗口不继承人工目标，周窗口继续累计。
			now = now.AddDate(0, 0, 1)
			status, err = GetChannelPeriodStatus(ctx, channel, 7)
			require.NoError(t, err)
			assert.Equal(t, int64(0), status.Metrics[0].Used)
			assert.Equal(t, int64(68), status.Metrics[3].Used)
		})
	}
}

func TestChannelModelUsageTogglePreservesLegacyAndCoverage(t *testing.T) {
	for _, useRedis := range []bool{false, true} {
		t.Run(fmt.Sprint(useRedis), func(t *testing.T) {
			now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
			channel := setupChannelPeriodTest(t, &now, useRedis)
			ctx := context.Background()
			view, err := SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{Config: budgetConfig(nil, modelUsageRow("user", "daily", "A", "B"))}, 1)
			require.NoError(t, err)
			legacyID := view.Config.Budgets[0].ID
			require.Nil(t, view.Config.ModelUsageTrackingEnabled)
			require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 30, "A"))
			require.NoError(t, SetChannelBudgetUsage(ctx, channel.Id, legacyID, dto.ChannelBudgetUsageInput{Scope: "user", UserID: 7, UsedQuota: 90}))
			enabled := true
			view.Config.ModelUsageTrackingEnabled = &enabled
			view, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
			require.NoError(t, err)
			require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 10, "A"))
			require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 20, "C"))
			// 旧客户端省略新增字段不能关闭开关，也不能重选已有身份的来源。
			view.Config.ModelUsageTrackingEnabled = nil
			view.Config.ModelUsageTracking = nil
			view.Config.Budgets = append(view.Config.Budgets, modelUsageRow("pool", "daily", "A", "B"), modelUsageRow("user", "daily", "A"))
			view, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
			require.NoError(t, err)
			require.NotNil(t, view.Config.ModelUsageTrackingEnabled)
			assert.True(t, *view.Config.ModelUsageTrackingEnabled)
			assert.Empty(t, view.Config.Budgets[len(view.Config.Budgets)-2].CounterSource)
			status, err := GetChannelPeriodStatus(ctx, channel, 7)
			require.NoError(t, err)
			assert.Equal(t, int64(100), status.Metrics[0].Used)
			assert.True(t, status.Blocked)
			now = now.Add(time.Hour)
			disabled := false
			view.Config.ModelUsageTrackingEnabled = &disabled
			view, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
			require.NoError(t, err)
			require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 5, "A"))
			require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 99, "C"))
			now = now.Add(time.Hour)
			view.Config.ModelUsageTrackingEnabled = &enabled
			view.Config.Budgets = append(view.Config.Budgets, modelUsageRow("user", "daily", "C"))
			view, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
			require.NoError(t, err)
			require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 2, "C"))
			status, err = GetChannelPeriodStatus(ctx, channel, 7)
			require.NoError(t, err)
			assert.Equal(t, int64(105), status.Metrics[0].Used)
			assert.Equal(t, int64(15), status.Metrics[2].Used)
			assert.Equal(t, int64(22), status.Metrics[3].Used)
			assert.Equal(t, "incomplete", status.Metrics[3].Coverage)
		})
	}
}

func TestChannelModelUsageAtomicFailureAndConcurrentAdjustment(t *testing.T) {
	for _, useRedis := range []bool{false, true} {
		t.Run(fmt.Sprint(useRedis), func(t *testing.T) {
			now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
			channel := setupChannelPeriodTest(t, &now, useRedis)
			ctx := context.Background()
			enabled := true
			config := budgetConfig(nil, modelUsageRow("user", "daily", "A", "B"))
			config.ModelUsageTrackingEnabled = &enabled
			view, err := SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
			require.NoError(t, err)
			row := channelBudgetRow{ChannelBudgetRow: view.Config.Budgets[0]}
			res, err := resolveChannelBudgetRows(buildChannelBudgetPlan(view), now, "")
			require.NoError(t, err)
			counter := newChannelBudgetCounter(channel.Id, row, res)
			require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 40, "A"))
			var wg sync.WaitGroup
			start := make(chan struct{})
			errs := make(chan error, 2)
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				errs <- SetChannelBudgetUsage(ctx, channel.Id, row.ID, dto.ChannelBudgetUsageInput{Scope: "user", UserID: 7, UsedQuota: 0})
			}()
			go func() { defer wg.Done(); <-start; errs <- RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 5, "A") }()
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			user, pool, _, err := readChannelBudgetCounter(ctx, counter, 7, now)
			require.NoError(t, err)
			assert.Contains(t, []int64{0, 5}, user)
			assert.Equal(t, int64(45), pool)
			require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 3, "A"))
			after, _, _, err := readChannelBudgetCounter(ctx, counter, 7, now)
			require.NoError(t, err)
			assert.Equal(t, user+3, after)
			// 一个事实桶溢出时，整体日周和其他事实都不得部分累计。
			if useRedis {
				require.NoError(t, common.RDB.HSet(ctx, counter.modelKeys[0], "__pool", math.MaxInt64).Err())
			} else {
				channelPeriodUsageMemory.values[counter.modelKeys[0]].values["__pool"] = math.MaxInt64
			}
			require.Error(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 1, "A"))
			daily, _, _, err := GetChannelUserDailyQuotaUsage(ctx, channel.Id, 7)
			require.NoError(t, err)
			assert.Equal(t, int64(48), daily)
		})
	}
}

func TestChannelModelUsageCoverageAcrossDisabledWindow(t *testing.T) {
	now := time.Date(2031, 9, 23, 10, 0, 0, 0, time.Local)
	tracking := &dto.ChannelModelUsageTracking{FirstEnabledAt: now.AddDate(0, 0, -10).Unix(), EnabledAt: now.Unix(), DisabledAt: now.AddDate(0, 0, -5).Unix()}
	row := channelBudgetRow{ChannelBudgetRow: modelUsageRow("user", "weekly", "A")}
	row.CounterSource = channelModelCounterSource
	counter := newChannelBudgetCounter(80, row, channelBudgetResolution{now: now, tracking: tracking})
	assert.True(t, counter.trackingGap, "关闭区段跨过周一时，本周仍应标记缺口")
	counter = newChannelBudgetCounter(80, row, channelBudgetResolution{now: now.AddDate(0, 0, 7), tracking: tracking})
	assert.False(t, counter.trackingGap, "重开后的完整新窗口不沿用旧缺口")
}

func TestChannelModelUsageAggregatePrecisionAndCorruption(t *testing.T) {
	facts := []map[string]string{{"7": "9007199254740993"}, {"7": "9"}}
	used, err := channelModelBudgetValue("7", facts, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(9007199254741002), used)
	used, err = channelModelBudgetValue("7", facts, map[string]string{"7": `["2","9007199254740991","8"]`})
	require.NoError(t, err)
	assert.Equal(t, int64(5), used)
	for _, raw := range []string{"-1", "01", "+1", "9223372036854775808", "bad"} {
		_, err = channelModelBudgetValue("7", []map[string]string{{"7": raw}}, nil)
		require.Error(t, err, raw)
	}
	_, err = channelModelBudgetValue("7", []map[string]string{{"7": "9223372036854775807"}, {"7": "1"}}, nil)
	require.Error(t, err)
	_, err = channelModelBudgetValue("7", facts, map[string]string{"7": `["0","9007199254740994","0"]`})
	require.Error(t, err, "源计数回退不能用负数抵扣")
}

func TestChannelModelUsageDefaultOffAndPositiveSettlementOnly(t *testing.T) {
	for _, useRedis := range []bool{false, true} {
		t.Run(fmt.Sprint(useRedis), func(t *testing.T) {
			now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
			channel := setupChannelPeriodTest(t, &now, useRedis)
			ctx := context.Background()
			view, err := SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{Config: budgetConfig(nil)}, 1)
			require.NoError(t, err)
			require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 30, "A"))
			enabled := true
			view.Config.ModelUsageTrackingEnabled = &enabled
			view, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
			require.NoError(t, err)
			for _, quota := range []int{0, -10} {
				require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, quota, "A"))
			}
			require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 9, "A"))
			require.NoError(t, RecordChannelUserQuotaUsage(ctx, channel.Id, 7, 3))
			view.Config.Budgets = append(view.Config.Budgets, modelUsageRow("user", "daily", "A"))
			view, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
			require.NoError(t, err)
			status, err := GetChannelPeriodStatus(ctx, channel, 7)
			require.NoError(t, err)
			require.Len(t, status.Metrics, 1)
			assert.Equal(t, int64(9), status.Metrics[0].Used)
			assert.Equal(t, "incomplete", status.Metrics[0].Coverage)
			now = now.AddDate(0, 0, 8)
			require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 1, "A"))
			status, err = GetChannelPeriodStatus(ctx, channel, 7)
			require.NoError(t, err)
			assert.Equal(t, int64(1), status.Metrics[0].Used)
		})
	}
}

func TestChannelModelUsageChannelIsolationAndReadFailure(t *testing.T) {
	for _, useRedis := range []bool{false, true} {
		t.Run(fmt.Sprint(useRedis), func(t *testing.T) {
			now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
			channel := setupChannelPeriodTest(t, &now, useRedis)
			other := *channel
			other.Id = 81
			require.NoError(t, model.DB.Create(&other).Error)
			ctx := context.Background()
			enabled, disabled := true, false
			views := make([]dto.ChannelPeriodPolicyView, 2)
			for i, ch := range []*model.Channel{channel, &other} {
				config := budgetConfig(nil)
				if i == 0 {
					config.ModelUsageTrackingEnabled = &enabled
				} else {
					config.ModelUsageTrackingEnabled = &disabled
				}
				view, err := SaveChannelPeriodPolicy(ctx, ch.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
				require.NoError(t, err)
				require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, ch.Id, 7, 60, "A"))
				view.Config.Budgets = append(view.Config.Budgets, modelUsageRow("user", "daily", "A"))
				views[i], err = SaveChannelPeriodPolicy(ctx, ch.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
				require.NoError(t, err)
				status, err := GetChannelPeriodStatus(ctx, ch, 7)
				require.NoError(t, err)
				assert.Equal(t, []int64{60, 0}[i], status.Metrics[0].Used)
			}
			view := views[0]
			tracked := len(view.Config.Budgets) - 1
			view.Config.Budgets[tracked].Enabled = false
			view.Config.ModelUsageTrackingEnabled = &disabled
			view, err := SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
			require.NoError(t, err)
			require.NoError(t, RecordChannelUserModelQuotaUsage(ctx, channel.Id, 7, 5, "A"))
			view.Config.Budgets[tracked].Enabled = true
			view, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
			require.NoError(t, err)
			usage, err := GetChannelBudgetUsage(ctx, channel.Id, view.Config.Budgets[tracked].ID, "user", 0, 20)
			require.NoError(t, err)
			assert.Equal(t, int64(65), usage.Items[0].UsedQuota)
			if useRedis {
				res, err := resolveChannelBudgetRows(buildChannelBudgetPlan(view), now, "")
				require.NoError(t, err)
				counter := newChannelBudgetCounter(channel.Id, channelBudgetRow{ChannelBudgetRow: view.Config.Budgets[tracked]}, res)
				require.NoError(t, common.RDB.Del(ctx, counter.modelKeys[0]).Err())
				require.NoError(t, common.RDB.Set(ctx, counter.modelKeys[0], "损坏的模型事实", 0).Err())
				_, err = GetChannelPeriodStatus(ctx, channel, 7)
				require.Error(t, err, "Redis 错误不能被读成零用量")
				require.Error(t, SetChannelBudgetUsage(ctx, channel.Id, view.Config.Budgets[tracked].ID, dto.ChannelBudgetUsageInput{Scope: "user", UserID: 7, UsedQuota: 0}))
				exists, err := common.RDB.Exists(ctx, counter.poolKey).Result()
				require.NoError(t, err)
				assert.Zero(t, exists, "源读取失败不得创建调整")
			}
		})
	}
}
