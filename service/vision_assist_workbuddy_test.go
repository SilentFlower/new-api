package service

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterWorkBuddyVisionAssistUserMessage(t *testing.T) {
	tests := []struct {
		name            string
		raw             string
		expected        string
		expectedChanged bool
	}{
		{
			name: "过滤线上系统上下文并保留查询和本地路径",
			raw: `<system-reminder data-role="user-context">
身份文件和连接器状态
</system-reminder>
<user_query>@image#1:HB7A3566.JPG 帮我把图中的背包扣下来</user_query>
<user_query><image_local_path>C:/Users/Administrator/Desktop/HB7A3566.JPG</image_local_path></user_query>`,
			expected: `<user_query>@image#1:HB7A3566.JPG 帮我把图中的背包扣下来</user_query>
<user_query><image_local_path>C:/Users/Administrator/Desktop/HB7A3566.JPG</image_local_path></user_query>`,
			expectedChanged: true,
		},
		{
			name: "兼容属性顺序引号大小写和分隔符变体",
			raw: `<SYSTEM-REMINDER source="workbuddy" DATA_ROLE='USER_CONTEXT'>上下文</SYSTEM-REMINDER>
<user_query>保留正文</user_query>`,
			expected:        `<user_query>保留正文</user_query>`,
			expectedChanged: true,
		},
		{
			name:            "兼容无引号属性值",
			raw:             `<system-reminder data-role=user-context extra=1>上下文</system-reminder>正文`,
			expected:        "正文",
			expectedChanged: true,
		},
		{
			name:            "只包含系统提醒时得到空文本",
			raw:             `<system-reminder data-role="user-context">上下文</system-reminder>`,
			expected:        "",
			expectedChanged: true,
		},
		{
			name: "删除多个系统提醒时稳定连接剩余正文",
			raw: `第一段
<system-reminder data-role="user-context">上下文一</system-reminder>
第二段
<system-reminder data_role="user_context">上下文二</system-reminder>
第三段`,
			expected:        "第一段\n第二段\n第三段",
			expectedChanged: true,
		},
		{
			name:            "非用户上下文提醒保持原文",
			raw:             `<system-reminder data-role="other">保留</system-reminder>`,
			expected:        `<system-reminder data-role="other">保留</system-reminder>`,
			expectedChanged: false,
		},
		{
			name:            "未闭合提醒保持原文",
			raw:             `<system-reminder data-role="user-context">上下文<user_query>正文</user_query>`,
			expected:        `<system-reminder data-role="user-context">上下文<user_query>正文</user_query>`,
			expectedChanged: false,
		},
		{
			name:            "开始标签尾部属性畸形时保持原文",
			raw:             `<system-reminder data-role="user-context" !!!>上下文</system-reminder>`,
			expected:        `<system-reminder data-role="user-context" !!!>上下文</system-reminder>`,
			expectedChanged: false,
		},
		{
			name:            "嵌套提醒保持原文",
			raw:             `<system-reminder data-role="user-context">外层<system-reminder data-role="user-context">内层</system-reminder></system-reminder>`,
			expected:        `<system-reminder data-role="user-context">外层<system-reminder data-role="user-context">内层</system-reminder></system-reminder>`,
			expectedChanged: false,
		},
		{
			name:            "普通标签和本地路径保持原文",
			raw:             `<user_query><image_local_path>C:/example.png</image_local_path></user_query>`,
			expected:        `<user_query><image_local_path>C:/example.png</image_local_path></user_query>`,
			expectedChanged: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			actual, changed := filterWorkBuddyVisionAssistUserMessage(testCase.raw)

			assert.Equal(t, testCase.expected, actual)
			assert.Equal(t, testCase.expectedChanged, changed)
		})
	}
}

func TestApplyVisionAssistUsesSystemReminderFilteredCacheKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, getVisionAssistCache().Purge())
	t.Cleanup(func() {
		assert.NoError(t, getVisionAssistCache().Purge())
	})

	stripImage := true
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:         true,
		AssistChannelId: 81,
		AssistModel:     "vision-workbuddy-filter-cache-test",
		StripImage:      &stripImage,
		CacheTTLSeconds: 3600,
	}}
	callCount := 0
	seenUserMessages := make([]string, 0, 2)
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		callCount++
		contents := request.Messages[0].ParseContent()
		require.GreaterOrEqual(t, len(contents), 2)
		seenUserMessages = append(seenUserMessages, contents[1].Text)
		return []VisionAssistResult{{Text: fmt.Sprintf("识图结果-%d", callCount)}}, nil
	}

	firstContext, firstRequest := applyWorkBuddyVisionAssistRequest(t, setting, "身份文件-A", "C:/Users/Administrator/Desktop/HB7A3566.JPG", caller)
	require.NotNil(t, firstContext)
	assert.Equal(t, 1, callCount)
	assert.Contains(t, firstRequest.Messages[0].ParseContent()[0].Text, "身份文件-A")

	secondContext, _ := applyWorkBuddyVisionAssistRequest(t, setting, "身份文件-B", "C:/Users/Administrator/Desktop/HB7A3566.JPG", caller)
	assert.Equal(t, 1, callCount)
	secondLogOther, ok := common.GetContextKeyType[map[string]interface{}](secondContext, constant.ContextKeyLogOther)
	require.True(t, ok)
	assert.Equal(t, 1, secondLogOther["vision_assist_cache_hits"])

	applyWorkBuddyVisionAssistRequest(t, setting, "身份文件-C", "D:/Images/HB7A3566.JPG", caller)
	assert.Equal(t, 2, callCount)
	require.Len(t, seenUserMessages, 2)
	for _, userMessage := range seenUserMessages {
		assert.NotContains(t, userMessage, "system-reminder")
		assert.NotContains(t, userMessage, "身份文件-")
		assert.Contains(t, userMessage, "<user_query>@image#1:HB7A3566.JPG 帮我把图中的背包扣下来</user_query>")
		assert.Contains(t, userMessage, "<image_local_path>")
	}
	assert.Contains(t, seenUserMessages[0], "C:/Users/Administrator/Desktop/HB7A3566.JPG")
	assert.Contains(t, seenUserMessages[1], "D:/Images/HB7A3566.JPG")
}

func TestApplyVisionAssistReusesLegacyWorkBuddyCacheAndBackfillsPrimary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, getVisionAssistCache().Purge())
	t.Cleanup(func() {
		assert.NoError(t, getVisionAssistCache().Purge())
	})

	stripImage := true
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:         true,
		AssistChannelId: 82,
		AssistModel:     "vision-workbuddy-legacy-cache-test",
		StripImage:      &stripImage,
		CacheTTLSeconds: 3600,
	}}
	request := newWorkBuddyVisionAssistRequest("旧身份文件", "C:/Users/Administrator/Desktop/HB7A3566.JPG")
	images := extractVisionAssistImages(request)
	require.Len(t, images, 1)
	rawUserMessage := resolveVisionAssistUserMessages(request, images)[0]
	filteredUserMessage, changed := filterWorkBuddyVisionAssistUserMessage(rawUserMessage)
	require.True(t, changed)
	prompt := normalizedVisionAssistPrompt(setting.VisionAssist)
	multiImageMode := normalizedVisionAssistMultiImageMode(setting.VisionAssist)
	legacyCacheKey := buildVisionAssistCacheKey(setting.VisionAssist, prompt, rawUserMessage, multiImageMode, images)
	primaryCacheKey := buildVisionAssistCacheKey(setting.VisionAssist, prompt, filteredUserMessage, multiImageMode, images)
	require.NotEqual(t, legacyCacheKey, primaryCacheKey)
	require.NoError(t, getVisionAssistCache().SetWithTTL(legacyCacheKey, visionAssistCacheValue{Text: "旧缓存识图结果"}, time.Hour))

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := newWorkBuddyVisionAssistRelayInfo(request, setting)
	callCount := 0
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		callCount++
		return []VisionAssistResult{{Text: "不应调用"}}, nil
	}

	require.Nil(t, ApplyVisionAssist(context, info, caller))
	assert.Equal(t, 0, callCount)
	primaryCached, found, err := getVisionAssistCache().Get(primaryCacheKey)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "旧缓存识图结果", primaryCached.Text)
	logOther, ok := common.GetContextKeyType[map[string]interface{}](context, constant.ContextKeyLogOther)
	require.True(t, ok)
	assert.Equal(t, 1, logOther["vision_assist_cache_hits"])
}

func TestApplyVisionAssistLegacyHitResolvesEarlierDuplicatePrimaryMiss(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, getVisionAssistCache().Purge())
	t.Cleanup(func() {
		assert.NoError(t, getVisionAssistCache().Purge())
	})

	stripImage := true
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:         true,
		AssistChannelId: 83,
		AssistModel:     "vision-workbuddy-legacy-deduplicate-test",
		StripImage:      &stripImage,
		CacheTTLSeconds: 3600,
	}}
	request := &dto.GeneralOpenAIRequest{
		Model: "target",
		Messages: []dto.Message{
			newWorkBuddyVisionAssistMessage("身份文件-A", "C:/Images/same.JPG"),
			newWorkBuddyVisionAssistMessage("身份文件-B", "C:/Images/same.JPG"),
		},
	}
	images := extractVisionAssistImages(request)
	require.Len(t, images, 2)
	rawUserMessages := resolveVisionAssistUserMessages(request, images)
	filteredUserMessage, changed := filterWorkBuddyVisionAssistUserMessage(rawUserMessages[0])
	require.True(t, changed)
	secondFilteredUserMessage, changed := filterWorkBuddyVisionAssistUserMessage(rawUserMessages[1])
	require.True(t, changed)
	require.Equal(t, filteredUserMessage, secondFilteredUserMessage)
	prompt := normalizedVisionAssistPrompt(setting.VisionAssist)
	multiImageMode := normalizedVisionAssistMultiImageMode(setting.VisionAssist)
	legacyCacheKey := buildVisionAssistCacheKey(setting.VisionAssist, prompt, rawUserMessages[1], multiImageMode, []VisionAssistImage{images[1]})
	require.NoError(t, getVisionAssistCache().SetWithTTL(legacyCacheKey, visionAssistCacheValue{Text: "重复图片旧缓存结果"}, time.Hour))

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := newWorkBuddyVisionAssistRelayInfo(request, setting)
	callCount := 0
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		callCount++
		return []VisionAssistResult{{Text: "不应调用"}}, nil
	}

	require.Nil(t, ApplyVisionAssist(context, info, caller))
	assert.Equal(t, 0, callCount)
	logOther, ok := common.GetContextKeyType[map[string]interface{}](context, constant.ContextKeyLogOther)
	require.True(t, ok)
	assert.Equal(t, 2, logOther["vision_assist_cache_hits"])
}

func applyWorkBuddyVisionAssistRequest(t *testing.T, setting dto.ChannelSettings, injectedContext string, imagePath string, caller VisionAssistCaller) (*gin.Context, *dto.GeneralOpenAIRequest) {
	t.Helper()
	request := newWorkBuddyVisionAssistRequest(injectedContext, imagePath)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := newWorkBuddyVisionAssistRelayInfo(request, setting)
	require.Nil(t, ApplyVisionAssist(context, info, caller))
	return context, request
}

func newWorkBuddyVisionAssistRequest(injectedContext string, imagePath string) *dto.GeneralOpenAIRequest {
	return &dto.GeneralOpenAIRequest{
		Model:    "target",
		Messages: []dto.Message{newWorkBuddyVisionAssistMessage(injectedContext, imagePath)},
	}
}

func newWorkBuddyVisionAssistMessage(injectedContext string, imagePath string) dto.Message {
	userMessage := fmt.Sprintf(`<system-reminder data-role="user-context">
%s
</system-reminder>
<user_query>@image#1:HB7A3566.JPG 帮我把图中的背包扣下来</user_query>
<user_query><image_local_path>%s</image_local_path></user_query>`, injectedContext, imagePath)
	return dto.Message{
		Role: "user",
		Content: []any{
			map[string]any{"type": "text", "text": userMessage},
			map[string]any{"type": "image_url", "image_url": "data:image/png;base64,workbuddy-image"},
		},
	}
}

func newWorkBuddyVisionAssistRelayInfo(request dto.Request, setting dto.ChannelSettings) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		Request:         request,
		RequestId:       "workbuddy-vision-assist-test",
		OriginModelName: "target",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         3,
			UpstreamModelName: "target",
			ChannelSetting:    setting,
		},
	}
}
