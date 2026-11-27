package controller

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// legacyGoogleVideoURL 还原旧任务的结果，缺少已保存的媒体时通过当前任务插件查询一次。
func legacyGoogleVideoURL(c *gin.Context, ch *model.Channel, task *model.Task) (string, error) {
	if saved := strings.TrimSpace(task.GetResultURL()); saved != "" && !isTaskMediaFallbackLoop(saved, task.TaskID) {
		return saved, nil
	}
	parse := extractGeminiVideoURLFromPayload
	if ch.Type == constant.ChannelTypeVertexAi {
		parse = extractVertexVideoURLFromPayload
	}
	if saved := parse(task.Data); saved != "" {
		return saved, nil
	}
	common.SetContextKey(c, constant.ContextKeyChannelId, ch.Id)
	if err := service.ApplyChannelLimitContext(c, ch); err != nil {
		return "", err
	}
	guard, limitErr := acquireChannelUserConcurrencyGuard(c)
	if limitErr != nil {
		return "", limitErr
	}
	defer func() { _ = finishChannelUserConcurrencyGuard(c, guard, nil) }()
	adaptor := relay.GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(ch.Type)))
	if adaptor == nil {
		return "", errTaskArtifactPluginUnavailable
	}
	baseURL := ch.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.GetChannelBaseURL(ch.Type)
	}
	key := task.PrivateData.Key
	if key == "" && ch.Type == constant.ChannelTypeVertexAi {
		key = getVertexTaskKey(ch, task)
	}
	if key == "" {
		return "", fmt.Errorf("历史视频任务缺少上游凭据")
	}
	settings := ch.GetSetting()
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: ch.Type, ChannelId: ch.Id, ApiKey: key, ChannelBaseUrl: baseURL, ChannelSetting: settings, HeadersOverride: ch.GetHeaderOverride()}})
	resp, err := relaychannel.FetchTaskWithContext(c.Request.Context(), adaptor, baseURL, key, task, settings.Proxy)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("历史视频任务查询返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(taskMediaDataURLMaxEncodedBytes)+1))
	if lost := finishChannelUserConcurrencyGuard(c, guard, nil); lost != nil {
		guard = nil
		return "", lost
	}
	guard = nil
	if err != nil {
		return "", err
	}
	if len(body) > taskMediaDataURLMaxEncodedBytes {
		return "", errTaskMediaRequestRejected
	}
	if result := parse(body); result != "" {
		return result, nil
	}
	return "", fmt.Errorf("历史任务缺少视频结果")
}
func extractGeminiVideoURLFromPayload(body []byte) string {
	var payload map[string]any
	if err := common.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return extractGeminiVideoURLFromMap(payload)
}

func extractGeminiVideoURLFromMap(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if uri, ok := payload["uri"].(string); ok && uri != "" {
		return uri
	}
	if resp, ok := payload["response"].(map[string]any); ok {
		if uri := extractGeminiVideoURLFromResponse(resp); uri != "" {
			return uri
		}
	}
	return ""
}

func extractGeminiVideoURLFromResponse(resp map[string]any) string {
	if resp == nil {
		return ""
	}
	if gvr, ok := resp["generateVideoResponse"].(map[string]any); ok {
		if uri := extractGeminiVideoURLFromGeneratedSamples(gvr); uri != "" {
			return uri
		}
	}
	if videos, ok := resp["videos"].([]any); ok {
		for _, video := range videos {
			if vm, ok := video.(map[string]any); ok {
				if uri, ok := vm["uri"].(string); ok && uri != "" {
					return uri
				}
			}
		}
	}
	if uri, ok := resp["video"].(string); ok && uri != "" {
		return uri
	}
	if uri, ok := resp["uri"].(string); ok && uri != "" {
		return uri
	}
	return ""
}

func extractGeminiVideoURLFromGeneratedSamples(gvr map[string]any) string {
	if gvr == nil {
		return ""
	}
	for _, field := range []string{"generatedSamples", "generatedVideos"} {
		samples, _ := gvr[field].([]any)
		for _, sample := range samples {
			if sm, ok := sample.(map[string]any); ok {
				if video, ok := sm["video"].(map[string]any); ok {
					if uri, ok := video["uri"].(string); ok && uri != "" {
						return uri
					}
				}
			}
		}
	}
	return ""
}

func getVertexTaskKey(channel *model.Channel, task *model.Task) string {
	if task != nil {
		if key := strings.TrimSpace(task.PrivateData.Key); key != "" {
			return key
		}
	}
	if channel == nil {
		return ""
	}
	keys := channel.GetKeys()
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" {
			return key
		}
	}
	return strings.TrimSpace(channel.Key)
}

func extractVertexVideoURLFromPayload(body []byte) string {
	var payload map[string]any
	if err := common.Unmarshal(body, &payload); err != nil {
		return ""
	}
	resp, ok := payload["response"].(map[string]any)
	if !ok || resp == nil {
		return ""
	}

	if videos, ok := resp["videos"].([]any); ok && len(videos) > 0 {
		if video, ok := videos[0].(map[string]any); ok && video != nil {
			if b64, _ := video["bytesBase64Encoded"].(string); strings.TrimSpace(b64) != "" {
				mime, _ := video["mimeType"].(string)
				enc, _ := video["encoding"].(string)
				return buildVideoDataURL(mime, enc, b64)
			}
		}
	}
	if b64, _ := resp["bytesBase64Encoded"].(string); strings.TrimSpace(b64) != "" {
		enc, _ := resp["encoding"].(string)
		return buildVideoDataURL("", enc, b64)
	}
	if video, _ := resp["video"].(string); strings.TrimSpace(video) != "" {
		if strings.HasPrefix(video, "data:") || strings.HasPrefix(video, "http://") || strings.HasPrefix(video, "https://") {
			return video
		}
		enc, _ := resp["encoding"].(string)
		return buildVideoDataURL("", enc, video)
	}
	return ""
}

func buildVideoDataURL(mimeType string, encoding string, base64Data string) string {
	mime := strings.TrimSpace(mimeType)
	if mime == "" {
		enc := strings.TrimSpace(encoding)
		if enc == "" {
			enc = "mp4"
		}
		if strings.Contains(enc, "/") {
			mime = enc
		} else {
			mime = "video/" + enc
		}
	}
	return "data:" + mime + ";base64," + base64Data
}
