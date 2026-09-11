package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResponsesRetryPreservesOriginalParameters 验证首次转换不能污染重试所需的原始 DTO。
// @param t 测试上下文。
func TestResponsesRetryPreservesOriginalParameters(t *testing.T) {
	var request dto.OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"alias-model","input":"原始输入","thinking_budget":0,"temperature":0,"stream":false}`), &request))
	original, err := cloneRelayRequest(&request)
	require.NoError(t, err)
	for _, upstreamModel := range []string{"gpt-4o", "qwen3-32b"} {
		attempt, err := cloneRelayRequest(original)
		require.NoError(t, err)
		responses := attempt.(*dto.OpenAIResponsesRequest)
		c, _ := gin.CreateTestContext(nil)
		mapping, err := common.Marshal(map[string]string{"alias-model": upstreamModel})
		require.NoError(t, err)
		c.Set("model_mapping", string(mapping))
		info := &relaycommon.RelayInfo{OriginModelName: "alias-model", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "alias-model"}}
		require.NoError(t, helper.ModelMappedHelper(c, info, responses))
		encoded, err := common.Marshal(responses)
		require.NoError(t, err)
		var sent map[string]any
		require.NoError(t, common.Unmarshal(encoded, &sent))
		assert.Equal(t, upstreamModel, sent["model"])
		assert.Equal(t, float64(0), sent["temperature"])
		assert.Equal(t, false, sent["stream"])
		if upstreamModel == "qwen3-32b" {
			assert.Equal(t, float64(0), sent["thinking_budget"])
		} else {
			assert.NotContains(t, sent, "thinking_budget")
		}
		// 转换器对切片和指针的修改也必须隔离在本次尝试内。
		responses.ThinkingBudget[0] = '9'
		responses.Input[1] = 'X'
		*responses.Temperature = 1
		*responses.Stream = true
	}
	preserved := original.(*dto.OpenAIResponsesRequest)
	assert.Equal(t, "alias-model", preserved.Model)
	assert.Equal(t, request.Input, preserved.Input)
	assert.Equal(t, "0", string(preserved.ThinkingBudget))
	assert.Equal(t, float64(0), *preserved.Temperature)
	assert.False(t, *preserved.Stream)
}
