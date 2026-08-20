package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const (
	maxChannelUserConcurrencyLimit             = 1000
	maxChannelUserDailyQuotaLimit              = common.MaxQuota
	maxChannelUserWeeklyQuotaLimit             = common.MaxQuota
	channelUserConcurrencyErrorLogRecordedKey  = "channel_user_concurrency_error_log_recorded"
	channelUserConcurrencyErrorLogAdminInfoKey = "channel_user_concurrency"
)

type channelUserConcurrencyGuard struct {
	lease       *service.ChannelUserConcurrencyLease
	baseContext context.Context
	cancel      context.CancelFunc
	channelID   int
	userID      int
	limit       int
}

func validateChannelUserConcurrencyLimit(limit *int) error {
	if limit == nil || (*limit >= 0 && *limit <= maxChannelUserConcurrencyLimit) {
		return nil
	}
	return fmt.Errorf("单用户最大并发数必须是 0 到 %d 之间的整数", maxChannelUserConcurrencyLimit)
}

func normalizeChannelUserConcurrencyLimitForUpdate(channel *model.Channel, requestData map[string]any) {
	if channel == nil {
		return
	}
	if value, ok := requestData["user_concurrency_limit"]; ok && value == nil {
		channel.UserConcurrencyLimit = common.GetPointer(0)
	}
}

func validateChannelUserDailyQuotaLimit(limit *int) error {
	if limit == nil || (*limit >= 0 && *limit <= maxChannelUserDailyQuotaLimit) {
		return nil
	}
	return fmt.Errorf("单用户每日额度必须是 0 到 %d 之间的整数", maxChannelUserDailyQuotaLimit)
}

func normalizeChannelUserDailyQuotaLimitForUpdate(channel *model.Channel, requestData map[string]any) {
	if channel == nil {
		return
	}
	if value, ok := requestData["user_daily_quota_limit"]; ok && value == nil {
		channel.UserDailyQuotaLimit = common.GetPointer(0)
	}
}

func validateChannelUserWeeklyQuotaLimit(limit *int) error {
	if limit == nil || (*limit >= 0 && *limit <= maxChannelUserWeeklyQuotaLimit) {
		return nil
	}
	return fmt.Errorf("单用户每周额度必须是 0 到 %d 之间的整数", maxChannelUserWeeklyQuotaLimit)
}

func normalizeChannelUserWeeklyQuotaLimitForUpdate(channel *model.Channel, requestData map[string]any) {
	if channel == nil {
		return
	}
	if value, ok := requestData["user_weekly_quota_limit"]; ok && value == nil {
		channel.UserWeeklyQuotaLimit = common.GetPointer(0)
	}
}

func acquireChannelUserConcurrencyGuard(c *gin.Context) (*channelUserConcurrencyGuard, *types.NewAPIError) {
	limit := common.GetContextKeyInt(c, constant.ContextKeyChannelUserConcurrencyLimit)
	if limit <= 0 {
		return nil, nil
	}
	channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	userID := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	baseContext := context.Background()
	if c.Request != nil {
		baseContext = c.Request.Context()
	}
	attemptContext, cancel := context.WithCancel(baseContext)
	guard := &channelUserConcurrencyGuard{
		baseContext: baseContext,
		cancel:      cancel,
		channelID:   channelID,
		userID:      userID,
		limit:       limit,
	}
	if c.Request != nil {
		c.Request = c.Request.WithContext(attemptContext)
	}

	lease, err := service.AcquireChannelUserConcurrency(attemptContext, channelID, userID, limit, func(renewErr error) {
		logger.LogWarn(attemptContext, fmt.Sprintf("渠道单用户并发租约丢失: channel_id=%d user_id=%d limit=%d error=%s", channelID, userID, limit, common.LocalLogPreview(renewErr.Error())))
		cancel()
	})
	if err != nil {
		cancel()
		if c.Request != nil {
			c.Request = c.Request.WithContext(baseContext)
		}
		apiErr := newChannelUserConcurrencyAPIError(err)
		logger.LogWarn(baseContext, fmt.Sprintf("获取渠道单用户并发租约失败: channel_id=%d user_id=%d limit=%d error_code=%s", channelID, userID, limit, apiErr.GetErrorCode()))
		recordRelayErrorLog(c, apiErr)
		return nil, apiErr
	}
	guard.lease = lease
	return guard, nil
}

func detachChannelUserConcurrencyGuardContext(c *gin.Context, guard *channelUserConcurrencyGuard) {
	if guard == nil || c.Request == nil {
		return
	}
	c.Request = c.Request.WithContext(guard.baseContext)
}

func channelUserConcurrencyLostSignal(guard *channelUserConcurrencyGuard) <-chan struct{} {
	if guard == nil {
		return nil
	}
	return guard.lease.LostSignal()
}

func finishChannelUserConcurrencyGuard(c *gin.Context, guard *channelUserConcurrencyGuard, currentErr *types.NewAPIError) *types.NewAPIError {
	if guard == nil {
		return currentErr
	}
	if err := guard.lease.Release(guard.baseContext); err != nil {
		logger.LogWarn(guard.baseContext, fmt.Sprintf("释放渠道单用户并发租约失败: channel_id=%d user_id=%d limit=%d error=%s", guard.channelID, guard.userID, guard.limit, common.LocalLogPreview(err.Error())))
	}
	guard.cancel()
	if c.Request != nil {
		c.Request = c.Request.WithContext(guard.baseContext)
	}
	if guard.lease.IsLost() {
		apiErr := newChannelUserConcurrencyAPIError(service.ErrChannelUserConcurrencyUnavailable)
		recordRelayErrorLog(c, apiErr)
		return apiErr
	}
	return currentErr
}

func newChannelUserConcurrencyAPIError(err error) *types.NewAPIError {
	if errors.Is(err, service.ErrChannelUserConcurrencyExceeded) {
		return types.NewOpenAIError(
			errors.New("channel user concurrency limit exceeded"),
			types.ErrorCodeChannelUserConcurrencyExceeded,
			http.StatusTooManyRequests,
			types.ErrOptionWithSkipRetry(),
		)
	}
	return types.NewOpenAIError(
		errors.New("channel user concurrency service unavailable"),
		types.ErrorCodeChannelUserConcurrencyUnavailable,
		http.StatusServiceUnavailable,
		types.ErrOptionWithSkipRetry(),
	)
}

func isChannelUserConcurrencyAPIError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	return err.GetErrorCode() == types.ErrorCodeChannelUserConcurrencyExceeded ||
		err.GetErrorCode() == types.ErrorCodeChannelUserConcurrencyUnavailable
}

func prepareChannelUserConcurrencyErrorLog(c *gin.Context, err *types.NewAPIError) bool {
	if !isChannelUserConcurrencyAPIError(err) {
		return true
	}
	if c.GetBool(channelUserConcurrencyErrorLogRecordedKey) {
		return false
	}
	c.Set(channelUserConcurrencyErrorLogRecordedKey, true)

	logOther, _ := common.GetContextKeyType[map[string]interface{}](c, constant.ContextKeyLogOther)
	if logOther == nil {
		logOther = map[string]interface{}{}
	}
	adminInfo, _ := logOther["admin_info"].(map[string]interface{})
	if adminInfo == nil {
		adminInfo = map[string]interface{}{}
		logOther["admin_info"] = adminInfo
	}
	adminInfo[channelUserConcurrencyErrorLogAdminInfoKey] = map[string]interface{}{
		"channel_id": common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		"user_id":    common.GetContextKeyInt(c, constant.ContextKeyUserId),
		"limit":      common.GetContextKeyInt(c, constant.ContextKeyChannelUserConcurrencyLimit),
		"error_code": err.GetErrorCode(),
	}
	common.SetContextKey(c, constant.ContextKeyLogOther, logOther)
	return true
}

func channelUserConcurrencyAPIErrorFromCode(code types.ErrorCode) *types.NewAPIError {
	switch code {
	case types.ErrorCodeChannelUserConcurrencyExceeded:
		return newChannelUserConcurrencyAPIError(service.ErrChannelUserConcurrencyExceeded)
	case types.ErrorCodeChannelUserConcurrencyUnavailable:
		return newChannelUserConcurrencyAPIError(service.ErrChannelUserConcurrencyUnavailable)
	default:
		return nil
	}
}

func recordChannelUserConcurrencyErrorCode(c *gin.Context, code types.ErrorCode) {
	apiErr := channelUserConcurrencyAPIErrorFromCode(code)
	if apiErr != nil {
		recordRelayErrorLog(c, apiErr)
	}
}

func channelUserConcurrencyMidjourneyHTTPStatus(response *dto.MidjourneyResponse) (int, bool) {
	if response == nil {
		return 0, false
	}
	switch types.ErrorCode(response.Description) {
	case types.ErrorCodeChannelUserConcurrencyExceeded:
		return http.StatusTooManyRequests, true
	case types.ErrorCodeChannelUserConcurrencyUnavailable:
		return http.StatusServiceUnavailable, true
	default:
		return 0, false
	}
}
