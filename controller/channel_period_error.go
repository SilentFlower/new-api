package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

func channelPeriodAPIErrorFromCode(code types.ErrorCode) *types.NewAPIError {
	status := http.StatusServiceUnavailable
	switch code {
	case types.ErrorCodeChannelPeriodQuotaExceeded:
		status = http.StatusTooManyRequests
	case types.ErrorCodeChannelPeriodQuotaUnavailable:
	default:
		return nil
	}
	return types.NewOpenAIError(errors.New("channel period quota unavailable or exhausted"), code, status, types.ErrOptionWithSkipRetry())
}

func recordChannelPeriodErrorCode(c *gin.Context, code types.ErrorCode) {
	if apiErr := channelPeriodAPIErrorFromCode(code); apiErr != nil {
		recordRelayErrorLog(c, apiErr)
	}
}

func prepareChannelPeriodErrorLog(c *gin.Context, apiErr *types.NewAPIError) bool {
	if apiErr == nil || channelPeriodAPIErrorFromCode(apiErr.GetErrorCode()) == nil {
		return true
	}
	if c.GetBool("channel_period_error_recorded") {
		return false
	}
	c.Set("channel_period_error_recorded", true)
	other, _ := common.GetContextKeyType[map[string]interface{}](c, constant.ContextKeyLogOther)
	if other == nil {
		other = make(map[string]interface{})
	}
	admin, _ := other["admin_info"].(map[string]interface{})
	if admin == nil {
		admin = make(map[string]interface{})
		other["admin_info"] = admin
	}
	if block, ok := c.Get("channel_period_block"); ok {
		admin["channel_period_limit"] = block
	}
	common.SetContextKey(c, constant.ContextKeyLogOther, other)
	return true
}
