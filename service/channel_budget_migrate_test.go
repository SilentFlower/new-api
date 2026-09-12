package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChannelBudgetUnmigratedChannelLimits 验证无策略记录时旧日/周上限仍拦截，保存 v2 后不再读取旧列。
// @param t 测试上下文。
// @return 无。
func TestChannelBudgetUnmigratedChannelLimits(t *testing.T) {
	for _, useRedis := range []bool{false, true} {
		for _, window := range []string{"daily", "weekly"} {
			t.Run(fmt.Sprintf("redis=%t/%s", useRedis, window), func(t *testing.T) {
				now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
				channel := setupChannelPeriodTest(t, &now, useRedis)
				limit := 100
				code := types.ErrorCodeChannelUserDailyQuotaExceeded
				if window == "weekly" {
					limit, code = 500, types.ErrorCodeChannelUserWeeklyQuotaExceeded
					channel.UserDailyQuotaLimit = nil
					require.NoError(t, model.DB.Model(channel).Update("user_daily_quota_limit", nil).Error)
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
				common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
				common.SetContextKey(c, constant.ContextKeyUserId, 7)
				common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-6-astra")
				c.Set("channel_period_base_channel", channel)
				require.NoError(t, RecordChannelUserModelQuotaUsage(c, channel.Id, 7, limit-1, "gpt-6-astra"))
				require.Nil(t, CheckSelectedChannelPeriodLimits(c))
				require.NoError(t, RecordChannelUserModelQuotaUsage(c, channel.Id, 7, 1, "gpt-6-astra"))
				apiErr := CheckSelectedChannelPeriodLimits(c)
				require.NotNil(t, apiErr)
				assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
				assert.Equal(t, code, apiErr.GetErrorCode())
				stored, err := model.GetChannelPeriodPolicy(c, channel.Id)
				require.NoError(t, err)
				assert.Nil(t, stored)
				view, err := GetChannelPeriodPolicy(c, channel.Id)
				require.NoError(t, err)
				assert.Zero(t, view.Revision)
				// 显式停用预算后，以保存的 v2 为准，不能复活旧渠道列。
				for i := range view.Config.Budgets {
					view.Config.Budgets[i].Enabled = false
				}
				_, err = SaveChannelPeriodPolicy(c, channel.Id, dto.ChannelPeriodPolicyInput{Config: view.Config}, 1)
				require.NoError(t, err)
				require.Nil(t, CheckSelectedChannelPeriodLimits(c))
			})
		}
	}
}

// TestMigrateChannelBudgetPoliciesConvertsLegacyState 验证 v1 策略、渠道列与两张旧覆盖表迁入 v2 且幂等。
func TestMigrateChannelBudgetPoliciesConvertsLegacyState(t *testing.T) {
	now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
	channel := setupChannelPeriodTest(t, &now, false)
	ruleID := strings.Repeat("a", 32)
	start, end := time.Date(2031, 9, 16, 0, 0, 0, 0, time.Local), time.Date(2031, 9, 20, 0, 0, 0, 0, time.Local)
	v1 := dto.ChannelPeriodPolicyConfigV1{SchemaVersion: 1, PoolDailyQuotaLimit: 100, Rules: []dto.ChannelPeriodRuleV1{{ID: ruleID, Name: "假期", Enabled: true, Kind: "date_range", StartLocal: "2031-09-16T00:00", EndLocal: "2031-09-20T00:00", StartAt: start.Unix(), EndAt: end.Unix(), CreatedAt: 1, UserDailyQuotaLimit: common.GetPointer(int64(5)), UserPeriodQuotaLimit: common.GetPointer(int64(15))}}, Fallback: dto.ChannelLimitFallback{Enabled: true, ChannelID: 99, Model: "gpt-4o-mini"}}
	raw, err := common.Marshal(v1)
	require.NoError(t, err)
	require.NoError(t, model.ReplaceChannelPeriodPolicy(t.Context(), &model.ChannelPeriodPolicy{ChannelId: channel.Id, Config: string(raw), UpdatedBy: 1}))
	require.NoError(t, model.ReplaceChannelUserLimitOverride(&model.ChannelUserLimitOverride{ChannelId: channel.Id, UserId: 7, UserDailyQuotaLimit: common.GetPointer(200), UserWeeklyQuotaLimit: common.GetPointer(900), UpdatedBy: 1}))
	require.NoError(t, model.ReplaceChannelUserPeriodOverride(t.Context(), &model.ChannelUserPeriodOverride{ChannelId: channel.Id, UserId: 7, RuleId: ruleID, UserPeriodQuotaLimit: 30, UpdatedBy: 1}))
	// 只有渠道列、没有策略的渠道也要生成 v2 行；两者都没有的渠道跳过。
	require.NoError(t, model.DB.Create(&model.Channel{Id: 81, Name: "仅渠道列", UserDailyQuotaLimit: common.GetPointer(700)}).Error)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 82, Name: "无限制"}).Error)

	MigrateChannelBudgetPolicies(t.Context())

	view, err := GetChannelPeriodPolicy(t.Context(), channel.Id)
	require.NoError(t, err)
	assert.Equal(t, 2, view.Revision)
	assert.Equal(t, dto.ChannelBudgetAction{Mode: "fallback", ChannelID: 99, Model: "gpt-4o-mini"}, view.Config.DefaultOnExceed)
	require.Len(t, view.Config.Schedules, 1)
	assert.Equal(t, ruleID, view.Config.Schedules[0].ID)
	limits := make(map[string]int64, len(view.Config.Budgets))
	for _, row := range view.Config.Budgets {
		limits[row.ID] = row.Limit
	}
	assert.Equal(t, map[string]int64{legacyUserDailyBudgetID: 100, legacyUserWeeklyBudgetID: 500, legacyPoolDailyBudgetID: 100, "rule-" + ruleID + "-user-daily": 5, "rule-" + ruleID + "-user-occurrence": 15}, limits)
	overrides, err := model.ListActiveChannelUserBudgetOverrides(t.Context(), channel.Id, 7, now.Unix())
	require.NoError(t, err)
	copied := make(map[string]int64, len(overrides))
	for _, item := range overrides {
		copied[item.BudgetId] = item.QuotaLimit
	}
	assert.Equal(t, map[string]int64{legacyUserDailyBudgetID: 200, legacyUserWeeklyBudgetID: 900, "rule-" + ruleID + "-user-occurrence": 30}, copied)
	// 迁移后的策略直接参与判定：个人提额 200 覆盖渠道列 100。
	status, err := GetChannelPeriodStatus(t.Context(), channel, 7)
	require.NoError(t, err)
	daily, ok := findMetric(status, "user", "daily", true)
	require.True(t, ok)
	assert.Equal(t, int64(200), daily.Limit)
	assert.Equal(t, int64(5), daily.BaseLimit)

	other, err := GetChannelPeriodPolicy(t.Context(), 81)
	require.NoError(t, err)
	require.Len(t, other.Config.Budgets, 1)
	assert.Equal(t, int64(700), other.Config.Budgets[0].Limit)
	none, err := GetChannelPeriodPolicy(t.Context(), 82)
	require.NoError(t, err)
	assert.Zero(t, none.Revision)

	// 重复启动不改版本、不重复复制提额。
	MigrateChannelBudgetPolicies(t.Context())
	again, err := GetChannelPeriodPolicy(t.Context(), channel.Id)
	require.NoError(t, err)
	assert.Equal(t, 2, again.Revision)
	overrides, err = model.ListActiveChannelUserBudgetOverrides(t.Context(), channel.Id, 7, now.Unix())
	require.NoError(t, err)
	assert.Len(t, overrides, 3)
}

// TestGetChannelPeriodPolicyConvertsUnmigratedV1OnRead 验证尚未迁移的 v1 存储在读取时按渠道列兜底转换，不改写存储。
func TestGetChannelPeriodPolicyConvertsUnmigratedV1OnRead(t *testing.T) {
	now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
	channel := setupChannelPeriodTest(t, &now, false)
	raw, err := common.Marshal(dto.ChannelPeriodPolicyConfigV1{SchemaVersion: 1, PoolWeeklyQuotaLimit: 4000, Rules: []dto.ChannelPeriodRuleV1{}})
	require.NoError(t, err)
	require.NoError(t, model.ReplaceChannelPeriodPolicy(t.Context(), &model.ChannelPeriodPolicy{ChannelId: channel.Id, Config: string(raw), UpdatedBy: 1}))
	view, err := GetChannelPeriodPolicy(t.Context(), channel.Id)
	require.NoError(t, err)
	assert.Equal(t, 1, view.Revision)
	assert.Equal(t, channelBudgetSchemaVersion, view.Config.SchemaVersion)
	limits := make(map[string]int64, len(view.Config.Budgets))
	for _, row := range view.Config.Budgets {
		limits[row.ID] = row.Limit
	}
	assert.Equal(t, map[string]int64{legacyUserDailyBudgetID: 100, legacyUserWeeklyBudgetID: 500, legacyPoolWeeklyBudgetID: 4000}, limits)
	stored, err := model.GetChannelPeriodPolicy(t.Context(), channel.Id)
	require.NoError(t, err)
	assert.Contains(t, stored.Config, `"schema_version":1`)
}
