package helper

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

// ShouldSupplementResponsesReasoningHeader 判断所选渠道是否声明原生 Responses 用量包含历史推理。
// @param info 已初始化渠道信息的当前请求。
// @return 开关已开启且请求为普通原生 Responses 时返回 true。
func ShouldSupplementResponsesReasoningHeader(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil || !info.ChannelSetting.ResponsesReasoningIncludedEnabled ||
		info.RelayMode != relayconstant.RelayModeResponses || info.IsResponsesCompact() ||
		info.RelayFormat != types.RelayFormatOpenAIResponses || info.GetFinalRequestRelayFormat() != types.RelayFormatOpenAIResponses {
		return false
	}
	switch info.ApiType {
	case constant.APITypeOpenAI, constant.APITypeCodex:
		return true
	case constant.APITypeAdvancedCustom:
		config := info.ChannelOtherSettings.AdvancedCustom
		if config == nil {
			return false
		}
		route, ok := config.MatchPathForModel(info.RequestURLPath, info.RoutingModel())
		converter := strings.TrimSpace(route.Converter)
		return ok && (converter == "" || converter == "none")
	default:
		return false
	}
}

// PrepareResponsesReasoningHeader 停止可能提前提交 HTTP 响应头的非流式保活。
// @param c 当前请求上下文。
// @param info 已初始化渠道信息的当前请求。
// @return 无返回值；未开启或不支持的请求保持原有保活行为。
func PrepareResponsesReasoningHeader(c *gin.Context, info *relaycommon.RelayInfo) {
	if c == nil || c.Writer == nil || !ShouldSupplementResponsesReasoningHeader(info) {
		return
	}
	// 非流式保活 writer 的 Header 会阻止后续心跳；必须等上游标识返回后再提交响应头。
	_ = c.Writer.Header()
}

// SupplementResponsesReasoningHeader 在成功响应缺少标识时补充历史推理已计入用量的响应头。
// @param c 当前请求上下文。
// @param info 已初始化渠道信息的当前请求。
// @param resp 上游成功响应；明确返回的 true、false 或其他值均原样保留。
// @return 无返回值；只补充响应头，不修改正文、usage 或计费。
func SupplementResponsesReasoningHeader(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) {
	if c == nil || c.Writer == nil || resp == nil || resp.StatusCode != http.StatusOK ||
		!ShouldSupplementResponsesReasoningHeader(info) || c.Writer.Written() {
		return
	}
	for name := range resp.Header {
		if strings.EqualFold(name, "X-Reasoning-Included") {
			return
		}
	}
	if resp.Header == nil {
		resp.Header = make(http.Header)
	}
	resp.Header.Set("X-Reasoning-Included", "true")
}
