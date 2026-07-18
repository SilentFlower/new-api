package service

import (
	"net/url"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

const responsesCompactAuditKey = "responses_compact"

// ClearResponsesCompactAudit 清除当前上下文中上一轮 Responses Compact 管理员审计标记。
// 仅用于同一 WebSocket 连接开始新 turn 时，保留其他请求级日志字段。
// @param ctx 当前 Gin 请求上下文。
func ClearResponsesCompactAudit(ctx *gin.Context) {
	if ctx == nil {
		return
	}
	logOther, ok := common.GetContextKeyType[map[string]interface{}](ctx, constant.ContextKeyLogOther)
	if !ok || logOther == nil {
		return
	}
	adminInfo, ok := logOther["admin_info"].(map[string]interface{})
	if !ok || adminInfo == nil {
		return
	}
	delete(adminInfo, responsesCompactAuditKey)
	if len(adminInfo) == 0 {
		delete(logOther, "admin_info")
	}
}

// SetResponsesCompactAudit 更新 Responses Compact 请求的管理员审计信息。
// 仅记录协议模式、路径、渠道、结局和上游请求 ID，不记录请求帧、query 或凭证。
// @param ctx 当前 Gin 请求上下文。
// @param relayInfo 当前 Relay 请求信息。
// @param outcome 当前请求结局；空值不会覆盖已有结局。
func SetResponsesCompactAudit(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, outcome string) {
	if ctx == nil || relayInfo == nil || !relayInfo.IsResponsesCompact() {
		return
	}
	logOther, _ := common.GetContextKeyType[map[string]interface{}](ctx, constant.ContextKeyLogOther)
	if logOther == nil {
		logOther = map[string]interface{}{}
	}
	adminInfo, _ := logOther["admin_info"].(map[string]interface{})
	if adminInfo == nil {
		adminInfo = map[string]interface{}{}
		logOther["admin_info"] = adminInfo
	}
	audit, _ := adminInfo[responsesCompactAuditKey].(map[string]interface{})
	if audit == nil {
		audit = map[string]interface{}{}
		adminInfo[responsesCompactAuditKey] = audit
	}

	audit["mode"] = string(relayInfo.ResponsesCompactMode)
	if ctx.Request != nil && ctx.Request.URL != nil {
		audit["inbound_path"] = ctx.Request.URL.EscapedPath()
	} else if path := responsesCompactAuditPath(relayInfo.RequestURLPath); path != "" {
		audit["inbound_path"] = path
	}
	if path := responsesCompactAuditPath(relayInfo.UpstreamRequestURLPath); path != "" {
		audit["upstream_path"] = path
	}
	if relayInfo.ChannelMeta != nil {
		audit["channel_id"] = relayInfo.ChannelId
		audit["channel_type"] = relayInfo.ChannelType
	}
	if channelName := common.GetContextKeyString(ctx, constant.ContextKeyChannelName); channelName != "" {
		audit["channel_name"] = channelName
	}
	if outcome != "" {
		audit["outcome"] = outcome
	}
	if upstreamRequestID := ctx.GetString(common.UpstreamRequestIdKey); upstreamRequestID != "" {
		audit["upstream_request_id"] = upstreamRequestID
	}
	common.SetContextKey(ctx, constant.ContextKeyLogOther, logOther)
}

func responsesCompactAuditPath(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsedURL.EscapedPath()
}
