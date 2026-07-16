package relay

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// ResponsesHelper 转发 OpenAI Responses、Compact V1 和历史 body bridge 请求。
// @param c 当前 Gin 请求上下文。
// @param info 当前 Relay 请求信息。
// @return 请求转换、上游调用、响应处理或计费错误。
func ResponsesHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)
	if info.UsesResponsesCompactEndpoint() {
		switch info.ApiType {
		case appconstant.APITypeOpenAI, appconstant.APITypeCodex:
		default:
			return types.NewErrorWithStatusCode(
				fmt.Errorf("unsupported endpoint %q for api type %d", "/v1/responses/compact", info.ApiType),
				types.ErrorCodeInvalidRequest,
				http.StatusBadRequest,
				types.ErrOptionWithSkipRetry(),
			)
		}
	}

	var responsesReq *dto.OpenAIResponsesRequest
	switch req := info.Request.(type) {
	case *dto.OpenAIResponsesRequest:
		responsesReq = req
	case *dto.OpenAIResponsesCompactionRequest:
		responsesReq = responsesRequestFromCompaction(req)
	default:
		return types.NewErrorWithStatusCode(
			fmt.Errorf("invalid request type, expected dto.OpenAIResponsesRequest or dto.OpenAIResponsesCompactionRequest, got %T", info.Request),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	request, err := common.DeepCopy(responsesReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to GeneralOpenAIRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if info.UsesResponsesCompactEndpoint() {
		request = responsesRequestForCompaction(request)
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	var requestBody io.Reader
	if (model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled) && !info.UsesResponsesCompactEndpoint() {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
		}
		requestBody = common.ReaderOnly(storage)
	} else {
		convertedRequest, err := adaptor.ConvertOpenAIResponsesRequest(c, info, *request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
		jsonData, err := common.Marshal(convertedRequest)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// remove disabled fields for OpenAI Responses API
		jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// apply param override
		if len(info.ParamOverride) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				return newAPIErrorFromParamOverride(err)
			}
		}

		if info.IsResponsesCompact() {
			logger.LogDebug(c, "Responses Compact upstream request prepared, bytes=%d", len(jsonData))
		} else {
			logger.LogDebug(c, "requestBody: %s", jsonData)
		}
		body, size, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		defer closer.Close()
		jsonData = nil
		info.UpstreamRequestBodySize = size
		requestBody = body
	}

	var httpResp *http.Response
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")

	if resp != nil {
		httpResp = resp.(*http.Response)

		if httpResp.StatusCode != http.StatusOK {
			newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
			// reset status code 重置状态码
			service.ResetStatusCode(newAPIError, statusCodeMappingStr)
			return newAPIError
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	usageDto := usage.(*dto.Usage)
	if info.UsesResponsesCompactEndpoint() {
		originModelName := info.OriginModelName
		originPriceData := info.PriceData
		originBillingModelName := info.ResolvedBillingModelName

		_, err := helper.ModelPriceHelper(c, info, info.GetEstimatePromptTokens(), &types.TokenCountMeta{})
		if err != nil {
			info.OriginModelName = originModelName
			info.PriceData = originPriceData
			info.ResolvedBillingModelName = originBillingModelName
			return types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry(), types.ErrOptionWithStatusCode(http.StatusBadRequest))
		}
		service.SetResponsesCompactAudit(c, info, "completed")
		service.PostTextConsumeQuota(c, info, usageDto, nil)

		info.OriginModelName = originModelName
		info.PriceData = originPriceData
		info.ResolvedBillingModelName = originBillingModelName
		return nil
	}

	if strings.HasPrefix(info.OriginModelName, "gpt-4o-audio") {
		service.PostAudioConsumeQuota(c, info, usageDto, "")
	} else {
		outcome := "completed"
		if info.IsResponsesCompactV2() && info.ResponsesUsageInfo != nil {
			if info.ResponsesUsageInfo.TerminalEventType != "" {
				outcome = info.ResponsesUsageInfo.TerminalEventType
			} else if !info.ResponsesUsageInfo.ResponseCompleted {
				outcome = "stream_incomplete"
			}
		}
		service.SetResponsesCompactAudit(c, info, outcome)
		service.PostTextConsumeQuota(c, info, usageDto, nil)
	}
	return nil
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
