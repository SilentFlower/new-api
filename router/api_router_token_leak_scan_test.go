package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenLeakScanRoutesRequireRootAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	routes := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/token-leak-scan/status"},
		{method: http.MethodGet, path: "/api/token-leak-scan/findings"},
		{method: http.MethodPost, path: "/api/token-leak-scan/run"},
		{method: http.MethodPost, path: "/api/token-leak-scan/findings/:id/disable-token"},
	}
	registeredRoutes := make(map[string]struct{}, len(engine.Routes()))
	for _, route := range engine.Routes() {
		registeredRoutes[route.Method+" "+route.Path] = struct{}{}
	}
	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			_, registered := registeredRoutes[route.method+" "+route.path]
			require.True(t, registered)

			requestPath := route.path
			if route.path == "/api/token-leak-scan/findings/:id/disable-token" {
				requestPath = "/api/token-leak-scan/findings/1/disable-token"
			}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(route.method, requestPath, nil)
			engine.ServeHTTP(recorder, request)

			assert.Equal(t, http.StatusUnauthorized, recorder.Code)
			assert.Contains(t, recorder.Body.String(), `"success":false`)
		})
	}
}
