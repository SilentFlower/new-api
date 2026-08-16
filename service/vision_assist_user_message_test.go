package service

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractVisionAssistUserMessage(t *testing.T) {
	responsesInput, err := common.Marshal([]any{
		map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "这个人物是谁？"},
			},
		},
		map[string]any{
			"type": "function_call_output",
			"output": []any{
				map[string]any{"type": "input_text", "text": "函数工具文本"},
			},
		},
		map[string]any{
			"type": "custom_tool_call_output",
			"output": []any{
				map[string]any{"type": "input_text", "text": "自定义工具文本"},
				map[string]any{"type": "input_image", "image_url": "data:image/png;base64,abc"},
			},
		},
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		request  dto.Request
		expected string
	}{
		{
			name: "OpenAI Chat 合并最新用户消息中的文本块",
			request: &dto.GeneralOpenAIRequest{Messages: []dto.Message{
				{Role: "user", Content: "旧问题"},
				{Role: "assistant", Content: "旧回答"},
				{Role: "user", Content: []any{
					map[string]any{"type": "text", "text": "这个人物"},
					map[string]any{"type": "image_url", "image_url": "data:image/png;base64,abc"},
					map[string]any{"type": "text", "text": "是谁？"},
				}},
			}},
			expected: "这个人物\n是谁？",
		},
		{
			name: "OpenAI Chat 图片消息无文本时回溯",
			request: &dto.GeneralOpenAIRequest{Messages: []dto.Message{
				{Role: "user", Content: "请识别人物"},
				{Role: "assistant", Content: "请提供图片"},
				{Role: "user", Content: []any{
					map[string]any{"type": "image_url", "image_url": "data:image/png;base64,abc"},
				}},
			}},
			expected: "请识别人物",
		},
		{
			name: "Claude 合并多模态用户文本",
			request: &dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
				{Role: "user", Content: []dto.ClaudeMediaMessage{
					{Type: dto.ContentTypeText, Text: common.GetPointer("请判断")},
					{Type: "image", Source: &dto.ClaudeMessageSource{Type: "base64", Data: "abc"}},
					{Type: dto.ContentTypeText, Text: common.GetPointer("人物身份")},
				}},
			}},
			expected: "请判断\n人物身份",
		},
		{
			name: "Claude 读取字符串用户文本",
			request: &dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
				{Role: "assistant", Content: "旧回答"},
				{Role: "user", Content: "这个人物是谁？"},
			}},
			expected: "这个人物是谁？",
		},
		{
			name: "Responses 忽略工具输出文本",
			request: &dto.OpenAIResponsesRequest{
				Input: responsesInput,
			},
			expected: "这个人物是谁？",
		},
		{
			name: "没有用户文本时返回空字符串",
			request: &dto.GeneralOpenAIRequest{Messages: []dto.Message{
				{Role: "assistant", Content: "不是用户问题"},
				{Role: "user", Content: []any{
					map[string]any{"type": "image_url", "image_url": "data:image/png;base64,abc"},
				}},
			}},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractVisionAssistUserMessage(tt.request))
		})
	}
}

func TestResolveVisionAssistUserMessagesKeepsHistoricalIntent(t *testing.T) {
	responsesInput, err := common.Marshal([]any{
		map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "第一张图是谁？"},
				map[string]any{"type": "input_image", "image_url": "data:image/png;base64,first"},
			},
		},
		map[string]any{"type": "message", "role": "assistant", "content": "第一轮回答"},
		map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "这个呢？"},
				map[string]any{"type": "input_image", "image_url": "data:image/png;base64,second"},
			},
		},
	})
	require.NoError(t, err)
	images := []VisionAssistImage{{MessageIndex: 0}, {MessageIndex: 2}}
	tests := []struct {
		name    string
		request dto.Request
	}{
		{
			name: "OpenAI Chat",
			request: &dto.GeneralOpenAIRequest{Messages: []dto.Message{
				{Role: "user", Content: []any{
					map[string]any{"type": "text", "text": "第一张图是谁？"},
					map[string]any{"type": "image_url", "image_url": "data:image/png;base64,first"},
				}},
				{Role: "assistant", Content: "第一轮回答"},
				{Role: "user", Content: []any{
					map[string]any{"type": "text", "text": "这个呢？"},
					map[string]any{"type": "image_url", "image_url": "data:image/png;base64,second"},
				}},
			}},
		},
		{
			name:    "OpenAI Responses",
			request: &dto.OpenAIResponsesRequest{Input: responsesInput},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, map[int]string{0: "第一张图是谁？", 2: "这个呢？"}, resolveVisionAssistUserMessages(tt.request, images))
		})
	}
}

func TestResolveVisionAssistUserMessagesDoesNotReuseOlderQuestionForNewImage(t *testing.T) {
	request := &dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
		{Role: "user", Content: "第一张图是谁？"},
		{Role: "assistant", Content: "第一轮回答"},
		{
			Role: "user",
			Content: []dto.ClaudeMediaMessage{{
				Type:   "image",
				Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: "image/png", Data: "second-image"},
			}},
		},
	}}

	assert.Empty(t, resolveVisionAssistUserMessages(request, []VisionAssistImage{{MessageIndex: 2}}))
}

func TestResolveVisionAssistUserMessagesKeepsOriginalQuestionOnTextOnlyFollowUp(t *testing.T) {
	request := &dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
		{
			Role: "user",
			Content: []dto.ClaudeMediaMessage{
				{Type: dto.ContentTypeText, Text: common.GetPointer("最初的图片问题")},
				{Type: "image", Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: "image/png", Data: "first-image"}},
			},
		},
		{Role: "assistant", Content: "第一轮回答"},
		{Role: "user", Content: "图片里的最后一句话是什么？"},
	}}

	assert.Equal(t, map[int]string{0: "最初的图片问题"}, resolveVisionAssistUserMessages(request, []VisionAssistImage{{MessageIndex: 0}}))

	request.Messages[0].Content = []dto.ClaudeMediaMessage{
		{Type: "image", Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: "image/png", Data: "first-image"}},
	}
	assert.Empty(t, resolveVisionAssistUserMessages(request, []VisionAssistImage{{MessageIndex: 0}}))
}

func TestBuildVisionAssistRequestIncludesUserMessage(t *testing.T) {
	setting := dto.ChannelVisionAssistSettings{AssistModel: "vision-model"}
	request := buildVisionAssistRequest(setting, "识图规则", "  这个人物是谁？  ", []VisionAssistImage{{
		Index:    1,
		Source:   &testFileSource{raw: "data:image/png;base64,abc"},
		Detail:   "high",
		MimeType: "image/png",
	}})

	require.Len(t, request.Messages, 1)
	assert.Equal(t, "user", request.Messages[0].Role)
	contents := request.Messages[0].ParseContent()
	require.Len(t, contents, 4)
	assert.Equal(t, "识图规则", contents[0].Text)
	assert.Contains(t, contents[1].Text, visionAssistUserMessageInstruction)
	assert.Contains(t, contents[1].Text, "这个人物是谁？")
	assert.NotContains(t, contents[1].Text, "  这个人物是谁？  ")
	assert.Equal(t, "图片 1：", contents[2].Text)
	assert.Equal(t, dto.ContentTypeImageURL, contents[3].Type)
}

func TestNormalizedVisionAssistPromptUsesUserIntent(t *testing.T) {
	prompt := normalizedVisionAssistPrompt(dto.ChannelVisionAssistSettings{})

	assert.Contains(t, prompt, "结合用户原始问题")
	assert.Contains(t, prompt, "如未提供用户原始问题")
	assert.Contains(t, prompt, "完整客观描述图片")
	assert.Contains(t, prompt, "人物身份")
	assert.Contains(t, prompt, "保留不确定性")
}

func TestBuildVisionAssistRequestWithoutUserMessageKeepsGenericFallback(t *testing.T) {
	setting := dto.ChannelVisionAssistSettings{AssistModel: "vision-model"}
	request := buildVisionAssistRequest(setting, normalizedVisionAssistPrompt(setting), "", []VisionAssistImage{{
		Index:    1,
		Source:   &testFileSource{raw: "data:image/png;base64,abc"},
		MimeType: "image/png",
	}})

	require.Len(t, request.Messages, 1)
	contents := request.Messages[0].ParseContent()
	require.Len(t, contents, 3)
	assert.Contains(t, contents[0].Text, "如未提供用户原始问题")
	assert.Contains(t, contents[0].Text, "完整客观描述图片")
	assert.Equal(t, "图片 1：", contents[1].Text)
	assert.Equal(t, dto.ContentTypeImageURL, contents[2].Type)
}

func TestApplyVisionAssistReusesInitialImageCacheForTextOnlyFollowUps(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, getVisionAssistCache().Purge())
	t.Cleanup(func() {
		assert.NoError(t, getVisionAssistCache().Purge())
	})
	strip := true
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:         true,
		AssistChannelId: 27,
		AssistModel:     "vision-text-follow-up-cache-test",
		StripImage:      &strip,
		MultiImageMode:  VisionAssistMultiImageModeCombined,
	}}
	callCount := 0
	seenUserMessages := make([]string, 0, 1)
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		callCount++
		require.Len(t, images, 2)
		contents := request.Messages[0].ParseContent()
		require.Len(t, contents, 7)
		seenUserMessages = append(seenUserMessages, contents[1].Text)
		return []VisionAssistResult{{Image: images[0], Text: "首次识图结果"}}, nil
	}
	apply := func(followUp string) (*gin.Context, *dto.ClaudeRequest) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		request := &dto.ClaudeRequest{
			Model: "target",
			Messages: []dto.ClaudeMessage{{
				Role: "user",
				Content: []dto.ClaudeMediaMessage{
					{Type: dto.ContentTypeText, Text: common.GetPointer("请完整描述图片内容")},
					{Type: "image", Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: "image/png", Data: "first-image"}},
					{Type: "image", Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: "image/png", Data: "second-image"}},
				},
			}},
		}
		if followUp != "" {
			request.Messages = append(request.Messages,
				dto.ClaudeMessage{Role: "assistant", Content: "第一轮回答"},
				dto.ClaudeMessage{Role: "user", Content: followUp},
			)
		}
		info := &relaycommon.RelayInfo{
			Request:         request,
			OriginModelName: "target",
			ChannelMeta: &relaycommon.ChannelMeta{
				ChannelId:         3,
				UpstreamModelName: "target",
				ChannelSetting:    setting,
			},
		}

		require.Nil(t, ApplyVisionAssist(c, info, caller))
		return c, request
	}

	_, first := apply("")
	secondContext, second := apply("第二个图片的最后一句话是什么？")
	thirdContext, third := apply("第一个提示信息是什么？")

	assert.Equal(t, 1, callCount)
	require.Len(t, seenUserMessages, 1)
	assert.Contains(t, seenUserMessages[0], "请完整描述图片内容")
	assert.Equal(t, "第二个图片的最后一句话是什么？", second.Messages[2].GetStringContent())
	assert.Equal(t, "第一个提示信息是什么？", third.Messages[2].GetStringContent())
	for _, request := range []*dto.ClaudeRequest{first, second, third} {
		assert.Contains(t, common.GetJsonString(request.Messages[0].Content), "[图片相关信息]")
		assert.NotContains(t, common.GetJsonString(request.Messages[0].Content), "first-image")
		assert.NotContains(t, common.GetJsonString(request.Messages[0].Content), "second-image")
	}
	secondLogOther, ok := common.GetContextKeyType[map[string]interface{}](secondContext, constant.ContextKeyLogOther)
	require.True(t, ok)
	assert.Equal(t, 2, secondLogOther["vision_assist_cache_hits"])
	thirdLogOther, ok := common.GetContextKeyType[map[string]interface{}](thirdContext, constant.ContextKeyLogOther)
	require.True(t, ok)
	assert.Equal(t, 2, thirdLogOther["vision_assist_cache_hits"])
}

func TestApplyVisionAssistDoesNotReidentifyHistoricalClaudeImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, getVisionAssistCache().Purge())
	t.Cleanup(func() {
		assert.NoError(t, getVisionAssistCache().Purge())
	})
	strip := true
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:         true,
		AssistChannelId: 32,
		AssistModel:     "vision-history-cache-test",
		StripImage:      &strip,
		MultiImageMode:  VisionAssistMultiImageModeCombined,
	}}
	callCount := 0
	seenSources := make([]string, 0, 2)
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		callCount++
		require.Len(t, images, 1)
		source := images[0].Source.GetRawData()
		seenSources = append(seenSources, source)
		contents := request.Messages[0].ParseContent()
		resultText := "第一张图片信息"
		if source == "second-image" {
			resultText = "第二张图片信息"
			require.Len(t, contents, 4)
			assert.Contains(t, contents[1].Text, "这个呢？")
		} else {
			require.Len(t, contents, 3)
		}
		return []VisionAssistResult{{Image: images[0], Text: resultText}}, nil
	}
	apply := func(request *dto.ClaudeRequest) (*gin.Context, *dto.ClaudeRequest) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		info := &relaycommon.RelayInfo{
			Request:         request,
			OriginModelName: "target",
			ChannelMeta: &relaycommon.ChannelMeta{
				ChannelId:         3,
				UpstreamModelName: "target",
				ChannelSetting:    setting,
			},
		}
		require.Nil(t, ApplyVisionAssist(c, info, caller))
		return c, request
	}

	apply(&dto.ClaudeRequest{Messages: []dto.ClaudeMessage{{
		Role: "user",
		Content: []dto.ClaudeMediaMessage{{
			Type:   "image",
			Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: "image/png", Data: "first-image"},
		}},
	}}})
	secondContext, secondRequest := apply(&dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
		{
			Role: "user",
			Content: []dto.ClaudeMediaMessage{{
				Type:   "image",
				Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: "image/png", Data: "first-image"},
			}},
		},
		{Role: "assistant", Content: "第一轮回答"},
		{
			Role: "user",
			Content: []dto.ClaudeMediaMessage{
				{Type: dto.ContentTypeText, Text: common.GetPointer("这个呢？")},
				{Type: "image", Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: "image/png", Data: "second-image"}},
			},
		},
	}})

	assert.Equal(t, 2, callCount)
	assert.Equal(t, []string{"first-image", "second-image"}, seenSources)
	assert.Contains(t, common.GetJsonString(secondRequest.Messages[0].Content), "第一张图片信息")
	assert.Contains(t, common.GetJsonString(secondRequest.Messages[2].Content), "这个呢？")
	assert.Contains(t, common.GetJsonString(secondRequest.Messages[2].Content), "第二张图片信息")
	logOther, ok := common.GetContextKeyType[map[string]interface{}](secondContext, constant.ContextKeyLogOther)
	require.True(t, ok)
	assert.Equal(t, 1, logOther["vision_assist_cache_hits"])
	assert.Equal(t, 2, logOther["vision_assist_image_count"])
}

func TestApplyVisionAssistUsesDescriptionForMultipleImagesSeparatelyByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, getVisionAssistCache().Purge())
	t.Cleanup(func() {
		assert.NoError(t, getVisionAssistCache().Purge())
	})
	strip := true
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:         true,
		AssistChannelId: 28,
		AssistModel:     "vision-user-message-multiple-images-test",
		StripImage:      &strip,
	}}
	request := &dto.GeneralOpenAIRequest{
		Model: "target",
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "对比这两张图中的人物"},
				map[string]any{"type": "image_url", "image_url": "data:image/png;base64,image-one"},
				map[string]any{"type": "image_url", "image_url": "data:image/png;base64,image-two"},
			},
		}},
	}
	info := &relaycommon.RelayInfo{
		Request:         request,
		OriginModelName: "target",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         3,
			UpstreamModelName: "target",
			ChannelSetting:    setting,
		},
	}
	callCount := 0
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, assistRequest *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		callCount++
		require.Len(t, images, 1)
		contents := assistRequest.Messages[0].ParseContent()
		require.Len(t, contents, 4)
		assert.Contains(t, contents[1].Text, "对比这两张图中的人物")
		assert.Equal(t, dto.ContentTypeImageURL, contents[3].Type)

		resultText := "第一张图的人物信息"
		if images[0].Index == 2 {
			resultText = "第二张图的人物信息"
		}
		return []VisionAssistResult{{Image: images[0], Text: resultText}}, nil
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	apiErr := ApplyVisionAssist(c, info, caller)

	require.Nil(t, apiErr)
	assert.Equal(t, 2, callCount)
	contents := request.Messages[0].ParseContent()
	require.Len(t, contents, 2)
	assert.Equal(t, "对比这两张图中的人物", contents[0].Text)
	assert.Contains(t, contents[1].Text, "图片 1：第一张图的人物信息")
	assert.Contains(t, contents[1].Text, "图片 2：第二张图的人物信息")
	for _, content := range contents {
		assert.NotEqual(t, dto.ContentTypeImageURL, content.Type)
	}
}

func TestApplyVisionAssistCombinesImagesFromSameMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, getVisionAssistCache().Purge())
	t.Cleanup(func() {
		assert.NoError(t, getVisionAssistCache().Purge())
	})
	strip := true
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:         true,
		AssistChannelId: 29,
		AssistModel:     "vision-user-message-combined-images-test",
		StripImage:      &strip,
		MultiImageMode:  VisionAssistMultiImageModeCombined,
	}}
	request := &dto.GeneralOpenAIRequest{
		Model: "target",
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "对比这两张图中的人物"},
				map[string]any{"type": "image_url", "image_url": "data:image/png;base64,image-one"},
				map[string]any{"type": "image_url", "image_url": "data:image/png;base64,image-two"},
			},
		}},
	}
	info := &relaycommon.RelayInfo{
		Request:         request,
		OriginModelName: "target",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         3,
			UpstreamModelName: "target",
			ChannelSetting:    setting,
		},
	}
	callCount := 0
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, assistRequest *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		callCount++
		require.Len(t, images, 2)
		contents := assistRequest.Messages[0].ParseContent()
		require.Len(t, contents, 7)
		assert.Contains(t, contents[1].Text, "对比这两张图中的人物")
		assert.Equal(t, visionAssistMultiImageInstruction, contents[2].Text)
		assert.Equal(t, "图片 1：", contents[3].Text)
		assert.Equal(t, dto.ContentTypeImageURL, contents[4].Type)
		assert.Equal(t, "图片 2：", contents[5].Text)
		assert.Equal(t, dto.ContentTypeImageURL, contents[6].Type)
		return []VisionAssistResult{{Image: images[0], Text: "第一张更高，第二张穿蓝色衣服"}}, nil
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	apiErr := ApplyVisionAssist(c, info, caller)

	require.Nil(t, apiErr)
	assert.Equal(t, 1, callCount)
	contents := request.Messages[0].ParseContent()
	require.Len(t, contents, 2)
	assert.Equal(t, "对比这两张图中的人物", contents[0].Text)
	assert.Contains(t, contents[1].Text, "多图综合信息：第一张更高，第二张穿蓝色衣服")
	assert.NotContains(t, contents[1].Text, "图片 1：第一张更高")
	for _, content := range contents {
		assert.NotEqual(t, dto.ContentTypeImageURL, content.Type)
	}
	logOther, ok := common.GetContextKeyType[map[string]interface{}](c, constant.ContextKeyLogOther)
	require.True(t, ok)
	assert.Equal(t, 2, logOther["vision_assist_image_count"])
}

func TestBuildVisionAssistUnitsDoesNotCombineAcrossMessages(t *testing.T) {
	images := []VisionAssistImage{
		{Index: 1, MessageIndex: 0},
		{Index: 2, MessageIndex: 1},
		{Index: 3, MessageIndex: 1},
	}

	plan := buildVisionAssistUnitPlan(dto.ChannelVisionAssistSettings{}, "", nil, images, VisionAssistMultiImageModeCombined, defaultVisionAssistCombinedMaxImages)
	units := plan.Units

	require.Len(t, units, 2)
	require.Len(t, units[0].Images, 1)
	require.Len(t, units[1].Images, 2)
	assert.Equal(t, 1, units[0].Images[0].Index)
	assert.Equal(t, 2, units[1].Images[0].Index)
	assert.Equal(t, 3, units[1].Images[1].Index)
}

func TestApplyVisionAssistCombinedFailureCountsAllImages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	strip := true
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:         true,
		AssistChannelId: 31,
		AssistModel:     "vision-combined-failure-test",
		FailurePolicy:   VisionAssistFailurePolicySkip,
		StripImage:      &strip,
		MultiImageMode:  VisionAssistMultiImageModeCombined,
	}}
	request := &dto.GeneralOpenAIRequest{
		Model: "target",
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "比较两张图片"},
				map[string]any{"type": "image_url", "image_url": "data:image/png;base64,image-one"},
				map[string]any{"type": "image_url", "image_url": "data:image/png;base64,image-two"},
			},
		}},
	}
	info := &relaycommon.RelayInfo{
		Request:         request,
		OriginModelName: "target",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: setting,
		},
	}
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, assistRequest *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		require.Len(t, images, 2)
		return nil, types.NewError(errors.New("批量识图失败"), types.ErrorCodeBadResponse)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	apiErr := ApplyVisionAssist(c, info, caller)

	require.Nil(t, apiErr)
	logOther, ok := common.GetContextKeyType[map[string]interface{}](c, constant.ContextKeyLogOther)
	require.True(t, ok)
	assert.Equal(t, false, logOther["vision_assist_applied"])
	assert.Equal(t, 2, logOther["vision_assist_failed_image_count"])
	assert.Contains(t, common.GetJsonString(request), "image_url")
}

func TestVisionAssistCombinedCacheKeyUsesImageOrder(t *testing.T) {
	setting := dto.ChannelVisionAssistSettings{AssistChannelId: 30, AssistModel: "vision-model"}
	first := VisionAssistImage{Source: &testFileSource{raw: "image-one"}, MimeType: "image/png"}
	second := VisionAssistImage{Source: &testFileSource{raw: "image-two"}, MimeType: "image/png"}

	keyAB := buildVisionAssistCacheKey(setting, "prompt", "question", VisionAssistMultiImageModeCombined, []VisionAssistImage{first, second})
	keyABAgain := buildVisionAssistCacheKey(setting, "prompt", "question", VisionAssistMultiImageModeCombined, []VisionAssistImage{first, second})
	keyBA := buildVisionAssistCacheKey(setting, "prompt", "question", VisionAssistMultiImageModeCombined, []VisionAssistImage{second, first})

	assert.Equal(t, keyAB, keyABAgain)
	assert.NotEqual(t, keyAB, keyBA)
}
