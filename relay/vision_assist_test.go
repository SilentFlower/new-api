package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/openai"
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

func TestResolveVisionAssistEndpointMode(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		channel    int
		model      string
		expected   string
	}{
		{
			name:     "Gemini 渠道默认原生 Gemini",
			channel:  constant.ChannelTypeGemini,
			model:    "gemini-2.5-flash",
			expected: service.VisionAssistEndpointModeGeminiNative,
		},
		{
			name:     "Vertex Gemini 默认原生 Gemini",
			channel:  constant.ChannelTypeVertexAi,
			model:    "gemini-2.5-flash",
			expected: service.VisionAssistEndpointModeGeminiNative,
		},
		{
			name:     "Vertex Claude 默认 Anthropic Messages",
			channel:  constant.ChannelTypeVertexAi,
			model:    "claude-sonnet-4-5",
			expected: service.VisionAssistEndpointModeAnthropicMessages,
		},
		{
			name:     "Anthropic 默认 Anthropic Messages",
			channel:  constant.ChannelTypeAnthropic,
			model:    "claude-sonnet-4-5",
			expected: service.VisionAssistEndpointModeAnthropicMessages,
		},
		{
			name:     "AWS Claude 默认 Anthropic Messages",
			channel:  constant.ChannelTypeAws,
			model:    "anthropic.claude-sonnet-4-5",
			expected: service.VisionAssistEndpointModeAnthropicMessages,
		},
		{
			name:     "其他渠道默认 OpenAI Chat",
			channel:  constant.ChannelTypeOpenAI,
			model:    "gpt-4o-mini",
			expected: service.VisionAssistEndpointModeOpenAIChat,
		},
		{
			name:       "显式 Responses 覆盖 auto",
			configured: service.VisionAssistEndpointModeOpenAIResponses,
			channel:    constant.ChannelTypeGemini,
			model:      "gemini-2.5-flash",
			expected:   service.VisionAssistEndpointModeOpenAIResponses,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := resolveVisionAssistEndpointMode(tt.configured, tt.channel, tt.model)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestBuildVisionAssistRelayInfoInitializesAssistChannelMeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages?beta=true", nil)
	baseURL := "https://assist.example.com"
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "vision-model")
	common.SetContextKey(c, constant.ContextKeyChannelId, 99)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, baseURL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "assist-key")
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})

	parent := &relaycommon.RelayInfo{
		RequestId:       "parent-request",
		RequestHeaders:  map[string]string{"Content-Type": "application/json"},
		OriginModelName: "target-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "target-model",
			ChannelSetting: dto.ChannelSettings{
				VisionAssist: dto.ChannelVisionAssistSettings{
					AssistModel: "vision-model",
				},
			},
		},
	}
	request := &dto.GeneralOpenAIRequest{Model: "vision-model"}

	assistInfo := buildVisionAssistRelayInfo(c, parent, request, service.VisionAssistEndpointModeOpenAIChat)
	require.NotNil(t, assistInfo.ChannelMeta)
	assert.Equal(t, baseURL, assistInfo.ChannelBaseUrl)
	assert.Equal(t, "vision-model", assistInfo.OriginModelName)

	adaptor := &openai.Adaptor{}
	adaptor.Init(assistInfo)
	requestURL, err := adaptor.GetRequestURL(assistInfo)
	require.NoError(t, err)
	assert.Equal(t, "https://assist.example.com/v1/chat/completions", requestURL)
}
