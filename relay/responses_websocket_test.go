package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDialResponsesWebSocketBuildsSafeUpstreamHandshake(t *testing.T) {
	gin.SetMode(gin.TestMode)
	captured := make(chan *http.Request, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured <- r.Clone(r.Context())
		responseHeader := http.Header{
			"X-Codex-Turn-State":    {"upstream-turn-state"},
			"X-Codex-Turn-Metadata": {"upstream-turn-metadata"},
			"X-Request-Id":          {"standard-upstream-request-id"},
			"X-Oneapi-Request-Id":   {"nested-oneapi-request-id"},
		}
		conn, err := upgrader.Upgrade(w, r, responseHeader)
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/responses?cursor=next&key=client-secret&client_secret=hidden-client-secret&password=hidden-password&custom_token_hint=hidden-token&signature_v2=hidden-signature",
		nil,
	)
	c.Request.Header.Set("Authorization", "Bearer client-secret")
	c.Request.Header.Set("X-Codex-Beta-Features", "remote_compaction_v2")
	c.Request.Header.Set("X-Codex-Turn-State", "turn-state")
	c.Request.Header.Set("Session-Id", "session-1")
	c.Request.Header.Set("Thread-Id", "thread-1")
	info := &relaycommon.RelayInfo{
		RelayMode:            relayconstant.RelayModeResponses,
		RequestURLPath:       "/v1/responses",
		ResponsesCompactMode: relayconstant.ResponsesCompactModeV2WebSocket,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       appconstant.ChannelTypeOpenAI,
			ApiType:           appconstant.APITypeOpenAI,
			ApiKey:            "upstream-key",
			ChannelBaseUrl:    server.URL,
			UpstreamModelName: "gpt-5",
		},
	}

	conn, _, apiErr := DialResponsesWebSocket(c, info)
	require.Nil(t, apiErr)
	require.NotNil(t, conn)
	defer conn.Close()

	request := <-captured
	assert.Equal(t, "/v1/responses", request.URL.Path)
	assert.Equal(t, "next", request.URL.Query().Get("cursor"))
	assert.Empty(t, request.URL.Query().Get("key"))
	assert.Empty(t, request.URL.Query().Get("client_secret"))
	assert.Empty(t, request.URL.Query().Get("password"))
	assert.Empty(t, request.URL.Query().Get("custom_token_hint"))
	assert.Empty(t, request.URL.Query().Get("signature_v2"))
	assert.NotContains(t, request.URL.RawQuery, "hidden-")
	assert.Equal(t, "Bearer upstream-key", request.Header.Get("Authorization"))
	assert.Equal(t, responsesWebSocketBeta, request.Header.Get("OpenAI-Beta"))
	assert.Equal(t, "remote_compaction_v2", request.Header.Get("X-Codex-Beta-Features"))
	assert.Equal(t, "turn-state", request.Header.Get("X-Codex-Turn-State"))
	assert.Equal(t, "session-1", request.Header.Get("Session-Id"))
	assert.Equal(t, "session-1", request.Header.Get("Session_id"))
	assert.Equal(t, "thread-1", request.Header.Get("Thread-Id"))
	assert.Equal(t, "thread-1", request.Header.Get("Thread_id"))
	assert.NotEqual(t, "Bearer client-secret", request.Header.Get("Authorization"))
	assert.Equal(t, "upstream-turn-state", c.Request.Header.Get("X-Codex-Turn-State"))
	assert.Equal(t, "upstream-turn-metadata", c.Request.Header.Get("X-Codex-Turn-Metadata"))
	assert.Equal(t, "standard-upstream-request-id", c.GetString(common.UpstreamRequestIdKey))
	assert.Equal(t, "/v1/responses", info.UpstreamRequestURLPath)
}

func TestDialResponsesWebSocketUsesAdvancedCustomNativeRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	capturedPath := make(chan string, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath <- r.URL.Path
		conn, err := upgrader.Upgrade(w, r, nil)
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeResponses,
		RequestURLPath: "/v1/responses",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       appconstant.ChannelTypeAdvancedCustom,
			ApiType:           appconstant.APITypeAdvancedCustom,
			ApiKey:            "upstream-key",
			ChannelBaseUrl:    server.URL,
			UpstreamModelName: "gpt-5",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
					{
						IncomingPath: "/v1/responses",
						UpstreamPath: "/native/responses",
						Converter:    "none",
					},
				}},
			},
		},
	}

	conn, _, apiErr := DialResponsesWebSocket(c, info)
	require.Nil(t, apiErr)
	require.NotNil(t, conn)
	defer conn.Close()

	assert.Equal(t, "/native/responses", <-capturedPath)
}
