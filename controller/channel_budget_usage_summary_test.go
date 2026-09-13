package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChannelBudgetUsageSummaryContract 验证聚合用量一次覆盖全部行：池子汇总、个人行最高用量用户、提额后的生效上限，且不推进策略版本。
func TestChannelBudgetUsageSummaryContract(t *testing.T) {
	db := setupChannelUserLimitsTestDB(t)
	require.NoError(t, db.Create(&model.Channel{Id: 81, Name: "预算渠道", Status: common.ChannelStatusEnabled, Models: "gpt-6-astra,gpt-6-mini", Group: "default"}).Error)
	require.NoError(t, db.Create(&model.User{Id: 77, Username: "budget-user", DisplayName: "预算用户", AffCode: "aff-77"}).Error)
	require.NoError(t, db.Create(&model.User{Id: 78, Username: "budget-other", DisplayName: "其他用户", AffCode: "aff-78"}).Error)
	row := func(name, scope, window string, limit int64, models ...string) dto.ChannelBudgetRow {
		if models == nil {
			models = []string{}
		}
		return dto.ChannelBudgetRow{Name: name, Enabled: true, Scope: scope, Window: window, Models: models, Limit: limit, OnExceed: dto.ChannelBudgetAction{Mode: "inherit"}}
	}
	view, err := service.SaveChannelPeriodPolicy(t.Context(), 81, dto.ChannelPeriodPolicyInput{Config: dto.ChannelPeriodPolicyConfig{SchemaVersion: 2, DefaultOnExceed: dto.ChannelBudgetAction{Mode: "reject"}, Schedules: []dto.ChannelBudgetSchedule{}, Budgets: []dto.ChannelBudgetRow{
		row("个人每日", "user", "daily", 100),
		row("池子每日", "pool", "daily", 1000),
		row("gpt-6-astra 每日", "pool", "daily", 500, "gpt-6-astra"),
		row("个人每周", "user", "weekly", 0),
	}}}, 1)
	require.NoError(t, err)
	userRow := view.Config.Budgets[0].ID
	require.NoError(t, service.RecordChannelUserModelQuotaUsage(t.Context(), 81, 77, 40, "gpt-6-astra"))
	require.NoError(t, service.RecordChannelUserModelQuotaUsage(t.Context(), 81, 78, 15, "gpt-6-mini"))
	require.NoError(t, service.ReplaceChannelUserBudgetOverride(t.Context(), 81, 77, userRow, dto.ChannelBudgetUserOverrideInput{Limit: 250, ExpiresAt: 0}, 1))

	c, recorder := newChannelUserLimitTestContext(http.MethodGet, "/api/channel/81/budgets/usage-summary", "", gin.Params{{Key: "id", Value: "81"}})
	GetChannelBudgetUsageSummary(c)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var summary struct {
		Success bool                              `json:"success"`
		Data    dto.ChannelBudgetUsageSummaryView `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &summary))
	require.True(t, summary.Success)
	assert.Equal(t, view.Revision, summary.Data.Revision)
	require.Len(t, summary.Data.Items, 4)

	// 个人日行：最高用量用户为 77，生效上限来自提额，池子汇总为两名用户合计。
	personal := summary.Data.Items[0]
	assert.Equal(t, userRow, personal.BudgetID)
	assert.Equal(t, "user", personal.Scope)
	assert.Equal(t, int64(40), personal.UsedQuota)
	assert.Equal(t, int64(55), personal.PoolUsedQuota)
	require.NotNil(t, personal.TopUser)
	assert.Equal(t, 77, personal.TopUser.UserID)
	assert.Equal(t, "budget-user", personal.TopUser.Username)
	assert.Equal(t, "预算用户", personal.TopUser.DisplayName)
	assert.Equal(t, int64(250), personal.TopUser.EffectiveLimit)
	assert.True(t, personal.TopUser.Override)

	// 池子日行与模型行各自读取自己的计数身份。
	assert.Equal(t, int64(55), summary.Data.Items[1].UsedQuota)
	assert.Nil(t, summary.Data.Items[1].TopUser)
	assert.Equal(t, int64(40), summary.Data.Items[2].UsedQuota)

	// 周行尚无用量：个人行没有最高用户、上限 0 表示不限，不能伪造用户。
	weekly := summary.Data.Items[3]
	assert.Equal(t, int64(55), weekly.PoolUsedQuota)
	assert.Equal(t, int64(40), weekly.UsedQuota)
	require.NotNil(t, weekly.TopUser)
	assert.Equal(t, int64(0), weekly.TopUser.EffectiveLimit)
	assert.False(t, weekly.TopUser.Override)

	after, err := service.GetChannelPeriodPolicy(t.Context(), 81)
	require.NoError(t, err)
	assert.Equal(t, view.Revision, after.Revision)
}
