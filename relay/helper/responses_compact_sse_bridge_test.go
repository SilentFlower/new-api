package helper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesCompactSSEBridgeWritesPingAndTerminalEventsInOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, common.RequestIdKey, "req-1")

	ticks := make(chan time.Time)
	stop := startResponsesCompactSSEBridge(c, ticks, nil)
	bridge, ok := getResponsesCompactSSEBridge(c)
	require.True(t, ok)
	require.True(t, bridge.writePing())

	responseBody := []byte(`{"output":[{"type":"compaction","encrypted_content":"opaque"}],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`)
	output := json.RawMessage(`[{"type":"compaction","encrypted_content":"opaque"}]`)
	require.NoError(t, WriteResponsesCompactSSECompleted(c, responseBody, output))
	stop()

	body := recorder.Body.String()
	assert.Contains(t, body, ": PING\n\n")
	assert.Contains(t, body, "event: response.output_item.done")
	assert.Contains(t, body, `"encrypted_content":"opaque"`)
	assert.Contains(t, body, "event: response.completed")
	assert.Contains(t, body, `"id":"resp_req-1"`)
	assert.Less(t, strings.Index(body, ": PING\n\n"), strings.Index(body, "event: response.output_item.done"))
	assert.Less(t, strings.Index(body, "event: response.output_item.done"), strings.Index(body, "event: response.completed"))
}

func TestResponsesCompactSSEBridgeStopsOnRequestCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	requestContext, cancel := context.WithCancel(context.Background())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestContext)
	ticks := make(chan time.Time)
	tickerStopped := false
	stop := startResponsesCompactSSEBridge(c, ticks, func() { tickerStopped = true })

	cancel()
	stop()

	assert.True(t, tickerStopped)
	assert.Empty(t, recorder.Body.String())
}

func TestResponsesCompactSSEBridgeReportsUncommittedBeforeHeartbeat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	stop := startResponsesCompactSSEBridge(c, make(chan time.Time), nil)

	active, committed := PrepareResponsesCompactSSEFinal(c)
	stop()

	assert.True(t, active)
	assert.False(t, committed)
	assert.Empty(t, recorder.Body.String())
}

func TestResponsesCompactSSEBridgeRejectsInvalidOutputBeforeCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	stop := startResponsesCompactSSEBridge(c, make(chan time.Time), nil)

	err := WriteResponsesCompactSSECompleted(c, []byte(`{"output":[]}`), json.RawMessage(`[`))
	stop()

	require.Error(t, err)
	assert.Empty(t, recorder.Body.String())
}

func TestResponsesCompactSSEBridgeSerializesHeartbeatAndFinalResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	stop := startResponsesCompactSSEBridge(c, make(chan time.Time), nil)
	bridge, ok := getResponsesCompactSSEBridge(c)
	require.True(t, ok)

	responseBody := []byte(`{"output":[{"type":"compaction"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	output := json.RawMessage(`[{"type":"compaction"}]`)
	finalErr := make(chan error, 1)
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		bridge.writePing()
	}()
	go func() {
		defer waitGroup.Done()
		finalErr <- WriteResponsesCompactSSECompleted(c, responseBody, output)
	}()
	waitGroup.Wait()
	require.NoError(t, <-finalErr)
	stop()

	body := recorder.Body.String()
	assert.Contains(t, body, "event: response.output_item.done")
	assert.Contains(t, body, "event: response.completed")
	if pingIndex := strings.Index(body, ": PING\n\n"); pingIndex >= 0 {
		assert.Less(t, pingIndex, strings.Index(body, "event: response.output_item.done"))
	}
}

func TestResponsesCompactSSEBridgeFillsMissingIDAndUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, common.RequestIdKey, "req-missing-fields")
	stop := startResponsesCompactSSEBridge(c, make(chan time.Time), nil)

	require.NoError(t, WriteResponsesCompactSSECompleted(c, []byte(`{"output":[]}`), json.RawMessage(`[]`)))
	stop()

	body := recorder.Body.String()
	assert.Contains(t, body, `"id":"resp_req-missing-fields"`)
	assert.Contains(t, body, `"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}`)
}

func TestResponsesCompactSSEBridgeDropsUsageCodexCannotParse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name  string
		usage string
	}{
		{name: "缺少 total_tokens", usage: `{"input_tokens":2,"output_tokens":1}`},
		{name: "字段类型错误", usage: `{"input_tokens":2,"output_tokens":"1","total_tokens":3}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			stop := startResponsesCompactSSEBridge(c, make(chan time.Time), nil)

			responseBody := []byte(`{"id":"resp_1","output":[],"usage":` + test.usage + `}`)
			require.NoError(t, WriteResponsesCompactSSECompleted(c, responseBody, json.RawMessage(`[]`)))
			stop()

			assert.Contains(t, recorder.Body.String(), "event: response.completed")
			assert.NotContains(t, recorder.Body.String(), `"usage"`)
		})
	}
}

func TestResponsesCompactSSEBridgeSkipsNonObjectOutputItems(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	stop := startResponsesCompactSSEBridge(c, make(chan time.Time), nil)
	output := json.RawMessage(`["invalid",{"type":"compaction","encrypted_content":"opaque"},7]`)
	responseBody := []byte(`{"id":"resp_1","output":` + string(output) + `,"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`)

	require.NoError(t, WriteResponsesCompactSSECompleted(c, responseBody, output))
	stop()

	body := recorder.Body.String()
	completedIndex := strings.Index(body, "event: response.completed")
	require.Greater(t, completedIndex, 0)
	outputEvents := body[:completedIndex]
	assert.Equal(t, 1, strings.Count(outputEvents, "event: response.output_item.done"))
	assert.Contains(t, outputEvents, `"output_index":0`)
	assert.Contains(t, outputEvents, `"encrypted_content":"opaque"`)
	assert.NotContains(t, outputEvents, `"item":"invalid"`)
}

func TestResponsesCompactSSEBridgeWritesFailedEventAfterCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	ticks := make(chan time.Time)
	stop := startResponsesCompactSSEBridge(c, ticks, nil)
	bridge, ok := getResponsesCompactSSEBridge(c)
	require.True(t, ok)
	require.True(t, bridge.writePing())
	active, committed := PrepareResponsesCompactSSEFinal(c)
	require.True(t, active)
	require.True(t, committed)
	require.NoError(t, WriteResponsesCompactSSEFailed(c, types.OpenAIError{Message: "upstream failed", Type: "upstream_error"}))
	stop()

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "event: response.failed")
	assert.Contains(t, recorder.Body.String(), "upstream failed")
}
