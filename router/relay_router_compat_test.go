package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetRelayRouterRegistersClaudeCountTokensRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	SetRelayRouter(engine)

	var count int
	for _, route := range engine.Routes() {
		if route.Method == http.MethodPost && route.Path == "/v1/messages/count_tokens" {
			require.Contains(t, route.Handler, "controller.RelayClaudeCountTokens")
			count++
		}
	}
	require.Equal(t, 1, count)
}

func TestSetRelayRouterRegistersAlphaSearchRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	SetRelayRouter(engine)

	for _, route := range engine.Routes() {
		if route.Method == http.MethodPost && route.Path == "/v1/alpha/search" {
			require.Contains(t, route.Handler, "SetRelayRouter")
			return
		}
	}
	t.Fatal("expected POST /v1/alpha/search to be registered")
}

func TestSetRelayRouterRegistersResponsesWebSocketRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	SetRelayRouter(engine)

	for _, route := range engine.Routes() {
		if route.Method == http.MethodGet && route.Path == "/v1/responses" {
			require.Contains(t, route.Handler, "controller.ResponsesWebSocket")
			return
		}
	}
	t.Fatal("expected GET /v1/responses to be registered")
}
