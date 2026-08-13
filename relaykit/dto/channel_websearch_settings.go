package dto

import (
	"fmt"
	"strings"
)

const (
	// ChannelWebSearchProviderTavily 表示使用 Tavily Search API 作为渠道 WebSearch 供应商。
	ChannelWebSearchProviderTavily = "tavily"
	// ChannelWebSearchProviderAnySearch 表示使用 AnySearch MCP search 工具作为渠道 WebSearch 供应商。
	ChannelWebSearchProviderAnySearch = "anysearch"
)

const (
	channelWebSearchDefaultMaxResults = 5
	channelWebSearchMaxResultsLimit   = 20
	channelWebSearchDefaultDepth      = "basic"
)

var channelWebSearchFreshnessValues = map[string]bool{
	"day":   true,
	"week":  true,
	"month": true,
	"year":  true,
}

var channelWebSearchContentTypes = map[string]bool{
	"web":      true,
	"news":     true,
	"code":     true,
	"doc":      true,
	"academic": true,
	"data":     true,
	"image":    true,
	"video":    true,
	"audio":    true,
}

// ChannelWebSearchSettings 描述渠道级 Claude Code WebSearch 模拟配置。
type ChannelWebSearchSettings struct {
	Enabled          bool     `json:"enabled,omitempty"`
	Provider         string   `json:"provider,omitempty"`
	APIKey           string   `json:"api_key,omitempty"`
	APIKeyConfigured bool     `json:"api_key_configured,omitempty"`
	ClearAPIKey      bool     `json:"clear_api_key,omitempty"`
	MaxResults       int      `json:"max_results,omitempty"`
	SearchDepth      string   `json:"search_depth,omitempty"`
	Freshness        string   `json:"freshness,omitempty"`
	ContentTypes     []string `json:"content_types,omitempty"`
}

// Normalize 归一化渠道 WebSearch 配置，填充默认值并裁剪不合法的可选参数。
func (s *ChannelWebSearchSettings) Normalize() {
	if s == nil {
		return
	}
	s.Provider = normalizeChannelWebSearchProvider(s.Provider)
	s.APIKey = strings.TrimSpace(s.APIKey)
	if s.Provider == "" && s.Enabled {
		s.Provider = ChannelWebSearchProviderTavily
	}
	if s.MaxResults <= 0 {
		s.MaxResults = channelWebSearchDefaultMaxResults
	} else if s.MaxResults > channelWebSearchMaxResultsLimit {
		s.MaxResults = channelWebSearchMaxResultsLimit
	}
	if s.SearchDepth != "advanced" {
		s.SearchDepth = channelWebSearchDefaultDepth
	}
	if !channelWebSearchFreshnessValues[s.Freshness] {
		s.Freshness = ""
	}
	s.ContentTypes = normalizeChannelWebSearchContentTypes(s.ContentTypes)
	s.APIKeyConfigured = s.HasAPIKey()
}

// HasAPIKey 判断当前配置是否包含真实 WebSearch 供应商密钥。
func (s ChannelWebSearchSettings) HasAPIKey() bool {
	return strings.TrimSpace(s.APIKey) != ""
}

// ValidateForRelay 校验启用状态下的 WebSearch 配置是否足够执行 relay 短路。
func (s ChannelWebSearchSettings) ValidateForRelay() error {
	s.Normalize()
	if !s.Enabled {
		return nil
	}
	if s.Provider == "" {
		return fmt.Errorf("web_search provider 不能为空")
	}
	if !IsChannelWebSearchProviderSupported(s.Provider) {
		return fmt.Errorf("不支持的 web_search provider: %s", s.Provider)
	}
	if s.Provider == ChannelWebSearchProviderTavily && !s.HasAPIKey() {
		return fmt.Errorf("启用 Tavily web_search 时必须配置 API Key")
	}
	return nil
}

// IsChannelWebSearchProviderSupported 判断 provider 是否为当前支持的渠道 WebSearch 供应商。
func IsChannelWebSearchProviderSupported(provider string) bool {
	switch normalizeChannelWebSearchProvider(provider) {
	case ChannelWebSearchProviderTavily, ChannelWebSearchProviderAnySearch:
		return true
	default:
		return false
	}
}

func normalizeChannelWebSearchProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case ChannelWebSearchProviderTavily:
		return ChannelWebSearchProviderTavily
	case ChannelWebSearchProviderAnySearch, "any_search":
		return ChannelWebSearchProviderAnySearch
	default:
		return ""
	}
}

func normalizeChannelWebSearchContentTypes(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] || !channelWebSearchContentTypes[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
