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
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func callMessageAuditReviewModel(ctx context.Context, input service.MessageAuditReviewModelRequest) (result service.MessageAuditReviewModelResponse, err error) {
	channel, err := model.GetChannelById(input.ChannelID, true)
	if err != nil {
		return service.MessageAuditReviewModelResponse{}, newMessageAuditReviewModelError("channel_lookup", 0)
	}
	if channel.Status != common.ChannelStatusEnabled || !containsMessageAuditReviewModel(channel.GetModels(), input.Model) {
		return service.MessageAuditReviewModelResponse{}, newMessageAuditReviewModelError("channel_config", 0)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	requestID := "message-audit-review:" + common.GetRandomString(16)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(context.WithValue(ctx, common.RequestIdKey, requestID))
	c.Set(common.RequestIdKey, requestID)
	c.Set("id", messageAuditReviewLogUserID(input))
	c.Set("token_name", "message-audit-review")
	c.Set("token_id", 0)
	c.Set("group", "audit")
	if username, usernameErr := model.GetUsernameById(messageAuditReviewLogUserID(input), false); usernameErr == nil {
		c.Set("username", username)
	}
	// 审核输入、工具结果和模型输出都属于敏感控制面数据，任何 adaptor 日志都必须被抑制。
	logger.SuppressSensitiveContentLogs(c)
	started := time.Now()
	defer func() {
		recordMessageAuditReviewModelLog(c, input, channel, started, result, err)
	}()
	if apiErr := middleware.SetupContextForSelectedChannel(c, channel, input.Model); apiErr != nil {
		return service.MessageAuditReviewModelResponse{}, newMessageAuditReviewModelError("channel_setup", 0)
	}
	stream := false
	parallel := true
	request := buildMessageAuditReviewOpenAIRequest(input, &stream, &parallel)
	info := relaycommon.GenRelayInfoOpenAI(c, request)
	info.RelayMode = relayconstant.RelayModeChatCompletions
	info.RequestURLPath = "/v1/chat/completions"
	info.OriginModelName = input.Model
	info.RequestId = requestID
	info.StartTime = time.Now()
	info.FirstResponseTime = info.StartTime.Add(-time.Second)
	info.InitChannelMeta(c)
	if err := helper.ModelMappedHelper(c, info, request); err != nil {
		return service.MessageAuditReviewModelResponse{}, newMessageAuditReviewModelError("model_mapping", 0)
	}
	// 审核控制面不得应用可能改写系统指令、工具或正文的渠道参数覆盖。
	info.ParamOverride = nil

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return service.MessageAuditReviewModelResponse{}, newMessageAuditReviewModelError("adaptor_unavailable", 0)
	}
	adaptor.Init(info)
	converted, err := adaptor.ConvertOpenAIRequest(c, info, request)
	if err != nil {
		return service.MessageAuditReviewModelResponse{}, newMessageAuditReviewModelError("request_conversion", 0)
	}
	info.InitRequestConversionChain()
	relaycommon.AppendRequestConversionFromRequest(info, converted)
	body, err := common.Marshal(converted)
	if err != nil {
		return service.MessageAuditReviewModelResponse{}, newMessageAuditReviewModelError("request_serialization", 0)
	}
	body, err = relaycommon.RemoveDisabledFields(body, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return service.MessageAuditReviewModelResponse{}, newMessageAuditReviewModelError("request_filtering", 0)
	}
	info.UpstreamRequestBodySize = int64(len(body))
	responseAny, err := adaptor.DoRequest(c, info, bytes.NewReader(body))
	if err != nil {
		return service.MessageAuditReviewModelResponse{}, newMessageAuditReviewModelError("upstream_request", 0)
	}
	response, ok := responseAny.(*http.Response)
	if !ok || response == nil {
		return service.MessageAuditReviewModelResponse{}, newMessageAuditReviewModelError("upstream_response", 0)
	}
	defer service.CloseResponseBodyGracefully(response)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 256*1024))
		if isMessageAuditReviewContextLimitError(body) {
			return service.MessageAuditReviewModelResponse{}, &service.MessageAuditReviewModelError{
				Stage: "upstream_http", HTTPStatus: response.StatusCode, Code: "context_limit",
			}
		}
		return service.MessageAuditReviewModelResponse{}, newMessageAuditReviewModelError("upstream_http", response.StatusCode)
	}
	response.Body = struct {
		io.Reader
		io.Closer
	}{
		Reader: io.LimitReader(response.Body, 4*1024*1024),
		Closer: response.Body,
	}
	if _, apiErr := adaptor.DoResponse(c, response, info); apiErr != nil {
		return service.MessageAuditReviewModelResponse{}, newMessageAuditReviewModelError("response_conversion", response.StatusCode)
	}
	result, err = parseMessageAuditReviewResponse(recorder.Body.Bytes())
	result.HTTPStatus = response.StatusCode
	if err != nil {
		err = newMessageAuditReviewModelError("response_parse", response.StatusCode)
	}
	if messageAuditReviewNeedsTextToolFallback(input, result, err) {
		reason := "tool_ignored"
		var modelErr *service.MessageAuditReviewModelError
		if errors.As(err, &modelErr) {
			reason = modelErr.Stage
		}
		return service.MessageAuditReviewModelResponse{ToolFallbackRequired: true, ToolFallbackReason: reason, HTTPStatus: response.StatusCode}, nil
	}
	if err != nil {
		return service.MessageAuditReviewModelResponse{}, err
	}
	if input.TextToolFallback {
		result, err = parseMessageAuditReviewTextToolResponse(result.Content)
		if err != nil {
			return service.MessageAuditReviewModelResponse{}, newMessageAuditReviewModelError("response_parse", response.StatusCode)
		}
		result.HTTPStatus = response.StatusCode
	}
	return result, nil
}

func buildMessageAuditReviewOpenAIRequest(input service.MessageAuditReviewModelRequest, stream *bool, parallel *bool) *dto.GeneralOpenAIRequest {
	tools := input.Tools
	parallelToolCalls := parallel
	var responseFormat *dto.ResponseFormat
	if input.RequireJSON {
		responseFormat = &dto.ResponseFormat{Type: "json_object"}
	}
	if input.TextToolFallback {
		tools = nil
		parallelToolCalls = nil
		responseFormat = &dto.ResponseFormat{Type: "json_object"}
	}
	if len(tools) == 0 {
		parallelToolCalls = nil
	}
	return &dto.GeneralOpenAIRequest{
		Model: input.Model, Messages: input.Messages, Tools: tools,
		ResponseFormat: responseFormat, Stream: stream, ParallelTooCalls: parallelToolCalls, MaxCompletionTokens: &input.MaxTokens,
	}
}

func messageAuditReviewLogUserID(input service.MessageAuditReviewModelRequest) int {
	if input.OperatorID > 0 {
		return input.OperatorID
	}
	return input.UserID
}

func recordMessageAuditReviewModelLog(c *gin.Context, input service.MessageAuditReviewModelRequest, channel *model.Channel, started time.Time, response service.MessageAuditReviewModelResponse, err error) {
	if c == nil || channel == nil {
		return
	}
	logUserID := messageAuditReviewLogUserID(input)
	if logUserID <= 0 {
		return
	}
	other := messageAuditReviewModelLogOther(input, response, err)
	useTimeSeconds := int(time.Since(started).Seconds())
	if err != nil {
		stage := "unknown"
		var modelErr *service.MessageAuditReviewModelError
		if errors.As(err, &modelErr) && modelErr.Stage != "" {
			stage = modelErr.Stage
		}
		model.RecordErrorLog(c, logUserID, channel.Id, input.Model, "message-audit-review", "消息审计 AI 审核渠道调用失败: "+stage, 0, useTimeSeconds, false, "audit", other)
		return
	}
	model.RecordConsumeLog(c, logUserID, model.RecordConsumeLogParams{
		ChannelId: channel.Id, ModelName: input.Model, TokenName: "message-audit-review",
		Quota: 0, Content: "消息审计 AI 审核渠道调用", TokenId: 0,
		UseTimeSeconds: useTimeSeconds, IsStream: false, Group: "audit", Other: other,
	})
}

func messageAuditReviewModelLogOther(input service.MessageAuditReviewModelRequest, response service.MessageAuditReviewModelResponse, err error) map[string]interface{} {
	protocol := messageAuditReviewProtocol(input)
	other := map[string]interface{}{
		"request_path":    "/internal/message-audit/review",
		"review_protocol": protocol,
		"tool_call_count": len(response.ToolCalls),
		"status_code":     response.HTTPStatus,
	}
	if response.ToolFallbackRequired {
		other["outcome"] = "fallback"
		other["error_stage"] = response.ToolFallbackReason
	} else if err != nil {
		other["outcome"] = "failed"
		var modelErr *service.MessageAuditReviewModelError
		if errors.As(err, &modelErr) {
			other["error_stage"] = modelErr.Stage
			if modelErr.HTTPStatus > 0 {
				other["status_code"] = modelErr.HTTPStatus
			}
			if modelErr.Code != "" {
				other["failure_code"] = modelErr.Code
			}
		} else {
			other["error_stage"] = "unknown"
		}
	} else if len(response.ToolCalls) > 0 {
		other["outcome"] = "tool_calls"
	} else {
		other["outcome"] = "final"
	}
	toolNames := make([]string, 0, len(response.ToolCalls))
	for _, call := range response.ToolCalls {
		toolNames = append(toolNames, safeMessageAuditReviewRelayToolName(call.Name))
	}
	if len(toolNames) > 0 {
		other["tool_names"] = toolNames
	}
	adminInfo := map[string]interface{}{
		"message_audit_review": true,
	}
	if input.OperatorID > 0 {
		adminInfo["operator_id"] = input.OperatorID
	}
	if input.UserID > 0 {
		adminInfo["reviewed_user_id"] = input.UserID
	}
	if input.AuditSessionID != "" {
		adminInfo["audit_session_id"] = input.AuditSessionID
	}
	if input.TargetRequestID != "" {
		adminInfo["target_request_id"] = input.TargetRequestID
	}
	if input.TaskID != "" {
		adminInfo["task_id"] = input.TaskID
	}
	other["admin_info"] = adminInfo
	return other
}

func messageAuditReviewProtocol(input service.MessageAuditReviewModelRequest) string {
	if input.Protocol != "" {
		return input.Protocol
	}
	if input.TextToolFallback {
		return "text_tool_fallback"
	}
	return "native_tools"
}

func safeMessageAuditReviewRelayToolName(name string) string {
	switch name {
	case "list_files", "read_file", "search_files", "search_files_regex":
		return name
	default:
		return "unknown_tool"
	}
}

func newMessageAuditReviewModelError(stage string, httpStatus int) error {
	return &service.MessageAuditReviewModelError{Stage: stage, HTTPStatus: httpStatus}
}

func messageAuditReviewNeedsTextToolFallback(input service.MessageAuditReviewModelRequest, response service.MessageAuditReviewModelResponse, responseErr error) bool {
	var modelErr *service.MessageAuditReviewModelError
	if errors.As(responseErr, &modelErr) && modelErr.Code == "context_limit" {
		return false
	}
	return !input.TextToolFallback && input.RequireToolCall && (responseErr != nil || len(response.ToolCalls) == 0)
}

func isMessageAuditReviewContextLimitError(body []byte) bool {
	values := []string{
		gjson.GetBytes(body, "error.code").String(),
		gjson.GetBytes(body, "error.type").String(),
		gjson.GetBytes(body, "error.message").String(),
		gjson.GetBytes(body, "code").String(),
		gjson.GetBytes(body, "type").String(),
		gjson.GetBytes(body, "message").String(),
	}
	combined := strings.ToLower(strings.Join(values, " "))
	for _, marker := range []string{
		"context_length_exceeded",
		"context window",
		"maximum context length",
		"context length",
		"too many tokens",
		"prompt is too long",
		"input is too long",
	} {
		if strings.Contains(combined, marker) {
			return true
		}
	}
	return false
}

func parseMessageAuditReviewTextToolResponse(content string) (service.MessageAuditReviewModelResponse, error) {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		if newline := strings.IndexByte(content, '\n'); newline >= 0 && strings.HasSuffix(content, "```") {
			content = strings.TrimSpace(strings.TrimSuffix(content[newline+1:], "```"))
		}
	}
	result := service.MessageAuditReviewModelResponse{Content: content}
	type textToolCallEnvelope struct {
		Name      string `json:"name"`
		Arguments any    `json:"arguments"`
	}
	var envelope struct {
		ToolCall  *textToolCallEnvelope  `json:"tool_call"`
		ToolCalls []textToolCallEnvelope `json:"tool_calls"`
	}
	if err := common.UnmarshalJsonStr(result.Content, &envelope); err != nil {
		return result, nil
	}
	calls := make([]textToolCallEnvelope, 0, len(envelope.ToolCalls)+1)
	if envelope.ToolCall != nil {
		calls = append(calls, *envelope.ToolCall)
	}
	calls = append(calls, envelope.ToolCalls...)
	if len(calls) == 0 {
		return result, nil
	}
	result.ToolCalls = make([]service.MessageAuditReviewToolCall, 0, len(calls))
	for _, call := range calls {
		if strings.TrimSpace(call.Name) == "" {
			return service.MessageAuditReviewModelResponse{}, errors.New("review text tool name missing")
		}
		arguments, err := common.Marshal(call.Arguments)
		if err != nil {
			return service.MessageAuditReviewModelResponse{}, err
		}
		result.ToolCalls = append(result.ToolCalls, service.MessageAuditReviewToolCall{
			ID: "text_tool_" + common.GetRandomString(8), Name: call.Name, Arguments: string(arguments),
		})
	}
	return result, nil
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
