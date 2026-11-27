package relay

import (
	"bytes"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type alphaSearchBillingStub struct {
	preConsumed int
	settled     int
	refunded    bool
}

// Settle 记录 Alpha Search 测试中的实际结算额度。
// @param actualQuota 实际结算额度。
// @return 始终返回 nil。
func (s *alphaSearchBillingStub) Settle(actualQuota int) error {
	s.settled = actualQuota
	return nil
}

// Refund 记录 Alpha Search 测试中的退款动作。
// @param c 当前 Gin 请求上下文。
func (s *alphaSearchBillingStub) Refund(c *gin.Context) {
	s.refunded = true
}

// NeedsRefund 返回测试会话是否仍需退款。
// @return 尚未结算且存在预扣额度时返回 true。
func (s *alphaSearchBillingStub) NeedsRefund() bool {
	return s.preConsumed > 0 && s.settled == 0 && !s.refunded
}

// GetPreConsumedQuota 返回测试会话的预扣额度。
// @return 预扣额度。
func (s *alphaSearchBillingStub) GetPreConsumedQuota() int {
	return s.preConsumed
}

// Reserve 更新测试会话的预扣额度。
// @param targetQuota 目标预扣额度。
// @return 始终返回 nil。
func (s *alphaSearchBillingStub) Reserve(targetQuota int) error {
	if targetQuota > s.preConsumed {
		s.preConsumed = targetQuota
	}
	return nil
}

func newAlphaSearchTestContext(t *testing.T, target string, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() {
		common.CleanupBodyStorage(ctx)
	})
	return ctx, recorder
}

func TestPrepareAlphaSearchRequestBodyPreservesRawFields(t *testing.T) {
	ctx, _ := newAlphaSearchTestContext(
		t,
		"/v1/alpha/search",
		`{"id":"search_1","model":"origin-model","max_output_tokens":0,"enabled":false,"future":{"count":0}}`,
	)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "mapped-model",
			ParamOverride: map[string]any{
				"temperature": 0,
			},
		},
	}

	jsonData, apiErr := prepareAlphaSearchRequestBody(ctx, info)
	require.Nil(t, apiErr)
	assert.Equal(t, "mapped-model", gjson.GetBytes(jsonData, "model").String())
	assert.True(t, gjson.GetBytes(jsonData, "max_output_tokens").Exists())
	assert.Equal(t, int64(0), gjson.GetBytes(jsonData, "max_output_tokens").Int())
	assert.True(t, gjson.GetBytes(jsonData, "enabled").Exists())
	assert.False(t, gjson.GetBytes(jsonData, "enabled").Bool())
	assert.True(t, gjson.GetBytes(jsonData, "future.count").Exists())
	assert.True(t, gjson.GetBytes(jsonData, "temperature").Exists())

	storage, err := common.GetBodyStorage(ctx)
	require.NoError(t, err)
	originalBody, err := storage.Bytes()
	require.NoError(t, err)
	assert.Equal(t, "origin-model", gjson.GetBytes(originalBody, "model").String())
}

func TestBuildAlphaSearchUpstreamURL(t *testing.T) {
	tests := []struct {
		name          string
		info          *relaycommon.RelayInfo
		wantPath      string
		wantQuery     url.Values
		wantAPIKeySet []string
	}{
		{
			name: "普通渠道",
			info: &relaycommon.RelayInfo{
				OriginModelName: "gpt-5",
				RequestURLPath:  "/v1/alpha/search?cursor=a&cursor=b",
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiType:        constant.APITypeOpenAI,
					ChannelType:    constant.ChannelTypeOpenAI,
					ChannelBaseUrl: "https://upstream.example",
				},
			},
			wantPath:  "/v1/alpha/search",
			wantQuery: url.Values{"cursor": {"a", "b"}},
		},
		{
			name: "Codex 渠道",
			info: &relaycommon.RelayInfo{
				OriginModelName: "gpt-5",
				RequestURLPath:  "/v1/alpha/search?cursor=a&cursor=b",
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiType:        constant.APITypeCodex,
					ChannelType:    constant.ChannelTypeCodex,
					ChannelBaseUrl: "https://chatgpt.com",
				},
			},
			wantPath:  "/backend-api/codex/alpha/search",
			wantQuery: url.Values{"cursor": {"a", "b"}},
		},
		{
			name: "Advanced Custom 渠道固定查询鉴权优先",
			info: &relaycommon.RelayInfo{
				OriginModelName: "gpt-5",
				RequestURLPath:  "/v1/alpha/search?cursor=a&cursor=b&api_key=client-secret",
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiType:        constant.APITypeAdvancedCustom,
					ChannelType:    constant.ChannelTypeAdvancedCustom,
					ChannelBaseUrl: "https://custom.example",
					ApiKey:         "upstream-secret",
					ChannelOtherSettings: dto.ChannelOtherSettings{
						AdvancedCustom: &dto.AdvancedCustomConfig{
							Routes: []dto.AdvancedCustomRoute{
								{
									IncomingPath: "/v1/alpha/search",
									UpstreamPath: "/custom/search?fixed=1",
									Auth: &dto.AdvancedCustomRouteAuth{
										Type:  dto.AdvancedCustomAuthTypeQuery,
										Name:  "api_key",
										Value: "{api_key}",
									},
								},
							},
						},
					},
				},
			},
			wantPath:      "/custom/search",
			wantQuery:     url.Values{"cursor": {"a", "b"}, "fixed": {"1"}},
			wantAPIKeySet: []string{"upstream-secret"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := newAlphaSearchTestContext(t, test.info.RequestURLPath, `{"model":"gpt-5"}`)
			adaptor := GetAdaptor(test.info.ApiType)
			require.NotNil(t, adaptor)
			adaptor.Init(test.info)

			rawURL, err := buildAlphaSearchUpstreamURL(ctx, test.info, adaptor)
			require.NoError(t, err)
			parsedURL, err := url.Parse(rawURL)
			require.NoError(t, err)
			assert.Equal(t, test.wantPath, parsedURL.Path)
			for key, values := range test.wantQuery {
				assert.Equal(t, values, parsedURL.Query()[key])
			}
			if test.wantAPIKeySet != nil {
				assert.Equal(t, test.wantAPIKeySet, parsedURL.Query()["api_key"])
				assert.NotContains(t, parsedURL.RawQuery, "client-secret")
			}
		})
	}
}

func TestNewAlphaSearchUpstreamRequestReplacesClientAuthorization(t *testing.T) {
	ctx, _ := newAlphaSearchTestContext(t, "/v1/alpha/search?cursor=next", `{"model":"gpt-5"}`)
	ctx.Request.Header.Set("Authorization", "Bearer client-secret")
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiType:        constant.APITypeOpenAI,
			ChannelType:    constant.ChannelTypeOpenAI,
			ChannelBaseUrl: "https://upstream.example",
			ApiKey:         "upstream-secret",
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "trace-1",
			},
		},
	}
	adaptor := GetAdaptor(info.ApiType)
	require.NotNil(t, adaptor)
	adaptor.Init(info)

	req, err := newAlphaSearchUpstreamRequest(ctx, info, adaptor, strings.NewReader(`{"model":"gpt-5"}`), 17)
	require.NoError(t, err)
	assert.Equal(t, "Bearer upstream-secret", req.Header.Get("Authorization"))
	assert.NotContains(t, req.Header.Get("Authorization"), "client-secret")
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	assert.Equal(t, "application/json", req.Header.Get("Accept"))
	assert.Equal(t, "trace-1", req.Header.Get("X-Upstream-Trace"))
	assert.Equal(t, int64(17), req.ContentLength)
}

func TestSafeAlphaSearchResponseFiltersUnsafeHeaders(t *testing.T) {
	ctx, recorder := newAlphaSearchTestContext(t, "/v1/alpha/search", `{"model":"gpt-5"}`)
	response := &http.Response{
		StatusCode: http.StatusCreated,
		Header: http.Header{
			"Connection":             {"X-Hop"},
			"Keep-Alive":             {"timeout=5"},
			"X-Hop":                  {"remove-me"},
			"Content-Length":         {"999"},
			common.RequestIdKey:      {"upstream-request-id"},
			"X-Safe-Upstream-Header": {"kept"},
		},
	}

	service.IOCopyBytesGracefully(ctx, safeAlphaSearchResponse(ctx, response), []byte(`{}`))
	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.Equal(t, `{}`, recorder.Body.String())
	assert.Equal(t, "kept", recorder.Header().Get("X-Safe-Upstream-Header"))
	assert.Empty(t, recorder.Header().Get("Connection"))
	assert.Empty(t, recorder.Header().Get("Keep-Alive"))
	assert.Empty(t, recorder.Header().Get("X-Hop"))
	assert.Equal(t, "2", recorder.Header().Get("Content-Length"))
	assert.Empty(t, recorder.Header().Get(common.RequestIdKey))
	assert.Equal(t, "upstream-request-id", ctx.GetString(common.UpstreamRequestIdKey))
}

func TestAlphaSearchHelperDoesNotCommitNon2xxResponse(t *testing.T) {
	service.InitHttpClient()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"temporarily unavailable"}}`)
	}))
	t.Cleanup(upstream.Close)

	ctx, recorder := newAlphaSearchTestContext(t, "/v1/alpha/search", `{"model":"gpt-5","future":false}`)
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5",
		RelayMode:       relayconstant.RelayModeAlphaSearch,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiType:           constant.APITypeOpenAI,
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelBaseUrl:    upstream.URL,
			ApiKey:            "upstream-secret",
			UpstreamModelName: "gpt-5",
		},
	}

	apiErr := AlphaSearchHelper(ctx, info)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	assert.Equal(t, "bad response status code 503", apiErr.Error())
	assert.Equal(t, "bad response status code 503", apiErr.ToOpenAIError().Message)
	assert.Empty(t, recorder.Body.String())
	assert.False(t, recorder.Flushed)
}

func TestPrepareRequestForSelectedChannelMapsAlphaSearchModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", nil)
	common.SetContextKey(c, constant.ContextKeyChannelId, 12)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://upstream.example")
	common.SetContextKey(c, constant.ContextKeyChannelKey, "upstream-key")
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "origin-model")
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, `{"origin-model":"mapped-model"}`)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	request := &dto.AlphaSearchRequest{Model: "origin-model"}
	info := &relaycommon.RelayInfo{
		OriginModelName: "origin-model",
		Request:         request,
		RelayFormat:     relaytypes.RelayFormatOpenAIAlphaSearch,
		RelayMode:       relayconstant.RelayModeAlphaSearch,
	}

	apiErr := PrepareRequestForSelectedChannel(c, info)

	require.Nil(t, apiErr)
	assert.Equal(t, "mapped-model", info.UpstreamModelName)
	assert.Equal(t, "mapped-model", request.Model)
	assert.True(t, info.IsModelMapped)
}

func TestAlphaSearchHelperSettlesFrozenToolBillingOnSuccess(t *testing.T) {
	service.InitHttpClient()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Result", "ok")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"searches":[{"title":"result"}]}`)
	}))
	t.Cleanup(upstream.Close)

	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled
	common.BatchUpdateEnabled = true
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		common.LogConsumeEnabled = oldLogConsumeEnabled
	})

	ctx, recorder := newAlphaSearchTestContext(t, "/v1/alpha/search", `{"model":"gpt-5"}`)
	billing := &alphaSearchBillingStub{preConsumed: 321}
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5",
		UserQuota:       math.MaxInt,
		StartTime:       time.Now(),
		Billing:         billing,
		RelayMode:       relayconstant.RelayModeAlphaSearch,
		PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{
			GroupRatio:        1,
			GroupSpecialRatio: -1,
		}},
		ToolCallBilling: &relaycommon.ToolCallResult{
			TotalQuota: 321,
			Items: []relaycommon.ToolCallItem{{
				Name:       service.ToolNameWebSearch,
				CallCount:  1,
				PricePer1K: 10,
				TotalPrice: 0.01,
				Quota:      321,
			}},
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiType:           constant.APITypeOpenAI,
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelBaseUrl:    upstream.URL,
			ApiKey:            "upstream-secret",
			UpstreamModelName: "gpt-5",
		},
	}
	info.FreezeBillingModelName("gpt-5")

	apiErr := AlphaSearchHelper(ctx, info)

	require.Nil(t, apiErr)
	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.JSONEq(t, `{"searches":[{"title":"result"}]}`, recorder.Body.String())
	assert.Equal(t, "ok", recorder.Header().Get("X-Upstream-Result"))
	assert.Equal(t, 321, billing.settled)
	assert.False(t, billing.refunded)
}

func TestAlphaSearchHelperDoesNotLogTransportURLQuery(t *testing.T) {
	service.InitHttpClient()
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	upstreamURL := upstream.URL
	upstream.Close()

	var logBuffer bytes.Buffer
	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	ctx, _ := newAlphaSearchTestContext(t, "/v1/alpha/search?cursor=client-query-secret", `{"model":"gpt-5"}`)
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5",
		RelayMode:       relayconstant.RelayModeAlphaSearch,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiType:           constant.APITypeOpenAI,
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelBaseUrl:    upstreamURL,
			ApiKey:            "upstream-secret",
			UpstreamModelName: "gpt-5",
		},
	}

	apiErr := AlphaSearchHelper(ctx, info)

	require.NotNil(t, apiErr)
	assert.Equal(t, "upstream error: do request failed", apiErr.Error())
	assert.NotContains(t, logBuffer.String(), "client-query-secret")
}

func TestBuildAlphaSearchRequestBodyPreservesUnknownFields(t *testing.T) {
	raw := []byte(`{
		"id":"req_1",
		"model":"gpt-5.1",
		"input":[{"role":"user","content":"hi"}],
		"commands":{"search_query":[{"q":"weather","recency":1}]},
		"settings":{"locale":"en"},
		"future_field":{"nested":true}
	}`)

	out, err := buildAlphaSearchRequestBody(raw, "gpt-5.1", "gpt-5.1-mapped")
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, common.Unmarshal(out, &body))
	assert.Equal(t, "gpt-5.1-mapped", body["model"])
	assert.Equal(t, "req_1", body["id"])
	require.Contains(t, body, "commands")
	require.Contains(t, body, "settings")
	require.Contains(t, body, "future_field")
	require.Contains(t, body, "input")

	commands, ok := body["commands"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, commands, "search_query")

	future, ok := body["future_field"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, future["nested"])
}

func TestBuildAlphaSearchRequestBodyNoMappingKeepsRawBytes(t *testing.T) {
	raw := []byte(`{"model":"gpt-5.1","commands":{"search_query":[{"q":"x"}]},"future_field":1}`)
	out, err := buildAlphaSearchRequestBody(raw, "gpt-5.1", "gpt-5.1")
	require.NoError(t, err)
	assert.Equal(t, raw, out)
}

// A single model is rewritten where it stands. Routing reads the first
// "model" and most upstreams read the last, so a repeated model comes out as
// one member carrying the mapped value.
func TestBuildAlphaSearchRequestBodyRewritesModel(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"single model keeps its place", `{"model":"gpt-5.1","input":[]}`, `{"model":"gpt-5.1-mapped","input":[]}`},
		{"repeated model collapses", `{"model":"gpt-5.1","input":[],"model":"gpt-5.5-pro"}`, `{"input":[],"model":"gpt-5.1-mapped"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := buildAlphaSearchRequestBody([]byte(tt.raw), "gpt-5.1", "gpt-5.1-mapped")
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(out))
		})
	}
}

type recordedSettlement struct {
	settled  []int
	refunded bool
}

func (s *recordedSettlement) Settle(quota int) error {
	s.settled = append(s.settled, quota)
	return nil
}
func (s *recordedSettlement) Refund(*gin.Context)    { s.refunded = true }
func (*recordedSettlement) NeedsRefund() bool        { return false }
func (*recordedSettlement) GetPreConsumedQuota() int { return 0 }
func (*recordedSettlement) Reserve(int) error        { return nil }

func (*recordedSettlement) ReserveBeforeSubmit(int) error { return nil }

// /v1/tools/search and /v1/tools/fetch follow /v1/alpha/search: the raw body
// reaches /v1/tools/* unchanged and the response returns unchanged. With only
// the built-in prices, a response DeepSeek bills (results, page content)
// settles one call at the routing name's request price and no tokens, an
// empty one releases the reservation, an upstream error keeps its status, and
// a channel that cannot relay the endpoint fails retryably without sending
// anything.
func TestDeepSeekToolsHelper(t *testing.T) {
	service.InitHttpClient()
	gin.SetMode(gin.TestMode)
	batch, logConsume := common.BatchUpdateEnabled, common.LogConsumeEnabled
	common.BatchUpdateEnabled, common.LogConsumeEnabled = true, false
	t.Cleanup(func() { common.BatchUpdateEnabled, common.LogConsumeEnabled = batch, logConsume })

	const search, fetch = "/v1/tools/search", "/v1/tools/fetch"
	const results = `{"results":[{"title":"DeepSeek","url":"https://deepseek.com/","rerank_score":2.1}]}`
	tests := []struct {
		name, path, body, response string
		channelType, status        int
		settled                    []int
		refunded                   bool
	}{
		{name: "search with results", path: search, body: `{"query":"deepseek","count":2,"future_field":{"nested":true}}`,
			response: results, channelType: constant.ChannelTypeDeepSeek, status: http.StatusOK, settled: []int{375}},
		{name: "search without results", path: search, body: `{"query":"site:none.invalid deepseek"}`,
			response: `{"results":[]}`, channelType: constant.ChannelTypeDeepSeek, status: http.StatusOK, refunded: true},
		{name: "fetch with page content", path: fetch, body: `{"url":"https://example.com/"}`,
			response: `{"title":"Example Domain","content":"# Example Domain"}`, channelType: constant.ChannelTypeDeepSeek, status: http.StatusOK, settled: []int{375}},
		{name: "fetch failure keeps its status", path: fetch, body: `{"url":"https://example.com/missing"}`,
			response:    `{"error":{"message":"failed to fetch URL","type":"not_found_error","param":null,"code":"invalid_request_error"}}`,
			channelType: constant.ChannelTypeDeepSeek, status: http.StatusNotFound},
		{name: "search through a New API channel", path: search, body: `{"query":"deepseek"}`,
			response: results, channelType: constant.ChannelTypeNewAPI, status: http.StatusOK, settled: []int{375}},
		{name: "channel without the endpoint", path: search, body: `{"query":"deepseek"}`, channelType: constant.ChannelTypeOpenAI},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			var gotBody []byte
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotBody, _ = io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.response)
			}))
			t.Cleanup(upstream.Close)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			routingName := map[string]string{search: constant.DeepSeekSearchModel, fetch: constant.DeepSeekFetchModel}[tc.path]
			common.SetContextKey(c, constant.ContextKeyOriginalModel, routingName)
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(c, constant.ContextKeyChannelType, tc.channelType)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(c, constant.ContextKeyChannelKey, "sk-test")

			request, err := helper.GetAndValidateRequest(c, relaytypes.RelayFormatDeepSeekTools)
			require.NoError(t, err)
			info, err := relaycommon.GenRelayInfo(c, relaytypes.RelayFormatDeepSeekTools, request, nil)
			require.NoError(t, err)
			info.BillingRequestInput = &billingexpr.RequestInput{Body: []byte(tc.body)}
			_, err = helper.ModelPriceHelper(c, info, 0, request.GetTokenCountMeta())
			require.NoError(t, err, "the routing name is priced out of the box")
			settlement := &recordedSettlement{}
			info.Billing = settlement

			apiErr := DeepSeekToolsHelper(c, info)
			if tc.status == 0 {
				require.NotNil(t, apiErr)
				assert.False(t, relaytypes.IsSkipRetryError(apiErr))
				assert.Empty(t, gotPath)
				return
			}
			assert.Equal(t, tc.path, gotPath)
			assert.Equal(t, tc.body, string(gotBody))
			assert.Equal(t, tc.settled, settlement.settled)
			assert.Equal(t, tc.refunded, settlement.refunded)
			if tc.status != http.StatusOK {
				require.NotNil(t, apiErr)
				assert.Equal(t, tc.status, apiErr.StatusCode)
				return
			}
			require.Nil(t, apiErr)
			assert.Equal(t, tc.status, recorder.Code)
			assert.Equal(t, tc.response, recorder.Body.String())
		})
	}
}

func (s *alphaSearchBillingStub) ReserveBeforeSubmit(quota int) error { return s.Reserve(quota) }
