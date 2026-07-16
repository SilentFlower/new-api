package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/websearch"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// syncAnthropicReasoningEffort 从 Anthropic output_config 同步日志字段。
func syncAnthropicReasoningEffort(info *relaycommon.RelayInfo, outputConfig []byte) {
	if info == nil || info.ChannelMeta == nil || info.ChannelType != constant.ChannelTypeAnthropic {
		return
	}

	effort := gjson.GetBytes(outputConfig, "effort")
	if effort.Type != gjson.String {
		info.ReasoningEffort = ""
		return
	}
	info.ReasoningEffort = effort.String()
}

// syncAnthropicReasoningEffortFromRequestBody 从最终上游请求体同步参数覆盖后的日志字段。
func syncAnthropicReasoningEffortFromRequestBody(info *relaycommon.RelayInfo, requestBody []byte) {
	outputConfig := gjson.GetBytes(requestBody, "output_config")
	syncAnthropicReasoningEffort(info, []byte(outputConfig.Raw))
}

func ClaudeHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {

	prepared := common.GetContextKeyBool(c, constant.ContextKeyVisionAssistPrepared)
	if !prepared {
		info.InitChannelMeta(c)
	}

	claudeReq, ok := info.Request.(*dto.ClaudeRequest)

	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected *dto.ClaudeRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	request, err := common.DeepCopy(claudeReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to ClaudeRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	if !prepared {
		err = helper.ModelMappedHelper(c, info, request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
		}
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	if request.MaxTokens == nil || *request.MaxTokens == 0 {
		defaultMaxTokens := uint(model_setting.GetClaudeSettings().GetDefaultMaxTokens(request.Model))
		request.MaxTokens = &defaultMaxTokens
	}

	if baseModel, effortLevel, ok := reasoning.TrimEffortSuffix(request.Model); ok && effortLevel != "" &&
		(strings.HasPrefix(request.Model, "claude-opus-4-6") ||
			strings.HasPrefix(request.Model, "claude-opus-4-7") ||
			strings.HasPrefix(request.Model, "claude-opus-4-8")) {
		request.Model = baseModel
		request.Thinking = &dto.Thinking{
			Type: "adaptive",
		}
		request.OutputConfig = json.RawMessage(fmt.Sprintf(`{"effort":"%s"}`, effortLevel))
		if strings.HasPrefix(request.Model, "claude-opus-4-7") ||
			strings.HasPrefix(request.Model, "claude-opus-4-8") {
			// Opus 4.7/4.8 reject non-default temperature/top_p/top_k with 400
			// and defaults display to "omitted"; restore the 4.6 visible summary.
			request.Thinking.Display = "summarized"
			request.Temperature = nil
			request.TopP = nil
			request.TopK = nil
		} else {
			request.Temperature = common.GetPointer[float64](1.0)
		}
		info.UpstreamModelName = request.Model
	} else if model_setting.GetClaudeSettings().ThinkingAdapterEnabled &&
		strings.HasSuffix(request.Model, "-thinking") {
		if request.Thinking == nil {
			baseModel := strings.TrimSuffix(request.Model, "-thinking")
			if strings.HasPrefix(baseModel, "claude-opus-4-7") ||
				strings.HasPrefix(baseModel, "claude-opus-4-8") {
				// Opus 4.7/4.8 reject thinking.type="enabled"; use adaptive at high effort.
				request.Thinking = &dto.Thinking{Type: "adaptive", Display: "summarized"}
				request.OutputConfig = json.RawMessage(`{"effort":"high"}`)
				request.Temperature = nil
				request.TopP = nil
				request.TopK = nil
			} else {
				// 因为BudgetTokens 必须大于1024
				if request.MaxTokens == nil || *request.MaxTokens < 1280 {
					request.MaxTokens = common.GetPointer[uint](1280)
				}

				// BudgetTokens 为 max_tokens 的 80%
				request.Thinking = &dto.Thinking{
					Type:         "enabled",
					BudgetTokens: common.GetPointer[int](int(float64(*request.MaxTokens) * model_setting.GetClaudeSettings().ThinkingAdapterBudgetTokensPercentage)),
				}
				// TODO: 临时处理
				// https://docs.anthropic.com/en/docs/build-with-claude/extended-thinking#important-considerations-when-using-extended-thinking
				request.Temperature = common.GetPointer[float64](1.0)
			}
		}
		if !model_setting.ShouldPreserveThinkingSuffix(info.OriginModelName) {
			request.Model = strings.TrimSuffix(request.Model, "-thinking")
		}
		info.UpstreamModelName = request.Model
	}

	if info.ChannelSetting.SystemPrompt != "" {
		if request.System == nil {
			request.SetStringSystem(info.ChannelSetting.SystemPrompt)
		} else if info.ChannelSetting.SystemPromptOverride {
			common.SetContextKey(c, constant.ContextKeySystemPromptOverride, true)
			if request.IsStringSystem() {
				existing := strings.TrimSpace(request.GetStringSystem())
				if existing == "" {
					request.SetStringSystem(info.ChannelSetting.SystemPrompt)
				} else {
					request.SetStringSystem(info.ChannelSetting.SystemPrompt + "\n" + existing)
				}
			} else {
				systemContents := request.ParseSystem()
				newSystem := dto.ClaudeMediaMessage{Type: dto.ContentTypeText}
				newSystem.SetText(info.ChannelSetting.SystemPrompt)
				if len(systemContents) == 0 {
					request.System = []dto.ClaudeMediaMessage{newSystem}
				} else {
					request.System = append([]dto.ClaudeMediaMessage{newSystem}, systemContents...)
				}
			}
		}
	}

	if websearch.IsPureClaudeWebSearchRequest(request) && shouldHandleClaudeWebSearchEmulation(info) {
		return handleClaudeWebSearchEmulation(c, info, request)
	}

	if !model_setting.GetGlobalSettings().PassThroughRequestEnabled &&
		!info.ChannelSetting.PassThroughBodyEnabled &&
		service.ShouldChatCompletionsUseResponsesGlobal(info.ChannelId, info.ChannelType, info.OriginModelName) {
		result, convErr := service.ConvertRequest(c, info, types.RelayFormatOpenAI, request)
		if convErr != nil {
			return types.NewError(convErr, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		openAIRequest, ok := result.Value.(*dto.GeneralOpenAIRequest)
		if !ok {
			return types.NewError(fmt.Errorf("expected OpenAI chat completions request, got %T", result.Value), types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		usage, newApiErr := chatCompletionsViaResponses(c, info, adaptor, openAIRequest)
		if newApiErr != nil {
			return newApiErr
		}

		service.PostTextConsumeQuota(c, info, usage, nil)
		return nil
	}

	var requestBody io.Reader
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		syncAnthropicReasoningEffort(info, request.OutputConfig)
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		info.UpstreamRequestBodySize = storage.Size()
		requestBody = common.ReaderOnly(storage)
	} else {
		convertedRequest, err := adaptor.ConvertClaudeRequest(c, info, request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
		jsonData, err := common.Marshal(convertedRequest)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// remove disabled fields for Claude API
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
		syncAnthropicReasoningEffortFromRequestBody(info, jsonData)

		logger.LogDebug(c, "requestBody: %s", jsonData)
		body, size, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		defer closer.Close()
		jsonData = nil
		info.UpstreamRequestBodySize = size
		requestBody = body
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")
	var httpResp *http.Response
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	if resp != nil {
		httpResp = resp.(*http.Response)
		info.IsStream = info.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")
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

	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), nil)
	return nil
}

func shouldHandleClaudeWebSearchEmulation(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil {
		return false
	}
	return info.ChannelSetting.WebSearch.Enabled
}

func handleClaudeWebSearchEmulation(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) *types.NewAPIError {
	settings := info.ChannelSetting.WebSearch
	settings.Normalize()
	if !settings.Enabled {
		return types.NewErrorWithStatusCode(fmt.Errorf("渠道未启用 Claude Code WebSearch"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if err := settings.ValidateForRelay(); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	query := websearch.ExtractClaudeWebSearchQuery(request)
	if query == "" {
		return types.NewErrorWithStatusCode(fmt.Errorf("无法从最后一条 user 消息中提取 WebSearch 查询"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	httpClient, err := service.NewProxyHttpClient(info.ChannelSetting.Proxy)
	if err != nil {
		return types.NewErrorWithStatusCode(fmt.Errorf("WebSearch 代理配置错误: %w", err), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	provider, err := websearch.NewProvider(settings, httpClient)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	searchCtx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	searchResp, err := provider.Search(searchCtx, websearch.SearchRequest{
		Query:        query,
		MaxResults:   settings.MaxResults,
		SearchDepth:  settings.SearchDepth,
		Freshness:    settings.Freshness,
		ContentTypes: settings.ContentTypes,
	})
	if err != nil {
		return types.NewErrorWithStatusCode(fmt.Errorf("WebSearch provider %s 调用失败: %w", provider.Name(), err), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	if searchResp == nil {
		return types.NewErrorWithStatusCode(fmt.Errorf("WebSearch provider %s 返回空响应", provider.Name()), types.ErrorCodeEmptyResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}

	modelName := strings.TrimSpace(info.UpstreamModelName)
	if modelName == "" {
		modelName = request.Model
	}
	inputTokens := info.GetEstimatePromptTokens()
	if inputTokens <= 0 {
		inputTokens = len([]rune(query))/4 + 1
	}
	outputTokens := websearch.EstimateOutputTokens(query, searchResp.Results)
	messageID := "msg_" + info.RequestId
	toolUseID := "srvtoolu_" + info.RequestId
	usage := websearch.BuildUsage(inputTokens, outputTokens)

	info.SetFirstResponseTime()
	c.Set("claude_web_search_requests", 1)
	if request.IsStream(c) {
		info.IsStream = true
		if err := writeClaudeWebSearchStream(c, messageID, toolUseID, modelName, query, searchResp.Results, inputTokens, outputTokens); err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
		}
	} else {
		response := websearch.BuildClaudeWebSearchResponse(messageID, toolUseID, modelName, query, searchResp.Results, inputTokens, outputTokens)
		jsonData, err := common.Marshal(response)
		if err != nil {
			return types.NewError(err, types.ErrorCodeJsonMarshalFailed, types.ErrOptionWithSkipRetry())
		}
		c.Data(http.StatusOK, "application/json", jsonData)
	}
	service.PostTextConsumeQuota(c, info, usage, nil)
	return nil
}

func writeClaudeWebSearchStream(c *gin.Context, messageID string, toolUseID string, modelName string, query string, results []websearch.SearchResult, inputTokens int, outputTokens int) error {
	helper.SetEventStreamHeaders(c)
	c.Status(http.StatusOK)
	for _, event := range websearch.BuildClaudeWebSearchStreamEvents(messageID, toolUseID, modelName, query, results, inputTokens, outputTokens) {
		if err := helper.ClaudeData(c, *event); err != nil {
			return err
		}
	}
	return nil
}
