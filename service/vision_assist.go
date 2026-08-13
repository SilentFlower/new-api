package service

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/cachex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
)

const (
	VisionAssistFailurePolicyError = "error"
	VisionAssistFailurePolicySkip  = "skip"

	VisionAssistEndpointModeAuto              = "auto"
	VisionAssistEndpointModeOpenAIChat        = "openai_chat"
	VisionAssistEndpointModeOpenAIResponses   = "openai_responses"
	VisionAssistEndpointModeAnthropicMessages = "anthropic_messages"
	VisionAssistEndpointModeGeminiNative      = "gemini_native"

	defaultVisionAssistPrompt           = "请客观描述图片内容，保留图片中的文字、表格、关键对象、空间关系和可能影响回答的细节。"
	defaultVisionAssistCacheTTLSeconds  = 86400
	visionAssistCacheCapacity           = 4096
	visionAssistInjectedTextHeader      = "[图片内容]"
	visionAssistInjectedTextInstruction = "以下内容是当前用户消息中图片的可见信息，请直接用于回答用户。"
	defaultVisionAssistRetryBackoffMs   = 500
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

type visionAssistExecutionStats struct {
	EndpointMode         string
	ResolvedEndpointMode string
	MaxConcurrency       int
	RetryCount           int
	RetryBackoffMs       int
	RetryAttempts        int
	FailedImageCount     int
	LastErrorCode        string
	LastError            string
}

type visionAssistImageAttemptResult struct {
	image                VisionAssistImage
	results              []VisionAssistResult
	err                  *types.NewAPIError
	retryAttempts        int
	resolvedEndpointMode string
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
	stats := visionAssistExecutionStats{
		EndpointMode:   normalizedVisionAssistEndpointMode(setting),
		MaxConcurrency: normalizedVisionAssistMaxConcurrency(setting),
		RetryCount:     normalizedVisionAssistRetryCount(setting),
		RetryBackoffMs: normalizedVisionAssistRetryBackoff(setting),
	}
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

	if len(missing) > 0 {
		executionResults := executeVisionAssistMissingImages(c, info, setting, prompt, missing, caller)
		var firstErr *types.NewAPIError
		for _, item := range executionResults {
			if stats.ResolvedEndpointMode == "" && item.resolvedEndpointMode != "" {
				stats.ResolvedEndpointMode = item.resolvedEndpointMode
			}
			stats.RetryAttempts += item.retryAttempts
			if item.err != nil {
				cacheKey := buildVisionAssistCacheKey(setting, prompt, item.image)
				stats.FailedImageCount += len(missingByCacheKey[cacheKey])
				if firstErr == nil {
					firstErr = item.err
				}
				stats.LastErrorCode = string(item.err.GetErrorCode())
				stats.LastError = common.LocalLogPreview(item.err.Error())
				continue
			}
			for _, result := range item.results {
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
		if firstErr != nil && normalizedVisionAssistFailurePolicy(setting) != VisionAssistFailurePolicySkip {
			mergeVisionAssistLogOther(c, buildVisionAssistFailureLogOther(info, setting, "assist_call_failed", firstErr.Error(), stats))
			return firstErr
		}
		if firstErr != nil {
			logger.LogWarn(c, "视觉辅助部分图片失败，按配置跳过: "+firstErr.Error())
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Image.Index < results[j].Image.Index
	})

	if len(results) == 0 {
		if stats.FailedImageCount > 0 {
			mergeVisionAssistLogOther(c, buildVisionAssistFailureLogOther(info, setting, "assist_call_failed", stats.LastError, stats))
			return nil
		}
		return nil
	}
	mergeVisionAssistLogOther(c, buildVisionAssistSuccessLogOther(info, setting, results, stats))
	if err := rewriteVisionAssistRequest(request, results, shouldStripVisionAssistImage(setting)); err != nil {
		mergeVisionAssistLogOther(c, buildVisionAssistFailureLogOther(info, setting, "rewrite_failed", err.Error(), stats))
		if normalizedVisionAssistFailurePolicy(setting) == VisionAssistFailurePolicySkip {
			logger.LogWarn(c, "视觉辅助改写失败，按配置跳过: "+err.Error())
			return nil
		}
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	info.Request = request
	return nil
}

func executeVisionAssistMissingImages(c *gin.Context, info *relaycommon.RelayInfo, setting dto.ChannelVisionAssistSettings, prompt string, missing []VisionAssistImage, caller VisionAssistCaller) []visionAssistImageAttemptResult {
	results := make([]visionAssistImageAttemptResult, len(missing))
	maxConcurrency := normalizedVisionAssistMaxConcurrency(setting)
	if maxConcurrency > len(missing) {
		maxConcurrency = len(missing)
	}
	if maxConcurrency <= 1 {
		for i, image := range missing {
			results[i] = executeVisionAssistImageWithRetry(c, info, setting, prompt, image, caller)
		}
		return results
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < maxConcurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				results[index] = executeVisionAssistImageWithRetry(c, info, setting, prompt, missing[index], caller)
			}
		}()
	}
	for i := range missing {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return results
}

func executeVisionAssistImageWithRetry(c *gin.Context, info *relaycommon.RelayInfo, setting dto.ChannelVisionAssistSettings, prompt string, image VisionAssistImage, caller VisionAssistCaller) visionAssistImageAttemptResult {
	retryCount := normalizedVisionAssistRetryCount(setting)
	backoff := normalizedVisionAssistRetryBackoff(setting)
	var lastErr *types.NewAPIError
	retryAttempts := 0
	resolvedEndpointMode := ""
	for attempt := 0; attempt <= retryCount; attempt++ {
		if attempt > 0 {
			retryAttempts++
		}
		assistRequest := buildVisionAssistRequest(setting, prompt, []VisionAssistImage{image})
		attemptCtx := cloneVisionAssistContext(c)
		newResults, apiErr := caller(attemptCtx, info, assistRequest, []VisionAssistImage{image})
		if resolved := common.GetContextKeyString(attemptCtx, constant.ContextKeyVisionAssistEndpointMode); resolved != "" {
			resolvedEndpointMode = resolved
		}
		if apiErr == nil {
			for i := range newResults {
				newResults[i].Text = strings.TrimSpace(newResults[i].Text)
			}
			return visionAssistImageAttemptResult{image: image, results: newResults, retryAttempts: retryAttempts, resolvedEndpointMode: resolvedEndpointMode}
		}
		lastErr = apiErr
		if attempt >= retryCount || !isRetriableVisionAssistError(apiErr) {
			break
		}
		if !sleepVisionAssistRetry(c, time.Duration(backoff*(attempt+1))*time.Millisecond) {
			break
		}
	}
	return visionAssistImageAttemptResult{image: image, err: lastErr, retryAttempts: retryAttempts, resolvedEndpointMode: resolvedEndpointMode}
}

func cloneVisionAssistContext(c *gin.Context) *gin.Context {
	if c == nil {
		return nil
	}
	return c.Copy()
}

func sleepVisionAssistRetry(c *gin.Context, duration time.Duration) bool {
	if duration <= 0 {
		return true
	}
	var ctx context.Context
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	} else {
		ctx = context.Background()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func isRetriableVisionAssistError(apiErr *types.NewAPIError) bool {
	if apiErr == nil {
		return false
	}
	if types.IsSkipRetryError(apiErr) {
		return false
	}
	if apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode == http.StatusRequestTimeout {
		return true
	}
	if apiErr.StatusCode >= http.StatusBadRequest && apiErr.StatusCode < http.StatusInternalServerError {
		return false
	}
	if apiErr.StatusCode >= http.StatusInternalServerError && apiErr.StatusCode <= 599 {
		return true
	}
	switch apiErr.GetErrorCode() {
	case types.ErrorCodeDoRequestFailed, types.ErrorCodeReadResponseBodyFailed, types.ErrorCodeEmptyResponse, types.ErrorCodeBadResponse:
		return true
	default:
		return false
	}
}

func buildVisionAssistSuccessLogOther(info *relaycommon.RelayInfo, setting dto.ChannelVisionAssistSettings, results []VisionAssistResult, stats visionAssistExecutionStats) map[string]interface{} {
	fields := map[string]interface{}{
		"vision_assist_applied":        true,
		"vision_assist_cache_hits":     countVisionAssistCacheHits(results),
		"vision_assist_reused_hits":    countVisionAssistReusedHits(results),
		"vision_assist_image_count":    len(results),
		"vision_assist_channel_id":     setting.AssistChannelId,
		"vision_assist_model":          strings.TrimSpace(setting.AssistModel),
		"vision_assist_target_channel": 0,
	}
	mergeVisionAssistStats(fields, stats)
	if info != nil {
		fields["vision_assist_target_channel"] = info.ChannelId
		fields["vision_assist_target_model"] = info.OriginModelName
		fields["vision_assist_upstream_model"] = info.UpstreamModelName
	}
	return fields
}

func mergeVisionAssistStats(fields map[string]interface{}, stats visionAssistExecutionStats) {
	fields["vision_assist_endpoint_mode"] = stats.EndpointMode
	fields["vision_assist_resolved_endpoint_mode"] = stats.ResolvedEndpointMode
	fields["vision_assist_max_concurrency"] = stats.MaxConcurrency
	fields["vision_assist_retry_count"] = stats.RetryCount
	fields["vision_assist_retry_backoff_ms"] = stats.RetryBackoffMs
	fields["vision_assist_retry_attempts"] = stats.RetryAttempts
	fields["vision_assist_failed_image_count"] = stats.FailedImageCount
	if stats.LastErrorCode != "" {
		fields["vision_assist_last_error_code"] = stats.LastErrorCode
	}
	if stats.LastError != "" {
		fields["vision_assist_last_error"] = stats.LastError
	}
}

func buildVisionAssistFailureLogOther(info *relaycommon.RelayInfo, setting dto.ChannelVisionAssistSettings, reason string, message string, stats visionAssistExecutionStats) map[string]interface{} {
	fields := map[string]interface{}{
		"vision_assist_applied":        false,
		"vision_assist_failure_reason": reason,
		"vision_assist_failure_policy": normalizedVisionAssistFailurePolicy(setting),
		"vision_assist_channel_id":     setting.AssistChannelId,
		"vision_assist_model":          strings.TrimSpace(setting.AssistModel),
	}
	mergeVisionAssistStats(fields, stats)
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

func normalizedVisionAssistEndpointMode(setting dto.ChannelVisionAssistSettings) string {
	mode := strings.ToLower(strings.TrimSpace(setting.EndpointMode))
	switch mode {
	case VisionAssistEndpointModeOpenAIChat,
		VisionAssistEndpointModeOpenAIResponses,
		VisionAssistEndpointModeAnthropicMessages,
		VisionAssistEndpointModeGeminiNative:
		return mode
	default:
		return VisionAssistEndpointModeAuto
	}
}

func normalizedVisionAssistMaxConcurrency(setting dto.ChannelVisionAssistSettings) int {
	if setting.MaxConcurrency <= 0 {
		return 1
	}
	if setting.MaxConcurrency > 8 {
		return 8
	}
	return setting.MaxConcurrency
}

func normalizedVisionAssistRetryCount(setting dto.ChannelVisionAssistSettings) int {
	if setting.RetryCount < 0 {
		return 0
	}
	if setting.RetryCount > 5 {
		return 5
	}
	return setting.RetryCount
}

func normalizedVisionAssistRetryBackoff(setting dto.ChannelVisionAssistSettings) int {
	if setting.RetryBackoffMs <= 0 {
		return defaultVisionAssistRetryBackoffMs
	}
	if setting.RetryBackoffMs > 30000 {
		return 30000
	}
	return setting.RetryBackoffMs
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
	case *dto.OpenAIResponsesRequest:
		return extractOpenAIResponsesVisionAssistImages(req)
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

func extractOpenAIResponsesVisionAssistImages(request *dto.OpenAIResponsesRequest) []VisionAssistImage {
	if request == nil || len(request.Input) == 0 || common.GetJsonType(request.Input) != "array" {
		return nil
	}
	var inputItems []any
	if err := common.Unmarshal(request.Input, &inputItems); err != nil {
		return nil
	}
	images := make([]VisionAssistImage, 0)
	for messageIndex, inputItemAny := range inputItems {
		inputItem, ok := inputItemAny.(map[string]any)
		if !ok {
			continue
		}
		contentKey := "content"
		switch common.Interface2String(inputItem["type"]) {
		case "function_call_output", "custom_tool_call_output":
			contentKey = "output"
		}
		contentItems, ok := inputItem[contentKey].([]any)
		if !ok {
			continue
		}
		for _, contentItemAny := range contentItems {
			contentItem, ok := contentItemAny.(map[string]any)
			if !ok || common.Interface2String(contentItem["type"]) != "input_image" {
				continue
			}
			imageURL, detail, mimeType := parseOpenAIResponsesVisionImage(contentItem)
			if imageURL == "" {
				continue
			}
			images = append(images, VisionAssistImage{
				Index:        len(images) + 1,
				MessageIndex: messageIndex,
				Source:       types.NewFileSourceFromData(imageURL, mimeType),
				Detail:       detail,
				MimeType:     mimeType,
			})
		}
	}
	return images
}

func parseOpenAIResponsesVisionImage(contentItem map[string]any) (imageURL string, detail string, mimeType string) {
	detail = common.Interface2String(contentItem["detail"])
	mimeType = common.Interface2String(contentItem["mime_type"])
	switch value := contentItem["image_url"].(type) {
	case string:
		imageURL = value
	case map[string]any:
		imageURL = common.Interface2String(value["url"])
		if detail == "" {
			detail = common.Interface2String(value["detail"])
		}
		if mimeType == "" {
			mimeType = common.Interface2String(value["mime_type"])
		}
	}
	if detail == "" {
		detail = "high"
	}
	return imageURL, detail, mimeType
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
	message := dto.Message{
		Role: "user",
	}
	message.SetMediaContent(content)
	return &dto.GeneralOpenAIRequest{
		Model:    strings.TrimSpace(setting.AssistModel),
		Stream:   &stream,
		Messages: []dto.Message{message},
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
	case *dto.OpenAIResponsesRequest:
		return rewriteOpenAIResponsesVisionAssistRequest(req, results, stripImage)
	case *dto.ClaudeRequest:
		return rewriteClaudeVisionAssistRequest(req, results, stripImage)
	default:
		return nil
	}
}

func visionAssistText(results []VisionAssistResult) string {
	lines := []string{visionAssistInjectedTextHeader, visionAssistInjectedTextInstruction}
	for _, result := range results {
		if strings.TrimSpace(result.Text) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("图片 %d：%s", result.Image.Index, strings.TrimSpace(result.Text)))
	}
	if len(lines) == 2 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func rewriteOpenAIVisionAssistRequest(request *dto.GeneralOpenAIRequest, results []VisionAssistResult, stripImage bool) error {
	byMessage := groupVisionAssistResultsByMessage(results)
	for i := range request.Messages {
		messageResults := byMessage[i]
		text := visionAssistText(messageResults)
		if text == "" {
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

func rewriteOpenAIResponsesVisionAssistRequest(request *dto.OpenAIResponsesRequest, results []VisionAssistResult, stripImage bool) error {
	if request == nil || len(request.Input) == 0 || common.GetJsonType(request.Input) != "array" {
		return nil
	}
	var inputItems []any
	if err := common.Unmarshal(request.Input, &inputItems); err != nil {
		return err
	}
	byMessage := groupVisionAssistResultsByMessage(results)
	rewrittenItems := make([]any, 0, len(inputItems))
	changed := false
	for i, inputItemAny := range inputItems {
		inputItem, ok := inputItemAny.(map[string]any)
		if !ok {
			rewrittenItems = append(rewrittenItems, inputItemAny)
			continue
		}
		text := visionAssistText(byMessage[i])
		if text == "" {
			rewrittenItems = append(rewrittenItems, inputItemAny)
			continue
		}
		itemType := common.Interface2String(inputItem["type"])
		contentKey := "content"
		customToolOutput := false
		switch itemType {
		case "function_call_output":
			contentKey = "output"
		case "custom_tool_call_output":
			contentKey = "output"
			customToolOutput = true
		}
		contentItems, ok := inputItem[contentKey].([]any)
		if !ok {
			rewrittenItems = append(rewrittenItems, inputItemAny)
			continue
		}
		next := make([]any, 0, len(contentItems)+1)
		foundImage := false
		for _, contentItemAny := range contentItems {
			contentItem, ok := contentItemAny.(map[string]any)
			if ok && common.Interface2String(contentItem["type"]) == "input_image" {
				if !foundImage && !customToolOutput {
					next = append(next, map[string]any{
						"type": "input_text",
						"text": text,
					})
				}
				foundImage = true
				if stripImage {
					continue
				}
			}
			next = append(next, contentItemAny)
		}
		if !foundImage {
			rewrittenItems = append(rewrittenItems, inputItemAny)
			continue
		}
		inputItem[contentKey] = next
		rewrittenItems = append(rewrittenItems, inputItem)
		if customToolOutput {
			rewrittenItems = append(rewrittenItems, map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "input_text",
						"text": text,
					},
				},
			})
		}
		changed = true
	}
	if !changed {
		return nil
	}
	input, err := common.Marshal(rewrittenItems)
	if err != nil {
		return err
	}
	request.Input = input
	return nil
}

func rewriteClaudeVisionAssistRequest(request *dto.ClaudeRequest, results []VisionAssistResult, stripImage bool) error {
	byMessage := groupVisionAssistResultsByMessage(results)
	for i := range request.Messages {
		messageResults := byMessage[i]
		text := visionAssistText(messageResults)
		if text == "" {
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
