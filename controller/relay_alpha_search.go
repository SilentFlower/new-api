package controller

import (
	"net/http"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func prepareAlphaSearchBilling(c *gin.Context, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	billingModelName := relayInfo.ResolveBillingModelName()
	relayInfo.FreezeBillingModelName(billingModelName)
	groupRatioInfo := helper.HandleGroupRatio(c, relayInfo)
	result := service.ComputeToolCallQuota(service.ToolCallUsage{
		ModelName:         billingModelName,
		WebSearchCalls:    1,
		WebSearchToolName: service.ToolNameWebSearch,
	}, groupRatioInfo.GroupRatio)

	relayInfo.PriceData.GroupRatioInfo = groupRatioInfo
	relayInfo.PriceData.Quota = result.TotalQuota
	relayInfo.PriceData.QuotaToPreConsume = result.TotalQuota
	relayInfo.QuotaClamp = result.QuotaClamp
	relayInfo.ToolCallBilling = &result

	if result.QuotaClamp != nil {
		return types.NewErrorWithStatusCode(
			result.QuotaClamp,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	if result.TotalQuota == 0 {
		if relayInfo.Billing != nil {
			relayInfo.FinalPreConsumedQuota = relayInfo.Billing.GetPreConsumedQuota()
		}
		return nil
	}
	if apiErr := checkChannelUserQuotaLimits(c); apiErr != nil {
		return apiErr
	}
	if relayInfo.Billing != nil {
		if err := relayInfo.Billing.Reserve(result.TotalQuota); err != nil {
			return types.NewError(err, types.ErrorCodePreConsumeTokenQuotaFailed, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		relayInfo.FinalPreConsumedQuota = relayInfo.Billing.GetPreConsumedQuota()
		return nil
	}
	return service.PreConsumeBilling(c, result.TotalQuota, relayInfo)
}
