package relay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func callMessageAuditReviewModel(ctx context.Context, input service.MessageAuditReviewModelRequest) (service.MessageAuditReviewModelResponse, error) {
	channel, err := model.GetChannelById(input.ChannelID, true)
	if err != nil {
		return service.MessageAuditReviewModelResponse{}, errors.New("review channel unavailable")
	}
	if channel.Status != common.ChannelStatusEnabled || !containsMessageAuditReviewModel(channel.GetModels(), input.Model) {
		return service.MessageAuditReviewModelResponse{}, errors.New("review channel configuration invalid")
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	// 审核输入、工具结果和模型输出都属于敏感控制面数据，任何 adaptor 日志都必须被抑制。
	logger.SuppressSensitiveContentLogs(c)
	if apiErr := middleware.SetupContextForSelectedChannel(c, channel, input.Model); apiErr != nil {
		return service.MessageAuditReviewModelResponse{}, errors.New("review channel setup failed")
	}
	stream := false
	parallel := false
	request := &dto.GeneralOpenAIRequest{
		Model: input.Model, Messages: input.Messages, Tools: input.Tools,
		Stream: &stream, ParallelTooCalls: &parallel, MaxCompletionTokens: &input.MaxTokens,
	}
	info := relaycommon.GenRelayInfoOpenAI(c, request)
	info.RelayMode = relayconstant.RelayModeChatCompletions
	info.RequestURLPath = "/v1/chat/completions"
	info.OriginModelName = input.Model
	info.RequestId = "message-audit-review:" + common.GetRandomString(16)
	info.StartTime = time.Now()
	info.FirstResponseTime = info.StartTime.Add(-time.Second)
	info.InitChannelMeta(c)
	if err := helper.ModelMappedHelper(c, info, request); err != nil {
		return service.MessageAuditReviewModelResponse{}, errors.New("review model mapping failed")
	}
	// 审核控制面不得应用可能改写系统指令、工具或正文的渠道参数覆盖。
	info.ParamOverride = nil

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return service.MessageAuditReviewModelResponse{}, errors.New("review adaptor unavailable")
	}
	adaptor.Init(info)
	converted, err := adaptor.ConvertOpenAIRequest(c, info, request)
	if err != nil {
		return service.MessageAuditReviewModelResponse{}, errors.New("review request conversion failed")
	}
	info.InitRequestConversionChain()
	relaycommon.AppendRequestConversionFromRequest(info, converted)
	body, err := common.Marshal(converted)
	if err != nil {
		return service.MessageAuditReviewModelResponse{}, errors.New("review request serialization failed")
	}
	body, err = relaycommon.RemoveDisabledFields(body, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return service.MessageAuditReviewModelResponse{}, errors.New("review request filtering failed")
	}
	info.UpstreamRequestBodySize = int64(len(body))
	responseAny, err := adaptor.DoRequest(c, info, bytes.NewReader(body))
	if err != nil {
		return service.MessageAuditReviewModelResponse{}, errors.New("review upstream request failed")
	}
	response, ok := responseAny.(*http.Response)
	if !ok || response == nil {
		return service.MessageAuditReviewModelResponse{}, errors.New("review upstream response unavailable")
	}
	defer service.CloseResponseBodyGracefully(response)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return service.MessageAuditReviewModelResponse{}, fmt.Errorf("review upstream status: %d", response.StatusCode)
	}
	response.Body = struct {
		io.Reader
		io.Closer
	}{
		Reader: io.LimitReader(response.Body, 4*1024*1024),
		Closer: response.Body,
	}
	if _, apiErr := adaptor.DoResponse(c, response, info); apiErr != nil {
		return service.MessageAuditReviewModelResponse{}, errors.New("review upstream response conversion failed")
	}
	return parseMessageAuditReviewResponse(recorder.Body.Bytes())
}

func parseMessageAuditReviewResponse(body []byte) (service.MessageAuditReviewModelResponse, error) {
	var openAIResponse dto.OpenAITextResponse
	if err := common.Unmarshal(body, &openAIResponse); err == nil && len(openAIResponse.Choices) > 0 {
		choice := openAIResponse.Choices[0]
		result := service.MessageAuditReviewModelResponse{Content: strings.TrimSpace(common.Interface2String(choice.Message.Content))}
		for _, call := range choice.Message.ParseToolCalls() {
			result.ToolCalls = append(result.ToolCalls, service.MessageAuditReviewToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
		}
		if result.Content != "" || len(result.ToolCalls) > 0 {
			return result, nil
		}
	}

	var claudeResponse dto.ClaudeResponse
	if err := common.Unmarshal(body, &claudeResponse); err == nil && len(claudeResponse.Content) > 0 {
		result := service.MessageAuditReviewModelResponse{}
		for _, content := range claudeResponse.Content {
			switch content.Type {
			case "text":
				if content.Text != nil {
					result.Content += *content.Text
				}
			case "tool_use":
				arguments, _ := common.Marshal(content.Input)
				result.ToolCalls = append(result.ToolCalls, service.MessageAuditReviewToolCall{ID: content.Id, Name: content.Name, Arguments: string(arguments)})
			}
		}
		if result.Content != "" || len(result.ToolCalls) > 0 {
			return result, nil
		}
	}

	var geminiResponse dto.GeminiChatResponse
	if err := common.Unmarshal(body, &geminiResponse); err == nil && len(geminiResponse.Candidates) > 0 {
		result := service.MessageAuditReviewModelResponse{}
		for index, part := range geminiResponse.Candidates[0].Content.Parts {
			result.Content += part.Text
			if part.FunctionCall == nil {
				continue
			}
			arguments, _ := common.Marshal(part.FunctionCall.Arguments)
			result.ToolCalls = append(result.ToolCalls, service.MessageAuditReviewToolCall{
				ID: fmt.Sprintf("gemini_call_%d", index), Name: part.FunctionCall.FunctionName, Arguments: string(arguments),
			})
		}
		if result.Content != "" || len(result.ToolCalls) > 0 {
			return result, nil
		}
	}
	return service.MessageAuditReviewModelResponse{}, errors.New("review upstream response format unsupported")
}

func containsMessageAuditReviewModel(models []string, target string) bool {
	for _, modelName := range models {
		if strings.TrimSpace(modelName) == target {
			return true
		}
	}
	return false
}

func init() {
	service.RegisterMessageAuditReviewCaller(callMessageAuditReviewModel)
}
