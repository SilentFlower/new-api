package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBuildClaudeCountTokensURLKeepsBetaQuery(t *testing.T) {
	t.Parallel()

	info := &relaycommon.RelayInfo{
		IsClaudeBetaQuery: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://example.com/",
		},
	}

	got, err := buildClaudeCountTokensURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/v1/messages/count_tokens?beta=true", got)
}

func TestBuildClaudeCountTokensHeadersMergesBetaTokens(t *testing.T) {
	t.Parallel()

	c, _ := newClaudeCountTokensTestContext(
		http.MethodPost,
		"/v1/messages/count_tokens?beta=true",
		`{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}]}`,
	)
	c.Request.Header.Set("anthropic-beta", "context-1,token-counting-2024-11-01")
	c.Request.Header.Set("anthropic-version", "2023-06-01")

	req := httptest.NewRequest(http.MethodPost, "https://example.com/v1/messages/count_tokens?beta=true", nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "claude-opus-4-8",
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey: "upstream-key",
			HeadersOverride: map[string]any{
				"anthropic-beta": "context-2",
			},
		},
	}

	err := buildClaudeCountTokensHeaders(c, req, info)
	require.NoError(t, err)
	require.Equal(t, "upstream-key", req.Header.Get("x-api-key"))
	require.Equal(t, "2023-06-01", req.Header.Get("anthropic-version"))
	require.Equal(t, "context-1,token-counting-2024-11-01,context-2", req.Header.Get("anthropic-beta"))
}

func TestBuildClaudeCountTokensHeadersDoesNotUseModelClaudeHeaders(t *testing.T) {
	oldHeaders := model_setting.GetClaudeSettings().HeadersSettings
	defer func() {
		model_setting.GetClaudeSettings().HeadersSettings = oldHeaders
	}()
	model_setting.GetClaudeSettings().HeadersSettings = map[string]map[string][]string{
		"claude-opus-4-8": {
			"anthropic-beta": {"messages-only-beta"},
		},
	}

	c, _ := newClaudeCountTokensTestContext(
		http.MethodPost,
		"/v1/messages/count_tokens?beta=true",
		`{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}]}`,
	)
	c.Request.Header.Set("anthropic-beta", "client-beta")

	req := httptest.NewRequest(http.MethodPost, "https://example.com/v1/messages/count_tokens?beta=true", nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "claude-opus-4-8",
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey: "upstream-key",
		},
	}

	err := buildClaudeCountTokensHeaders(c, req, info)
	require.NoError(t, err)
	require.Equal(t, "client-beta,token-counting-2024-11-01", req.Header.Get("anthropic-beta"))
	require.NotContains(t, strings.Split(req.Header.Get("anthropic-beta"), ","), "messages-only-beta")
}

func TestBuildClaudeCountTokensRequestBodyRemovesGenerationFields(t *testing.T) {
	t.Parallel()

	body := map[string]any{
		"model":       "claude-opus-4-8",
		"max_tokens":  1,
		"stream":      false,
		"temperature": 0.3,
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
		"tools": []any{
			map[string]any{"name": "tool_a"},
		},
		"thinking": map[string]any{"type": "enabled"},
		"system":   "system prompt",
	}

	got, err := buildClaudeCountTokensRequestBody(body, nil)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, common.Unmarshal(got, &decoded))
	require.Equal(t, "claude-opus-4-8", decoded["model"])
	require.Contains(t, decoded, "messages")
	require.Contains(t, decoded, "tools")
	require.Contains(t, decoded, "thinking")
	require.Contains(t, decoded, "system")
	require.NotContains(t, decoded, "max_tokens")
	require.NotContains(t, decoded, "stream")
	require.NotContains(t, decoded, "temperature")
}

func TestRelayClaudeCountTokensProxiesSuccessResponse(t *testing.T) {
	oldDoRequest := claudeCountTokensDoRequest
	oldRedisEnabled := common.RedisEnabled
	defer func() {
		claudeCountTokensDoRequest = oldDoRequest
		common.RedisEnabled = oldRedisEnabled
	}()
	common.RedisEnabled = false

	c, recorder := newClaudeCountTokensTestContext(
		http.MethodPost,
		"/v1/messages/count_tokens?beta=true",
		`{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}],"stream":false,"max_tokens":1}`,
	)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://upstream.example")
	common.SetContextKey(c, constant.ContextKeyChannelKey, "upstream-key")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeAnthropic)
	common.SetContextKey(c, constant.ContextKeyChannelId, 801)
	common.SetContextKey(c, constant.ContextKeyUserId, 331)
	common.SetContextKey(c, constant.ContextKeyChannelUserConcurrencyLimit, 1)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{})

	var upstreamPath string
	var upstreamBody map[string]any
	var upstreamBeta string
	claudeCountTokensDoRequest = func(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) (*http.Response, error) {
		require.Equal(t, c.Request.Context(), req.Context())
		upstreamPath = req.URL.String()
		upstreamBeta = req.Header.Get("anthropic-beta")
		bodyBytes, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.NoError(t, common.Unmarshal(bodyBytes, &upstreamBody))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
			},
			Body: io.NopCloser(strings.NewReader(`{"input_tokens":42}`)),
		}, nil
	}

	RelayClaudeCountTokens(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"input_tokens":42}`, recorder.Body.String())
	require.Equal(t, "https://upstream.example/v1/messages/count_tokens?beta=true", upstreamPath)
	require.Contains(t, strings.Split(upstreamBeta, ","), claudeTokenCountingBeta)
	require.Equal(t, "claude-opus-4-8", upstreamBody["model"])
	require.NotContains(t, upstreamBody, "stream")
	require.NotContains(t, upstreamBody, "max_tokens")
}

func TestRelayClaudeCountTokensRejectsConcurrencyBeforeUpstream(t *testing.T) {
	oldDoRequest := claudeCountTokensDoRequest
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		claudeCountTokensDoRequest = oldDoRequest
		common.RedisEnabled = oldRedisEnabled
	})

	lease, err := service.AcquireChannelUserConcurrency(context.Background(), 802, 332, 1, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, lease.Release(context.Background()))
	})

	c, recorder := newClaudeCountTokensTestContext(
		http.MethodPost,
		"/v1/messages/count_tokens?beta=true",
		`{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}]}`,
	)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://upstream.example")
	common.SetContextKey(c, constant.ContextKeyChannelKey, "upstream-key")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeAnthropic)
	common.SetContextKey(c, constant.ContextKeyChannelId, 802)
	common.SetContextKey(c, constant.ContextKeyUserId, 332)
	common.SetContextKey(c, constant.ContextKeyChannelUserConcurrencyLimit, 1)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{})

	var upstreamCalls atomic.Int32
	claudeCountTokensDoRequest = func(_ *gin.Context, _ *http.Request, _ *relaycommon.RelayInfo) (*http.Response, error) {
		upstreamCalls.Add(1)
		return nil, nil
	}

	RelayClaudeCountTokens(c)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Zero(t, upstreamCalls.Load())
	require.Contains(t, recorder.Body.String(), string(types.ErrorCodeChannelUserConcurrencyExceeded))
}

func TestRelayClaudeCountTokensProxiesErrorResponse(t *testing.T) {
	oldDoRequest := claudeCountTokensDoRequest
	defer func() {
		claudeCountTokensDoRequest = oldDoRequest
	}()

	c, recorder := newClaudeCountTokensTestContext(
		http.MethodPost,
		"/v1/messages/count_tokens?beta=true",
		`{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hi"}]}`,
	)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://upstream.example")
	common.SetContextKey(c, constant.ContextKeyChannelKey, "upstream-key")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeAnthropic)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{})

	claudeCountTokensDoRequest = func(_ *gin.Context, _ *http.Request, _ *relaycommon.RelayInfo) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
			},
			Body: io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`)),
		}, nil
	}

	RelayClaudeCountTokens(c)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.JSONEq(t, `{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`, recorder.Body.String())
}

func newClaudeCountTokensTestContext(method string, target string, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}
