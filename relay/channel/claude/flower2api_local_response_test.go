package claude

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const localSummarySSE = `data: {"type":"message_start","message":{"id":"msg_fixture","model":"gemini-fixture","usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0}}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Reading sample file"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0}}

data: {"type":"message_stop"}

`

func newLocalSummaryRelayInfo(trust bool, isStream bool) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		IsStream:    isStream,
		StartTime:   time.Now(),
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-fixture",
			ChannelSetting:    dto.ChannelSettings{TrustFlower2APILocalResponse: trust},
		},
	}
	info.SetEstimatePromptTokens(230753)
	return info
}

func TestClaudeFlower2APILocalSummaryStreamUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })

	const summary = dto.LocalResponseAGYAgentSummary
	paid := strings.NewReplacer(
		`"input_tokens":0`, `"input_tokens":2409`,
		`"output_tokens":0`, `"output_tokens":369`,
		`"cache_read_input_tokens":0`, `"cache_read_input_tokens":267859`,
	).Replace(localSummarySSE)
	tests := []struct {
		name         string
		trust        bool
		receipts     []string
		body         string
		wantLocal    string
		wantPrompt   int
		wantCached   int
		wantFallback bool
	}{
		{"可信完整回执", true, []string{summary}, localSummarySSE, summary, 0, 0, false},
		{"渠道缺省关闭", false, []string{summary}, localSummarySSE, "", 230753, 0, true},
		{"只有客户端同名头", true, nil, localSummarySSE, "", 230753, 0, true},
		{"未知回执", true, []string{"other"}, localSummarySSE, "", 230753, 0, true},
		{"重复回执", true, []string{summary, summary}, localSummarySSE, "", 230753, 0, true},
		{"缺少流终态", true, []string{summary}, strings.ReplaceAll(localSummarySSE, `data: {"type":"message_stop"}`, ""), "", 230753, 0, true},
		{"没有标签文本", true, []string{summary}, strings.ReplaceAll(localSummarySSE, `"text":"Reading sample file"`, `"text":""`), "", 230753, 0, true},
		{"缺少原始输入用量", true, []string{summary}, strings.ReplaceAll(localSummarySSE, `"input_tokens":0,`, ""), "", 230753, 0, true},
		{"普通生成与缓存", true, []string{summary}, paid, "", 2409, 267859, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			c.Request.Header.Set(flower2APILocalResponseHeader, summary)
			resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(test.body))}
			for _, receipt := range test.receipts {
				resp.Header.Add(flower2APILocalResponseHeader, receipt)
			}
			usage, apiErr := ClaudeStreamHandler(c, resp, newLocalSummaryRelayInfo(test.trust, true))
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, test.wantLocal, usage.LocalResponse)
			assert.Equal(t, test.wantPrompt, usage.PromptTokens)
			assert.Equal(t, test.wantCached, usage.PromptTokensDetails.CachedTokens)
			assert.Equal(t, test.wantFallback, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
			assert.Empty(t, recorder.Header().Values(flower2APILocalResponseHeader))
			if test.wantLocal != "" {
				assert.Zero(t, usage.CompletionTokens)
				assert.Contains(t, recorder.Body.String(), "Reading sample file")
				assert.Contains(t, recorder.Body.String(), "event: message_stop")
			}
		})
	}
}

func TestClaudeFlower2APILocalSummaryJSONUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const body = `{"type":"message","stop_reason":"end_turn","content":[{"type":"text","text":"Reading sample file"}],"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0}}`
	for _, test := range []struct {
		name      string
		trust     bool
		body      string
		wantLocal string
	}{
		{"可信完整回执", true, body, dto.LocalResponseAGYAgentSummary},
		{"渠道缺省关闭", false, body, ""},
		{"仅有零值缓存字段", true, strings.ReplaceAll(body, `"input_tokens":0,"output_tokens":0,`, ""), ""},
		{"缓存读取有用量", true, strings.ReplaceAll(body, `"cache_read_input_tokens":0`, `"cache_read_input_tokens":42`), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(test.body))}
			resp.Header.Set(flower2APILocalResponseHeader, dto.LocalResponseAGYAgentSummary)
			usage, apiErr := ClaudeHandler(c, resp, newLocalSummaryRelayInfo(test.trust, false))
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, test.wantLocal, usage.LocalResponse)
			assert.JSONEq(t, test.body, recorder.Body.String())
			assert.Empty(t, recorder.Header().Values(flower2APILocalResponseHeader))
		})
	}
}
