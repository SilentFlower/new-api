package controller

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const (
	channelUserWeeklyQuotaErrorLogRecordedKey  = "channel_user_weekly_quota_error_log_recorded"
	channelUserWeeklyQuotaErrorLogAdminInfoKey = "channel_user_weekly_quota"
)

func checkChannelUserQuotaLimits(c *gin.Context) *types.NewAPIError {
	if apiErr := checkChannelUserDailyQuota(c); apiErr != nil {
		return apiErr
	}
	return checkChannelUserWeeklyQuota(c)
}

func checkChannelUserWeeklyQuota(c *gin.Context) *types.NewAPIError {
	limit := common.GetContextKeyInt(c, constant.ContextKeyChannelUserWeeklyQuotaLimit)
	if limit <= 0 {
		return nil
	}
	channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	userID := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	usedQuota, err := service.CheckChannelUserWeeklyQuota(c, channelID, userID, limit)
	common.SetContextKey(c, constant.ContextKeyChannelUserWeeklyQuotaUsed, usedQuota)
	if err == nil {
		return nil
	}
	apiErr := newChannelUserWeeklyQuotaAPIError(err)
	logger.LogWarn(c, fmt.Sprintf(
		"检查渠道单用户每周额度失败: channel_id=%d user_id=%d limit=%d used=%d error_code=%s",
		channelID,
		userID,
		limit,
		usedQuota,
		apiErr.GetErrorCode(),
	))
	recordRelayErrorLog(c, apiErr)
	return apiErr
}

func newChannelUserWeeklyQuotaAPIError(err error) *types.NewAPIError {
	if errors.Is(err, service.ErrChannelUserWeeklyQuotaExceeded) {
		return types.NewOpenAIError(
			errors.New("channel user weekly quota limit exceeded"),
			types.ErrorCodeChannelUserWeeklyQuotaExceeded,
			http.StatusTooManyRequests,
			types.ErrOptionWithSkipRetry(),
		)
	}
	return types.NewOpenAIError(
		errors.New("channel user weekly quota service unavailable"),
		types.ErrorCodeChannelUserWeeklyQuotaUnavailable,
		http.StatusServiceUnavailable,
		types.ErrOptionWithSkipRetry(),
	)
}

func isChannelUserWeeklyQuotaAPIError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	return err.GetErrorCode() == types.ErrorCodeChannelUserWeeklyQuotaExceeded ||
		err.GetErrorCode() == types.ErrorCodeChannelUserWeeklyQuotaUnavailable
}

func prepareChannelUserWeeklyQuotaErrorLog(c *gin.Context, err *types.NewAPIError) bool {
	if !isChannelUserWeeklyQuotaAPIError(err) {
		return true
	}
	if c.GetBool(channelUserWeeklyQuotaErrorLogRecordedKey) {
		return false
	}
	c.Set(channelUserWeeklyQuotaErrorLogRecordedKey, true)

	logOther, _ := common.GetContextKeyType[map[string]interface{}](c, constant.ContextKeyLogOther)
	if logOther == nil {
		logOther = map[string]interface{}{}
	}
	adminInfo, _ := logOther["admin_info"].(map[string]interface{})
	if adminInfo == nil {
		adminInfo = map[string]interface{}{}
		logOther["admin_info"] = adminInfo
	}
	usedQuota, _ := common.GetContextKeyType[int64](c, constant.ContextKeyChannelUserWeeklyQuotaUsed)
	adminInfo[channelUserWeeklyQuotaErrorLogAdminInfoKey] = map[string]interface{}{
		"channel_id": common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		"user_id":    common.GetContextKeyInt(c, constant.ContextKeyUserId),
		"limit":      common.GetContextKeyInt(c, constant.ContextKeyChannelUserWeeklyQuotaLimit),
		"used":       usedQuota,
		"error_code": err.GetErrorCode(),
	}
	common.SetContextKey(c, constant.ContextKeyLogOther, logOther)
	return true
}

func channelUserWeeklyQuotaAPIErrorFromCode(code types.ErrorCode) *types.NewAPIError {
	switch code {
	case types.ErrorCodeChannelUserWeeklyQuotaExceeded:
		return newChannelUserWeeklyQuotaAPIError(service.ErrChannelUserWeeklyQuotaExceeded)
	case types.ErrorCodeChannelUserWeeklyQuotaUnavailable:
		return newChannelUserWeeklyQuotaAPIError(service.ErrChannelUserWeeklyQuotaUnavailable)
	default:
		return nil
	}
}

func recordChannelUserWeeklyQuotaErrorCode(c *gin.Context, code types.ErrorCode) {
	apiErr := channelUserWeeklyQuotaAPIErrorFromCode(code)
	if apiErr != nil {
		recordRelayErrorLog(c, apiErr)
	}
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
	return 0, false
}
