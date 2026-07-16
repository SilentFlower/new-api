package channel

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

var responsesMetadataHeaderNames = []string{
	"x-codex-beta-features",
	"x-codex-turn-state",
	"x-codex-turn-metadata",
	"x-codex-installation-id",
	"x-codex-window-id",
	"x-codex-parent-thread-id",
	"x-client-request-id",
	"originator",
	"user-agent",
}

var responsesMetadataHeaderAliases = [][2]string{
	{"session-id", "session_id"},
	{"thread-id", "thread_id"},
}

var responsesUpstreamStateHeaderNames = []string{
	"x-codex-turn-state",
	"x-codex-turn-metadata",
}

// CopyResponsesMetadataHeaders 复制 Responses Compact 允许透传的客户端元数据请求头。
// @param c 当前 Gin 请求上下文。
// @param target 即将发送给上游的请求头。
func CopyResponsesMetadataHeaders(c *gin.Context, target *http.Header) {
	if c == nil || c.Request == nil || target == nil {
		return
	}
	for _, name := range responsesMetadataHeaderNames {
		values := c.Request.Header.Values(name)
		if len(values) == 0 {
			continue
		}
		target.Del(name)
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				target.Add(name, value)
			}
		}
	}
	// Codex 官方客户端使用连字符，sub2api 历史入口使用下划线；统一成同一值并同时发送。
	for _, aliases := range responsesMetadataHeaderAliases {
		values := c.Request.Header.Values(aliases[0])
		if len(values) == 0 {
			values = c.Request.Header.Values(aliases[1])
		}
		if len(values) == 0 {
			continue
		}
		for _, name := range aliases {
			target.Del(name)
			for _, value := range values {
				if strings.TrimSpace(value) != "" {
					target.Add(name, value)
				}
			}
		}
	}
}

// CaptureResponsesMetadataHeaders 保存上游握手返回的 Responses turn 状态，供连接内后续重连使用。
// @param c 当前 Gin 请求上下文。
// @param source 上游 WebSocket 握手响应头。
func CaptureResponsesMetadataHeaders(c *gin.Context, source http.Header) {
	if c == nil || c.Request == nil || source == nil {
		return
	}
	for _, name := range responsesUpstreamStateHeaderNames {
		values := source.Values(name)
		if len(values) == 0 {
			continue
		}
		c.Request.Header.Del(name)
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				c.Request.Header.Add(name, value)
			}
		}
	}
}
