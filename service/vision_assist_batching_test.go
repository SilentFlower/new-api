package service

import (
	"fmt"
	"net/http/httptest"
	"strings"
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

func TestBuildVisionAssistUnitPlanSplitsCombinedImagesByConfiguredLimit(t *testing.T) {
	images := make([]VisionAssistImage, 0, 39)
	for index := 1; index <= 39; index++ {
		images = append(images, VisionAssistImage{
			Index:        index,
			MessageIndex: 0,
			Source:       &testFileSource{raw: fmt.Sprintf("image-%d", index)},
		})
	}

	plan := buildVisionAssistUnitPlan(dto.ChannelVisionAssistSettings{}, "", nil, images, VisionAssistMultiImageModeCombined, 5)

	assert.Equal(t, []int{5, 5, 5, 5, 5, 5, 5, 4}, visionAssistUnitImageCounts(plan.Units))
	assert.True(t, plan.SplitByImageCount)
	assert.False(t, plan.SplitByPayloadSize)
}

func TestBuildVisionAssistUnitPlanSplitsCombinedImagesByFullRequestPayloadSize(t *testing.T) {
	largeImage := "data:image/png;base64," + strings.Repeat("a", (maxVisionAssistCombinedPayloadBytes-4096)/2)
	images := []VisionAssistImage{
		{Index: 1, MessageIndex: 0, Source: &testFileSource{raw: largeImage}},
		{Index: 2, MessageIndex: 0, Source: &testFileSource{raw: largeImage}},
	}
	prompt := strings.Repeat("识", 4096)

	imageBytes := 0
	for _, image := range images {
		imageBytes += len(visionAssistImageURL(image))
	}
	require.Less(t, imageBytes, maxVisionAssistCombinedPayloadBytes)
	plan := buildVisionAssistUnitPlan(dto.ChannelVisionAssistSettings{}, prompt, nil, images, VisionAssistMultiImageModeCombined, 5)

	assert.Equal(t, []int{1, 1}, visionAssistUnitImageCounts(plan.Units))
	assert.False(t, plan.SplitByImageCount)
	assert.True(t, plan.SplitByPayloadSize)
}

func TestBuildVisionAssistUnitPlanKeepsOversizedSingleImageInOwnBatch(t *testing.T) {
	oversizedImage := "data:image/png;base64," + strings.Repeat("a", maxVisionAssistCombinedPayloadBytes+1)
	images := []VisionAssistImage{
		{Index: 1, MessageIndex: 0, Source: &testFileSource{raw: oversizedImage}},
		{Index: 2, MessageIndex: 0, Source: &testFileSource{raw: "small-image"}},
	}

	plan := buildVisionAssistUnitPlan(dto.ChannelVisionAssistSettings{}, "", nil, images, VisionAssistMultiImageModeCombined, 5)

	assert.Equal(t, []int{1, 1}, visionAssistUnitImageCounts(plan.Units))
	assert.True(t, plan.SplitByPayloadSize)
}

func TestEstimateVisionAssistRequestEnvelopeMatchesSerializedPayload(t *testing.T) {
	setting := dto.ChannelVisionAssistSettings{AssistModel: "vision-envelope-test"}
	images := []VisionAssistImage{
		{Index: 1, Source: &testFileSource{raw: "data:image/png;base64,a&<b>"}, Detail: "high", MimeType: "image/png"},
		{Index: 2, Source: &testFileSource{raw: "data:image/jpeg;base64,c+d"}, MimeType: "image/jpeg"},
	}

	envelopeBytes, ok := estimateVisionAssistRequestEnvelopeBytes(setting, "识图规则", "比较图片", images)
	require.True(t, ok)
	estimatedBytes := envelopeBytes
	for _, image := range images {
		imageURLBytes, imageURLBytesOK := estimateVisionAssistImageURLPayloadBytes(image)
		require.True(t, imageURLBytesOK)
		estimatedBytes += imageURLBytes
	}
	serializedRequest, err := common.Marshal(buildVisionAssistRequest(setting, "识图规则", "比较图片", images))
	require.NoError(t, err)

	assert.Equal(t, len(serializedRequest), estimatedBytes)
}

func TestNormalizedVisionAssistCombinedMaxImages(t *testing.T) {
	tests := []struct {
		name     string
		value    int
		expected int
	}{
		{name: "缺失时使用默认值", value: 0, expected: 5},
		{name: "合法值保持不变", value: 12, expected: 12},
		{name: "负数回退默认值", value: -1, expected: 5},
		{name: "超过上限回退默认值", value: 65, expected: 5},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			setting := dto.ChannelVisionAssistSettings{CombinedMaxImages: testCase.value}

			assert.Equal(t, testCase.expected, normalizedVisionAssistCombinedMaxImages(setting))
		})
	}
}

func TestApplyVisionAssistReusesCombinedBatchCacheAcrossRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, getVisionAssistCache().Purge())
	stripImage := true
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:           true,
		AssistChannelId:   48,
		AssistModel:       "vision-combined-cache-batching-test",
		StripImage:        &stripImage,
		MultiImageMode:    VisionAssistMultiImageModeCombined,
		CombinedMaxImages: 2,
		CacheTTLSeconds:   3600,
	}}
	firstRequest := newVisionAssistBatchCacheRequest(false)
	firstInfo := newVisionAssistBatchCacheRelayInfo(firstRequest, setting, "request-one")
	firstContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	firstCalls := 0
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		firstCalls++
		return []VisionAssistResult{{Text: fmt.Sprintf("批次包含 %d 张图片", len(images))}}, nil
	}

	apiErr := ApplyVisionAssist(firstContext, firstInfo, caller)

	require.Nil(t, apiErr)
	assert.Equal(t, 2, firstCalls)

	secondSetting := setting
	secondSetting.VisionAssist.MaxConcurrency = 3
	secondRequest := newVisionAssistBatchCacheRequest(true)
	secondInfo := newVisionAssistBatchCacheRelayInfo(secondRequest, secondSetting, "request-two")
	secondContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	secondCalls := 0
	secondCaller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		secondCalls++
		return []VisionAssistResult{{Text: "不应调用"}}, nil
	}

	apiErr = ApplyVisionAssist(secondContext, secondInfo, secondCaller)

	require.Nil(t, apiErr)
	assert.Equal(t, 0, secondCalls)
	logOther, ok := common.GetContextKeyType[map[string]interface{}](secondContext, constant.ContextKeyLogOther)
	require.True(t, ok)
	assert.Equal(t, 3, logOther["vision_assist_cache_hits"])
	assert.Equal(t, 2, logOther["vision_assist_batch_count"])
	assert.Equal(t, []int{2, 1}, logOther["vision_assist_batch_image_counts"])
	assert.Equal(t, true, logOther["vision_assist_split_applied"])
	assert.Equal(t, "image_count", logOther["vision_assist_split_reason"])
}

func TestApplyVisionAssistCombinedCacheSeparatesChangedBatchCompositions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, getVisionAssistCache().Purge())
	stripImage := true
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:           true,
		AssistChannelId:   50,
		AssistModel:       "vision-combined-cache-boundary-test",
		StripImage:        &stripImage,
		MultiImageMode:    VisionAssistMultiImageModeCombined,
		CombinedMaxImages: 2,
		CacheTTLSeconds:   3600,
	}}
	firstRequest := newVisionAssistBatchCacheRequest(false)
	firstInfo := newVisionAssistBatchCacheRelayInfo(firstRequest, setting, "request-boundary-one")
	firstContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	firstCalls := 0
	firstCaller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		firstCalls++
		return []VisionAssistResult{{Text: fmt.Sprintf("首次批次 %d", len(images))}}, nil
	}

	apiErr := ApplyVisionAssist(firstContext, firstInfo, firstCaller)

	require.Nil(t, apiErr)
	assert.Equal(t, 2, firstCalls)

	setting.VisionAssist.CombinedMaxImages = 1
	secondRequest := newVisionAssistBatchCacheRequest(false)
	secondInfo := newVisionAssistBatchCacheRelayInfo(secondRequest, setting, "request-boundary-two")
	secondContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	secondCalls := 0
	secondCaller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		secondCalls++
		return []VisionAssistResult{{Text: "新单图批次"}}, nil
	}

	apiErr = ApplyVisionAssist(secondContext, secondInfo, secondCaller)

	require.Nil(t, apiErr)
	assert.Equal(t, 2, secondCalls)
	secondLogOther, ok := common.GetContextKeyType[map[string]interface{}](secondContext, constant.ContextKeyLogOther)
	require.True(t, ok)
	assert.Equal(t, 1, secondLogOther["vision_assist_cache_hits"])
	assert.Equal(t, []int{1, 1, 1}, secondLogOther["vision_assist_batch_image_counts"])

	thirdRequest := newVisionAssistBatchCacheRequest(true)
	thirdInfo := newVisionAssistBatchCacheRelayInfo(thirdRequest, setting, "request-boundary-three")
	thirdContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	thirdCalls := 0
	thirdCaller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		thirdCalls++
		return []VisionAssistResult{{Text: "不应调用"}}, nil
	}

	apiErr = ApplyVisionAssist(thirdContext, thirdInfo, thirdCaller)

	require.Nil(t, apiErr)
	assert.Equal(t, 0, thirdCalls)
	thirdLogOther, ok := common.GetContextKeyType[map[string]interface{}](thirdContext, constant.ContextKeyLogOther)
	require.True(t, ok)
	assert.Equal(t, 3, thirdLogOther["vision_assist_cache_hits"])
}

func TestApplyVisionAssistCombinedBatchingKeepsOriginalImagesWhenConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, getVisionAssistCache().Purge())
	stripImage := false
	setting := dto.ChannelSettings{VisionAssist: dto.ChannelVisionAssistSettings{
		Enabled:           true,
		AssistChannelId:   49,
		AssistModel:       "vision-combined-keep-images-test",
		StripImage:        &stripImage,
		MultiImageMode:    VisionAssistMultiImageModeCombined,
		CombinedMaxImages: 2,
	}}
	request := newVisionAssistBatchCacheRequest(false)
	info := newVisionAssistBatchCacheRelayInfo(request, setting, "request-keep-images")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	caller := func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError) {
		return []VisionAssistResult{{Text: "识图结果"}}, nil
	}

	apiErr := ApplyVisionAssist(context, info, caller)

	require.Nil(t, apiErr)
	contents := request.Messages[0].ParseContent()
	imageURLs := make([]string, 0, 3)
	for _, content := range contents {
		if content.Type == dto.ContentTypeImageURL {
			imageURLs = append(imageURLs, content.GetImageMedia().Url)
		}
	}
	assert.Equal(t, []string{
		"data:image/png;base64,image-a",
		"data:image/png;base64,image-b",
		"data:image/png;base64,image-c",
	}, imageURLs)
}

func visionAssistUnitImageCounts(units []visionAssistUnit) []int {
	counts := make([]int, 0, len(units))
	for _, unit := range units {
		counts = append(counts, len(unit.Images))
	}
	return counts
}

func newVisionAssistBatchCacheRequest(shiftMessageIndex bool) *dto.GeneralOpenAIRequest {
	messages := make([]dto.Message, 0, 2)
	if shiftMessageIndex {
		messages = append(messages, dto.Message{Role: "assistant", Content: "历史回答"})
	}
	messages = append(messages, dto.Message{
		Role: "user",
		Content: []any{
			map[string]any{"type": "text", "text": "比较这些图片"},
			map[string]any{"type": "image_url", "image_url": "data:image/png;base64,image-a"},
			map[string]any{"type": "image_url", "image_url": "data:image/png;base64,image-b"},
			map[string]any{"type": "image_url", "image_url": "data:image/png;base64,image-c"},
		},
	})
	return &dto.GeneralOpenAIRequest{Model: "target", Messages: messages}
}

func newVisionAssistBatchCacheRelayInfo(request dto.Request, setting dto.ChannelSettings, requestID string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		Request:         request,
		RequestId:       requestID,
		OriginModelName: "target",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         3,
			UpstreamModelName: "target",
			ChannelSetting:    setting,
		},
	}
}
