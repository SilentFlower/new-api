package relay

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageAuditReviewNeedsTextToolFallbackWhenNativeToolIsIgnored(t *testing.T) {
	input := service.MessageAuditReviewModelRequest{RequireToolCall: true}
	assert.True(t, messageAuditReviewNeedsTextToolFallback(input, service.MessageAuditReviewModelResponse{Content: `{"summary":"premature"}`}, nil))
	assert.True(t, messageAuditReviewNeedsTextToolFallback(input, service.MessageAuditReviewModelResponse{}, errors.New("unsupported response")))
	assert.False(t, messageAuditReviewNeedsTextToolFallback(input, service.MessageAuditReviewModelResponse{ToolCalls: []service.MessageAuditReviewToolCall{{Name: "read_file"}}}, nil))
	input.TextToolFallback = true
	assert.False(t, messageAuditReviewNeedsTextToolFallback(input, service.MessageAuditReviewModelResponse{}, nil))
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

	final, err := parseMessageAuditReviewTextToolResponse(`{"summary":"done","risk_level":"none","categories":[],"findings":[]}`)
	require.NoError(t, err)
	assert.Empty(t, final.ToolCalls)
	assert.Contains(t, final.Content, `"summary":"done"`)
}

func TestMessageAuditReviewModelErrorKeepsOnlySafeStageAndStatus(t *testing.T) {
	err := newMessageAuditReviewModelError("upstream_http", 429)
	var modelErr *service.MessageAuditReviewModelError
	require.ErrorAs(t, err, &modelErr)
	assert.Equal(t, "upstream_http", modelErr.Stage)
	assert.Equal(t, 429, modelErr.HTTPStatus)
	assert.NotContains(t, err.Error(), "response body")
}
