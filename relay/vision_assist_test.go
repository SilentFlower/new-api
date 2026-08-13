package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestPreparationStateLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	assert.False(t, isRequestPreparationComplete(c))
	markRequestPreparationComplete(c)
	assert.True(t, isRequestPreparationComplete(c))
	ResetRequestPreparation(c)
	assert.False(t, isRequestPreparationComplete(c))
}

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

func TestShouldSkipVisionAssistPreprocessAllowsOpenAIResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAIResponses,
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	assert.False(t, shouldSkipVisionAssistPreprocess(c, info))

	info.RelayMode = relayconstant.RelayModeResponsesCompact
	assert.True(t, shouldSkipVisionAssistPreprocess(c, info))
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

func TestValidateVisionAssistEndpointModeRejectsGeminiNativeUnsupportedChannel(t *testing.T) {
	err := validateVisionAssistEndpointMode(service.VisionAssistEndpointModeGeminiNative, constant.ChannelTypeOpenAI)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "gemini_native")

	err = validateVisionAssistEndpointMode(service.VisionAssistEndpointModeGeminiNative, constant.ChannelTypeGemini)
	require.NoError(t, err)
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

func TestBuildVisionAssistGeminiRequestUsesCleanUserContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := &dto.GeneralOpenAIRequest{
		Model:     "gemini-2.5-flash",
		MaxTokens: common.GetPointer[uint](256),
		Messages: []dto.Message{
			{Role: "assistant", Content: "历史回复不应进入视觉辅助 Gemini 请求"},
			{
				Role: "user",
				Content: []dto.MediaContent{
					{Type: dto.ContentTypeText, Text: "描述图片"},
					{Type: dto.ContentTypeImageURL, ImageUrl: &dto.MessageImageUrl{
						Url:      "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=",
						Detail:   "low",
						MimeType: "image/png",
					}},
				},
			},
		},
	}

	geminiRequest, err := buildVisionAssistGeminiRequest(c, request)

	require.NoError(t, err)
	require.Len(t, geminiRequest.Contents, 1)
	assert.Equal(t, "user", geminiRequest.Contents[0].Role)
	require.Len(t, geminiRequest.Contents[0].Parts, 2)
	assert.Equal(t, "描述图片", geminiRequest.Contents[0].Parts[0].Text)
	require.NotNil(t, geminiRequest.Contents[0].Parts[1].InlineData)
	assert.Equal(t, "image/png", geminiRequest.Contents[0].Parts[1].InlineData.MimeType)
	assert.Equal(t, "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=", geminiRequest.Contents[0].Parts[1].InlineData.Data)

	body := common.GetJsonString(geminiRequest)
	assert.NotContains(t, body, "max_tokens")
	assert.NotContains(t, body, "stream_options")
	assert.NotContains(t, body, "历史回复")
}

func TestCallVisionAssistModelRejectsConcurrencyBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldRedisEnabled := common.RedisEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldFreeModelPreConsume := operation_setting.GetQuotaSetting().EnableFreeModelPreConsume
	savedModelRatios := ratio_setting.ModelRatio2JSONString()
	common.RedisEnabled = false
	common.MemoryCacheEnabled = true
	operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = false
	modelRatios, err := common.Marshal(map[string]float64{"vision-concurrency-model": 0})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(modelRatios)))
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = oldFreeModelPreConsume
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModelRatios))
	})

	baseURL := "https://assist.example.com"
	assistChannel := &model.Channel{
		Id:                   990,
		Type:                 constant.ChannelTypeOpenAI,
		Key:                  "assist-key",
		Status:               common.ChannelStatusEnabled,
		Name:                 "vision-assist-concurrency",
		BaseURL:              &baseURL,
		Models:               "vision-concurrency-model",
		Group:                "default",
		UserConcurrencyLimit: common.GetPointer(1),
	}
	assistChannel.SetSetting(dto.ChannelSettings{})
	model.CacheUpdateChannel(assistChannel)

	lease, err := service.AcquireChannelUserConcurrency(context.Background(), assistChannel.Id, 334, 1, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, lease.Release(context.Background()))
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyUserId, 334)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	parent := &relaycommon.RelayInfo{
		UserId:          334,
		UserQuota:       100000,
		UsingGroup:      "default",
		UserGroup:       "default",
		RequestHeaders:  map[string]string{"Content-Type": "application/json"},
		OriginModelName: "target-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{
				VisionAssist: dto.ChannelVisionAssistSettings{
					AssistChannelId: assistChannel.Id,
					AssistModel:     "vision-concurrency-model",
				},
			},
		},
	}
	request := &dto.GeneralOpenAIRequest{
		Model: "vision-concurrency-model",
		Messages: []dto.Message{{
			Role:    "user",
			Content: "描述图片",
		}},
	}

	results, apiErr := callVisionAssistModel(c, parent, request, nil)

	require.Nil(t, results)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeChannelUserConcurrencyExceeded, apiErr.GetErrorCode())
}
