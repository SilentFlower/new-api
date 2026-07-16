package constant

// ResponsesCompactMode 表示 Responses 远端压缩的入口协议形态。
type ResponsesCompactMode string

const (
	ResponsesCompactModeNone         ResponsesCompactMode = ""
	ResponsesCompactModeV1Path       ResponsesCompactMode = "v1_path"
	ResponsesCompactModeV1BodyBridge ResponsesCompactMode = "v1_body_bridge"
	ResponsesCompactModeV2HTTP       ResponsesCompactMode = "v2_http"
	ResponsesCompactModeV2WebSocket  ResponsesCompactMode = "v2_websocket"
)

// IsCompact 判断当前模式是否属于 Responses 远端压缩。
// @return 属于任一 Compact 模式时返回 true。
func (m ResponsesCompactMode) IsCompact() bool {
	return m != ResponsesCompactModeNone
}

// IsV2 判断当前模式是否使用原生 Responses V2 协议。
// @return 使用 HTTP 或 WebSocket V2 协议时返回 true。
func (m ResponsesCompactMode) IsV2() bool {
	return m == ResponsesCompactModeV2HTTP || m == ResponsesCompactModeV2WebSocket
}

// UsesCompactEndpoint 判断上游是否应使用 /responses/compact。
// @return V1 path 或历史 body bridge 模式返回 true。
func (m ResponsesCompactMode) UsesCompactEndpoint() bool {
	return m == ResponsesCompactModeV1Path || m == ResponsesCompactModeV1BodyBridge
}
