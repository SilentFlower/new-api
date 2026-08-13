package relay

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageAuditReviewNeedsTextToolFallbackWhenNativeToolIsIgnored(t *testing.T) {
	input := service.MessageAuditReviewModelRequest{RequireToolCall: true}
	assert.True(t, messageAuditReviewNeedsTextToolFallback(input, service.MessageAuditReviewModelResponse{Content: `{"summary":"premature"}`}, nil))
	assert.True(t, messageAuditReviewNeedsTextToolFallback(input, service.MessageAuditReviewModelResponse{}, errors.New("unsupported response")))
	assert.False(t, messageAuditReviewNeedsTextToolFallback(input, service.MessageAuditReviewModelResponse{}, &service.MessageAuditReviewModelError{Code: "context_limit"}))
	assert.False(t, messageAuditReviewNeedsTextToolFallback(input, service.MessageAuditReviewModelResponse{ToolCalls: []service.MessageAuditReviewToolCall{{Name: "read_file"}}}, nil))
	input.TextToolFallback = true
	assert.False(t, messageAuditReviewNeedsTextToolFallback(input, service.MessageAuditReviewModelResponse{}, nil))
}

func TestMessageAuditReviewOpenAIRequestDoesNotForceRequiredToolChoice(t *testing.T) {
	stream := false
	parallel := true
	maxTokens := uint(256)
	request := buildMessageAuditReviewOpenAIRequest(service.MessageAuditReviewModelRequest{
		Model: "review-model", RequireToolCall: true, MaxTokens: maxTokens,
		Tools: []dto.ToolCallRequest{{Type: "function", Function: dto.FunctionRequest{Name: "read_file"}}},
	}, &stream, &parallel)

	require.Len(t, request.Tools, 1)
	assert.Nil(t, request.ToolChoice)
	require.NotNil(t, request.MaxCompletionTokens)
	assert.Equal(t, maxTokens, *request.MaxCompletionTokens)
	assert.False(t, *request.Stream)
	assert.True(t, *request.ParallelTooCalls)

	jsonToolRequest := buildMessageAuditReviewOpenAIRequest(service.MessageAuditReviewModelRequest{
		Model: "review-model", RequireJSON: true, MaxTokens: maxTokens,
		Tools: []dto.ToolCallRequest{{Type: "function", Function: dto.FunctionRequest{Name: "read_file"}}},
	}, &stream, &parallel)
	require.Len(t, jsonToolRequest.Tools, 1)
	require.NotNil(t, jsonToolRequest.ResponseFormat)
	assert.Equal(t, "json_object", jsonToolRequest.ResponseFormat.Type)
	require.NotNil(t, jsonToolRequest.ParallelTooCalls)
	assert.True(t, *jsonToolRequest.ParallelTooCalls)

	jsonRequest := buildMessageAuditReviewOpenAIRequest(service.MessageAuditReviewModelRequest{
		Model: "review-model", RequireJSON: true, MaxTokens: maxTokens,
	}, &stream, &parallel)
	require.NotNil(t, jsonRequest.ResponseFormat)
	assert.Equal(t, "json_object", jsonRequest.ResponseFormat.Type)
	assert.Nil(t, jsonRequest.ParallelTooCalls)

	fallbackRequest := buildMessageAuditReviewOpenAIRequest(service.MessageAuditReviewModelRequest{
		Model: "review-model", TextToolFallback: true, MaxTokens: maxTokens,
		Tools: []dto.ToolCallRequest{{Type: "function", Function: dto.FunctionRequest{Name: "read_file"}}},
	}, &stream, &parallel)
	assert.Empty(t, fallbackRequest.Tools)
	assert.Nil(t, fallbackRequest.ParallelTooCalls)
	require.NotNil(t, fallbackRequest.ResponseFormat)
	assert.Equal(t, "json_object", fallbackRequest.ResponseFormat.Type)
}

func TestMessageAuditReviewRecognizesUpstreamContextLimit(t *testing.T) {
	assert.True(t, isMessageAuditReviewContextLimitError([]byte(`{"error":{"code":"context_length_exceeded","message":"context too large"}}`)))
	assert.True(t, isMessageAuditReviewContextLimitError([]byte(`{"error":{"message":"Maximum context length was exceeded"}}`)))
	assert.False(t, isMessageAuditReviewContextLimitError([]byte(`{"error":{"message":"rate limit exceeded"}}`)))
}

func TestParseMessageAuditReviewTextToolResponse(t *testing.T) {
	response, err := parseMessageAuditReviewTextToolResponse(`{"tool_call":{"name":"read_file","arguments":{"file_id":"request:one","cursor":0,"limit":1}}}`)
	require.NoError(t, err)
	require.Len(t, response.ToolCalls, 1)
	assert.Equal(t, "read_file", response.ToolCalls[0].Name)
	assert.JSONEq(t, `{"file_id":"request:one","cursor":0,"limit":1}`, response.ToolCalls[0].Arguments)

	fenced, err := parseMessageAuditReviewTextToolResponse("```json\n{\"tool_call\":{\"name\":\"list_files\",\"arguments\":{}}}\n```")
	require.NoError(t, err)
	require.Len(t, fenced.ToolCalls, 1)
	assert.Equal(t, "list_files", fenced.ToolCalls[0].Name)

	multi, err := parseMessageAuditReviewTextToolResponse(`{"tool_calls":[{"name":"list_files","arguments":{}},{"name":"read_file","arguments":{"file_id":"request:one","cursor":0,"limit":10}}]}`)
	require.NoError(t, err)
	require.Len(t, multi.ToolCalls, 2)
	assert.Equal(t, "list_files", multi.ToolCalls[0].Name)
	assert.Equal(t, "read_file", multi.ToolCalls[1].Name)
	assert.JSONEq(t, `{"file_id":"request:one","cursor":0,"limit":10}`, multi.ToolCalls[1].Arguments)

	final, err := parseMessageAuditReviewTextToolResponse(`{"summary":"done","risk_level":"none","categories":[],"findings":[]}`)
	require.NoError(t, err)
	assert.Empty(t, final.ToolCalls)
	assert.Contains(t, final.Content, `"summary":"done"`)
}

func TestMessageAuditReviewModelLogOtherKeepsDiagnosticsSafe(t *testing.T) {
	other := messageAuditReviewModelLogOther(service.MessageAuditReviewModelRequest{
		TextToolFallback: true, UserID: 12, OperatorID: 1, Protocol: "text_tool_fallback",
		AuditSessionID: "audsess_safe", TargetRequestID: "req_safe", TaskID: "task_safe",
	}, service.MessageAuditReviewModelResponse{
		HTTPStatus: 415,
		ToolCalls: []service.MessageAuditReviewToolCall{{
			Name: "read_file", Arguments: `{"file_id":"request:secret","cursor":0,"limit":1}`,
		}},
	}, &service.MessageAuditReviewModelError{Stage: "upstream_http", HTTPStatus: 415})

	assert.Equal(t, "/internal/message-audit/review", other["request_path"])
	assert.Equal(t, "text_tool_fallback", other["review_protocol"])
	assert.Equal(t, "failed", other["outcome"])
	assert.Equal(t, "upstream_http", other["error_stage"])
	assert.Equal(t, 415, other["status_code"])
	assert.Equal(t, []string{"read_file"}, other["tool_names"])
	assert.NotContains(t, common.GetJsonString(other), "request:secret")
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, adminInfo["message_audit_review"])
	assert.Equal(t, "audsess_safe", adminInfo["audit_session_id"])

	mergedOther := messageAuditReviewModelLogOther(service.MessageAuditReviewModelRequest{Protocol: "merged_context"}, service.MessageAuditReviewModelResponse{}, nil)
	assert.Equal(t, "merged_context", mergedOther["review_protocol"])
}

func TestMessageAuditReviewModelErrorKeepsOnlySafeStageAndStatus(t *testing.T) {
	err := newMessageAuditReviewModelError("upstream_http", 429)
	var modelErr *service.MessageAuditReviewModelError
	require.ErrorAs(t, err, &modelErr)
	assert.Equal(t, "upstream_http", modelErr.Stage)
	assert.Equal(t, 429, modelErr.HTTPStatus)
	assert.NotContains(t, err.Error(), "response body")
}
