package controller

import (
	"net/http"
	"os"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChannelModelUsageManagementContract 验证模型持续累计策略的保存、查询和旧客户端兼容契约。
// @param t 测试上下文。
func TestChannelModelUsageManagementContract(t *testing.T) {
	const channelID = 68100
	channelIDText := strconv.Itoa(channelID)
	db := setupChannelUserLimitsTestDB(t)
	require.NoError(t, db.Create(&model.Channel{Id: channelID, Name: "模型累计合同", Status: common.ChannelStatusEnabled, Models: "A,B", Group: "default"}).Error)
	require.NoError(t, db.Create(&model.User{Id: 77, Username: "model-user"}).Error)
	params := gin.Params{{Key: "id", Value: channelIDText}}
	enabled := true
	config := dto.ChannelPeriodPolicyConfig{SchemaVersion: 2, DefaultOnExceed: dto.ChannelBudgetAction{Mode: "reject"}, Schedules: []dto.ChannelBudgetSchedule{}, Budgets: []dto.ChannelBudgetRow{}, ModelUsageTrackingEnabled: &enabled}
	view, err := service.SaveChannelPeriodPolicy(t.Context(), channelID, dto.ChannelPeriodPolicyInput{Config: config}, 1)
	require.NoError(t, err)
	require.NoError(t, service.RecordChannelUserModelQuotaUsage(t.Context(), channelID, 77, 60, "A"))
	view.Config.Budgets = append(view.Config.Budgets, dto.ChannelBudgetRow{ID: "new-model", Name: "A 每日", Enabled: true, Scope: "user", Window: "daily", Models: []string{"A"}, Limit: 100, OnExceed: dto.ChannelBudgetAction{Mode: "inherit"}})
	body, err := common.Marshal(dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config})
	require.NoError(t, err)
	c, recorder := newChannelUserLimitTestContext(http.MethodPut, "/api/channel/"+channelIDText+"/period-policy", string(body), params)
	SetChannelPeriodPolicy(c)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var saved struct {
		Data dto.ChannelPeriodPolicyView `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &saved))
	assert.Equal(t, "continuous_model", saved.Data.Config.Budgets[0].CounterSource)
	statusContext, statusRecorder := newChannelUserLimitTestContext(http.MethodGet, "/api/channel/"+channelIDText+"/user-limit-status/77", "", append(params, gin.Param{Key: "user_id", Value: "77"}))
	GetChannelUserLimitStatus(statusContext)
	require.Equal(t, http.StatusOK, statusRecorder.Code)
	usageContext, usageRecorder := newChannelUserLimitTestContext(http.MethodGet, "/api/channel/"+channelIDText+"/budgets/"+saved.Data.Config.Budgets[0].ID+"/usage?scope=user&p=1&page_size=20", "", append(params, gin.Param{Key: "budget_id", Value: saved.Data.Config.Budgets[0].ID}))
	GetChannelBudgetUsage(usageContext)
	require.Equal(t, http.StatusOK, usageRecorder.Code)
	var usage struct {
		Data dto.ChannelBudgetUsageView `json:"data"`
	}
	require.NoError(t, common.Unmarshal(usageRecorder.Body.Bytes(), &usage))
	require.Len(t, usage.Data.Items, 1)
	assert.Equal(t, int64(60), usage.Data.Items[0].UsedQuota)
	// 新可选字段不能以 null/错误类型绕过严格正文校验。
	for _, extension := range []string{`"model_usage_tracking_enabled":null`, `"model_usage_tracking_enabled":"true"`, `"model_usage_tracking":null`, `"model_usage_tracking":{"first_enabled_at":1}`, `"unknown_tracking":true`} {
		base := `{"expected_revision":2,"config":{"schema_version":2,"default_on_exceed":{"mode":"reject","channel_id":0,"model":""},"schedules":[],"budgets":[],` + extension + `}}`
		invalid, result := newChannelUserLimitTestContext(http.MethodPut, "/api/channel/"+channelIDText+"/period-policy", base, params)
		SetChannelPeriodPolicy(invalid)
		assert.Equal(t, http.StatusBadRequest, result.Code, extension)
	}
	// 从旧客户端重新保存必须保留开关、时间及预算来源。
	previous := saved.Data.Config
	saved.Data.Config.ModelUsageTrackingEnabled = nil
	saved.Data.Config.ModelUsageTracking = nil
	saved.Data.Config.Budgets[0].CounterSource = ""
	body, err = common.Marshal(dto.ChannelPeriodPolicyInput{ExpectedRevision: saved.Data.Revision, Config: saved.Data.Config})
	require.NoError(t, err)
	c, recorder = newChannelUserLimitTestContext(http.MethodPut, "/api/channel/"+channelIDText+"/period-policy", string(body), params)
	SetChannelPeriodPolicy(c)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &saved))
	assert.Equal(t, previous, saved.Data.Config)
	statusContext, statusRecorder = newChannelUserLimitTestContext(http.MethodGet, "/api/channel/"+channelIDText+"/user-limit-status/77", "", append(params, gin.Param{Key: "user_id", Value: "77"}))
	GetChannelUserLimitStatus(statusContext)
	require.Equal(t, http.StatusOK, statusRecorder.Code)
	if output := os.Getenv("CHANNEL_MODEL_USAGE_CONTRACT_OUTPUT"); output != "" {
		data, err := common.Marshal(map[string]any{"policy": saved.Data, "status": commonValueFromJSON(t, statusRecorder.Body.Bytes()), "usage": usage.Data})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(output, data, 0600))
	}
}
