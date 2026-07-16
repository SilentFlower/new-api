package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelMappedHelperResponsesCompactV2KeepsSuffixOutOfUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("model_mapping", `{"gpt-5":"gpt-5.1"}`)
	request := &dto.OpenAIResponsesRequest{Model: "gpt-5"}
	info := &relaycommon.RelayInfo{
		RelayMode:            relayconstant.RelayModeResponses,
		ResponsesCompactMode: relayconstant.ResponsesCompactModeV2HTTP,
		OriginModelName:      ratio_setting.WithCompactModelSuffix("gpt-5"),
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: ratio_setting.WithCompactModelSuffix("gpt-5"),
		},
	}

	require.NoError(t, ModelMappedHelper(c, info, request))
	assert.Equal(t, "gpt-5.1", info.UpstreamModelName)
	assert.Equal(t, ratio_setting.WithCompactModelSuffix("gpt-5.1"), info.OriginModelName)
	assert.Equal(t, "gpt-5.1", request.Model)
	assert.True(t, info.IsModelMapped)
}

func TestModelMappedHelperResponsesCompactSelfMappingIsNotMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("model_mapping", `{"gpt-5":"gpt-5"}`)
	request := &dto.OpenAIResponsesRequest{Model: "gpt-5"}
	info := &relaycommon.RelayInfo{
		RelayMode:            relayconstant.RelayModeResponses,
		ResponsesCompactMode: relayconstant.ResponsesCompactModeV2HTTP,
		OriginModelName:      ratio_setting.WithCompactModelSuffix("gpt-5"),
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: ratio_setting.WithCompactModelSuffix("gpt-5"),
		},
	}

	require.NoError(t, ModelMappedHelper(c, info, request))
	assert.Equal(t, "gpt-5", info.UpstreamModelName)
	assert.Equal(t, ratio_setting.WithCompactModelSuffix("gpt-5"), info.OriginModelName)
	assert.Equal(t, "gpt-5", request.Model)
	assert.False(t, info.IsModelMapped)
}
