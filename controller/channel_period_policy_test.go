package controller

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelPeriodPolicyManagementContract(t *testing.T) {
	db := setupChannelUserLimitsTestDB(t)
	channel := model.Channel{Id: 80, Name: "合同渠道", Key: "must-not-leak", Status: common.ChannelStatusEnabled, Models: "gpt-6-astra,gpt-6-mini", Group: "default"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.User{Id: 77, Username: "period-user", DisplayName: "周期用户"}).Error)
	now := time.Now().In(time.Local)
	row := func(name, scope, window, scheduleID string, limit int64) dto.ChannelBudgetRow {
		return dto.ChannelBudgetRow{Name: name, Enabled: true, Scope: scope, Window: window, ScheduleID: scheduleID, Models: []string{}, Limit: limit, OnExceed: dto.ChannelBudgetAction{Mode: "inherit"}}
	}
	input := dto.ChannelPeriodPolicyInput{Config: dto.ChannelPeriodPolicyConfig{SchemaVersion: 2, DefaultOnExceed: dto.ChannelBudgetAction{Mode: "reject"},
		Schedules: []dto.ChannelBudgetSchedule{{ID: "new-holiday", Name: "指定假期", Enabled: true, Kind: "date_range", StartLocal: now.AddDate(0, 0, -1).Format("2006-01-02T00:00"), EndLocal: now.AddDate(0, 0, 4).Format("2006-01-02T00:00")}},
		Budgets: []dto.ChannelBudgetRow{
			row("池子每日", "pool", "daily", "", 25000000),
			row("池子每周", "pool", "weekly", "", 100000000),
			row("假期个人每日", "user", "daily", "new-holiday", 5000000),
			row("假期个人整段", "user", "occurrence", "new-holiday", 15000000),
			{Name: "gpt-6-astra 每日", Enabled: true, Scope: "pool", Window: "daily", Models: []string{"gpt-6-astra"}, Limit: 4000000, OnExceed: dto.ChannelBudgetAction{Mode: "fallback", ChannelID: 80, Model: "gpt-6-mini"}},
		}}}
	body, err := common.Marshal(input)
	require.NoError(t, err)
	params := gin.Params{{Key: "id", Value: "80"}}
	previewContext, previewRecorder := newChannelUserLimitTestContext(http.MethodPost, "/api/channel/80/period-policy/preview", string(body), params)
	PreviewChannelPeriodPolicy(previewContext)
	require.Equal(t, http.StatusOK, previewRecorder.Code, previewRecorder.Body.String())
	var preview struct {
		Success bool                     `json:"success"`
		Data    dto.ChannelBudgetPreview `json:"data"`
	}
	require.NoError(t, common.Unmarshal(previewRecorder.Body.Bytes(), &preview))
	require.True(t, preview.Success)
	require.Len(t, preview.Data.Config.Schedules, 1)
	require.Len(t, preview.Data.Rows, 5)
	// 预览不建立身份：新时段与新行保留客户端临时 id，行引用也保持可保存。
	assert.Equal(t, "new-holiday", preview.Data.Config.Schedules[0].ID)
	assert.Equal(t, "new-holiday", preview.Data.Config.Budgets[2].ScheduleID)
	assert.Empty(t, preview.Data.Config.Budgets[0].ID)
	assert.True(t, preview.Data.Rows[2].Active)
	assert.True(t, preview.Data.Rows[2].Enforced)
	assert.Equal(t, "date_range", preview.Data.Rows[2].Source.Kind)
	// 指向本渠道的行级降级归一为同渠道换模型。
	assert.Zero(t, preview.Data.Config.Budgets[4].OnExceed.ChannelID)
	input.Config = preview.Data.Config
	body, err = common.Marshal(input)
	require.NoError(t, err)
	saveContext, saveRecorder := newChannelUserLimitTestContext(http.MethodPut, "/api/channel/80/period-policy", string(body), params)
	SetChannelPeriodPolicy(saveContext)
	require.Equal(t, http.StatusOK, saveRecorder.Code, saveRecorder.Body.String())
	var saved struct {
		Success bool                        `json:"success"`
		Data    dto.ChannelPeriodPolicyView `json:"data"`
	}
	require.NoError(t, common.Unmarshal(saveRecorder.Body.Bytes(), &saved))
	require.True(t, saved.Success)
	assert.Equal(t, 1, saved.Data.Revision)
	require.Len(t, saved.Data.Config.Schedules[0].ID, 32)
	require.Len(t, saved.Data.Config.Budgets, 5)
	for _, item := range saved.Data.Config.Budgets {
		assert.Len(t, item.ID, 32)
	}
	assert.Equal(t, saved.Data.Config.Schedules[0].ID, saved.Data.Config.Budgets[2].ScheduleID)

	conflictContext, conflictRecorder := newChannelUserLimitTestContext(http.MethodPut, "/api/channel/80/period-policy", string(body), params)
	SetChannelPeriodPolicy(conflictContext)
	assert.Equal(t, http.StatusConflict, conflictRecorder.Code)
	for _, invalid := range []string{`{"expected_revision":1}`, `{"expected_revision":null,"config":null}`, `{"config":{},"unexpected":true}`, `{"expected_revision":1,"config":{"schema_version":1,"pool_daily_quota_limit":1,"pool_weekly_quota_limit":0,"rules":[],"fallback":{"enabled":false,"channel_id":0,"model":""}}}`} {
		c, recorder := newChannelUserLimitTestContext(http.MethodPut, "/api/channel/80/period-policy", invalid, params)
		SetChannelPeriodPolicy(c)
		assert.Equal(t, http.StatusBadRequest, recorder.Code, invalid)
	}
	require.NoError(t, service.RecordChannelUserQuotaUsage(t.Context(), 80, 77, 1250000))
	statusContext, statusRecorder := newChannelUserLimitTestContext(http.MethodGet, "/api/channel/80/user-limit-status/77", "", append(params, gin.Param{Key: "user_id", Value: "77"}))
	GetChannelUserLimitStatus(statusContext)
	var status struct {
		Success bool                           `json:"success"`
		Data    channelUserLimitStatusResponse `json:"data"`
	}
	require.NoError(t, common.Unmarshal(statusRecorder.Body.Bytes(), &status))
	require.True(t, status.Success, statusRecorder.Body.String())
	// 管理端展示全部行：池子日、池子周、假期个人日、模型行（未命中模型也展示）、假期个人整段。
	require.Len(t, status.Data.PeriodLimits.Metrics, 5)
	assert.Equal(t, int64(1250000), status.Data.PeriodLimits.Metrics[4].Used)
	assert.Equal(t, "occurrence", status.Data.PeriodLimits.Metrics[4].Period)
	assert.Equal(t, []string{"gpt-6-astra"}, status.Data.PeriodLimits.Metrics[3].Models)
	assert.Equal(t, saved.Data.Config.Budgets[3].ID, status.Data.PeriodLimits.Metrics[4].BudgetID)
	assert.NotContains(t, statusRecorder.Body.String(), "must-not-leak")

	summaryContext, summaryRecorder := newChannelUserLimitTestContext(http.MethodGet, "/api/channel/80/budgets/usage-summary", "", params)
	GetChannelBudgetUsageSummary(summaryContext)
	var summary struct {
		Success bool                              `json:"success"`
		Data    dto.ChannelBudgetUsageSummaryView `json:"data"`
	}
	require.NoError(t, common.Unmarshal(summaryRecorder.Body.Bytes(), &summary))
	require.True(t, summary.Success, summaryRecorder.Body.String())
	// 摘要覆盖全部已保存行且顺序与策略一致；个人整段行返回用量最高用户及其生效上限。
	require.Len(t, summary.Data.Items, 5)
	for i, item := range summary.Data.Items {
		assert.Equal(t, saved.Data.Config.Budgets[i].ID, item.BudgetID)
	}
	require.NotNil(t, summary.Data.Items[3].TopUser)
	assert.Equal(t, 77, summary.Data.Items[3].TopUser.UserID)
	assert.Equal(t, "period-user", summary.Data.Items[3].TopUser.Username)
	assert.Equal(t, int64(1250000), summary.Data.Items[3].UsedQuota)
	assert.Equal(t, int64(15000000), summary.Data.Items[3].TopUser.EffectiveLimit)
	assert.Equal(t, int64(1250000), summary.Data.Items[0].UsedQuota)
	assert.NotContains(t, summaryRecorder.Body.String(), "must-not-leak")

	targetsContext, targetsRecorder := newChannelUserLimitTestContext(http.MethodGet, "/api/channel/80/period-policy/targets", "", params)
	GetChannelPeriodPolicyTargets(targetsContext)
	var targets struct {
		Success bool `json:"success"`
		Data    []struct {
			ID   int  `json:"id"`
			Self bool `json:"self"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(targetsRecorder.Body.Bytes(), &targets))
	require.True(t, targets.Success)
	selfSeen := false
	for _, item := range targets.Data {
		selfSeen = selfSeen || (item.ID == 80 && item.Self)
	}
	assert.True(t, selfSeen)
	// 导出真实 Controller 响应给两仓合同测试；不为线上流量生成测试文件。
	if output := os.Getenv("CHANNEL_PERIOD_CONTRACT_OUTPUT"); output != "" {
		fixture, err := common.Marshal(map[string]any{"policy": saved.Data, "preview": commonValueFromJSON(t, previewRecorder.Body.Bytes()), "status": status.Data, "targets": commonValueFromJSON(t, targetsRecorder.Body.Bytes()), "usage_summary": summary.Data})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(output, fixture, 0600))
	}
}

func commonValueFromJSON(t *testing.T, body []byte) any {
	t.Helper()
	var response struct {
		Data any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(body, &response))
	return response.Data
}
