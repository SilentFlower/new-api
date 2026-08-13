package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

type responsesCompactChannelTestResponse struct {
	Usage json.RawMessage `json:"usage"`
}

func normalizeResponsesCompactChannelTestModel(modelName, endpointType string) string {
	modelName = strings.TrimSpace(modelName)
	if constant.EndpointType(endpointType) != constant.EndpointTypeOpenAIResponseCompact {
		return modelName
	}
	return strings.TrimSuffix(modelName, ratio_setting.CompactModelSuffix)
}

func testResponsesCompactPassthroughChannel(
	c *gin.Context,
	channel *model.Channel,
	testUserID int,
	startedAt time.Time,
	info *relaycommon.RelayInfo,
	request dto.Request,
) testResult {
	if apiErr := relay.PrepareResponsesCompactPassthrough(c, info); apiErr != nil {
		return testResult{context: c, localErr: apiErr, newAPIError: apiErr}
	}
	if err := attachTestBillingRequestInput(info, request); err != nil {
		apiErr := types.NewError(err, types.ErrorCodeJsonMarshalFailed)
		return testResult{context: c, localErr: err, newAPIError: apiErr}
	}

	priceData, err := helper.ModelPriceHelper(c, info, 0, request.GetTokenCountMeta())
	if err != nil {
		apiErr := types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return testResult{context: c, localErr: err, newAPIError: apiErr}
	}

	usage, apiErr := executeResponsesCompactChannelTest(c, info, request)
	if apiErr != nil {
		return testResult{context: c, localErr: apiErr, newAPIError: apiErr}
	}

	info.SetEstimatePromptTokens(usage.PromptTokens)
	service.SetResponsesCompactAudit(c, info, "completed")
	quota, tieredResult := settleTestQuota(info, priceData, usage)
	consumedTime := float64(time.Since(startedAt).Milliseconds()) / 1000.0
	other := buildTestLogOther(c, info, priceData, usage, tieredResult)
	model.RecordConsumeLog(c, testUserID, model.RecordConsumeLogParams{
		ChannelId:        channel.Id,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		ModelName:        info.BillingModelName(),
		TokenName:        "模型测试",
		Quota:            quota,
		Content:          "模型测试",
		UseTimeSeconds:   int(consumedTime),
		IsStream:         info.IsStream,
		Group:            info.UsingGroup,
		Other:            other,
	})
	common.SysLog(fmt.Sprintf("testing channel #%d with Responses Compact succeeded", channel.Id))
	return testResult{context: c}
}

func executeResponsesCompactChannelTest(c *gin.Context, info *relaycommon.RelayInfo, request dto.Request) (*dto.Usage, *types.NewAPIError) {
	if info == nil || info.ChannelMeta == nil {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("Responses Compact channel test context is incomplete"),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	switch info.ApiType {
	case constant.APITypeOpenAI, constant.APITypeCodex, constant.APITypeAdvancedCustom:
	default:
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("unsupported Responses Compact channel test api type: %d", info.ApiType),
			types.ErrorCodeInvalidApiType,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	adaptor := relay.GetAdaptor(info.ApiType)
	if adaptor == nil {
		return nil, types.NewError(
			fmt.Errorf("invalid api type: %d, adaptor is nil", info.ApiType),
			types.ErrorCodeInvalidApiType,
			types.ErrOptionWithSkipRetry(),
		)
	}

	requestBody, err := common.Marshal(request)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeJsonMarshalFailed, types.ErrOptionWithSkipRetry())
	}
	info.UpstreamRequestBodySize = int64(len(requestBody))
	c.Request.Body = io.NopCloser(bytes.NewReader(requestBody))
	adaptor.Init(info)

	response, err := adaptor.DoRequest(c, info, bytes.NewReader(requestBody))
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	httpResponse, ok := response.(*http.Response)
	if !ok || httpResponse == nil {
		return nil, types.NewOpenAIError(
			fmt.Errorf("invalid Responses Compact channel test response: %T", response),
			types.ErrorCodeBadResponse,
			http.StatusInternalServerError,
		)
	}
	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		return nil, service.RelayErrorHandlerWithoutBodyLog(c.Request.Context(), httpResponse)
	}
	defer service.CloseResponseBodyGracefully(httpResponse)

	info.SetFirstResponseTime()
	responseBody, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	if err := validateTestResponseBody(responseBody, false); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	var payload responsesCompactChannelTestResponse
	if err := common.Unmarshal(responseBody, &payload); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	usage, valid := relay.ParseResponsesCompactPassthroughUsage(payload.Usage)
	if !valid {
		return nil, types.NewOpenAIError(
			fmt.Errorf("Responses Compact channel test response does not contain valid usage"),
			types.ErrorCodeBadResponseBody,
			http.StatusInternalServerError,
		)
	}
	return usage, nil
}
