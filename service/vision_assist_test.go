package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractOpenAIVisionAssistImages(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model: "text-model",
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "看图"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc", "detail": "low"}},
			},
		}},
	}

	images := extractOpenAIVisionAssistImages(request)

	require.Len(t, images, 1)
	assert.Equal(t, 1, images[0].Index)
	assert.Equal(t, 0, images[0].MessageIndex)
	assert.Equal(t, "low", images[0].Detail)
	assert.Equal(t, "data:image/png;base64,abc", images[0].Source.GetRawData())
}

func TestExtractOpenAIVisionAssistImagesSupportsStringImageURL(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model: "text-model",
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "image_url", "image_url": "https://example.com/a.png"},
			},
		}},
	}

	images := extractOpenAIVisionAssistImages(request)

	require.Len(t, images, 1)
	assert.Equal(t, "https://example.com/a.png", images[0].Source.GetRawData())
	assert.Equal(t, "high", images[0].Detail)
}

func TestRewriteOpenAIVisionAssistRequestStripImage(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model: "text-model",
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "原始问题"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/a.png"}},
			},
		}},
	}
	results := []VisionAssistResult{{
		Image: VisionAssistImage{Index: 1, MessageIndex: 0},
		Text:  "一张图片",
	}}

	err := rewriteOpenAIVisionAssistRequest(request, results, true)

	require.NoError(t, err)
	contents := request.Messages[0].ParseContent()
	require.Len(t, contents, 2)
	assert.Equal(t, dto.ContentTypeText, contents[0].Type)
	assert.Equal(t, "原始问题", contents[0].Text)
	assert.Equal(t, dto.ContentTypeText, contents[1].Type)
	assert.Contains(t, contents[1].Text, "[图片内容]")
	assert.Contains(t, contents[1].Text, "一张图片")
	assert.NotContains(t, contents[1].Text, "辅助识别")
}

func TestExtractOpenAIResponsesVisionAssistImagesSupportsStringImageURL(t *testing.T) {
	input, err := common.Marshal([]any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "看图"},
				map[string]any{"type": "input_image", "image_url": "https://example.com/a.png"},
			},
		},
	})
	require.NoError(t, err)
	request := &dto.OpenAIResponsesRequest{Input: input}

	images := extractOpenAIResponsesVisionAssistImages(request)

	require.Len(t, images, 1)
	assert.Equal(t, 1, images[0].Index)
	assert.Equal(t, 0, images[0].MessageIndex)
	assert.Equal(t, "high", images[0].Detail)
	assert.Equal(t, "https://example.com/a.png", images[0].Source.GetRawData())
}

func TestExtractOpenAIResponsesVisionAssistImagesSupportsObjectImageURL(t *testing.T) {
	input, err := common.Marshal([]any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type": "input_image",
					"image_url": map[string]any{
						"url":       "data:image/png;base64,abc",
						"detail":    "low",
						"mime_type": "image/png",
					},
				},
			},
		},
	})
	require.NoError(t, err)
	request := &dto.OpenAIResponsesRequest{Input: input}

	images := extractOpenAIResponsesVisionAssistImages(request)

	require.Len(t, images, 1)
	assert.Equal(t, "low", images[0].Detail)
	assert.Equal(t, "image/png", images[0].MimeType)
	assert.Equal(t, "data:image/png;base64,abc", images[0].Source.GetRawData())
}

func TestRewriteOpenAIResponsesVisionAssistRequestStripImage(t *testing.T) {
	input, err := common.Marshal([]any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "原始问题"},
				map[string]any{"type": "input_image", "image_url": "https://example.com/a.png"},
			},
		},
	})
	require.NoError(t, err)
	request := &dto.OpenAIResponsesRequest{Input: input}
	results := []VisionAssistResult{{
		Image: VisionAssistImage{Index: 1, MessageIndex: 0},
		Text:  "一张图片",
	}}

	err = rewriteOpenAIResponsesVisionAssistRequest(request, results, true)

	require.NoError(t, err)
	body := common.GetJsonString(request)
	assert.Contains(t, body, "一张图片")
	assert.Contains(t, body, "input_text")
	assert.NotContains(t, body, "input_image")
}

func TestRewriteOpenAIResponsesVisionAssistRequestKeepImage(t *testing.T) {
	input, err := common.Marshal([]any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_image", "image_url": "https://example.com/a.png"},
			},
		},
	})
	require.NoError(t, err)
	request := &dto.OpenAIResponsesRequest{Input: input}
	results := []VisionAssistResult{{
		Image: VisionAssistImage{Index: 1, MessageIndex: 0},
		Text:  "保留原图",
	}}

	err = rewriteOpenAIResponsesVisionAssistRequest(request, results, false)

	require.NoError(t, err)
	var rewritten []any
	require.NoError(t, common.Unmarshal(request.Input, &rewritten))
	require.Len(t, rewritten, 1)
	message, ok := rewritten[0].(map[string]any)
	require.True(t, ok)
	contents, ok := message["content"].([]any)
	require.True(t, ok)
	require.Len(t, contents, 2)
	textItem, ok := contents[0].(map[string]any)
	require.True(t, ok)
	imageItem, ok := contents[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "input_text", textItem["type"])
	assert.Contains(t, common.Interface2String(textItem["text"]), "保留原图")
	assert.Equal(t, "input_image", imageItem["type"])
}

func TestRewriteOpenAIResponsesVisionAssistRequestKeepsCustomToolImage(t *testing.T) {
	input, err := common.Marshal([]any{
		map[string]any{
			"type":    "custom_tool_call_output",
			"call_id": "call_view",
			"output": []any{
				map[string]any{"type": "input_text", "text": "工具输出"},
				map[string]any{"type": "input_image", "image_url": "https://example.com/a.png"},
			},
		},
	})
	require.NoError(t, err)
	request := &dto.OpenAIResponsesRequest{Input: input}
	results := []VisionAssistResult{{
		Image: VisionAssistImage{Index: 1, MessageIndex: 0},
		Text:  "保留工具图片",
	}}

	err = rewriteOpenAIResponsesVisionAssistRequest(request, results, false)

	require.NoError(t, err)
	var rewritten []map[string]any
	require.NoError(t, common.Unmarshal(request.Input, &rewritten))
	require.Len(t, rewritten, 2)
	assert.Equal(t, "custom_tool_call_output", rewritten[0]["type"])
	output, ok := rewritten[0]["output"].([]any)
	require.True(t, ok)
	require.Len(t, output, 2)
	textOutput, ok := output[0].(map[string]any)
	require.True(t, ok)
	imageOutput, ok := output[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "input_text", textOutput["type"])
	assert.Equal(t, "input_image", imageOutput["type"])
	assert.Equal(t, "message", rewritten[1]["type"])
	assert.Equal(t, "user", rewritten[1]["role"])
	content, ok := rewritten[1]["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 1)
	textContent, ok := content[0].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, common.Interface2String(textContent["text"]), "保留工具图片")
}

func TestExtractAndRewriteClaudeVisionAssistImages(t *testing.T) {
	request := &dto.ClaudeRequest{
		Model: "claude-text",
		Messages: []dto.ClaudeMessage{{
			Role: "user",
			Content: []dto.ClaudeMediaMessage{
				{Type: dto.ContentTypeText, Text: common.GetPointer("看图")},
				{Type: "image", Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: "image/jpeg", Data: "abc"}},
			},
		}},
	}

	images := extractClaudeVisionAssistImages(request)
	require.Len(t, images, 1)
	assert.Equal(t, "image/jpeg", images[0].MimeType)

	err := rewriteClaudeVisionAssistRequest(request, []VisionAssistResult{{
		Image: VisionAssistImage{Index: 1, MessageIndex: 0},
		Text:  "图片里有文字",
	}}, true)

	require.NoError(t, err)
	contents, err := request.Messages[0].ParseContent()
	require.NoError(t, err)
	require.Len(t, contents, 2)
	assert.Equal(t, dto.ContentTypeText, contents[0].Type)
	assert.Equal(t, dto.ContentTypeText, contents[1].Type)
	assert.Contains(t, contents[1].GetText(), "[图片内容]")
	assert.Contains(t, contents[1].GetText(), "图片里有文字")
	assert.NotContains(t, contents[1].GetText(), "辅助识别")
}

func TestVisionAssistTextSkipsEmptyResultsAndHidesImplementationDetails(t *testing.T) {
	emptyText := visionAssistText([]VisionAssistResult{{
		Image: VisionAssistImage{Index: 1, MessageIndex: 0},
		Text:  "  ",
	}})
	assert.Empty(t, emptyText)

	text := visionAssistText([]VisionAssistResult{{
		Image: VisionAssistImage{Index: 1, MessageIndex: 0},
		Text:  "图片里有表格",
	}})
	assert.Contains(t, text, "[图片内容]")
	assert.Contains(t, text, "图片里有表格")
	assert.NotContains(t, text, "辅助识别")
}

func TestVisionAssistCacheKeyUsesAssistSettings(t *testing.T) {
	setting := dto.ChannelVisionAssistSettings{
		AssistChannelId: 1,
		AssistModel:     "vision-a",
	}
	image := VisionAssistImage{
		Source:   &testFileSource{raw: "image-data"},
		Detail:   "high",
		MimeType: "image/png",
	}

	keyA := buildVisionAssistCacheKey(setting, "prompt-a", image)
	setting.AssistModel = "vision-b"
	keyB := buildVisionAssistCacheKey(setting, "prompt-a", image)
	setting.AssistModel = "vision-a"
	keyC := buildVisionAssistCacheKey(setting, "prompt-c", image)

	assert.NotEmpty(t, keyA)
	assert.NotEqual(t, keyA, keyB)
	assert.NotEqual(t, keyA, keyC)
	assert.NotContains(t, keyA, "image-data")
}

func TestShouldApplyVisionAssistUsesUpstreamModelAndProcessingFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	setting := dto.ChannelVisionAssistSettings{
		Enabled:         true,
		AssistChannelId: 2,
		AssistModel:     "vision",
		TargetModels:    []string{"final-model"},
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "origin-model",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "final-model"},
	}

	assert.True(t, shouldApplyVisionAssist(c, info, setting))

	common.SetContextKey(c, constant.ContextKeyVisionAssistProcessing, true)
	assert.False(t, shouldApplyVisionAssist(c, info, setting))
}

func TestApplyVisionAssistUsesRequestLevelDuplicateCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NoError(t, getVisionAssistCache().Purge())
	strip := true
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:         true,
		AssistChannelId: 7,
		AssistModel:     "vision-duplicate-test",
		StripImage:      &strip,
	}}
	request := &dto.GeneralOpenAIRequest{
		Model: "target",
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
			},
		}},
	}
	info := &relaycommon.RelayInfo{
		Request:         request,
		OriginModelName: "target",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "target",
			ChannelSetting:    setting,
		},
	}
	callCount := 0
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		callCount++
		return []VisionAssistResult{{Image: images[0], Text: "相同图片"}}, nil
	}

	err := ApplyVisionAssist(c, info, caller)

	require.Nil(t, err)
	assert.Equal(t, 1, callCount)
	logOther, ok := common.GetContextKeyType[map[string]interface{}](c, constant.ContextKeyLogOther)
	require.True(t, ok)
	assert.Equal(t, 1, logOther["vision_assist_reused_hits"])
	assert.NotContains(t, strings.ToLower(common.GetJsonString(info.Request)), "image_url")
	assert.Contains(t, common.GetJsonString(info.Request), "相同图片")
}

func TestApplyVisionAssistRewritesOpenAIResponsesRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NoError(t, getVisionAssistCache().Purge())
	strip := true
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:         true,
		AssistChannelId: 8,
		AssistModel:     "vision-responses-test",
		StripImage:      &strip,
	}}
	input, err := common.Marshal([]any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "看图回答"},
				map[string]any{"type": "input_image", "image_url": map[string]any{"url": "data:image/png;base64,abc", "detail": "low"}},
			},
		},
	})
	require.NoError(t, err)
	request := &dto.OpenAIResponsesRequest{
		Model: "target",
		Input: input,
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
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		require.Len(t, images, 1)
		assert.Equal(t, "low", images[0].Detail)
		assert.Equal(t, "data:image/png;base64,abc", images[0].Source.GetRawData())
		return []VisionAssistResult{{Image: images[0], Text: "Responses 图片描述"}}, nil
	}

	err = ApplyVisionAssist(c, info, caller)

	require.Nil(t, err)
	body := common.GetJsonString(info.Request)
	assert.Contains(t, body, "Responses 图片描述")
	assert.Contains(t, body, "input_text")
	assert.NotContains(t, body, "input_image")
}

func TestApplyVisionAssistRewritesOpenAIResponsesToolOutputs(t *testing.T) {
	tests := []struct {
		name             string
		assistModel      string
		inputItem        map[string]any
		expectedDetail   string
		expectedMimeType string
		expectedSource   string
		expectedItems    int
	}{
		{
			name:        "function call output",
			assistModel: "vision-function-output-test",
			inputItem: map[string]any{
				"type":    "function_call_output",
				"call_id": "call_function",
				"output": []any{
					map[string]any{"type": "input_image", "image_url": "https://example.com/function.png"},
				},
			},
			expectedDetail: "high",
			expectedSource: "https://example.com/function.png",
			expectedItems:  1,
		},
		{
			name:        "custom tool output",
			assistModel: "vision-custom-output-test",
			inputItem: map[string]any{
				"type":    "custom_tool_call_output",
				"call_id": "call_custom",
				"output": []any{
					map[string]any{"type": "input_text", "text": "Script completed"},
					map[string]any{
						"type": "input_image",
						"image_url": map[string]any{
							"url":       "data:image/png;base64,custom-tool",
							"detail":    "low",
							"mime_type": "image/png",
						},
					},
				},
			},
			expectedDetail:   "low",
			expectedMimeType: "image/png",
			expectedSource:   "data:image/png;base64,custom-tool",
			expectedItems:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			require.NoError(t, getVisionAssistCache().Purge())
			strip := true
			setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
				Enabled:         true,
				AssistChannelId: 18,
				AssistModel:     tt.assistModel,
				StripImage:      &strip,
			}}
			input, err := common.Marshal([]any{tt.inputItem})
			require.NoError(t, err)
			request := &dto.OpenAIResponsesRequest{Model: "target", Input: input}
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
			caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
				callCount++
				require.Len(t, images, 1)
				assert.Equal(t, tt.expectedDetail, images[0].Detail)
				assert.Equal(t, tt.expectedMimeType, images[0].MimeType)
				assert.Equal(t, tt.expectedSource, images[0].Source.GetRawData())
				return []VisionAssistResult{{Image: images[0], Text: "工具图片描述"}}, nil
			}

			err = ApplyVisionAssist(c, info, caller)

			require.Nil(t, err)
			assert.Equal(t, 1, callCount)
			var rewritten []map[string]any
			require.NoError(t, common.Unmarshal(request.Input, &rewritten))
			require.Len(t, rewritten, tt.expectedItems)
			body := common.GetJsonString(info.Request)
			assert.Contains(t, body, "工具图片描述")
			assert.NotContains(t, body, "input_image")
			output, ok := rewritten[0]["output"].([]any)
			require.True(t, ok)
			require.Len(t, output, 1)
			outputContent, ok := output[0].(map[string]any)
			require.True(t, ok)
			if tt.expectedItems == 1 {
				assert.Equal(t, "input_text", outputContent["type"])
				assert.Contains(t, common.Interface2String(outputContent["text"]), "工具图片描述")
			} else {
				assert.Equal(t, "Script completed", outputContent["text"])
				assert.Equal(t, "message", rewritten[1]["type"])
				assert.Equal(t, "user", rewritten[1]["role"])
			}
		})
	}
}

func TestApplyVisionAssistSkipsOpenAIResponsesToolOutputWithoutImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:         true,
		AssistChannelId: 19,
		AssistModel:     "vision-no-tool-image-test",
	}}
	input, err := common.Marshal([]any{
		map[string]any{
			"type":    "custom_tool_call_output",
			"call_id": "call_text",
			"output": []any{
				map[string]any{"type": "input_text", "text": "纯文本工具结果"},
			},
		},
	})
	require.NoError(t, err)
	request := &dto.OpenAIResponsesRequest{Model: "target", Input: input}
	info := &relaycommon.RelayInfo{
		Request:         request,
		OriginModelName: "target",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "target",
			ChannelSetting:    setting,
		},
	}
	callCount := 0
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		callCount++
		return nil, nil
	}

	apiErr := ApplyVisionAssist(c, info, caller)

	require.Nil(t, apiErr)
	assert.Equal(t, 0, callCount)
	assert.Equal(t, string(input), string(request.Input))
}

func TestApplyVisionAssistReusesDuplicateOpenAIResponsesToolImages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NoError(t, getVisionAssistCache().Purge())
	strip := true
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:         true,
		AssistChannelId: 20,
		AssistModel:     "vision-duplicate-tool-output-test",
		StripImage:      &strip,
	}}
	image := map[string]any{"type": "input_image", "image_url": "data:image/png;base64,duplicate-tool"}
	input, err := common.Marshal([]any{
		map[string]any{
			"type":    "custom_tool_call_output",
			"call_id": "call_duplicate",
			"output":  []any{image, image},
		},
	})
	require.NoError(t, err)
	request := &dto.OpenAIResponsesRequest{Model: "target", Input: input}
	info := &relaycommon.RelayInfo{
		Request:         request,
		OriginModelName: "target",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "target",
			ChannelSetting:    setting,
		},
	}
	callCount := 0
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		callCount++
		require.Len(t, images, 1)
		return []VisionAssistResult{{Image: images[0], Text: "重复工具图片"}}, nil
	}

	apiErr := ApplyVisionAssist(c, info, caller)

	require.Nil(t, apiErr)
	assert.Equal(t, 1, callCount)
	body := common.GetJsonString(info.Request)
	assert.Contains(t, body, "图片 1：重复工具图片")
	assert.Contains(t, body, "图片 2：重复工具图片")
	assert.NotContains(t, body, "input_image")
}

func TestApplyVisionAssistWritesLogOther(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NoError(t, getVisionAssistCache().Purge())
	strip := true
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:         true,
		AssistChannelId: 9,
		AssistModel:     "vision-log-test",
		StripImage:      &strip,
	}}
	request := &dto.GeneralOpenAIRequest{
		Model: "target",
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "image_url", "image_url": "https://example.com/a.png"},
			},
		}},
	}
	info := &relaycommon.RelayInfo{
		Request:         request,
		OriginModelName: "origin-target",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         3,
			UpstreamModelName: "final-target",
			ChannelSetting:    setting,
		},
	}
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		return []VisionAssistResult{{Image: images[0], Text: "日志图片"}}, nil
	}

	err := ApplyVisionAssist(c, info, caller)

	require.Nil(t, err)
	logOther, ok := common.GetContextKeyType[map[string]interface{}](c, constant.ContextKeyLogOther)
	require.True(t, ok)
	assert.Equal(t, true, logOther["vision_assist_applied"])
	assert.Equal(t, 9, logOther["vision_assist_channel_id"])
	assert.Equal(t, "vision-log-test", logOther["vision_assist_model"])
	assert.Equal(t, 3, logOther["vision_assist_target_channel"])
	assert.Equal(t, "origin-target", logOther["vision_assist_target_model"])
	assert.Equal(t, "final-target", logOther["vision_assist_upstream_model"])
}

func TestApplyVisionAssistSkipFailureWritesLogOther(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NoError(t, getVisionAssistCache().Purge())
	strip := true
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:         true,
		AssistChannelId: 9,
		AssistModel:     "vision-skip-test",
		FailurePolicy:   VisionAssistFailurePolicySkip,
		StripImage:      &strip,
	}}
	request := &dto.GeneralOpenAIRequest{
		Model: "target",
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "image_url", "image_url": "https://example.com/a.png"},
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
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		return nil, types.NewError(errors.New("辅助失败"), types.ErrorCodeEmptyResponse)
	}

	err := ApplyVisionAssist(c, info, caller)

	require.Nil(t, err)
	logOther, ok := common.GetContextKeyType[map[string]interface{}](c, constant.ContextKeyLogOther)
	require.True(t, ok)
	assert.Equal(t, false, logOther["vision_assist_applied"])
	assert.Equal(t, "assist_call_failed", logOther["vision_assist_failure_reason"])
	assert.Equal(t, VisionAssistFailurePolicySkip, logOther["vision_assist_failure_policy"])
	assert.Contains(t, common.GetJsonString(info.Request), "image_url")
}

func TestVisionAssistExecutionSettingsNormalizeDefaults(t *testing.T) {
	setting := dto.ChannelVisionAssistSettings{}

	assert.Equal(t, VisionAssistEndpointModeAuto, normalizedVisionAssistEndpointMode(setting))
	assert.Equal(t, 1, normalizedVisionAssistMaxConcurrency(setting))
	assert.Equal(t, 0, normalizedVisionAssistRetryCount(setting))
	assert.Equal(t, defaultVisionAssistRetryBackoffMs, normalizedVisionAssistRetryBackoff(setting))

	setting.EndpointMode = VisionAssistEndpointModeGeminiNative
	setting.MaxConcurrency = 99
	setting.RetryCount = 99
	setting.RetryBackoffMs = 99999

	assert.Equal(t, VisionAssistEndpointModeGeminiNative, normalizedVisionAssistEndpointMode(setting))
	assert.Equal(t, 8, normalizedVisionAssistMaxConcurrency(setting))
	assert.Equal(t, 5, normalizedVisionAssistRetryCount(setting))
	assert.Equal(t, 30000, normalizedVisionAssistRetryBackoff(setting))
}

func TestBuildVisionAssistRequestKeepsTypedMediaContentParsable(t *testing.T) {
	setting := dto.ChannelVisionAssistSettings{
		AssistModel: "gemini-2.5-flash",
	}
	request := buildVisionAssistRequest(setting, "描述图片", []VisionAssistImage{{
		Index:    1,
		Source:   &testFileSource{raw: "data:image/png;base64,abc"},
		Detail:   "low",
		MimeType: "image/png",
	}})

	require.Len(t, request.Messages, 1)
	assert.Equal(t, "user", request.Messages[0].Role)
	contents := request.Messages[0].ParseContent()
	require.Len(t, contents, 3)
	assert.Equal(t, dto.ContentTypeText, contents[0].Type)
	assert.Equal(t, "描述图片", contents[0].Text)
	assert.Equal(t, dto.ContentTypeText, contents[1].Type)
	assert.Equal(t, "图片 1：", contents[1].Text)
	assert.Equal(t, dto.ContentTypeImageURL, contents[2].Type)
	assert.Equal(t, "data:image/png;base64,abc", contents[2].GetImageMedia().Url)
}

func TestApplyVisionAssistRespectsMaxConcurrency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NoError(t, getVisionAssistCache().Purge())
	strip := true
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:         true,
		AssistChannelId: 10,
		AssistModel:     "vision-concurrency-test",
		MaxConcurrency:  2,
		StripImage:      &strip,
	}}
	request := &dto.GeneralOpenAIRequest{
		Model: "target",
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "image_url", "image_url": "data:image/png;base64,a"},
				map[string]any{"type": "image_url", "image_url": "data:image/png;base64,b"},
				map[string]any{"type": "image_url", "image_url": "data:image/png;base64,c"},
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
	var current int32
	var maxSeen int32
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		now := atomic.AddInt32(&current, 1)
		for {
			old := atomic.LoadInt32(&maxSeen)
			if now <= old || atomic.CompareAndSwapInt32(&maxSeen, old, now) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&current, -1)
		return []VisionAssistResult{{Image: images[0], Text: "图片"}}, nil
	}

	err := ApplyVisionAssist(c, info, caller)

	require.Nil(t, err)
	assert.LessOrEqual(t, atomic.LoadInt32(&maxSeen), int32(2))
	logOther, ok := common.GetContextKeyType[map[string]interface{}](c, constant.ContextKeyLogOther)
	require.True(t, ok)
	assert.Equal(t, 2, logOther["vision_assist_max_concurrency"])
}

func TestApplyVisionAssistRetriesRetriableErrorsOnly(t *testing.T) {
	tests := []struct {
		name          string
		err           *types.NewAPIError
		expectedCalls int
	}{
		{
			name:          "429 会重试",
			err:           types.NewErrorWithStatusCode(errors.New("rate limited"), types.ErrorCodeBadResponse, http.StatusTooManyRequests),
			expectedCalls: 2,
		},
		{
			name:          "400 不重试",
			err:           types.NewErrorWithStatusCode(errors.New("invalid argument"), types.ErrorCodeBadResponse, http.StatusBadRequest),
			expectedCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			require.NoError(t, getVisionAssistCache().Purge())
			strip := true
			setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
				Enabled:         true,
				AssistChannelId: 11,
				AssistModel:     "vision-retry-test-" + tt.name,
				RetryCount:      1,
				RetryBackoffMs:  1,
				StripImage:      &strip,
			}}
			request := &dto.GeneralOpenAIRequest{
				Model: "target",
				Messages: []dto.Message{{
					Role: "user",
					Content: []any{
						map[string]any{"type": "image_url", "image_url": "data:image/png;base64,retry"},
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
			caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
				callCount++
				if callCount == 1 {
					return nil, tt.err
				}
				return []VisionAssistResult{{Image: images[0], Text: "重试成功"}}, nil
			}

			err := ApplyVisionAssist(c, info, caller)

			if tt.expectedCalls == 1 {
				require.NotNil(t, err)
			} else {
				require.Nil(t, err)
			}
			assert.Equal(t, tt.expectedCalls, callCount)
		})
	}
}

func TestApplyVisionAssistSkipPartialFailureKeepsSuccessfulResults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NoError(t, getVisionAssistCache().Purge())
	strip := false
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:         true,
		AssistChannelId: 12,
		AssistModel:     "vision-partial-skip-test",
		FailurePolicy:   VisionAssistFailurePolicySkip,
		StripImage:      &strip,
	}}
	request := &dto.GeneralOpenAIRequest{
		Model: "target",
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "image_url", "image_url": "data:image/png;base64,ok"},
				map[string]any{"type": "image_url", "image_url": "data:image/png;base64,bad"},
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
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		if images[0].Index == 2 {
			return nil, types.NewError(errors.New("辅助失败"), types.ErrorCodeEmptyResponse)
		}
		return []VisionAssistResult{{Image: images[0], Text: "第一张成功"}}, nil
	}

	err := ApplyVisionAssist(c, info, caller)

	require.Nil(t, err)
	body := common.GetJsonString(info.Request)
	assert.Contains(t, body, "第一张成功")
	assert.Contains(t, body, "image_url")
	logOther, ok := common.GetContextKeyType[map[string]interface{}](c, constant.ContextKeyLogOther)
	require.True(t, ok)
	assert.Equal(t, true, logOther["vision_assist_applied"])
	assert.Equal(t, 1, logOther["vision_assist_failed_image_count"])
}

type testFileSource struct {
	raw string
	mu  sync.Mutex
}

func (t *testFileSource) IsURL() bool { return false }

func (t *testFileSource) GetIdentifier() string { return t.raw }

func (t *testFileSource) GetRawData() string { return t.raw }

func (t *testFileSource) ClearRawData() {}

func (t *testFileSource) SetCache(data *types.CachedFileData) {}

func (t *testFileSource) GetCache() *types.CachedFileData { return nil }

func (t *testFileSource) HasCache() bool { return false }

func (t *testFileSource) ClearCache() {}

func (t *testFileSource) IsRegistered() bool { return false }

func (t *testFileSource) SetRegistered(registered bool) {}

func (t *testFileSource) Mu() *sync.Mutex { return &t.mu }
