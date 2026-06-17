package service

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/cachex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
)

const (
	VisionAssistFailurePolicyError = "error"
	VisionAssistFailurePolicySkip  = "skip"

	defaultVisionAssistPrompt          = "请客观描述图片内容，保留图片中的文字、表格、关键对象、空间关系和可能影响回答的细节。"
	defaultVisionAssistCacheTTLSeconds = 86400
	visionAssistCacheCapacity          = 4096
)

// VisionAssistCaller 调用实际视觉辅助模型，并返回每张图片的文字识别结果。
type VisionAssistCaller func(ctx *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest, images []VisionAssistImage) ([]VisionAssistResult, *types.NewAPIError)

// VisionAssistImage 描述从用户请求中抽取出的单张图片及其定位信息。
type VisionAssistImage struct {
	Index        int
	MessageIndex int
	Source       types.FileSource
	Detail       string
	MimeType     string
}

// VisionAssistResult 表示单张图片的辅助识别结果。
type VisionAssistResult struct {
	Image    VisionAssistImage
	Text     string
	CacheHit bool
	Reused   bool
}

type visionAssistCacheValue struct {
	Text string `json:"text"`
}

var (
	visionAssistCache     *cachex.HybridCache[visionAssistCacheValue]
	visionAssistCacheOnce sync.Once
)

// ApplyVisionAssist 根据渠道配置对含图片请求执行辅助识别并改写请求。
func ApplyVisionAssist(c *gin.Context, info *relaycommon.RelayInfo, caller VisionAssistCaller) *types.NewAPIError {
	if c == nil || info == nil || info.ChannelMeta == nil || caller == nil {
		return nil
	}
	setting := info.ChannelSetting.VisionAssist
	if !shouldApplyVisionAssist(c, info, setting) {
		return nil
	}

	request := info.Request
	images := extractVisionAssistImages(request)
	if len(images) == 0 {
		return nil
	}

	prompt := normalizedVisionAssistPrompt(setting)
	ttl := normalizedVisionAssistTTL(setting)
	results := make([]VisionAssistResult, 0, len(images))
	missing := make([]VisionAssistImage, 0, len(images))
	requestCache := map[string]string{}
	missingByCacheKey := map[string][]VisionAssistImage{}

	for _, image := range images {
		cacheKey := buildVisionAssistCacheKey(setting, prompt, image)
		if text, ok := requestCache[cacheKey]; ok {
			results = append(results, VisionAssistResult{Image: image, Text: text, CacheHit: true})
			continue
		}
		if cached, found, err := getVisionAssistCache().Get(cacheKey); err == nil && found && strings.TrimSpace(cached.Text) != "" {
			requestCache[cacheKey] = cached.Text
			results = append(results, VisionAssistResult{Image: image, Text: cached.Text, CacheHit: true})
			continue
		} else if err != nil {
			logger.LogWarn(c, "读取视觉辅助缓存失败: "+err.Error())
		}
		if duplicatedMissing, ok := missingByCacheKey[cacheKey]; ok {
			missingByCacheKey[cacheKey] = append(duplicatedMissing, image)
			continue
		}
		missingByCacheKey[cacheKey] = []VisionAssistImage{image}
		missing = append(missing, image)
	}

	for _, image := range missing {
		assistRequest := buildVisionAssistRequest(setting, prompt, []VisionAssistImage{image})
		newResults, apiErr := caller(c, info, assistRequest, []VisionAssistImage{image})
		if apiErr != nil {
			mergeVisionAssistLogOther(c, buildVisionAssistFailureLogOther(info, setting, "assist_call_failed", apiErr.Error()))
			if normalizedVisionAssistFailurePolicy(setting) == VisionAssistFailurePolicySkip {
				logger.LogWarn(c, "视觉辅助失败，按配置跳过: "+apiErr.Error())
				return nil
			}
			return apiErr
		}
		for _, result := range newResults {
			result.Text = strings.TrimSpace(result.Text)
			if result.Text == "" {
				continue
			}
			cacheKey := buildVisionAssistCacheKey(setting, prompt, result.Image)
			requestCache[cacheKey] = result.Text
			results = append(results, result)
			for _, duplicatedImage := range missingByCacheKey[cacheKey][1:] {
				results = append(results, VisionAssistResult{
					Image:  duplicatedImage,
					Text:   result.Text,
					Reused: true,
				})
			}
			if err := getVisionAssistCache().SetWithTTL(cacheKey, visionAssistCacheValue{Text: result.Text}, ttl); err != nil {
				logger.LogWarn(c, "写入视觉辅助缓存失败: "+err.Error())
			}
		}
	}

	if len(results) == 0 {
		return nil
	}
	mergeVisionAssistLogOther(c, map[string]interface{}{
		"vision_assist_applied":        true,
		"vision_assist_cache_hits":     countVisionAssistCacheHits(results),
		"vision_assist_reused_hits":    countVisionAssistReusedHits(results),
		"vision_assist_image_count":    len(results),
		"vision_assist_channel_id":     setting.AssistChannelId,
		"vision_assist_model":          strings.TrimSpace(setting.AssistModel),
		"vision_assist_target_channel": info.ChannelId,
		"vision_assist_target_model":   info.OriginModelName,
		"vision_assist_upstream_model": info.UpstreamModelName,
	})
	if err := rewriteVisionAssistRequest(request, results, shouldStripVisionAssistImage(setting)); err != nil {
		mergeVisionAssistLogOther(c, buildVisionAssistFailureLogOther(info, setting, "rewrite_failed", err.Error()))
		if normalizedVisionAssistFailurePolicy(setting) == VisionAssistFailurePolicySkip {
			logger.LogWarn(c, "视觉辅助改写失败，按配置跳过: "+err.Error())
			return nil
		}
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	info.Request = request
	return nil
}

func buildVisionAssistFailureLogOther(info *relaycommon.RelayInfo, setting dto.ChannelVisionAssistSettings, reason string, message string) map[string]interface{} {
	fields := map[string]interface{}{
		"vision_assist_applied":        false,
		"vision_assist_failure_reason": reason,
		"vision_assist_failure_policy": normalizedVisionAssistFailurePolicy(setting),
		"vision_assist_channel_id":     setting.AssistChannelId,
		"vision_assist_model":          strings.TrimSpace(setting.AssistModel),
	}
	if message != "" {
		fields["vision_assist_error"] = common.LocalLogPreview(message)
	}
	if info != nil {
		fields["vision_assist_target_channel"] = info.ChannelId
		fields["vision_assist_target_model"] = info.OriginModelName
		fields["vision_assist_upstream_model"] = info.UpstreamModelName
	}
	return fields
}

func mergeVisionAssistLogOther(c *gin.Context, fields map[string]interface{}) {
	if c == nil || len(fields) == 0 {
		return
	}
	logOther, _ := common.GetContextKeyType[map[string]interface{}](c, constant.ContextKeyLogOther)
	if logOther == nil {
		logOther = map[string]interface{}{}
	}
	for key, value := range fields {
		logOther[key] = value
	}
	common.SetContextKey(c, constant.ContextKeyLogOther, logOther)
}

func countVisionAssistCacheHits(results []VisionAssistResult) int {
	count := 0
	for _, result := range results {
		if result.CacheHit {
			count++
		}
	}
	return count
}

func countVisionAssistReusedHits(results []VisionAssistResult) int {
	count := 0
	for _, result := range results {
		if result.Reused {
			count++
		}
	}
	return count
}

func getVisionAssistCache() *cachex.HybridCache[visionAssistCacheValue] {
	visionAssistCacheOnce.Do(func() {
		visionAssistCache = cachex.NewHybridCache[visionAssistCacheValue](cachex.HybridCacheConfig[visionAssistCacheValue]{
			Namespace:  cachex.Namespace("vision_assist:v1"),
			Redis:      common.RDB,
			RedisCodec: cachex.JSONCodec[visionAssistCacheValue]{},
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			Memory: func() *hot.HotCache[string, visionAssistCacheValue] {
				return hot.NewHotCache[string, visionAssistCacheValue](hot.LRU, visionAssistCacheCapacity).Build()
			},
		})
	})
	return visionAssistCache
}

func shouldApplyVisionAssist(c *gin.Context, info *relaycommon.RelayInfo, setting dto.ChannelVisionAssistSettings) bool {
	if !setting.Enabled {
		return false
	}
	if common.GetContextKeyBool(c, constant.ContextKeyVisionAssistProcessing) {
		return false
	}
	if setting.AssistChannelId <= 0 || strings.TrimSpace(setting.AssistModel) == "" {
		return false
	}
	if model_setting := info.ChannelSetting; model_setting.PassThroughBodyEnabled {
		return false
	}
	model := strings.TrimSpace(info.UpstreamModelName)
	if model == "" {
		model = strings.TrimSpace(info.OriginModelName)
	}
	if len(setting.TargetModels) == 0 {
		return true
	}
	for _, item := range setting.TargetModels {
		if strings.EqualFold(strings.TrimSpace(item), model) {
			return true
		}
	}
	return false
}

func normalizedVisionAssistPrompt(setting dto.ChannelVisionAssistSettings) string {
	prompt := strings.TrimSpace(setting.Prompt)
	if prompt == "" {
		return defaultVisionAssistPrompt
	}
	return prompt
}

func normalizedVisionAssistTTL(setting dto.ChannelVisionAssistSettings) time.Duration {
	seconds := setting.CacheTTLSeconds
	if seconds <= 0 {
		seconds = defaultVisionAssistCacheTTLSeconds
	}
	return time.Duration(seconds) * time.Second
}

func normalizedVisionAssistFailurePolicy(setting dto.ChannelVisionAssistSettings) string {
	policy := strings.ToLower(strings.TrimSpace(setting.FailurePolicy))
	switch policy {
	case VisionAssistFailurePolicySkip:
		return VisionAssistFailurePolicySkip
	default:
		return VisionAssistFailurePolicyError
	}
}

func shouldStripVisionAssistImage(setting dto.ChannelVisionAssistSettings) bool {
	if setting.StripImage == nil {
		return true
	}
	return *setting.StripImage
}

func extractVisionAssistImages(request dto.Request) []VisionAssistImage {
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		return extractOpenAIVisionAssistImages(req)
	case *dto.ClaudeRequest:
		return extractClaudeVisionAssistImages(req)
	default:
		return nil
	}
}

func extractOpenAIVisionAssistImages(request *dto.GeneralOpenAIRequest) []VisionAssistImage {
	images := make([]VisionAssistImage, 0)
	for i := range request.Messages {
		contents := request.Messages[i].ParseContent()
		for _, content := range contents {
			if content.Type != dto.ContentTypeImageURL {
				continue
			}
			image := content.GetImageMedia()
			if image == nil || image.Url == "" {
				continue
			}
			images = append(images, VisionAssistImage{
				Index:        len(images) + 1,
				MessageIndex: i,
				Source:       types.NewFileSourceFromData(image.Url, image.MimeType),
				Detail:       image.Detail,
				MimeType:     image.MimeType,
			})
		}
	}
	return images
}

func extractClaudeVisionAssistImages(request *dto.ClaudeRequest) []VisionAssistImage {
	images := make([]VisionAssistImage, 0)
	for i := range request.Messages {
		if request.Messages[i].IsStringContent() {
			continue
		}
		contents, _ := request.Messages[i].ParseContent()
		for _, content := range contents {
			if content.Type != "image" {
				continue
			}
			source := content.ToFileSource()
			if source == nil {
				continue
			}
			mimeType := ""
			if content.Source != nil {
				mimeType = content.Source.MediaType
			}
			images = append(images, VisionAssistImage{
				Index:        len(images) + 1,
				MessageIndex: i,
				Source:       source,
				MimeType:     mimeType,
			})
		}
	}
	return images
}

func buildVisionAssistRequest(setting dto.ChannelVisionAssistSettings, prompt string, images []VisionAssistImage) *dto.GeneralOpenAIRequest {
	stream := false
	content := []dto.MediaContent{{
		Type: dto.ContentTypeText,
		Text: prompt,
	}}
	for _, image := range images {
		content = append(content, dto.MediaContent{
			Type: dto.ContentTypeText,
			Text: fmt.Sprintf("图片 %d：", image.Index),
		})
		content = append(content, dto.MediaContent{
			Type: dto.ContentTypeImageURL,
			ImageUrl: &dto.MessageImageUrl{
				Url:      visionAssistImageURL(image),
				Detail:   image.Detail,
				MimeType: image.MimeType,
			},
		})
	}
	return &dto.GeneralOpenAIRequest{
		Model:  strings.TrimSpace(setting.AssistModel),
		Stream: &stream,
		Messages: []dto.Message{{
			Role:    "user",
			Content: content,
		}},
	}
}

func buildVisionAssistCacheKey(setting dto.ChannelVisionAssistSettings, prompt string, image VisionAssistImage) string {
	sourceHash := ""
	if image.Source != nil {
		sourceHash = stableVisionAssistHash(image.Source.GetRawData())
	}
	parts := []string{
		fmt.Sprintf("channel:%d", setting.AssistChannelId),
		"model:" + strings.TrimSpace(setting.AssistModel),
		"prompt:" + stableVisionAssistHash(prompt),
		"source:" + sourceHash,
		"detail:" + strings.TrimSpace(image.Detail),
		"mime:" + strings.TrimSpace(image.MimeType),
	}
	return stableVisionAssistHash(strings.Join(parts, "|"))
}

func stableVisionAssistHash(value string) string {
	return hex.EncodeToString(common.Sha256Raw([]byte(value)))
}

func visionAssistImageURL(image VisionAssistImage) string {
	if image.Source == nil {
		return ""
	}
	raw := image.Source.GetRawData()
	if image.Source.IsURL() || strings.HasPrefix(raw, "data:") {
		return raw
	}
	mimeType := strings.TrimSpace(image.MimeType)
	if mimeType == "" {
		mimeType = "image/png"
	}
	return "data:" + mimeType + ";base64," + raw
}

func rewriteVisionAssistRequest(request dto.Request, results []VisionAssistResult, stripImage bool) error {
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		return rewriteOpenAIVisionAssistRequest(req, results, stripImage)
	case *dto.ClaudeRequest:
		return rewriteClaudeVisionAssistRequest(req, results, stripImage)
	default:
		return nil
	}
}

func visionAssistText(results []VisionAssistResult) string {
	lines := []string{"[图片辅助识别结果]"}
	for _, result := range results {
		if strings.TrimSpace(result.Text) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("图片 %d：%s", result.Image.Index, strings.TrimSpace(result.Text)))
	}
	return strings.Join(lines, "\n")
}

func rewriteOpenAIVisionAssistRequest(request *dto.GeneralOpenAIRequest, results []VisionAssistResult, stripImage bool) error {
	byMessage := groupVisionAssistResultsByMessage(results)
	for i := range request.Messages {
		messageResults := byMessage[i]
		text := visionAssistText(messageResults)
		if strings.TrimSpace(text) == "[图片辅助识别结果]" {
			continue
		}
		contents := request.Messages[i].ParseContent()
		if len(contents) == 0 {
			continue
		}
		next := make([]dto.MediaContent, 0, len(contents)+1)
		inserted := false
		for _, content := range contents {
			if content.Type == dto.ContentTypeImageURL {
				if !inserted {
					next = append(next, dto.MediaContent{Type: dto.ContentTypeText, Text: text})
					inserted = true
				}
				if stripImage {
					continue
				}
			}
			next = append(next, content)
		}
		if inserted {
			request.Messages[i].SetMediaContent(next)
		}
	}
	return nil
}

func rewriteClaudeVisionAssistRequest(request *dto.ClaudeRequest, results []VisionAssistResult, stripImage bool) error {
	byMessage := groupVisionAssistResultsByMessage(results)
	for i := range request.Messages {
		messageResults := byMessage[i]
		text := visionAssistText(messageResults)
		if strings.TrimSpace(text) == "[图片辅助识别结果]" {
			continue
		}
		if request.Messages[i].IsStringContent() {
			continue
		}
		contents, err := request.Messages[i].ParseContent()
		if err != nil {
			return err
		}
		next := make([]dto.ClaudeMediaMessage, 0, len(contents)+1)
		inserted := false
		for _, content := range contents {
			if content.Type == "image" {
				if !inserted {
					textBlock := dto.ClaudeMediaMessage{Type: dto.ContentTypeText}
					textBlock.SetText(text)
					next = append(next, textBlock)
					inserted = true
				}
				if stripImage {
					continue
				}
			}
			next = append(next, content)
		}
		if inserted {
			request.Messages[i].SetContent(next)
		}
	}
	return nil
}

func groupVisionAssistResultsByMessage(results []VisionAssistResult) map[int][]VisionAssistResult {
	grouped := make(map[int][]VisionAssistResult)
	for _, result := range results {
		grouped[result.Image.MessageIndex] = append(grouped[result.Image.MessageIndex], result)
	}
	for messageIndex := range grouped {
		sort.Slice(grouped[messageIndex], func(i, j int) bool {
			return grouped[messageIndex][i].Image.Index < grouped[messageIndex][j].Image.Index
		})
	}
	return grouped
}
