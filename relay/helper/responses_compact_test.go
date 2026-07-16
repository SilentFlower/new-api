package helper

import (
	"net/http"
	"testing"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/stretchr/testify/assert"
)

func TestDetectResponsesCompactMode(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		path      string
		headers   http.Header
		body      string
		transport ResponsesTransport
		expected  relayconstant.ResponsesCompactMode
	}{
		{
			name:      "v1 compact path",
			method:    http.MethodPost,
			path:      "/v1/responses/compact",
			transport: ResponsesTransportHTTP,
			expected:  relayconstant.ResponsesCompactModeV1Path,
		},
		{
			name:      "v2 http",
			method:    http.MethodPost,
			path:      "/v1/responses",
			headers:   http.Header{"X-Codex-Beta-Features": {"responses_websockets_v2, remote_compaction_v2, other"}},
			body:      `{"stream":true,"input":[{"type":"message"},{"type":"compaction_trigger"}]}`,
			transport: ResponsesTransportHTTP,
			expected:  relayconstant.ResponsesCompactModeV2HTTP,
		},
		{
			name:      "v2 websocket with multiple header values",
			method:    http.MethodGet,
			path:      "/v1/responses/",
			headers:   http.Header{"X-Codex-Beta-Features": {"other", " remote_compaction_v2 "}},
			body:      `{"type":"response.create","stream":true,"input":[{"type":"compaction_trigger"}]}`,
			transport: ResponsesTransportWebSocket,
			expected:  relayconstant.ResponsesCompactModeV2WebSocket,
		},
		{
			name:      "legacy body signal without feature",
			method:    http.MethodPost,
			path:      "/v1/responses",
			body:      `{"stream":true,"input":[{"type":"compaction_trigger"}]}`,
			transport: ResponsesTransportHTTP,
			expected:  relayconstant.ResponsesCompactModeV1BodyBridge,
		},
		{
			name:      "feature substring does not match",
			method:    http.MethodPost,
			path:      "/v1/responses",
			headers:   http.Header{"X-Codex-Beta-Features": {"not_remote_compaction_v2"}},
			body:      `{"stream":true,"input":[{"type":"compaction_trigger"}]}`,
			transport: ResponsesTransportHTTP,
			expected:  relayconstant.ResponsesCompactModeV1BodyBridge,
		},
		{
			name:      "websocket signal without feature is ordinary",
			method:    http.MethodGet,
			path:      "/v1/responses",
			body:      `{"stream":true,"input":[{"type":"compaction_trigger"}]}`,
			transport: ResponsesTransportWebSocket,
			expected:  relayconstant.ResponsesCompactModeNone,
		},
		{
			name:      "stream false is legacy bridge",
			method:    http.MethodPost,
			path:      "/v1/responses",
			headers:   http.Header{"X-Codex-Beta-Features": {"remote_compaction_v2"}},
			body:      `{"stream":false,"input":[{"type":"compaction_trigger"}]}`,
			transport: ResponsesTransportHTTP,
			expected:  relayconstant.ResponsesCompactModeV1BodyBridge,
		},
		{
			name:      "ordinary responses",
			method:    http.MethodPost,
			path:      "/v1/responses",
			body:      `{"stream":true,"input":[{"type":"message"}]}`,
			transport: ResponsesTransportHTTP,
			expected:  relayconstant.ResponsesCompactModeNone,
		},
		{
			name:      "invalid json",
			method:    http.MethodPost,
			path:      "/v1/responses",
			body:      `{"stream":true`,
			transport: ResponsesTransportHTTP,
			expected:  relayconstant.ResponsesCompactModeNone,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := DetectResponsesCompactMode(test.method, test.path, test.headers, []byte(test.body), test.transport)
			assert.Equal(t, test.expected, actual)
		})
	}
}
