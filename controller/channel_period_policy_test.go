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
	daily := 5000000
	ruleDaily, whole := int64(daily), int64(15000000)
	channel := model.Channel{Id: 80, Name: "合同渠道", Key: "must-not-leak", Status: common.ChannelStatusEnabled, UserDailyQuotaLimit: &daily}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.User{Id: 77, Username: "period-user", DisplayName: "周期用户"}).Error)
	now := time.Now().In(time.Local)
	input := dto.ChannelPeriodPolicyInput{Config: dto.ChannelPeriodPolicyConfig{SchemaVersion: 1, PoolDailyQuotaLimit: 25000000, PoolWeeklyQuotaLimit: 100000000, Rules: []dto.ChannelPeriodRule{{Name: "指定假期", Enabled: true, Kind: "date_range", StartLocal: now.AddDate(0, 0, -1).Format("2006-01-02T00:00"), EndLocal: now.AddDate(0, 0, 4).Format("2006-01-02T00:00"), UserDailyQuotaLimit: &ruleDaily, UserPeriodQuotaLimit: &whole}}}}
	body, err := common.Marshal(input)
	require.NoError(t, err)
	params := gin.Params{{Key: "id", Value: "80"}}
	previewContext, previewRecorder := newChannelUserLimitTestContext(http.MethodPost, "/api/channel/80/period-policy/preview", string(body), params)
	PreviewChannelPeriodPolicy(previewContext)
	require.Equal(t, http.StatusOK, previewRecorder.Code, previewRecorder.Body.String())
	var preview struct {
		Success bool                        `json:"success"`
		Data    dto.ChannelPeriodPolicyView `json:"data"`
	}
	require.NoError(t, common.Unmarshal(previewRecorder.Body.Bytes(), &preview))
	require.True(t, preview.Success)
	require.Len(t, preview.Data.Config.Rules, 1)
	assert.Empty(t, preview.Data.Config.Rules[0].ID)
	// 预览的结果可直接保存，身份只由真正的写操作建立。
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
	require.Len(t, saved.Data.Config.Rules[0].ID, 32)

	conflictContext, conflictRecorder := newChannelUserLimitTestContext(http.MethodPut, "/api/channel/80/period-policy", string(body), params)
	SetChannelPeriodPolicy(conflictContext)
	assert.Equal(t, http.StatusConflict, conflictRecorder.Code)
	for _, invalid := range []string{`{"expected_revision":1}`, `{"expected_revision":null,"config":null}`, `{"config":{},"unexpected":true}`} {
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
	require.Len(t, status.Data.PeriodLimits.Metrics, 6)
	assert.Equal(t, int64(1250000), status.Data.PeriodLimits.Metrics[4].Used)
	assert.Equal(t, "custom", status.Data.PeriodLimits.Metrics[4].Period)
	assert.NotContains(t, statusRecorder.Body.String(), "must-not-leak")
	// 导出真实 Controller 响应给两仓合同测试；不为线上流量生成测试文件。
	if output := os.Getenv("CHANNEL_PERIOD_CONTRACT_OUTPUT"); output != "" {
		fixture, err := common.Marshal(map[string]any{"policy": saved.Data, "preview": commonValueFromJSON(t, previewRecorder.Body.Bytes()), "status": status.Data})
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
