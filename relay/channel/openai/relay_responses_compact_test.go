package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appconstant "github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesCompactionHandlerForwardsBodyAndMapsUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	const body = `{"id":"cmp_1","object":"response.compaction","output":[],"usage":{"input_tokens":123,"output_tokens":7,"total_tokens":130,"input_tokens_details":{"cached_tokens":11,"cache_write_tokens":3}}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": {"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}

	usage, apiErr := OaiResponsesCompactionHandler(c, resp, nil)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 123, usage.PromptTokens)
	assert.Equal(t, 7, usage.CompletionTokens)
	assert.Equal(t, 130, usage.TotalTokens)
	assert.Equal(t, 11, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 3, usage.PromptTokensDetails.CacheWriteTokens)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, body, recorder.Body.String())
}

func TestOaiResponsesStreamHandlerObservesCompactV2WithoutRewritingPayload(t *testing.T) {
	oldStreamingTimeout := appconstant.StreamingTimeout
	appconstant.StreamingTimeout = 30
	t.Cleanup(func() { appconstant.StreamingTimeout = oldStreamingTimeout })

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	const body = "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"compaction\",\"id\":\"cmp_1\",\"encrypted_content\":\"opaque-value\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"usage\":{\"input_tokens\":8,\"output_tokens\":2,\"total_tokens\":10}}}\n\n" +
		"data: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		IsStream:             true,
		StartTime:            time.Now(),
		ResponsesCompactMode: relayconstant.ResponsesCompactModeV2HTTP,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5",
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{},
		},
	}

	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 8, usage.PromptTokens)
	assert.Equal(t, 2, usage.CompletionTokens)
	assert.Equal(t, 1, info.ResponsesUsageInfo.OutputItemDoneCount)
	assert.Equal(t, 1, info.ResponsesUsageInfo.CompactionOutputItemCount)
	assert.True(t, info.ResponsesUsageInfo.ResponseCompleted)
	assert.Contains(t, recorder.Body.String(), `"encrypted_content":"opaque-value"`)
}

func TestOaiResponsesCompactionHandlerBridgesUnaryResponseToSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		ResponsesCompactMode:  relayconstant.ResponsesCompactModeV1BodyBridge,
		ResponsesClientStream: true,
	}
	stopBridge := helper.StartResponsesCompactSSEBridge(c, info)
	const body = `{"id":"resp_1","object":"response.compaction","output":[{"type":"compaction","encrypted_content":"opaque"}],"usage":{"input_tokens":4,"output_tokens":1,"total_tokens":5}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, apiErr := OaiResponsesCompactionHandler(c, resp, info)
	stopBridge()

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 4, usage.PromptTokens)
	assert.Equal(t, 1, usage.CompletionTokens)
	assert.Contains(t, recorder.Body.String(), "event: response.output_item.done")
	assert.Contains(t, recorder.Body.String(), `"encrypted_content":"opaque"`)
	assert.Contains(t, recorder.Body.String(), "event: response.completed")
	assert.NotEqual(t, body, recorder.Body.String())
}
