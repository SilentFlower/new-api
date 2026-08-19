package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newChannelUserDailyQuotaTestContext(channelID int, userID int, limit int) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyChannelId, channelID)
	common.SetContextKey(c, constant.ContextKeyUserId, userID)
	common.SetContextKey(c, constant.ContextKeyChannelUserDailyQuotaLimit, limit)
	return c
}

func TestCheckChannelUserDailyQuotaReturnsStableLimitError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
	})
	const channelID = 9601
	const userID = 9602
	require.NoError(t, service.SetChannelUserDailyQuota(t.Context(), channelID, userID, 100))

	apiErr := checkChannelUserDailyQuota(newChannelUserDailyQuotaTestContext(channelID, userID, 100))

	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
	assert.Equal(t, types.ErrorCodeChannelUserDailyQuotaExceeded, apiErr.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.False(t, service.ShouldDisableChannel(apiErr))
}

func TestCheckChannelUserDailyQuotaFailsClosedWhenRedisUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousRedisEnabled, previousRedis := common.RedisEnabled, common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = previousRedisEnabled, previousRedis
	})

	apiErr := checkChannelUserDailyQuota(newChannelUserDailyQuotaTestContext(9701, 9702, 100))

	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	assert.Equal(t, types.ErrorCodeChannelUserDailyQuotaUnavailable, apiErr.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.False(t, service.ShouldDisableChannel(apiErr))
}

func TestCheckChannelUserDailyQuotaRecordsErrorLogOnce(t *testing.T) {
	db := setupRelayErrorLogTestDB(t)
	previousRedisEnabled := common.RedisEnabled
	previousErrorLogEnabled := constant.ErrorLogEnabled
	common.RedisEnabled = false
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		constant.ErrorLogEnabled = previousErrorLogEnabled
	})
	const channelID = 9801
	const userID = 9802
	require.NoError(t, service.SetChannelUserDailyQuota(t.Context(), channelID, userID, 125))
	c := newChannelUserDailyQuotaTestContext(channelID, userID, 100)
	c.Set("username", "daily-quota-user")
	c.Set("token_name", "test-token")
	c.Set("original_model", "gpt-test")
	c.Set("token_id", 9)
	c.Set("group", "default")
	c.Set("channel_name", "channel-9801")
	c.Set("channel_type", constant.ChannelTypeOpenAI)
	c.Set(common.RequestIdKey, "request-daily-quota-exceeded")
	common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())

	apiErr := checkChannelUserDailyQuota(c)
	require.NotNil(t, apiErr)
	recordRelayErrorLog(c, apiErr)

	var logs []model.Log
	require.NoError(t, db.Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, "request-daily-quota-exceeded", logs[0].RequestId)
	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.Equal(t, string(types.ErrorCodeChannelUserDailyQuotaExceeded), other["error_code"])
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	dailyInfo, ok := adminInfo[channelUserDailyQuotaErrorLogAdminInfoKey].(map[string]interface{})
	require.True(t, ok)
	assert.EqualValues(t, channelID, dailyInfo["channel_id"])
	assert.EqualValues(t, userID, dailyInfo["user_id"])
	assert.EqualValues(t, 100, dailyInfo["limit"])
	assert.EqualValues(t, 125, dailyInfo["used"])
}
