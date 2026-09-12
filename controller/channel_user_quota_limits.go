package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const (
	channelUserDailyQuotaErrorLogRecordedKey   = "channel_user_daily_quota_error_log_recorded"
	channelUserDailyQuotaErrorLogAdminInfoKey  = "channel_user_daily_quota"
	channelUserWeeklyQuotaErrorLogRecordedKey  = "channel_user_weekly_quota_error_log_recorded"
	channelUserWeeklyQuotaErrorLogAdminInfoKey = "channel_user_weekly_quota"
)

// checkChannelUserQuotaLimits 在 Controller 入口按预算行检查所选渠道的全部金额限制。
func checkChannelUserQuotaLimits(c *gin.Context) *types.NewAPIError {
	return service.CheckSelectedChannelPeriodLimits(c)
}

// channelUserDailyQuotaAPIErrorFromCode 把稳定错误码还原为渠道级个人日限错误，供任务与 Midjourney 路径记录。
func channelUserDailyQuotaAPIErrorFromCode(code types.ErrorCode) *types.NewAPIError {
	switch code {
	case types.ErrorCodeChannelUserDailyQuotaExceeded:
		return types.NewOpenAIError(errors.New("channel user daily quota limit exceeded"), code, http.StatusTooManyRequests, types.ErrOptionWithSkipRetry())
	case types.ErrorCodeChannelUserDailyQuotaUnavailable:
		return types.NewOpenAIError(errors.New("channel user daily quota service unavailable"), code, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	default:
		return nil
	}
}

// channelUserWeeklyQuotaAPIErrorFromCode 把稳定错误码还原为渠道级个人周限错误。
func channelUserWeeklyQuotaAPIErrorFromCode(code types.ErrorCode) *types.NewAPIError {
	switch code {
	case types.ErrorCodeChannelUserWeeklyQuotaExceeded:
		return types.NewOpenAIError(errors.New("channel user weekly quota limit exceeded"), code, http.StatusTooManyRequests, types.ErrOptionWithSkipRetry())
	case types.ErrorCodeChannelUserWeeklyQuotaUnavailable:
		return types.NewOpenAIError(errors.New("channel user weekly quota service unavailable"), code, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	default:
		return nil
	}
}

func recordChannelUserDailyQuotaErrorCode(c *gin.Context, code types.ErrorCode) {
	if apiErr := channelUserDailyQuotaAPIErrorFromCode(code); apiErr != nil {
		recordRelayErrorLog(c, apiErr)
	}
}

func recordChannelUserWeeklyQuotaErrorCode(c *gin.Context, code types.ErrorCode) {
	if apiErr := channelUserWeeklyQuotaAPIErrorFromCode(code); apiErr != nil {
		recordRelayErrorLog(c, apiErr)
	}
}

func prepareChannelUserDailyQuotaErrorLog(c *gin.Context, err *types.NewAPIError) bool {
	return prepareChannelUserQuotaErrorLog(c, err, channelUserDailyQuotaAPIErrorFromCode, channelUserDailyQuotaErrorLogRecordedKey, channelUserDailyQuotaErrorLogAdminInfoKey)
}

func prepareChannelUserWeeklyQuotaErrorLog(c *gin.Context, err *types.NewAPIError) bool {
	return prepareChannelUserQuotaErrorLog(c, err, channelUserWeeklyQuotaAPIErrorFromCode, channelUserWeeklyQuotaErrorLogRecordedKey, channelUserWeeklyQuotaErrorLogAdminInfoKey)
}

// prepareChannelUserQuotaErrorLog 只为渠道级个人日/周错误码写一次管理员可见信息；上限与已用来自触发拦截的指标。
func prepareChannelUserQuotaErrorLog(c *gin.Context, err *types.NewAPIError, fromCode func(types.ErrorCode) *types.NewAPIError, recordedKey, adminKey string) bool {
	if err == nil || fromCode(err.GetErrorCode()) == nil {
		return true
	}
	if c.GetBool(recordedKey) {
		return false
	}
	c.Set(recordedKey, true)
	logOther, _ := common.GetContextKeyType[map[string]interface{}](c, constant.ContextKeyLogOther)
	if logOther == nil {
		logOther = map[string]interface{}{}
	}
	adminInfo, _ := logOther["admin_info"].(map[string]interface{})
	if adminInfo == nil {
		adminInfo = map[string]interface{}{}
		logOther["admin_info"] = adminInfo
	}
	info := map[string]interface{}{
		"channel_id": common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		"user_id":    common.GetContextKeyInt(c, constant.ContextKeyUserId),
		"error_code": err.GetErrorCode(),
	}
	if value, ok := c.Get("channel_period_block"); ok {
		if block, valid := value.(*service.ChannelPeriodBlock); valid {
			info["limit"], info["used"], info["budget_id"] = block.Metric.Limit, block.Metric.Used, block.Metric.BudgetID
		}
	}
	adminInfo[adminKey] = info
	common.SetContextKey(c, constant.ContextKeyLogOther, logOther)
	return true
}

func channelUserQuotaMidjourneyHTTPStatus(response *dto.MidjourneyResponse) (int, bool) {
	if response == nil {
		return 0, false
	}
	code := types.ErrorCode(response.Description)
	if apiErr := channelUserDailyQuotaAPIErrorFromCode(code); apiErr != nil {
		return apiErr.StatusCode, true
	}
	if apiErr := channelUserWeeklyQuotaAPIErrorFromCode(code); apiErr != nil {
		return apiErr.StatusCode, true
	}
	if apiErr := channelPeriodAPIErrorFromCode(code); apiErr != nil {
		return apiErr.StatusCode, true
	}
	return 0, false
}
