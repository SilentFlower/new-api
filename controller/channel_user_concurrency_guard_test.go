package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newChannelUserConcurrencyTestContext(channelID int, userID int, limit int) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyChannelId, channelID)
	common.SetContextKey(c, constant.ContextKeyUserId, userID)
	common.SetContextKey(c, constant.ContextKeyChannelUserConcurrencyLimit, limit)
	return c
}

func TestAcquireChannelUserConcurrencyGuardReturnsStableLimitError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
	})

	firstContext := newChannelUserConcurrencyTestContext(80, 33, 1)
	first, apiErr := acquireChannelUserConcurrencyGuard(firstContext)
	require.Nil(t, apiErr)
	require.NotNil(t, first)

	secondContext := newChannelUserConcurrencyTestContext(80, 33, 1)
	second, apiErr := acquireChannelUserConcurrencyGuard(secondContext)
	require.Nil(t, second)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
	assert.Equal(t, types.ErrorCodeChannelUserConcurrencyExceeded, apiErr.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.False(t, service.ShouldDisableChannel(apiErr))
	assert.Equal(t, types.ErrorCodeChannelUserConcurrencyExceeded, apiErr.ToOpenAIError().Code)
	assert.Equal(t, string(types.ErrorCodeChannelUserConcurrencyExceeded), apiErr.ToClaudeError().Type)

	require.Nil(t, finishChannelUserConcurrencyGuard(firstContext, first, nil))
}

func TestAcquireChannelUserConcurrencyGuardFailsClosedWhenRedisUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRedisEnabled := common.RedisEnabled
	originalRedis := common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedis
	})

	c := newChannelUserConcurrencyTestContext(80, 33, 4)
	guard, apiErr := acquireChannelUserConcurrencyGuard(c)

	require.Nil(t, guard)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	assert.Equal(t, types.ErrorCodeChannelUserConcurrencyUnavailable, apiErr.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.False(t, service.ShouldDisableChannel(apiErr))
}

func TestAcquireChannelUserConcurrencyGuardRecordsErrorLogOnce(t *testing.T) {
	db := setupRelayErrorLogTestDB(t)
	originalErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})

	firstContext := newChannelUserConcurrencyTestContext(80, 33, 1)
	first, apiErr := acquireChannelUserConcurrencyGuard(firstContext)
	require.Nil(t, apiErr)
	require.NotNil(t, first)
	t.Cleanup(func() {
		_ = finishChannelUserConcurrencyGuard(firstContext, first, nil)
	})

	secondContext := newChannelUserConcurrencyTestContext(80, 33, 1)
	secondContext.Set("username", "concurrency-user")
	secondContext.Set("token_name", "test-token")
	secondContext.Set("original_model", "claude-sonnet")
	secondContext.Set("token_id", 9)
	secondContext.Set("group", "default")
	secondContext.Set("channel_name", "channel-80")
	secondContext.Set("channel_type", constant.ChannelTypeAnthropic)
	secondContext.Set(common.RequestIdKey, "request-concurrency-exceeded")
	common.SetContextKey(secondContext, constant.ContextKeyRequestStartTime, time.Now())

	second, apiErr := acquireChannelUserConcurrencyGuard(secondContext)
	require.Nil(t, second)
	require.NotNil(t, apiErr)
	recordRelayErrorLog(secondContext, apiErr)

	var logs []model.Log
	require.NoError(t, db.Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, model.LogTypeError, logs[0].Type)
	assert.Equal(t, 33, logs[0].UserId)
	assert.Equal(t, 80, logs[0].ChannelId)
	assert.Equal(t, "request-concurrency-exceeded", logs[0].RequestId)
	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.Equal(t, string(types.ErrorCodeChannelUserConcurrencyExceeded), other["error_code"])
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	concurrencyInfo, ok := adminInfo[channelUserConcurrencyErrorLogAdminInfoKey].(map[string]interface{})
	require.True(t, ok)
	assert.EqualValues(t, 80, concurrencyInfo["channel_id"])
	assert.EqualValues(t, 33, concurrencyInfo["user_id"])
	assert.EqualValues(t, 1, concurrencyInfo["limit"])
	assert.Equal(t, string(types.ErrorCodeChannelUserConcurrencyExceeded), concurrencyInfo["error_code"])
}

func TestRecordChannelUserConcurrencyErrorCodePersistsStableErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		code       types.ErrorCode
		statusCode int
	}{
		{
			name:       "达到并发上限",
			code:       types.ErrorCodeChannelUserConcurrencyExceeded,
			statusCode: http.StatusTooManyRequests,
		},
		{
			name:       "并发服务不可用",
			code:       types.ErrorCodeChannelUserConcurrencyUnavailable,
			statusCode: http.StatusServiceUnavailable,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := setupRelayErrorLogTestDB(t)
			originalErrorLogEnabled := constant.ErrorLogEnabled
			constant.ErrorLogEnabled = true
			t.Cleanup(func() {
				constant.ErrorLogEnabled = originalErrorLogEnabled
			})

			c := newChannelUserConcurrencyTestContext(80, 33, 4)
			recordChannelUserConcurrencyErrorCode(c, test.code)

			var logs []model.Log
			require.NoError(t, db.Find(&logs).Error)
			require.Len(t, logs, 1)
			other, err := common.StrToMap(logs[0].Other)
			require.NoError(t, err)
			assert.Equal(t, string(test.code), other["error_code"])
			assert.EqualValues(t, test.statusCode, other["status_code"])
		})
	}
}

func TestShouldRetryTaskRelaySkipsLocalConcurrencyErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)

	for _, statusCode := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			taskErr := &dto.TaskError{
				StatusCode: statusCode,
				LocalError: true,
			}

			assert.False(t, shouldRetryTaskRelay(c, 80, taskErr, 1))
		})
	}
}

func TestChannelUserConcurrencyMidjourneyHTTPStatus(t *testing.T) {
	tests := []struct {
		name           string
		description    string
		expectedStatus int
		expected       bool
	}{
		{
			name:           "达到并发上限",
			description:    string(types.ErrorCodeChannelUserConcurrencyExceeded),
			expectedStatus: http.StatusTooManyRequests,
			expected:       true,
		},
		{
			name:           "并发服务不可用",
			description:    string(types.ErrorCodeChannelUserConcurrencyUnavailable),
			expectedStatus: http.StatusServiceUnavailable,
			expected:       true,
		},
		{
			name:        "普通上游错误",
			description: "upstream_error",
			expected:    false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statusCode, ok := channelUserConcurrencyMidjourneyHTTPStatus(&dto.MidjourneyResponse{Description: test.description})

			assert.Equal(t, test.expected, ok)
			assert.Equal(t, test.expectedStatus, statusCode)
		})
	}
}
