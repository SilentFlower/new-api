package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
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
