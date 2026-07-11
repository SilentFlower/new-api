package relay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/relayconvert"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type visionAssistPreparedRequest struct {
	info *relaycommon.RelayInfo
	req  dto.Request
	mode string
}

// PrepareRequestForSelectedChannel 在主请求计费前完成渠道元信息、模型映射与视觉辅助改写。
func PrepareRequestForSelectedChannel(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if c == nil || info == nil || info.Request == nil {
		return nil
	}
	info.InitChannelMeta(c)
	if err := helper.ModelMappedHelper(c, info, info.Request); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	common.SetContextKey(c, constant.ContextKeyVisionAssistPrepared, true)
	if shouldSkipVisionAssistPreprocess(c, info) {
		return nil
	}
	return service.ApplyVisionAssist(c, info, callVisionAssistModel)
}

func shouldSkipVisionAssistPreprocess(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if common.GetContextKeyBool(c, constant.ContextKeyVisionAssistProcessing) {
		return true
	}
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled {
		return true
	}
	if info.ChannelSetting.PassThroughBodyEnabled {
		return true
	}
	if info.RelayFormat == types.RelayFormatClaude {
		return false
	}
	if info.RelayFormat == types.RelayFormatOpenAI && info.RelayMode == relayconstant.RelayModeChatCompletions {
		return false
	}
	return info.RelayFormat != types.RelayFormatOpenAIResponses || info.RelayMode != relayconstant.RelayModeResponses
}

func callVisionAssistModel(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []service.VisionAssistImage) ([]service.VisionAssistResult, *types.NewAPIError) {
	if c == nil || info == nil || request == nil {
		return nil, nil
	}
	setting := info.ChannelSetting.VisionAssist
	channelModel, err := model.CacheGetChannel(setting.AssistChannelId)
	if err != nil {
		return nil, types.NewError(fmt.Errorf("获取视觉辅助渠道失败: %w", err), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channelModel.Status != common.ChannelStatusEnabled {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("视觉辅助渠道#%d未启用", channelModel.Id), types.ErrorCodeGetChannelFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}

	restore, apiErr := switchContextToVisionAssistChannel(c, channelModel, request.Model)
	if apiErr != nil {
		return nil, apiErr
	}
	defer restore()

	prepared, apiErr := prepareVisionAssistRequest(c, info, request, channelModel)
	if apiErr != nil {
		return nil, apiErr
	}
	assistInfo := prepared.info
	common.SetContextKey(c, constant.ContextKeyVisionAssistEndpointMode, prepared.mode)

	meta := prepared.req.GetTokenCountMeta()
	tokens, err := service.EstimateRequestToken(c, meta, assistInfo)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeCountTokenFailed, types.ErrOptionWithSkipRetry())
	}
	assistInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, assistInfo, tokens, meta)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest), types.ErrOptionWithSkipRetry())
	}
	if !priceData.FreeModel {
		if apiErr := service.PreConsumeBilling(c, priceData.QuotaToPreConsume, assistInfo); apiErr != nil {
			return nil, apiErr
		}
	}

	text, usage, apiErr := doVisionAssistRequest(c, assistInfo, prepared.req, prepared.mode)
	if apiErr != nil {
		if assistInfo.Billing != nil {
			assistInfo.Billing.Refund(c)
		}
		return nil, apiErr
	}
	service.PostTextConsumeQuota(c, assistInfo, usage, []string{"视觉辅助识别"})

	results := make([]service.VisionAssistResult, 0, len(images))
	for _, image := range images {
		results = append(results, service.VisionAssistResult{
			Image: image,
			Text:  text,
		})
	}
	return results, nil
}

func prepareVisionAssistRequest(c *gin.Context, parent *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, channelModel *model.Channel) (*visionAssistPreparedRequest, *types.NewAPIError) {
	mode := resolveVisionAssistEndpointMode(parent.ChannelSetting.VisionAssist.EndpointMode, channelModel.Type, strings.TrimSpace(request.Model))
	if err := validateVisionAssistEndpointMode(mode, channelModel.Type); err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	switch mode {
	case service.VisionAssistEndpointModeOpenAIResponses:
		responsesRequest, err := service.ChatCompletionsRequestToResponsesRequest(request)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		assistInfo := buildVisionAssistRelayInfo(c, parent, responsesRequest, mode)
		if err := helper.ModelMappedHelper(c, assistInfo, responsesRequest); err != nil {
			return nil, types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
		}
		return &visionAssistPreparedRequest{info: assistInfo, req: responsesRequest, mode: mode}, nil
	case service.VisionAssistEndpointModeAnthropicMessages:
		claudeRequest, err := relayconvert.OpenAIChatRequestToClaudeMessages(c, *request)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		assistInfo := buildVisionAssistRelayInfo(c, parent, claudeRequest, mode)
		if err := helper.ModelMappedHelper(c, assistInfo, claudeRequest); err != nil {
			return nil, types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
		}
		return &visionAssistPreparedRequest{info: assistInfo, req: claudeRequest, mode: mode}, nil
	case service.VisionAssistEndpointModeGeminiNative:
		geminiRequest, err := buildVisionAssistGeminiRequest(c, request)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		assistInfo := buildVisionAssistRelayInfo(c, parent, geminiRequest, mode)
		if err := helper.ModelMappedHelper(c, assistInfo, geminiRequest); err != nil {
			return nil, types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
		}
		return &visionAssistPreparedRequest{info: assistInfo, req: geminiRequest, mode: mode}, nil
	default:
		assistInfo := buildVisionAssistRelayInfo(c, parent, request, service.VisionAssistEndpointModeOpenAIChat)
		if err := helper.ModelMappedHelper(c, assistInfo, request); err != nil {
			return nil, types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
		}
		return &visionAssistPreparedRequest{info: assistInfo, req: request, mode: service.VisionAssistEndpointModeOpenAIChat}, nil
	}
}

func buildVisionAssistRelayInfo(c *gin.Context, parent *relaycommon.RelayInfo, request dto.Request, mode string) *relaycommon.RelayInfo {
	var assistInfo *relaycommon.RelayInfo
	switch req := request.(type) {
	case *dto.OpenAIResponsesRequest:
		assistInfo = relaycommon.GenRelayInfoResponses(c, req)
		assistInfo.RequestURLPath = "/v1/responses"
	case *dto.ClaudeRequest:
		assistInfo = relaycommon.GenRelayInfoClaude(c, req)
		assistInfo.RequestURLPath = "/v1/messages"
	case *dto.GeminiChatRequest:
		assistInfo = relaycommon.GenRelayInfoGemini(c, req)
		assistInfo.RelayMode = relayconstant.RelayModeGemini
		assistInfo.RequestURLPath = "/v1beta/models/" + strings.TrimSpace(parent.ChannelSetting.VisionAssist.AssistModel) + ":generateContent"
	default:
		assistInfo = relaycommon.GenRelayInfoOpenAI(c, request)
		assistInfo.RelayMode = relayconstant.RelayModeChatCompletions
		assistInfo.RequestURLPath = "/v1/chat/completions"
	}
	// 此时 context 已切到辅助渠道，必须初始化辅助渠道元信息，否则适配器会拿空 base_url 拼出相对 URL。
	assistInfo.InitChannelMeta(c)
	assistInfo.RequestHeaders = cloneVisionAssistHeaders(parent.RequestHeaders)
	assistInfo.UserSetting = parent.UserSetting
	assistInfo.UserQuota = parent.UserQuota
	assistInfo.UserEmail = parent.UserEmail
	assistInfo.TokenGroup = parent.TokenGroup
	assistInfo.UsingGroup = parent.UsingGroup
	assistInfo.UserGroup = parent.UserGroup
	assistInfo.TokenId = parent.TokenId
	assistInfo.TokenKey = parent.TokenKey
	assistInfo.TokenUnlimited = parent.TokenUnlimited
	assistInfo.IsPlayground = parent.IsPlayground
	assistInfo.OriginModelName = strings.TrimSpace(parent.ChannelSetting.VisionAssist.AssistModel)
	assistInfo.RequestId = parent.RequestId + ":vision_assist:" + common.GetRandomString(8)
	assistInfo.StartTime = time.Now()
	assistInfo.FirstResponseTime = assistInfo.StartTime.Add(-time.Second)
	assistInfo.InitRequestConversionChain()
	common.SetContextKey(c, constant.ContextKeyVisionAssistEndpointMode, mode)
	return assistInfo
}

func cloneVisionAssistHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	clone := make(map[string]string, len(headers))
	for key, value := range headers {
		clone[key] = value
	}
	return clone
}

func resolveVisionAssistEndpointMode(configuredMode string, channelType int, modelName string) string {
	mode := strings.ToLower(strings.TrimSpace(configuredMode))
	switch mode {
	case service.VisionAssistEndpointModeOpenAIChat,
		service.VisionAssistEndpointModeOpenAIResponses,
		service.VisionAssistEndpointModeAnthropicMessages,
		service.VisionAssistEndpointModeGeminiNative:
		return mode
	}

	modelName = strings.ToLower(strings.TrimSpace(modelName))
	switch channelType {
	case constant.ChannelTypeGemini:
		return service.VisionAssistEndpointModeGeminiNative
	case constant.ChannelTypeVertexAi:
		if strings.HasPrefix(modelName, "claude") {
			return service.VisionAssistEndpointModeAnthropicMessages
		}
		return service.VisionAssistEndpointModeGeminiNative
	case constant.ChannelTypeAnthropic:
		return service.VisionAssistEndpointModeAnthropicMessages
	case constant.ChannelTypeAws:
		if strings.Contains(modelName, "claude") {
			return service.VisionAssistEndpointModeAnthropicMessages
		}
	}
	return service.VisionAssistEndpointModeOpenAIChat
}

func validateVisionAssistEndpointMode(mode string, channelType int) error {
	if mode != service.VisionAssistEndpointModeGeminiNative {
		return nil
	}
	switch channelType {
	case constant.ChannelTypeGemini, constant.ChannelTypeVertexAi:
		return nil
	default:
		return fmt.Errorf("视觉辅助端点模式 gemini_native 需要 Gemini 或 Vertex AI 辅助渠道，当前渠道类型: %d", channelType)
	}
}

func buildVisionAssistGeminiRequest(c *gin.Context, request *dto.GeneralOpenAIRequest) (*dto.GeminiChatRequest, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	parts := make([]dto.GeminiPart, 0)
	for _, message := range request.Messages {
		if message.Role != "" && message.Role != "user" {
			continue
		}
		for _, content := range message.ParseContent() {
			switch content.Type {
			case dto.ContentTypeText:
				if strings.TrimSpace(content.Text) != "" {
					parts = append(parts, dto.GeminiPart{Text: content.Text})
				}
			case dto.ContentTypeImageURL:
				image := content.GetImageMedia()
				if image == nil || strings.TrimSpace(image.Url) == "" {
					continue
				}
				source := types.NewFileSourceFromData(image.Url, image.MimeType)
				base64Data, mimeType, err := service.GetBase64Data(c, source, "formatting image for Gemini vision assist")
				if err != nil {
					return nil, fmt.Errorf("get file data from '%s' failed: %w", source.GetIdentifier(), err)
				}
				if strings.TrimSpace(mimeType) == "" {
					mimeType = image.MimeType
				}
				if strings.TrimSpace(mimeType) == "" {
					mimeType = "image/png"
				}
				parts = append(parts, dto.GeminiPart{
					InlineData: &dto.GeminiInlineData{
						MimeType: mimeType,
						Data:     base64Data,
					},
				})
			}
		}
	}
	if len(parts) == 0 {
		return nil, errors.New("vision assist Gemini request has no content")
	}
	return &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{{
			Role:  "user",
			Parts: parts,
		}},
	}, nil
}

func doVisionAssistRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.Request, mode string) (string, *dto.Usage, *types.NewAPIError) {
	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return "", nil, types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	var convertedRequest any
	var err error
	switch req := request.(type) {
	case *dto.OpenAIResponsesRequest:
		convertedRequest, err = adaptor.ConvertOpenAIResponsesRequest(c, info, *req)
	case *dto.ClaudeRequest:
		convertedRequest, err = adaptor.ConvertClaudeRequest(c, info, req)
	case *dto.GeminiChatRequest:
		convertedRequest, err = adaptor.ConvertGeminiRequest(c, info, req)
	case *dto.GeneralOpenAIRequest:
		convertedRequest, err = adaptor.ConvertOpenAIRequest(c, info, req)
	default:
		err = fmt.Errorf("unsupported vision assist request type: %T", request)
	}
	if err != nil {
		return "", nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return "", nil, types.NewError(err, types.ErrorCodeJsonMarshalFailed, types.ErrOptionWithSkipRetry())
	}
	jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return "", nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			return "", nil, newAPIErrorFromParamOverride(err)
		}
	}
	info.UpstreamRequestBodySize = int64(len(jsonData))

	respAny, err := adaptor.DoRequest(c, info, bytes.NewReader(jsonData))
	if err != nil {
		return "", nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	httpResp, ok := respAny.(*http.Response)
	if !ok || httpResp == nil {
		return "", nil, types.NewError(errors.New("视觉辅助上游响应为空"), types.ErrorCodeEmptyResponse)
	}
	if httpResp.StatusCode != http.StatusOK {
		return "", nil, service.RelayErrorHandler(c.Request.Context(), httpResp, false)
	}
	defer service.CloseResponseBodyGracefully(httpResp)

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return "", nil, types.NewError(err, types.ErrorCodeReadResponseBodyFailed)
	}
	switch mode {
	case service.VisionAssistEndpointModeOpenAIResponses:
		return parseVisionAssistResponsesResponse(body, info)
	case service.VisionAssistEndpointModeAnthropicMessages:
		return parseVisionAssistClaudeResponse(body, info)
	case service.VisionAssistEndpointModeGeminiNative:
		return parseVisionAssistGeminiResponse(body, info)
	default:
		return parseVisionAssistOpenAIResponse(body, info)
	}
}

func parseVisionAssistOpenAIResponse(body []byte, info *relaycommon.RelayInfo) (string, *dto.Usage, *types.NewAPIError) {
	var response dto.OpenAITextResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return "", nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if openaiErr := response.GetOpenAIError(); openaiErr != nil {
		return "", nil, types.WithOpenAIError(*openaiErr, http.StatusBadGateway)
	}
	text := extractVisionAssistResponseText(response)
	usage := response.Usage
	if text == "" {
		var claudeResponse dto.ClaudeResponse
		if err := common.Unmarshal(body, &claudeResponse); err == nil {
			if claudeErr := claudeResponse.GetClaudeError(); claudeErr != nil && claudeErr.Type != "" {
				return "", nil, types.WithClaudeError(*claudeErr, http.StatusBadGateway)
			}
			text = extractVisionAssistClaudeResponseText(claudeResponse)
			usage = visionAssistClaudeUsage(claudeResponse.Usage)
		}
	}
	if text == "" {
		return "", nil, types.NewError(errors.New("视觉辅助响应为空"), types.ErrorCodeEmptyResponse)
	}
	if usage.TotalTokens == 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
		usage.CompletionTokens = service.EstimateTokenByModel(info.OriginModelName, text)
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return text, &usage, nil
}

func parseVisionAssistResponsesResponse(body []byte, info *relaycommon.RelayInfo) (string, *dto.Usage, *types.NewAPIError) {
	var response dto.OpenAIResponsesResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return "", nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if openaiErr := response.GetOpenAIError(); openaiErr != nil {
		return "", nil, types.WithOpenAIError(*openaiErr, http.StatusBadGateway)
	}
	text := strings.TrimSpace(service.ExtractOutputTextFromResponses(&response))
	if text == "" {
		return "", nil, types.NewError(errors.New("视觉辅助响应为空"), types.ErrorCodeEmptyResponse)
	}
	usage := dto.Usage{}
	if response.Usage != nil {
		usage.PromptTokens = response.Usage.InputTokens
		usage.CompletionTokens = response.Usage.OutputTokens
		usage.TotalTokens = response.Usage.TotalTokens
		if response.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = response.Usage.InputTokensDetails.CachedTokens
		}
	}
	fillVisionAssistUsageFallback(&usage, info, text)
	return text, &usage, nil
}

func parseVisionAssistClaudeResponse(body []byte, info *relaycommon.RelayInfo) (string, *dto.Usage, *types.NewAPIError) {
	var response dto.ClaudeResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return "", nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if claudeErr := response.GetClaudeError(); claudeErr != nil && claudeErr.Type != "" {
		return "", nil, types.WithClaudeError(*claudeErr, http.StatusBadGateway)
	}
	text := strings.TrimSpace(extractVisionAssistClaudeResponseText(response))
	if text == "" {
		return "", nil, types.NewError(errors.New("视觉辅助响应为空"), types.ErrorCodeEmptyResponse)
	}
	usage := visionAssistClaudeUsage(response.Usage)
	fillVisionAssistUsageFallback(&usage, info, text)
	return text, &usage, nil
}

func parseVisionAssistGeminiResponse(body []byte, info *relaycommon.RelayInfo) (string, *dto.Usage, *types.NewAPIError) {
	var response dto.GeminiChatResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return "", nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	text := strings.TrimSpace(extractVisionAssistGeminiResponseText(response))
	if text == "" {
		return "", nil, types.NewError(errors.New("视觉辅助响应为空"), types.ErrorCodeEmptyResponse)
	}
	usage := visionAssistGeminiUsage(response.UsageMetadata, info.GetEstimatePromptTokens())
	fillVisionAssistUsageFallback(&usage, info, text)
	return text, &usage, nil
}

func fillVisionAssistUsageFallback(usage *dto.Usage, info *relaycommon.RelayInfo, text string) {
	if usage == nil || usage.TotalTokens != 0 {
		return
	}
	usage.PromptTokens = info.GetEstimatePromptTokens()
	usage.CompletionTokens = service.EstimateTokenByModel(info.OriginModelName, text)
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
}

func extractVisionAssistResponseText(response dto.OpenAITextResponse) string {
	for _, choice := range response.Choices {
		if text := strings.TrimSpace(choice.Message.StringContent()); text != "" {
			return text
		}
		contents := choice.Message.ParseContent()
		parts := make([]string, 0, len(contents))
		for _, content := range contents {
			if content.Type == dto.ContentTypeText && strings.TrimSpace(content.Text) != "" {
				parts = append(parts, strings.TrimSpace(content.Text))
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return ""
}

func extractVisionAssistClaudeResponseText(response dto.ClaudeResponse) string {
	if strings.TrimSpace(response.Completion) != "" {
		return strings.TrimSpace(response.Completion)
	}
	parts := make([]string, 0, len(response.Content))
	for _, content := range response.Content {
		if content.Type == dto.ContentTypeText && strings.TrimSpace(content.GetText()) != "" {
			parts = append(parts, strings.TrimSpace(content.GetText()))
		}
	}
	return strings.Join(parts, "\n")
}

func extractVisionAssistGeminiResponseText(response dto.GeminiChatResponse) string {
	parts := make([]string, 0)
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			if strings.TrimSpace(part.Text) != "" {
				parts = append(parts, strings.TrimSpace(part.Text))
			}
		}
	}
	return strings.Join(parts, "\n")
}

func visionAssistClaudeUsage(usage *dto.ClaudeUsage) dto.Usage {
	if usage == nil {
		return dto.Usage{}
	}
	return dto.Usage{
		PromptTokens:                usage.InputTokens,
		CompletionTokens:            usage.OutputTokens,
		TotalTokens:                 usage.InputTokens + usage.OutputTokens,
		UsageSemantic:               "anthropic",
		UsageSource:                 "anthropic",
		ClaudeCacheCreation5mTokens: usage.GetCacheCreation5mTokens(),
		ClaudeCacheCreation1hTokens: usage.GetCacheCreation1hTokens(),
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         usage.CacheReadInputTokens,
			CachedCreationTokens: usage.CacheCreationInputTokens,
		},
	}
}

func visionAssistGeminiUsage(metadata dto.GeminiUsageMetadata, fallbackPromptTokens int) dto.Usage {
	promptTokens := metadata.PromptTokenCount + metadata.ToolUsePromptTokenCount
	if promptTokens <= 0 && fallbackPromptTokens > 0 {
		promptTokens = fallbackPromptTokens
	}
	usage := dto.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: metadata.CandidatesTokenCount + metadata.ThoughtsTokenCount,
		TotalTokens:      metadata.TotalTokenCount,
		UsageSemantic:    "gemini",
		UsageSource:      "gemini",
	}
	usage.CompletionTokenDetails.ReasoningTokens = metadata.ThoughtsTokenCount
	usage.PromptTokensDetails.CachedTokens = metadata.CachedContentTokenCount
	for _, detail := range metadata.PromptTokensDetails {
		switch detail.Modality {
		case "AUDIO":
			usage.PromptTokensDetails.AudioTokens += detail.TokenCount
		case "TEXT":
			usage.PromptTokensDetails.TextTokens += detail.TokenCount
		case "IMAGE":
			usage.PromptTokensDetails.ImageTokens += detail.TokenCount
		}
	}
	for _, detail := range metadata.ToolUsePromptTokensDetails {
		switch detail.Modality {
		case "AUDIO":
			usage.PromptTokensDetails.AudioTokens += detail.TokenCount
		case "TEXT":
			usage.PromptTokensDetails.TextTokens += detail.TokenCount
		case "IMAGE":
			usage.PromptTokensDetails.ImageTokens += detail.TokenCount
		}
	}
	for _, detail := range metadata.CandidatesTokensDetails {
		switch detail.Modality {
		case "IMAGE":
			usage.CompletionTokenDetails.ImageTokens += detail.TokenCount
		case "AUDIO":
			usage.CompletionTokenDetails.AudioTokens += detail.TokenCount
		case "TEXT":
			usage.CompletionTokenDetails.TextTokens += detail.TokenCount
		}
	}
	if usage.TotalTokens > 0 && usage.CompletionTokens <= 0 {
		usage.CompletionTokens = usage.TotalTokens - usage.PromptTokens
	}
	if usage.PromptTokens > 0 && usage.PromptTokensDetails.TextTokens == 0 && usage.PromptTokensDetails.AudioTokens == 0 && usage.PromptTokensDetails.ImageTokens == 0 {
		usage.PromptTokensDetails.TextTokens = usage.PromptTokens
	}
	return usage
}

type visionAssistContextSnapshot struct {
	values map[string]any
}

func switchContextToVisionAssistChannel(c *gin.Context, channelModel *model.Channel, modelName string) (func(), *types.NewAPIError) {
	keys := []constant.ContextKey{
		constant.ContextKeyChannelId,
		constant.ContextKeyChannelName,
		constant.ContextKeyChannelCreateTime,
		constant.ContextKeyChannelBaseUrl,
		constant.ContextKeyChannelType,
		constant.ContextKeyChannelSetting,
		constant.ContextKeyChannelOtherSetting,
		constant.ContextKeyChannelParamOverride,
		constant.ContextKeyChannelHeaderOverride,
		constant.ContextKeyChannelOrganization,
		constant.ContextKeyChannelAutoBan,
		constant.ContextKeyChannelModelMapping,
		constant.ContextKeyChannelStatusCodeMapping,
		constant.ContextKeyChannelIsMultiKey,
		constant.ContextKeyChannelMultiKeyIndex,
		constant.ContextKeyChannelKey,
		constant.ContextKeyOriginalModel,
		constant.ContextKeyIsStream,
		constant.ContextKeyVisionAssistProcessing,
		constant.ContextKeyVisionAssistPrepared,
		constant.ContextKeyLogOther,
	}
	snapshot := visionAssistContextSnapshot{values: make(map[string]any, len(keys)+6)}
	for _, key := range keys {
		if value, ok := common.GetContextKey(c, key); ok {
			snapshot.values[string(key)] = value
		}
	}
	for _, key := range []string{"api_version", "region", "plugin", "bot_id", "chat_completion_web_search_context_size", common.UpstreamRequestIdKey} {
		if value, ok := c.Get(key); ok {
			snapshot.values[key] = value
		}
	}

	newAPIError := middleware.SetupContextForSelectedChannel(c, channelModel, modelName)
	if newAPIError != nil {
		restore := func() {
			for _, key := range keys {
				delete(c.Keys, string(key))
			}
			for _, key := range []string{"api_version", "region", "plugin", "bot_id", "chat_completion_web_search_context_size", common.UpstreamRequestIdKey} {
				delete(c.Keys, key)
			}
			for key, value := range snapshot.values {
				c.Set(key, value)
			}
		}
		restore()
		return func() {}, newAPIError
	}
	common.SetContextKey(c, constant.ContextKeyVisionAssistProcessing, true)
	common.SetContextKey(c, constant.ContextKeyVisionAssistPrepared, false)
	common.SetContextKey(c, constant.ContextKeyLogOther, map[string]interface{}{
		"vision_assist":            true,
		"assist_channel_id":        channelModel.Id,
		"assist_channel_type":      channelModel.Type,
		"assist_model":             modelName,
		"assist_channel_multi_key": channelModel.ChannelInfo.IsMultiKey,
	})

	return func() {
		for _, key := range keys {
			delete(c.Keys, string(key))
		}
		for _, key := range []string{"api_version", "region", "plugin", "bot_id", "chat_completion_web_search_context_size", common.UpstreamRequestIdKey} {
			delete(c.Keys, key)
		}
		for key, value := range snapshot.values {
			c.Set(key, value)
		}
	}, nil
}
