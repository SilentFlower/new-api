package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestResponsesWebSocketFirstFrameTimeout 验证实际首帧读取使用配置期限并保留后续校验。
// @param t 测试上下文。
func TestResponsesWebSocketFirstFrameTimeout(t *testing.T) {
	original := constant.ResponsesWebSocketFirstFrameTimeoutSeconds
	constant.ResponsesWebSocketFirstFrameTimeoutSeconds = 1
	t.Cleanup(func() { constant.ResponsesWebSocketFirstFrameTimeoutSeconds = original })

	for _, tc := range []struct {
		name    string
		payload string
		message string
	}{
		{name: "未发送首帧", message: "read first"},
		{name: "及时首帧继续校验模型", payload: `{"type":"response.create"}`, message: "model is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/v1/responses", RelayResponsesWebSocket)
			server := httptest.NewServer(router)
			t.Cleanup(server.Close)

			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
			if tc.payload != "" {
				require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(tc.payload)))
			}

			messageType, payload, err := conn.ReadMessage()

			require.NoError(t, err)
			assert.Equal(t, websocket.TextMessage, messageType)
			assert.Equal(t, "error", gjson.GetBytes(payload, "type").String())
			assert.Equal(t, int64(http.StatusBadRequest), gjson.GetBytes(payload, "status").Int())
			assert.Equal(t, "invalid_request", gjson.GetBytes(payload, "error.code").String())
			assert.Contains(t, gjson.GetBytes(payload, "error.message").String(), tc.message)
		})
	}
}
