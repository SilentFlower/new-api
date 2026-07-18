package common

import relayconstant "github.com/QuantumNous/new-api/relay/constant"

// IsResponsesCompact 判断当前请求是否属于任一 Responses Compact 协议。
// @return 属于 Compact 模式或旧版 Compact relay mode 时返回 true。
func (info *RelayInfo) IsResponsesCompact() bool {
	if info == nil {
		return false
	}
	return info.ResponsesCompactMode.IsCompact() || info.RelayMode == relayconstant.RelayModeResponsesCompact
}

// IsResponsesCompactV2 判断当前请求是否属于原生 Responses Compact V2。
// @return HTTP 或 WebSocket V2 模式返回 true。
func (info *RelayInfo) IsResponsesCompactV2() bool {
	return info != nil && info.ResponsesCompactMode.IsV2()
}

// UsesResponsesCompactEndpoint 判断当前请求上游是否应使用 /responses/compact。
// @return V1 path、历史 body bridge 或旧版 Compact relay mode 返回 true。
func (info *RelayInfo) UsesResponsesCompactEndpoint() bool {
	if info == nil {
		return false
	}
	return info.ResponsesCompactMode.UsesCompactEndpoint() || info.RelayMode == relayconstant.RelayModeResponsesCompact
}

// UsesUpstreamStream 判断当前渠道请求是否实际使用流式上游协议。
// @return 原生流式请求返回 true；历史 Compact SSE bridge 的 unary 上游返回 false。
func (info *RelayInfo) UsesUpstreamStream() bool {
	return info != nil && info.IsStream && !info.UsesResponsesCompactEndpoint()
}
