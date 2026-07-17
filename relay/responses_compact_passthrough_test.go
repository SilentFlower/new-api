package relay

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/relayconvert"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type responsesCompactPassthroughBillingStub struct {
	preConsumed int
	settleCalls int
	settled     int
	refunded    bool
}

// Settle 记录 Compact 透传测试中的实际结算额度。
// @param actualQuota 实际结算额度。
// @return 始终返回 nil。
func (s *responsesCompactPassthroughBillingStub) Settle(actualQuota int) error {
	s.settleCalls++
	s.settled = actualQuota
	return nil
}

// Refund 记录 Compact 透传测试中的退款动作。
// @param c 当前 Gin 请求上下文。
func (s *responsesCompactPassthroughBillingStub) Refund(c *gin.Context) {
	s.refunded = true
}

// NeedsRefund 返回测试会话是否仍需退款。
// @return 尚未结算且未退款时返回 true。
func (s *responsesCompactPassthroughBillingStub) NeedsRefund() bool {
	return s.settleCalls == 0 && !s.refunded
}

// GetPreConsumedQuota 返回测试会话的预扣额度。
// @return 预扣额度。
func (s *responsesCompactPassthroughBillingStub) GetPreConsumedQuota() int {
	return s.preConsumed
}

// Reserve 更新测试会话的预扣额度。
// @param targetQuota 目标预扣额度。
// @return 始终返回 nil。
func (s *responsesCompactPassthroughBillingStub) Reserve(targetQuota int) error {
	if targetQuota > s.preConsumed {
		s.preConsumed = targetQuota
	}
	return nil
}

func newResponsesCompactPassthroughTestContext(t *testing.T, path string, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c, recorder
}

func newResponsesCompactPassthroughTestInfo(c *gin.Context, mode relayconstant.ResponsesCompactMode, baseURL string, billing relaycommon.BillingSettler) *relaycommon.RelayInfo {
	baseModel := "gpt-5.6-sol"
	common.SetContextKey(c, appconstant.ContextKeyChannelId, 91)
	common.SetContextKey(c, appconstant.ContextKeyChannelType, appconstant.ChannelTypeOpenAI)
	common.SetContextKey(c, appconstant.ContextKeyChannelBaseUrl, baseURL)
	common.SetContextKey(c, appconstant.ContextKeyChannelKey, "upstream-secret")
	common.SetContextKey(c, appconstant.ContextKeyOriginalModel, baseModel)
	common.SetContextKey(c, appconstant.ContextKeyChannelModelMapping, `{"gpt-5.6-sol":"mapped-model"}`)
	common.SetContextKey(c, appconstant.ContextKeyChannelSetting, dto.ChannelSettings{ResponsesCompactPassthroughEnabled: true})
	common.SetContextKey(c, appconstant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{})
	common.SetContextKey(c, appconstant.ContextKeyResponsesCompactMode, mode)
	common.SetContextKey(c, appconstant.ContextKeyRequestStartTime, time.Now())

	var info *relaycommon.RelayInfo
	if mode == relayconstant.ResponsesCompactModeV1Path {
		info = relaycommon.GenRelayInfoResponsesCompaction(c, &dto.OpenAIResponsesCompactionRequest{Model: baseModel})
	} else {
		info = relaycommon.GenRelayInfoResponses(c, &dto.OpenAIResponsesRequest{Model: baseModel})
	}
	info.ResponsesCompactMode = mode
	info.IsStream = mode != relayconstant.ResponsesCompactModeV1Path
	info.Billing = billing
	info.PriceData = types.PriceData{GroupRatioInfo: types.GroupRatioInfo{
		GroupRatio:        1,
		GroupSpecialRatio: -1,
	}}
	if mode == relayconstant.ResponsesCompactModeV1Path {
		info.RelayMode = relayconstant.RelayModeResponsesCompact
		info.RelayFormat = types.RelayFormatOpenAIResponsesCompaction
	} else {
		info.RelayMode = relayconstant.RelayModeResponses
		info.RelayFormat = types.RelayFormatOpenAIResponses
	}
	if mode != relayconstant.ResponsesCompactModeV1Path {
		if info.ResponsesUsageInfo == nil {
			info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{BuiltInTools: make(map[string]*relaycommon.BuildInToolInfo)}
		}
	}
	return info
}

func withResponsesCompactPassthroughQuotaTestState(t *testing.T) {
	t.Helper()
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled
	common.BatchUpdateEnabled = true
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		common.LogConsumeEnabled = oldLogConsumeEnabled
	})
}

func TestPrepareResponsesCompactPassthroughRequiresSelectedChannelCapability(t *testing.T) {
	c, _ := newResponsesCompactPassthroughTestContext(t, "/v1/responses/compact", `{"model":"gpt-5.6-sol"}`)
	common.SetContextKey(c, appconstant.ContextKeyChannelId, 91)
	common.SetContextKey(c, appconstant.ContextKeyChannelType, appconstant.ChannelTypeOpenAI)
	common.SetContextKey(c, appconstant.ContextKeyChannelBaseUrl, "https://upstream.example")
	common.SetContextKey(c, appconstant.ContextKeyChannelKey, "upstream-secret")
	common.SetContextKey(c, appconstant.ContextKeyOriginalModel, "gpt-5.6-sol")
	common.SetContextKey(c, appconstant.ContextKeyChannelSetting, dto.ChannelSettings{})
	request := &dto.OpenAIResponsesCompactionRequest{Model: "gpt-5.6-sol"}
	info := &relaycommon.RelayInfo{
		OriginModelName:      "gpt-5.6-sol",
		Request:              request,
		RelayMode:            relayconstant.RelayModeResponsesCompact,
		ResponsesCompactMode: relayconstant.ResponsesCompactModeV1Path,
	}

	apiErr := PrepareResponsesCompactPassthrough(c, info)

	require.NotNil(t, apiErr)
	assert.Equal(t, responsesCompactPassthroughDisabledErrorCode, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.False(t, types.IsChannelError(apiErr))
	assert.False(t, types.IsRecordErrorLog(apiErr))
	assert.Equal(t, "gpt-5.6-sol", request.Model)
}

func TestPrepareResponsesCompactPassthroughKeepsBaseModelAndIgnoresMapping(t *testing.T) {
	c, _ := newResponsesCompactPassthroughTestContext(t, "/v1/responses/compact", `{"model":"gpt-5.6-sol"}`)
	info := newResponsesCompactPassthroughTestInfo(c, relayconstant.ResponsesCompactModeV1Path, "https://upstream.example", nil)

	apiErr := PrepareResponsesCompactPassthrough(c, info)

	require.Nil(t, apiErr)
	assert.Equal(t, "gpt-5.6-sol", info.OriginModelName)
	assert.Equal(t, "gpt-5.6-sol", info.UpstreamModelName)
	assert.Equal(t, "gpt-5.6-sol", info.Request.(*dto.OpenAIResponsesCompactionRequest).Model)
	assert.False(t, info.IsModelMapped)
}

func TestResponsesCompactPassthroughRejectsUnsupportedAPITypeForEveryHTTPMode(t *testing.T) {
	tests := []struct {
		name string
		mode relayconstant.ResponsesCompactMode
	}{
		{name: "V1 Compact", mode: relayconstant.ResponsesCompactModeV1Path},
		{name: "历史 bridge", mode: relayconstant.ResponsesCompactModeV1BodyBridge},
		{name: "V2 HTTP", mode: relayconstant.ResponsesCompactModeV2HTTP},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := newResponsesCompactPassthroughTestContext(t, "/v1/responses", `{"model":"gpt-5.6-sol"}`)
			info := newResponsesCompactPassthroughTestInfo(c, test.mode, "https://upstream.example", nil)
			info.ChannelMeta = &relaycommon.ChannelMeta{ApiType: appconstant.APITypeAnthropic}

			apiErr := ResponsesCompactPassthroughHelper(c, info)

			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeInvalidRequest, apiErr.GetErrorCode())
			assert.True(t, types.IsSkipRetryError(apiErr))
		})
	}
}

func TestResponsesCompactPassthroughUsesModePathAndPreservesRawBody(t *testing.T) {
	service.InitHttpClient()
	withResponsesCompactPassthroughQuotaTestState(t)
	tests := []struct {
		name         string
		mode         relayconstant.ResponsesCompactMode
		downstream   string
		expectedPath string
	}{
		{name: "V1 path", mode: relayconstant.ResponsesCompactModeV1Path, downstream: "/v1/responses/compact", expectedPath: "/v1/responses/compact"},
		{name: "历史 bridge", mode: relayconstant.ResponsesCompactModeV1BodyBridge, downstream: "/v1/responses?cursor=1", expectedPath: "/v1/responses"},
		{name: "V2 HTTP", mode: relayconstant.ResponsesCompactModeV2HTTP, downstream: "/v1/responses", expectedPath: "/v1/responses"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestBody := `{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"compaction_trigger"}],"enabled":false,"future":{"count":0}}`
			var upstreamPath string
			var upstreamBody string
			var upstreamAuthorization string
			var upstreamCookie string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamPath = r.URL.Path
				body, _ := io.ReadAll(r.Body)
				upstreamBody = string(body)
				upstreamAuthorization = r.Header.Get("Authorization")
				upstreamCookie = r.Header.Get("Cookie")
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Upstream-Result", "ok")
				_, _ = io.WriteString(w, `{"id":"compact_1","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12,"input_tokens_details":{"cached_tokens":3}},"future":{"kept":true}}`)
			}))
			defer upstream.Close()

			c, recorder := newResponsesCompactPassthroughTestContext(t, test.downstream, requestBody)
			c.Request.Header.Set("Authorization", "Bearer client-secret")
			c.Request.Header.Set("Cookie", "session=client-secret")
			billing := &responsesCompactPassthroughBillingStub{preConsumed: 10}
			info := newResponsesCompactPassthroughTestInfo(c, test.mode, upstream.URL, billing)
			require.Nil(t, PrepareResponsesCompactPassthrough(c, info))
			info.FreezeBillingModelName("gpt-5.6-sol")

			apiErr := ResponsesCompactPassthroughHelper(c, info)

			require.Nil(t, apiErr)
			assert.Equal(t, test.expectedPath, upstreamPath)
			assert.Equal(t, requestBody, upstreamBody)
			assert.Equal(t, "Bearer upstream-secret", upstreamAuthorization)
			assert.Empty(t, upstreamCookie)
			assert.JSONEq(t, `{"id":"compact_1","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12,"input_tokens_details":{"cached_tokens":3}},"future":{"kept":true}}`, recorder.Body.String())
			assert.Equal(t, "ok", recorder.Header().Get("X-Upstream-Result"))
			assert.Equal(t, 1, billing.settleCalls)
			assert.False(t, billing.refunded)
			assert.NotContains(t, upstreamBody, "openai-compact")
			assert.True(t, info.HasSendResponse())
		})
	}
}

func TestResponsesCompactPassthroughFiltersClientQueryCredentials(t *testing.T) {
	service.InitHttpClient()
	withResponsesCompactPassthroughQuotaTestState(t)
	tests := []struct {
		name string
		mode relayconstant.ResponsesCompactMode
	}{
		{name: "历史 bridge", mode: relayconstant.ResponsesCompactModeV1BodyBridge},
		{name: "V2 HTTP", mode: relayconstant.ResponsesCompactModeV2HTTP},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var upstreamQuery string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamQuery = r.URL.RawQuery
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
			}))
			defer upstream.Close()

			path := "/v1/responses?cursor=page-2&api_key=client-secret&custom_token_hint=hidden-token&signature_v2=hidden-signature&password=hidden-password"
			c, _ := newResponsesCompactPassthroughTestContext(t, path, `{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"compaction_trigger"}]}`)
			billing := &responsesCompactPassthroughBillingStub{preConsumed: 10}
			info := newResponsesCompactPassthroughTestInfo(c, test.mode, upstream.URL, billing)
			require.Nil(t, PrepareResponsesCompactPassthrough(c, info))

			apiErr := ResponsesCompactPassthroughHelper(c, info)

			require.Nil(t, apiErr)
			query, err := url.ParseQuery(upstreamQuery)
			require.NoError(t, err)
			assert.Equal(t, "page-2", query.Get("cursor"))
			assert.Empty(t, query.Get("api_key"))
			assert.Empty(t, query.Get("custom_token_hint"))
			assert.Empty(t, query.Get("signature_v2"))
			assert.Empty(t, query.Get("password"))
			assert.NotContains(t, upstreamQuery, "client-secret")
			assert.NotContains(t, upstreamQuery, "hidden-")
		})
	}
}

func TestResponsesCompactPassthroughCodexPathMatrix(t *testing.T) {
	tests := []struct {
		name         string
		mode         relayconstant.ResponsesCompactMode
		expectedPath string
	}{
		{name: "V1 Compact", mode: relayconstant.ResponsesCompactModeV1Path, expectedPath: "/backend-api/codex/responses/compact"},
		{name: "历史 bridge", mode: relayconstant.ResponsesCompactModeV1BodyBridge, expectedPath: "/backend-api/codex/responses"},
		{name: "V2 HTTP", mode: relayconstant.ResponsesCompactModeV2HTTP, expectedPath: "/backend-api/codex/responses"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			relayMode := relayconstant.RelayModeResponses
			if test.mode == relayconstant.ResponsesCompactModeV1Path {
				relayMode = relayconstant.RelayModeResponsesCompact
			}
			info := &relaycommon.RelayInfo{
				RelayMode:            relayMode,
				ResponsesCompactMode: test.mode,
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiType:        appconstant.APITypeCodex,
					ChannelType:    appconstant.ChannelTypeCodex,
					ChannelBaseUrl: "https://sub2api.example",
				},
			}
			outboundInfo := responsesCompactPassthroughOutboundInfo(info)
			adaptor := GetAdaptor(appconstant.APITypeCodex)
			require.NotNil(t, adaptor)
			adaptor.Init(outboundInfo)

			requestURL, err := adaptor.GetRequestURL(outboundInfo)

			require.NoError(t, err)
			assert.Equal(t, "https://sub2api.example"+test.expectedPath, requestURL)
		})
	}
}

func TestResponsesCompactPassthroughUsesAdvancedCustomNativeRoutes(t *testing.T) {
	service.InitHttpClient()
	withResponsesCompactPassthroughQuotaTestState(t)
	tests := []struct {
		name         string
		mode         relayconstant.ResponsesCompactMode
		downstream   string
		upstreamPath string
	}{
		{name: "V1 Compact", mode: relayconstant.ResponsesCompactModeV1Path, downstream: "/v1/responses/compact", upstreamPath: "/native/responses/compact"},
		{name: "历史 bridge", mode: relayconstant.ResponsesCompactModeV1BodyBridge, downstream: "/v1/responses", upstreamPath: "/native/responses"},
		{name: "V2 HTTP", mode: relayconstant.ResponsesCompactModeV2HTTP, downstream: "/v1/responses", upstreamPath: "/native/responses"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var receivedPath string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedPath = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`)
			}))
			defer upstream.Close()

			c, recorder := newResponsesCompactPassthroughTestContext(t, test.downstream, `{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"compaction_trigger"}],"future":0}`)
			billing := &responsesCompactPassthroughBillingStub{preConsumed: 10}
			info := newResponsesCompactPassthroughTestInfo(c, test.mode, upstream.URL, billing)
			common.SetContextKey(c, appconstant.ContextKeyChannelType, appconstant.ChannelTypeAdvancedCustom)
			common.SetContextKey(c, appconstant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
				AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
					{
						IncomingPath: test.downstream,
						UpstreamPath: test.upstreamPath,
						Converter:    relayconvert.ConverterNone,
					},
				}},
			})
			require.Nil(t, PrepareResponsesCompactPassthrough(c, info))

			apiErr := ResponsesCompactPassthroughHelper(c, info)

			require.Nil(t, apiErr)
			assert.Equal(t, appconstant.APITypeAdvancedCustom, info.ApiType)
			assert.Equal(t, test.upstreamPath, receivedPath)
			assert.Equal(t, 1, billing.settleCalls)
			assert.False(t, billing.refunded)
			assert.JSONEq(t, `{"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`, recorder.Body.String())
		})
	}
}

func TestResponsesCompactPassthroughStreamsExactSSEAndObservesUsage(t *testing.T) {
	service.InitHttpClient()
	withResponsesCompactPassthroughQuotaTestState(t)
	streamBody := ": upstream-ping\r\nevent: response.output_item.done\r\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"compaction\",\"encrypted_content\":\"opaque\",\"future\":true}}\r\n\r\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":8,\"output_tokens\":1,\"total_tokens\":9,\"input_tokens_details\":{\"cache_write_tokens\":2}},\"future\":{\"kept\":true}}}\r\n\r\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Codex-Turn-State", "turn-state")
		_, _ = io.WriteString(w, streamBody)
	}))
	defer upstream.Close()

	c, recorder := newResponsesCompactPassthroughTestContext(t, "/v1/responses", `{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"compaction_trigger"}]}`)
	billing := &responsesCompactPassthroughBillingStub{preConsumed: 10}
	info := newResponsesCompactPassthroughTestInfo(c, relayconstant.ResponsesCompactModeV2HTTP, upstream.URL, billing)
	require.Nil(t, PrepareResponsesCompactPassthrough(c, info))
	info.FreezeBillingModelName("gpt-5.6-sol")

	apiErr := ResponsesCompactPassthroughHelper(c, info)

	require.Nil(t, apiErr)
	assert.Equal(t, streamBody, recorder.Body.String())
	assert.Equal(t, "turn-state", recorder.Header().Get("X-Codex-Turn-State"))
	assert.Equal(t, 1, billing.settleCalls)
	assert.False(t, billing.refunded)
	require.NotNil(t, info.ResponsesUsageInfo)
	assert.True(t, info.ResponsesUsageInfo.ResponseCompleted)
	assert.Equal(t, 1, info.ResponsesUsageInfo.CompactionOutputItemCount)
	assert.True(t, info.HasSendResponse())
}

func TestResponsesCompactPassthroughRefundsFailedOrAmbiguousStreams(t *testing.T) {
	service.InitHttpClient()
	withResponsesCompactPassthroughQuotaTestState(t)
	completed := "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"
	tests := []struct {
		name             string
		streamBody       string
		expectedTerminal string
	}{
		{name: "仅失败终态", streamBody: "data: {\"type\":\"response.failed\"}\n\n", expectedTerminal: "response.failed"},
		{name: "成功后失败", streamBody: completed + "data: {\"type\":\"response.failed\"}\n\n", expectedTerminal: "multiple_terminal_events"},
		{name: "失败后成功", streamBody: "data: {\"type\":\"response.failed\"}\n\n" + completed, expectedTerminal: "multiple_terminal_events"},
		{name: "重复成功终态", streamBody: completed + completed, expectedTerminal: "multiple_terminal_events"},
		{name: "无终态 EOF", streamBody: "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"compaction\"}}\n\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, test.streamBody)
			}))
			defer upstream.Close()

			c, recorder := newResponsesCompactPassthroughTestContext(t, "/v1/responses", `{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"compaction_trigger"}]}`)
			billing := &responsesCompactPassthroughBillingStub{preConsumed: 10}
			info := newResponsesCompactPassthroughTestInfo(c, relayconstant.ResponsesCompactModeV2HTTP, upstream.URL, billing)
			require.Nil(t, PrepareResponsesCompactPassthrough(c, info))

			apiErr := ResponsesCompactPassthroughHelper(c, info)

			require.Nil(t, apiErr)
			assert.Equal(t, test.streamBody, recorder.Body.String())
			assert.True(t, billing.refunded)
			assert.Zero(t, billing.settleCalls)
			assert.True(t, info.HasSendResponse())
			if test.expectedTerminal != "" {
				require.NotNil(t, info.ResponsesUsageInfo)
				assert.Equal(t, test.expectedTerminal, info.ResponsesUsageInfo.TerminalEventType)
				assert.False(t, info.ResponsesUsageInfo.ResponseCompleted)
			}
		})
	}
}

func TestResponsesCompactPassthroughRefundsMissingUsageWithoutChangingResponse(t *testing.T) {
	service.InitHttpClient()
	responseBody := `{"id":"compact_1","usage":null,"future":{"kept":true}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, responseBody)
	}))
	defer upstream.Close()

	c, recorder := newResponsesCompactPassthroughTestContext(t, "/v1/responses/compact", `{"model":"gpt-5.6-sol"}`)
	billing := &responsesCompactPassthroughBillingStub{preConsumed: 10}
	info := newResponsesCompactPassthroughTestInfo(c, relayconstant.ResponsesCompactModeV1Path, upstream.URL, billing)
	require.Nil(t, PrepareResponsesCompactPassthrough(c, info))

	apiErr := ResponsesCompactPassthroughHelper(c, info)

	require.Nil(t, apiErr)
	assert.Equal(t, responseBody, recorder.Body.String())
	assert.True(t, billing.refunded)
	assert.Equal(t, 0, billing.settleCalls)
	assert.True(t, info.HasSendResponse())
}

func TestSafeResponsesCompactPassthroughResponseFiltersHopByHopHeaders(t *testing.T) {
	c, _ := newResponsesCompactPassthroughTestContext(t, "/v1/responses/compact", `{"model":"gpt-5.6-sol"}`)
	response := &http.Response{Header: http.Header{
		"Connection":             {"X-Hop"},
		"Keep-Alive":             {"timeout=5"},
		"X-Hop":                  {"remove-me"},
		"Content-Length":         {"999"},
		common.RequestIdKey:      {"upstream-request-id"},
		"X-Safe-Upstream-Header": {"kept"},
	}}

	safe := safeResponsesCompactPassthroughResponse(c, response)

	assert.Equal(t, "kept", safe.Header.Get("X-Safe-Upstream-Header"))
	assert.Empty(t, safe.Header.Get("Connection"))
	assert.Empty(t, safe.Header.Get("Keep-Alive"))
	assert.Empty(t, safe.Header.Get("X-Hop"))
	assert.Empty(t, safe.Header.Get("Content-Length"))
	assert.Empty(t, safe.Header.Get(common.RequestIdKey))
	assert.Equal(t, "upstream-request-id", c.GetString(common.UpstreamRequestIdKey))
}

func TestParseResponsesCompactPassthroughUsageRejectsIncompleteOrInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "缺少 total_tokens", raw: `{"input_tokens":1,"output_tokens":2}`},
		{name: "负数 token", raw: `{"input_tokens":-1,"output_tokens":2,"total_tokens":1}`},
		{name: "总数不一致", raw: `{"input_tokens":1,"output_tokens":2,"total_tokens":4}`},
		{name: "超过计费安全上限", raw: `{"input_tokens":2147483648,"output_tokens":0,"total_tokens":2147483648}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usage, valid := ParseResponsesCompactPassthroughUsage(json.RawMessage(test.raw))

			assert.False(t, valid)
			assert.Nil(t, usage)
		})
	}
}

func TestParseResponsesCompactPassthroughUsageAcceptsExplicitZeroValues(t *testing.T) {
	usage, valid := ParseResponsesCompactPassthroughUsage(json.RawMessage(`{"input_tokens":0,"output_tokens":0,"total_tokens":0}`))

	require.True(t, valid)
	require.NotNil(t, usage)
	assert.Zero(t, usage.PromptTokens)
	assert.Zero(t, usage.CompletionTokens)
	assert.Zero(t, usage.TotalTokens)
}
