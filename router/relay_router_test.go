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

	for _, route := range engine.Routes() {
		if route.Method == http.MethodPost && route.Path == "/v1/messages/count_tokens" {
			require.Contains(t, route.Handler, "RelayClaudeCountTokens")
			return
		}
	}
	t.Fatalf("expected POST /v1/messages/count_tokens to be registered")
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
