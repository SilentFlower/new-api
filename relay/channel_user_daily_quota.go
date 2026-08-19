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

func checkChannelUserDailyQuota(c *gin.Context) *types.NewAPIError {
	limit := common.GetContextKeyInt(c, constant.ContextKeyChannelUserDailyQuotaLimit)
	if limit <= 0 {
		return nil
	}
	usedQuota, err := service.CheckChannelUserDailyQuota(
		c,
		common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		common.GetContextKeyInt(c, constant.ContextKeyUserId),
		limit,
	)
	common.SetContextKey(c, constant.ContextKeyChannelUserDailyQuotaUsed, usedQuota)
	if err == nil {
		return nil
	}
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

func channelUserDailyQuotaMidjourneyError(apiErr *types.NewAPIError) *dto.MidjourneyResponse {
	if apiErr == nil {
		return nil
	}
	code := 4
	if apiErr.GetErrorCode() == types.ErrorCodeChannelUserDailyQuotaExceeded {
		code = 30
	}
	return &dto.MidjourneyResponse{
		Code:        code,
		Description: string(apiErr.GetErrorCode()),
		Result:      apiErr.Error(),
	}
}
