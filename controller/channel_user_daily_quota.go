package controller

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const (
	channelUserDailyQuotaErrorLogRecordedKey  = "channel_user_daily_quota_error_log_recorded"
	channelUserDailyQuotaErrorLogAdminInfoKey = "channel_user_daily_quota"
)

func checkChannelUserDailyQuota(c *gin.Context) *types.NewAPIError {
	limit := common.GetContextKeyInt(c, constant.ContextKeyChannelUserDailyQuotaLimit)
	if limit <= 0 {
		return nil
	}
	channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	userID := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	usedQuota, err := service.CheckChannelUserDailyQuota(c, channelID, userID, limit)
	common.SetContextKey(c, constant.ContextKeyChannelUserDailyQuotaUsed, usedQuota)
	if err == nil {
		return nil
	}
	apiErr := newChannelUserDailyQuotaAPIError(err)
	logger.LogWarn(c, fmt.Sprintf(
		"检查渠道单用户每日额度失败: channel_id=%d user_id=%d limit=%d used=%d error_code=%s",
		channelID,
		userID,
		limit,
		usedQuota,
		apiErr.GetErrorCode(),
	))
	recordRelayErrorLog(c, apiErr)
	return apiErr
}

func newChannelUserDailyQuotaAPIError(err error) *types.NewAPIError {
	if errors.Is(err, service.ErrChannelUserDailyQuotaExceeded) {
		return types.NewOpenAIError(
			errors.New("channel user daily quota limit exceeded"),
			types.ErrorCodeChannelUserDailyQuotaExceeded,
			http.StatusTooManyRequests,
			types.ErrOptionWithSkipRetry(),
		)
	}
	return types.NewOpenAIError(
		errors.New("channel user daily quota service unavailable"),
		types.ErrorCodeChannelUserDailyQuotaUnavailable,
		http.StatusServiceUnavailable,
		types.ErrOptionWithSkipRetry(),
	)
}

func isChannelUserDailyQuotaAPIError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	return err.GetErrorCode() == types.ErrorCodeChannelUserDailyQuotaExceeded ||
		err.GetErrorCode() == types.ErrorCodeChannelUserDailyQuotaUnavailable
}

func prepareChannelUserDailyQuotaErrorLog(c *gin.Context, err *types.NewAPIError) bool {
	if !isChannelUserDailyQuotaAPIError(err) {
		return true
	}
	if c.GetBool(channelUserDailyQuotaErrorLogRecordedKey) {
		return false
	}
	c.Set(channelUserDailyQuotaErrorLogRecordedKey, true)

	logOther, _ := common.GetContextKeyType[map[string]interface{}](c, constant.ContextKeyLogOther)
	if logOther == nil {
		logOther = map[string]interface{}{}
	}
	adminInfo, _ := logOther["admin_info"].(map[string]interface{})
	if adminInfo == nil {
		adminInfo = map[string]interface{}{}
		logOther["admin_info"] = adminInfo
	}
	usedQuota, _ := common.GetContextKeyType[int64](c, constant.ContextKeyChannelUserDailyQuotaUsed)
	adminInfo[channelUserDailyQuotaErrorLogAdminInfoKey] = map[string]interface{}{
		"channel_id": common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		"user_id":    common.GetContextKeyInt(c, constant.ContextKeyUserId),
		"limit":      common.GetContextKeyInt(c, constant.ContextKeyChannelUserDailyQuotaLimit),
		"used":       usedQuota,
		"error_code": err.GetErrorCode(),
	}
	common.SetContextKey(c, constant.ContextKeyLogOther, logOther)
	return true
}

func channelUserDailyQuotaAPIErrorFromCode(code types.ErrorCode) *types.NewAPIError {
	switch code {
	case types.ErrorCodeChannelUserDailyQuotaExceeded:
		return newChannelUserDailyQuotaAPIError(service.ErrChannelUserDailyQuotaExceeded)
	case types.ErrorCodeChannelUserDailyQuotaUnavailable:
		return newChannelUserDailyQuotaAPIError(service.ErrChannelUserDailyQuotaUnavailable)
	default:
		return nil
	}
}

func recordChannelUserDailyQuotaErrorCode(c *gin.Context, code types.ErrorCode) {
	apiErr := channelUserDailyQuotaAPIErrorFromCode(code)
	if apiErr != nil {
		recordRelayErrorLog(c, apiErr)
	}
}
