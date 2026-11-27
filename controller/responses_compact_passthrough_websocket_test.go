package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResponsesCompactWebSocketRawFrameAndTerminalUsage 核对普通渠道上的原始 Compact 帧和无效用量退款。
func TestResponsesCompactWebSocketRawFrameAndTerminalUsage(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		enabled     bool
		wantQuota   []int
	}{
		{"渠道未开启", "null", false, nil},
		{"缺少用量", "null", true, nil},
		{"有效用量", `{"input_tokens":2,"output_tokens":3,"total_tokens":5}`, true, []int{1000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const raw = `{"type":"response.create", "stream":true,"model":"ws-billing","input":[{"type":"compaction_trigger"}],"future":{"off":false,"zero":0}}`
			fixture := newResponsesWSBillingTest(t, "tier(\"request\", fixed(0.002))", func(ws *websocket.Conn, _ *http.Request) {
				_, frame, err := ws.ReadMessage()
				if !assert.NoError(t, err) {
					return
				}
				assert.Equal(t, raw, string(frame))
				_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"compact","usage":`+tc.usage+`}}`))
				_, _, _ = ws.ReadMessage()
			})
			settings := fixture.channel.GetSetting()
			settings.ResponsesCompactPassthroughEnabled = tc.enabled
			fixture.channel.SetSetting(settings)
			require.NoError(t, model.DB.Model(fixture.channel).Update("setting", fixture.channel.Setting).Error)
			require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(raw)))
			event := readResponsesWSTestEvent(t, fixture.client)
			if tc.enabled {
				assert.Equal(t, "response.completed", event["type"])
			} else {
				assert.Equal(t, float64(503), event["status"])
				assert.Zero(t, fixture.connections.Load())
			}
			fixture.closeAndWait(t)
			assertResponsesWSAccounting(t, fixture, tc.wantQuota)
		})
	}
}
