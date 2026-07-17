package relay

import (
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/tidwall/gjson"
)

// syncAnthropicReasoningEffort 从 Anthropic output_config 同步日志字段。
func syncAnthropicReasoningEffort(info *relaycommon.RelayInfo, outputConfig []byte) {
	if info == nil || info.ChannelMeta == nil || info.ChannelType != constant.ChannelTypeAnthropic {
		return
	}

	effort := gjson.GetBytes(outputConfig, "effort")
	if effort.Type != gjson.String {
		info.ReasoningEffort = ""
		return
	}
	info.ReasoningEffort = effort.String()
}

// syncAnthropicReasoningEffortFromRequestBody 从最终上游请求体同步参数覆盖后的日志字段。
func syncAnthropicReasoningEffortFromRequestBody(info *relaycommon.RelayInfo, requestBody []byte) {
	outputConfig := gjson.GetBytes(requestBody, "output_config")
	syncAnthropicReasoningEffort(info, []byte(outputConfig.Raw))
}
