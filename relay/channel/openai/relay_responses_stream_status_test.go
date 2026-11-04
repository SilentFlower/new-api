package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newResponsesStreamStatusTestContext 为真实扫描和最终日志生成提供明确初始化的夹具。
// @param t 测试上下文。
// @return Gin 上下文、下游记录器和转发信息。
func newResponsesStreamStatusTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	original := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = original })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(common.RequestIdKey, "responses-stream-status-test")
	info := &relaycommon.RelayInfo{
		IsStream:        true,
		DisablePing:     true,
		StartTime:       time.Now(),
		OriginModelName: "gpt-5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         1,
			UpstreamModelName: "gpt-5",
		},
	}
	return c, recorder, info
}

// TestResponsesStreamStatusProtocolTerminals 验证业务结果、最终日志、原始事件及 usage 一致。
// @param t 测试上下文。
func TestResponsesStreamStatusProtocolTerminals(t *testing.T) {
	const completed = `{"type":"response.completed","response":{"status":"completed","output":[],"usage":{"input_tokens":8,"output_tokens":2,"total_tokens":10}}}`
	const failed = `{"type":"response.failed","response":{"status":"failed","error":{"code":"upstream_unavailable","message":"不能写入日志的上游私密信息"}}}`
	for _, tc := range []struct {
		name           string
		events         []string
		done           bool
		wantError      string
		wantPrompt     int
		wantCompletion int
	}{
		{name: "失败后DONE", events: []string{failed}, done: true, wantError: "response.failed"},
		{name: "失败后EOF", events: []string{failed}, wantError: "response.failed"},
		{name: "不完整终态", events: []string{`{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`}, done: true, wantError: "response.incomplete"},
		{name: "上游取消", events: []string{`{"type":"response.cancelled","response":{"status":"cancelled"}}`}, done: true, wantError: "response.cancelled"},
		{name: "兼容取消拼写", events: []string{`{"type":"response.canceled","response":{"status":"canceled"}}`}, done: true, wantError: "response.canceled"},
		{name: "响应错误事件", events: []string{`{"type":"response.error","response":{"error":{"code":"server_error"}}}`}, done: true, wantError: "response.error"},
		{name: "顶层错误事件", events: []string{`{"type":"error","error":{"code":"server_error","message":"不能写入日志的上游私密信息"}}`}, done: true, wantError: "error"},
		{name: "DONE缺少完成事件", events: []string{`{"type":"response.created"}`}, done: true, wantError: "缺少"},
		{name: "EOF缺少完成事件", events: []string{`{"type":"response.in_progress"}`}, wantError: "缺少"},
		{name: "正常完成后DONE", events: []string{completed}, done: true, wantPrompt: 8, wantCompletion: 2},
		{name: "正常完成后EOF", events: []string{completed}, wantPrompt: 8, wantCompletion: 2},
		{name: "兼容done事件", events: []string{`{"type":"response.done","response":{"status":"completed","usage":{"input_tokens":8,"output_tokens":2,"total_tokens":10}}}`}, done: true, wantPrompt: 8, wantCompletion: 2},
		{name: "预热零生成输出", events: []string{`{"type":"response.completed","response":{"status":"completed","output":[],"usage":{"input_tokens":2,"output_tokens":0,"total_tokens":2}}}`}, done: true, wantPrompt: 2},
		{name: "合法零usage", events: []string{`{"type":"response.completed","response":{"status":"completed","output":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`}, done: true},
		{name: "仅工具调用", events: []string{`{"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call","name":"lookup","arguments":"{}"}],"usage":{"input_tokens":8,"output_tokens":2,"total_tokens":10}}}`}, done: true, wantPrompt: 8, wantCompletion: 2},
		{name: "未知事件仍转发", events: []string{`{"type":"future.opaque","opaque":{"keep":true}}`, completed}, done: true, wantPrompt: 8, wantCompletion: 2},
		{name: "完成事件含失败状态", events: []string{`{"type":"response.completed","response":{"status":"incomplete"}}`}, done: true, wantError: "异常状态"},
		{name: "后续完成不清除失败", events: []string{failed, completed}, done: true, wantError: "response.failed", wantPrompt: 8, wantCompletion: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, recorder, info := newResponsesStreamStatusTestContext(t)
			var body strings.Builder
			for _, event := range tc.events {
				body.WriteString("data: " + event + "\n\n")
			}
			if tc.done {
				body.WriteString("data: [DONE]\n\n")
			}
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(body.String())),
			}

			usage, apiErr := OaiResponsesStreamHandler(c, info, resp)

			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, tc.wantPrompt, usage.PromptTokens)
			assert.Equal(t, tc.wantCompletion, usage.CompletionTokens)
			other := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 0, 1)
			status, ok := other["stream_status"].(map[string]interface{})
			require.True(t, ok)
			wantStatus := "ok"
			if tc.wantError != "" {
				wantStatus = "error"
				messages, ok := status["errors"].([]string)
				require.True(t, ok)
				assert.Contains(t, strings.Join(messages, "\n"), tc.wantError)
			}
			assert.Equal(t, wantStatus, status["status"])
			encodedStatus, err := common.Marshal(status)
			require.NoError(t, err)
			assert.NotContains(t, string(encodedStatus), "不能写入日志的上游私密信息")

			var forwarded []string
			for _, line := range strings.Split(recorder.Body.String(), "\n") {
				if strings.HasPrefix(line, "data: ") {
					forwarded = append(forwarded, strings.TrimPrefix(line, "data: "))
				}
			}
			assert.Equal(t, tc.events, forwarded)
		})
	}
}

// TestResponsesStreamStatusPreservesTransportErrors 验证原始读取、取消和解析错误不会被补记终态覆盖。
// @param t 测试上下文。
func TestResponsesStreamStatusPreservesTransportErrors(t *testing.T) {
	for _, name := range []string{"读取错误", "客户端取消", "超时", "解析错误"} {
		t.Run(name, func(t *testing.T) {
			c, _, info := newResponsesStreamStatusTestContext(t)
			var body io.ReadCloser
			var expected relaycommon.StreamEndReason
			switch name {
			case "读取错误":
				body = io.NopCloser(iotest.ErrReader(io.ErrUnexpectedEOF))
				expected = relaycommon.StreamEndReasonScannerErr
			case "客户端取消", "超时":
				reader, writer := io.Pipe()
				body = reader
				t.Cleanup(func() { _ = writer.Close() })
				if name == "客户端取消" {
					ctx, cancel := context.WithCancel(c.Request.Context())
					cancel()
					c.Request = c.Request.WithContext(ctx)
					expected = relaycommon.StreamEndReasonClientGone
				} else {
					constant.StreamingTimeout = 1
					expected = relaycommon.StreamEndReasonTimeout
				}
			case "解析错误":
				body = io.NopCloser(strings.NewReader("data: {\n\ndata: [DONE]\n\n"))
				expected = relaycommon.StreamEndReasonDone
			}
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}

			_, apiErr := OaiResponsesStreamHandler(c, info, resp)

			require.Nil(t, apiErr)
			assert.Equal(t, expected, info.StreamStatus.EndReason)
			other := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 0, 1)
			status, ok := other["stream_status"].(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, "error", status["status"])
			encodedStatus, err := common.Marshal(status)
			require.NoError(t, err)
			assert.NotContains(t, string(encodedStatus), "缺少 response.completed")
		})
	}
}
