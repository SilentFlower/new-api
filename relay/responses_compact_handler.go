package relay

import (
	"fmt"
	"net/http"

	appconstant "github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func validateResponsesCompactEndpoint(info *relaycommon.RelayInfo) *types.NewAPIError {
	if !info.UsesResponsesCompactEndpoint() {
		return nil
	}
	switch info.ApiType {
	case appconstant.APITypeOpenAI, appconstant.APITypeCodex:
		return nil
	default:
		return types.NewErrorWithStatusCode(
			fmt.Errorf("unsupported endpoint %q for api type %d", "/v1/responses/compact", info.ApiType),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
}

func responsesRequestForHandler(info *relaycommon.RelayInfo) (*dto.OpenAIResponsesRequest, *types.NewAPIError) {
	var responsesReq *dto.OpenAIResponsesRequest
	switch req := info.Request.(type) {
	case *dto.OpenAIResponsesRequest:
		responsesReq = req
	case *dto.OpenAIResponsesCompactionRequest:
		responsesReq = responsesRequestFromCompaction(req)
	default:
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("invalid request type, expected dto.OpenAIResponsesRequest or dto.OpenAIResponsesCompactionRequest, got %T", info.Request),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	if info.UsesResponsesCompactEndpoint() {
		responsesReq = responsesRequestForCompaction(responsesReq)
	}
	return responsesReq, nil
}

func postResponsesCompactEndpointQuota(c *gin.Context, info *relaycommon.RelayInfo, usageDto *dto.Usage) *types.NewAPIError {
	originModelName := info.OriginModelName
	originPriceData := info.PriceData
	originBillingModelName := info.FrozenBillingModelName()
	defer func() {
		// Compact endpoint 临时按特殊请求查价，返回前必须恢复进入 helper 前的冻结计费快照。
		info.OriginModelName = originModelName
		info.PriceData = originPriceData
		info.FreezeBillingModelName(originBillingModelName)
	}()

	_, err := helper.ModelPriceHelper(c, info, info.GetEstimatePromptTokens(), &types.TokenCountMeta{})
	if err != nil {
		return types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry(), types.ErrOptionWithStatusCode(http.StatusBadRequest))
	}
	service.SetResponsesCompactAudit(c, info, "completed")
	service.PostTextConsumeQuota(c, info, usageDto, nil)
	return nil
}

func responsesCompactAuditOutcome(info *relaycommon.RelayInfo) string {
	outcome := "completed"
	if info.IsResponsesCompactV2() && info.ResponsesUsageInfo != nil {
		if info.ResponsesUsageInfo.TerminalEventType != "" {
			outcome = info.ResponsesUsageInfo.TerminalEventType
		} else if !info.ResponsesUsageInfo.ResponseCompleted {
			outcome = "stream_incomplete"
		}
	}
	return outcome
}

func responsesRequestFromCompaction(req *dto.OpenAIResponsesCompactionRequest) *dto.OpenAIResponsesRequest {
	if req == nil {
		return nil
	}
	return &dto.OpenAIResponsesRequest{
		Model:             req.Model,
		Input:             req.Input,
		Instructions:      req.Instructions,
		Tools:             req.Tools,
		ParallelToolCalls: req.ParallelToolCalls,
		Reasoning:         req.Reasoning,
		ServiceTier:       req.ServiceTier,
		PromptCacheKey:    req.PromptCacheKey,
		Text:              req.Text,
	}
}

func responsesRequestForCompaction(req *dto.OpenAIResponsesRequest) *dto.OpenAIResponsesRequest {
	if req == nil {
		return nil
	}
	return &dto.OpenAIResponsesRequest{
		Model:             req.Model,
		Input:             req.Input,
		Instructions:      req.Instructions,
		Tools:             req.Tools,
		ParallelToolCalls: req.ParallelToolCalls,
		Reasoning:         req.Reasoning,
		ServiceTier:       req.ServiceTier,
		PromptCacheKey:    req.PromptCacheKey,
		Text:              req.Text,
	}
}
