package relay

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

func checkChannelUserQuotaLimits(c *gin.Context) *types.NewAPIError {
	if apiErr := checkChannelUserDailyQuota(c); apiErr != nil {
		return apiErr
	}
	return checkChannelUserWeeklyQuota(c)
}

func channelUserQuotaLimitMidjourneyError(apiErr *types.NewAPIError) *dto.MidjourneyResponse {
	if apiErr == nil {
		return nil
	}
	if apiErr.GetErrorCode() == types.ErrorCodeChannelUserWeeklyQuotaExceeded ||
		apiErr.GetErrorCode() == types.ErrorCodeChannelUserWeeklyQuotaUnavailable {
		return channelUserWeeklyQuotaMidjourneyError(apiErr)
	}
	return channelUserDailyQuotaMidjourneyError(apiErr)
}

func checkChannelUserWeeklyQuota(c *gin.Context) *types.NewAPIError {
	limit := common.GetContextKeyInt(c, constant.ContextKeyChannelUserWeeklyQuotaLimit)
	if limit <= 0 {
		return nil
	}
	usedQuota, err := service.CheckChannelUserWeeklyQuota(
		c,
		common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		common.GetContextKeyInt(c, constant.ContextKeyUserId),
		limit,
	)
	common.SetContextKey(c, constant.ContextKeyChannelUserWeeklyQuotaUsed, usedQuota)
	if err == nil {
		return nil
	}
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

func channelUserWeeklyQuotaMidjourneyError(apiErr *types.NewAPIError) *dto.MidjourneyResponse {
	if apiErr == nil {
		return nil
	}
	code := 4
	if apiErr.GetErrorCode() == types.ErrorCodeChannelUserWeeklyQuotaExceeded {
		code = 30
	}
	return &dto.MidjourneyResponse{
		Code:        code,
		Description: string(apiErr.GetErrorCode()),
		Result:      apiErr.Error(),
	}
}
