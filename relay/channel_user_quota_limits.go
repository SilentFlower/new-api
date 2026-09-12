package relay

import (
	"net/http"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// checkChannelUserQuotaLimits 在 Relay 入口按预算行检查所选渠道的全部金额限制。
func checkChannelUserQuotaLimits(c *gin.Context) *types.NewAPIError {
	return service.CheckSelectedChannelPeriodLimits(c)
}

// channelUserQuotaLimitMidjourneyError 把额度错误映射为 Midjourney 响应：429 用 30，其余用 4。
func channelUserQuotaLimitMidjourneyError(apiErr *types.NewAPIError) *dto.MidjourneyResponse {
	if apiErr == nil {
		return nil
	}
	code := 4
	if apiErr.StatusCode == http.StatusTooManyRequests {
		code = 30
	}
	return &dto.MidjourneyResponse{Code: code, Description: string(apiErr.GetErrorCode()), Result: apiErr.Error()}
}
