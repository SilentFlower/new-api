package relay

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyVisionAssistAfterModelMappingUsesFinalUpstreamModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	strip := true
	common.SetContextKey(c, constant.ContextKeyChannelId, 12)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "origin-model")
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, `{"origin-model":"middle-model","middle-model":"final-model"}`)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{
		VisionAssist: dto.ChannelVisionAssistSettings{
			Enabled:         true,
			AssistChannelId: 99,
			AssistModel:     "vision-model",
			TargetModels:    []string{"final-model"},
			StripImage:      &strip,
		},
	})

	request := &dto.GeneralOpenAIRequest{
		Model: "origin-model",
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "image_url", "image_url": "https://example.com/a.png"},
			},
		}},
	}
	info := relaycommon.GenRelayInfoOpenAI(c, request)
	info.RelayMode = relayconstant.RelayModeChatCompletions

	info.InitChannelMeta(c)
	require.NoError(t, helper.ModelMappedHelper(c, info, request))
	err := service.ApplyVisionAssist(c, info, func(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, assistRequest *dto.GeneralOpenAIRequest, images []service.VisionAssistImage) ([]service.VisionAssistResult, *types.NewAPIError) {
		assert.Equal(t, "final-model", relayInfo.UpstreamModelName)
		assert.Equal(t, "final-model", request.Model)
		return []service.VisionAssistResult{{Image: images[0], Text: "映射后的图片描述"}}, nil
	})

	require.Nil(t, err)
	assert.Contains(t, common.GetJsonString(info.Request), "映射后的图片描述")
	assert.NotContains(t, common.GetJsonString(info.Request), "image_url")
}
