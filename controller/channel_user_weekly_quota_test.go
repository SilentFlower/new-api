package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newChannelUserWeeklyQuotaTestContext(channelID int, userID int, limit int) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyChannelId, channelID)
	common.SetContextKey(c, constant.ContextKeyUserId, userID)
	common.SetContextKey(c, constant.ContextKeyChannelUserWeeklyQuotaLimit, limit)
	return c
}

// TestCheckChannelUserWeeklyQuotaReturnsStableLimitError 验证周限拒绝不会换渠重试或禁用渠道。
func TestCheckChannelUserWeeklyQuotaReturnsStableLimitError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
	})
	const channelID = 9811
	const userID = 9812
	require.NoError(t, service.SetChannelUserWeeklyQuota(t.Context(), channelID, userID, 100))

	apiErr := checkChannelUserWeeklyQuota(newChannelUserWeeklyQuotaTestContext(channelID, userID, 100))

	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
	assert.Equal(t, types.ErrorCodeChannelUserWeeklyQuotaExceeded, apiErr.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.False(t, service.ShouldDisableChannel(apiErr))
}

// TestCheckChannelUserWeeklyQuotaFailsClosedWhenRedisUnavailable 验证启用周限时 Redis 故障按 503 失败关闭。
func TestCheckChannelUserWeeklyQuotaFailsClosedWhenRedisUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousRedisEnabled, previousRedis := common.RedisEnabled, common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = previousRedisEnabled, previousRedis
	})

	apiErr := checkChannelUserWeeklyQuota(newChannelUserWeeklyQuotaTestContext(9821, 9822, 100))

	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	assert.Equal(t, types.ErrorCodeChannelUserWeeklyQuotaUnavailable, apiErr.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.False(t, service.ShouldDisableChannel(apiErr))
}

// TestChannelUserQuotaMidjourneyHTTPStatus 验证日限和周限在 Midjourney 响应中保持稳定状态码。
func TestChannelUserQuotaMidjourneyHTTPStatus(t *testing.T) {
	tests := []struct {
		name           string
		code           types.ErrorCode
		expectedStatus int
		expected       bool
	}{
		{name: "达到每日额度上限", code: types.ErrorCodeChannelUserDailyQuotaExceeded, expectedStatus: http.StatusTooManyRequests, expected: true},
		{name: "每日额度服务不可用", code: types.ErrorCodeChannelUserDailyQuotaUnavailable, expectedStatus: http.StatusServiceUnavailable, expected: true},
		{name: "达到每周额度上限", code: types.ErrorCodeChannelUserWeeklyQuotaExceeded, expectedStatus: http.StatusTooManyRequests, expected: true},
		{name: "每周额度服务不可用", code: types.ErrorCodeChannelUserWeeklyQuotaUnavailable, expectedStatus: http.StatusServiceUnavailable, expected: true},
		{name: "普通上游错误", code: types.ErrorCode("upstream_error"), expected: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statusCode, ok := channelUserQuotaMidjourneyHTTPStatus(&dto.MidjourneyResponse{Description: string(test.code)})

			assert.Equal(t, test.expected, ok)
			assert.Equal(t, test.expectedStatus, statusCode)
		})
	}
}
