package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChannelBudgetUnknownStoredVersion 验证未知或缺失的持久化版本不会变成宽松 v2 策略。
// @param t 测试上下文。
func TestChannelBudgetUnknownStoredVersion(t *testing.T) {
	for _, raw := range []string{`{"schema_version":3,"budgets":[]}`, `{"schema_version":0}`, `{}`, `null`, ""} {
		t.Run(raw, func(t *testing.T) {
			now := time.Now()
			channel := setupChannelPeriodTest(t, &now, false)
			require.NoError(t, model.ReplaceChannelPeriodPolicy(t.Context(), &model.ChannelPeriodPolicy{ChannelId: channel.Id, Config: raw}))
			_, err := GetChannelPeriodPolicy(t.Context(), channel.Id)
			require.Error(t, err)
			assert.Equal(t, http.StatusServiceUnavailable, ChannelPeriodAPIError(err).StatusCode)
			MigrateChannelBudgetPolicies(t.Context())
			stored, err := model.GetChannelPeriodPolicy(t.Context(), channel.Id)
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, raw, stored.Config)
			assert.Equal(t, 1, stored.Revision)
		})
	}
}

// TestChannelBudgetModelChangeTrackingStart 验证模型身份变化后按实际修改时间统计，改名或调额不改变起点。
// @param t 测试上下文。
func TestChannelBudgetModelChangeTrackingStart(t *testing.T) {
	for _, useRedis := range []bool{false, true} {
		for _, window := range []string{"daily", "weekly", "occurrence"} {
			t.Run(window+"/"+map[bool]string{false: "memory", true: "redis"}[useRedis], func(t *testing.T) {
				now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
				channel := setupChannelPeriodTest(t, &now, useRedis)
				config := poolDailyConfig(100)
				config.Budgets[0].Models = []string{"gpt-6-astra"}
				config.Budgets[0].Window = window
				if window == "occurrence" {
					config.Schedules = []dto.ChannelBudgetSchedule{dateRangeSchedule("new-period", "假期", "2031-09-16T00:00", "2031-09-20T00:00")}
					config.Budgets[0].ScheduleID = "new-period"
				}
				view, err := SaveChannelPeriodPolicy(t.Context(), channel.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
				require.NoError(t, err)
				require.NoError(t, RecordChannelUserModelQuotaUsage(t.Context(), channel.Id, 7, 40, "gpt-6-astra"))
				now = now.Add(time.Hour)
				view.Config.Budgets[0].Models = []string{"gpt-6-mini"}
				view, err = SaveChannelPeriodPolicy(t.Context(), channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
				require.NoError(t, err)
				changedAt := now.Unix()
				usage, err := GetChannelBudgetUsage(t.Context(), channel.Id, view.Config.Budgets[0].ID, "pool", 0, 20)
				require.NoError(t, err)
				assert.Zero(t, usage.UsedQuota)
				assert.Equal(t, changedAt, usage.TrackingSince)
				require.NoError(t, RecordChannelUserModelQuotaUsage(t.Context(), channel.Id, 7, 12, "gpt-6-mini"))
				now = now.Add(time.Hour)
				view.Config.Budgets[0].Name, view.Config.Budgets[0].Limit = "调整后的预算", 200
				view, err = SaveChannelPeriodPolicy(t.Context(), channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
				require.NoError(t, err)
				usage, err = GetChannelBudgetUsage(t.Context(), channel.Id, view.Config.Budgets[0].ID, "pool", 0, 20)
				require.NoError(t, err)
				assert.Equal(t, int64(12), usage.UsedQuota)
				assert.Equal(t, changedAt, usage.TrackingSince)
			})
		}
	}
}

// TestChannelBudgetUsagePageHighestFirst 验证预算表的用户摘要取全局最高用量，分页同值顺序稳定。
// @param t 测试上下文。
func TestChannelBudgetUsagePageHighestFirst(t *testing.T) {
	for _, useRedis := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "redis"}[useRedis], func(t *testing.T) {
			now := time.Now()
			channel := setupChannelPeriodTest(t, &now, useRedis)
			config := poolDailyConfig(100)
			config.Budgets[0].Scope = "user"
			view, err := SaveChannelPeriodPolicy(t.Context(), channel.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
			require.NoError(t, err)
			for _, item := range []dto.ChannelBudgetUsageItem{{UserID: 1, UsedQuota: 10}, {UserID: 7, UsedQuota: 80}, {UserID: 5, UsedQuota: 80}} {
				require.NoError(t, RecordChannelUserQuotaUsage(t.Context(), channel.Id, item.UserID, int(item.UsedQuota)))
			}
			first, err := GetChannelBudgetUsage(t.Context(), channel.Id, view.Config.Budgets[0].ID, "user", 0, 1)
			require.NoError(t, err)
			assert.Equal(t, 3, first.Total)
			assert.Equal(t, []dto.ChannelBudgetUsageItem{{UserID: 5, UsedQuota: 80}}, first.Items)
			remaining, err := GetChannelBudgetUsage(t.Context(), channel.Id, view.Config.Budgets[0].ID, "user", 1, 2)
			require.NoError(t, err)
			assert.Equal(t, []dto.ChannelBudgetUsageItem{{UserID: 7, UsedQuota: 80}, {UserID: 1, UsedQuota: 10}}, remaining.Items)
		})
	}
}
