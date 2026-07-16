package helper

import (
	"net/http"
	"strings"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/tidwall/gjson"
)

// ResponsesTransport 表示 Responses 请求使用的传输协议。
type ResponsesTransport string

const (
	ResponsesTransportHTTP      ResponsesTransport = "http"
	ResponsesTransportWebSocket ResponsesTransport = "websocket"
)

const remoteCompactionV2Feature = "remote_compaction_v2"

// DetectResponsesCompactMode 识别 OpenAI Codex Responses Compact 协议形态。
// @param method 入站 HTTP 方法。
// @param requestPath 入站请求路径。
// @param headers 入站请求头。
// @param body HTTP 请求体或 WebSocket response.create 帧。
// @param transport 当前传输协议。
// @return 识别到的 Compact 模式；普通 Responses 或非法信号返回 none。
func DetectResponsesCompactMode(method string, requestPath string, headers http.Header, body []byte, transport ResponsesTransport) relayconstant.ResponsesCompactMode {
	normalizedPath := strings.TrimRight(strings.TrimSpace(requestPath), "/")
	if transport == ResponsesTransportHTTP && method == http.MethodPost && isResponsesCompactPath(normalizedPath) {
		return relayconstant.ResponsesCompactModeV1Path
	}
	if !isBareResponsesPath(normalizedPath) || !hasCompactionTrigger(body) {
		return relayconstant.ResponsesCompactModeNone
	}

	isRemoteV2 := gjson.GetBytes(body, "stream").Bool() && headerContainsToken(headers, "x-codex-beta-features", remoteCompactionV2Feature)
	if transport == ResponsesTransportWebSocket {
		if method == http.MethodGet && isRemoteV2 {
			return relayconstant.ResponsesCompactModeV2WebSocket
		}
		return relayconstant.ResponsesCompactModeNone
	}
	if method != http.MethodPost {
		return relayconstant.ResponsesCompactModeNone
	}
	if isRemoteV2 {
		return relayconstant.ResponsesCompactModeV2HTTP
	}
	return relayconstant.ResponsesCompactModeV1BodyBridge
}

func isResponsesCompactPath(path string) bool {
	return path == "/v1/responses/compact" || path == "/responses/compact"
}

func isBareResponsesPath(path string) bool {
	return path == "/v1/responses" || path == "/responses"
}

func hasCompactionTrigger(body []byte) bool {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return false
	}
	found := false
	input.ForEach(func(_, item gjson.Result) bool {
		if item.Get("type").String() == "compaction_trigger" {
			found = true
			return false
		}
		return true
	})
	return found
}

func headerContainsToken(headers http.Header, name string, expected string) bool {
	for _, value := range headers.Values(name) {
		for _, token := range strings.Split(value, ",") {
			if strings.TrimSpace(token) == expected {
				return true
			}
		}
	}
	return false
}
