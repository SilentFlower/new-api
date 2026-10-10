package relay

import (
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/tidwall/gjson"
)

// syncAdvancedChannelReasoningEffortFromBody 记录高级渠道最终出站正文中的明确强度。
func syncAdvancedChannelReasoningEffortFromBody(info *relaycommon.RelayInfo, body []byte) {
	if info == nil || info.ChannelMeta == nil || info.ChannelType != constant.ChannelTypeAdvancedCustom {
		return
	}

	// 每次尝试均按最终协议重新取值，删除参数不能沿用适配器或上一渠道的旧值。
	info.ReasoningEffort = ""
	var effort gjson.Result
	switch info.GetFinalRequestRelayFormat() {
	case types.RelayFormatOpenAI:
		effort = gjson.GetBytes(body, "reasoning_effort")
		if effort.Type == gjson.Null || (effort.Type == gjson.String && effort.String() == "") {
			effort = gjson.GetBytes(body, "reasoning.effort")
		}
	case types.RelayFormatOpenAIResponses:
		effort = gjson.GetBytes(body, "reasoning.effort")
	case types.RelayFormatClaude:
		effort = gjson.GetBytes(body, "output_config.effort")
	case types.RelayFormatGemini:
		config := gjson.GetBytes(body, "generationConfig.thinkingConfig")
		if snake := gjson.GetBytes(body, "generationConfig.thinking_config"); snake.Type == gjson.JSON {
			config = snake
		}
		effort = config.Get("thinkingLevel")
		if snake := config.Get("thinking_level"); snake.Type == gjson.String && snake.String() != "" {
			effort = snake
		}
	}
	if effort.Type == gjson.String {
		info.ReasoningEffort = effort.String()
	}
}

// syncAdvancedChannelReasoningEffortFromRequest 复用原入站 DTO，避免日志处理重复读取透传正文。
func syncAdvancedChannelReasoningEffortFromRequest(info *relaycommon.RelayInfo, request dto.Request) {
	if info == nil || info.ChannelMeta == nil || info.ChannelType != constant.ChannelTypeAdvancedCustom {
		return
	}
	info.ReasoningEffort = ""
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		if req != nil {
			info.ReasoningEffort = req.ReasoningEffort
			if req.ReasoningEffort == "" {
				effort := gjson.GetBytes(req.Reasoning, "effort")
				if effort.Type == gjson.String {
					info.ReasoningEffort = effort.String()
				}
			}
		}
	case *dto.OpenAIResponsesRequest:
		if req != nil && req.Reasoning != nil {
			info.ReasoningEffort = req.Reasoning.Effort
		}
	case *dto.ClaudeRequest:
		if req != nil {
			effort := gjson.GetBytes(req.OutputConfig, "effort")
			if effort.Type == gjson.String {
				info.ReasoningEffort = effort.String()
			}
		}
	case *dto.GeminiChatRequest:
		if req != nil && req.GenerationConfig.ThinkingConfig != nil {
			info.ReasoningEffort = req.GenerationConfig.ThinkingConfig.ThinkingLevel
		}
	}
}
