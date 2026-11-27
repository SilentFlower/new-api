package controller

import (
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResponsesWebSocketFirstResponseTimingIgnoresControlFrames 通过真实上游验证控制帧无法产生消费。
func TestResponsesWebSocketFirstResponseTimingIgnoresControlFrames(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, "tier(\"request\", fixed(0.002))", func(ws *websocket.Conn, _ *http.Request) {
		if _, _, err := ws.ReadMessage(); !assert.NoError(t, err) {
			return
		}
		ws.SetPongHandler(func(string) error {
			return ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"), time.Now().Add(time.Second))
		})
		if !assert.NoError(t, ws.WriteControl(websocket.PingMessage, []byte("probe"), time.Now().Add(time.Second))) {
			return
		}
		_, _, _ = ws.ReadMessage()
	})
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)))
	_, _, err := fixture.client.ReadMessage()
	require.Error(t, err)
	fixture.closeAndWait(t)
	assertResponsesWSAccounting(t, fixture, nil)
}
