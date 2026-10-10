package controller

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
)

// TestResetMainRelayAttemptReasoningEffort 验证换渠道时不继承旧思考强度或最终协议。
// @param t 测试上下文。
func TestResetMainRelayAttemptReasoningEffort(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI, ReasoningEffort: "high",
		FinalRequestRelayFormat: types.RelayFormatClaude,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}
	resetMainRelayAttemptFields(info, "deepseek-flash")
	assert.Empty(t, info.ReasoningEffort)
	assert.Empty(t, info.FinalRequestRelayFormat)
	assert.Equal(t, []types.RelayFormat{types.RelayFormatOpenAI}, info.RequestConversionChain)
	assert.Equal(t, types.RelayFormatOpenAI, info.GetFinalRequestRelayFormat())
	info.AppendRequestConversion(types.RelayFormatOpenAIResponses)
	assert.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}
