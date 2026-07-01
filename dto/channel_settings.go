package dto

import (
	"fmt"
	"strings"
)

type ChannelSettings struct {
	ForceFormat                bool                        `json:"force_format,omitempty"`
	ThinkingToContent          bool                        `json:"thinking_to_content,omitempty"`
	Proxy                      string                      `json:"proxy"`
	PassThroughBodyEnabled     bool                        `json:"pass_through_body_enabled,omitempty"`
	UseUpstreamModelForBilling bool                        `json:"use_upstream_model_for_billing,omitempty"`
	SystemPrompt               string                      `json:"system_prompt,omitempty"`
	SystemPromptOverride       bool                        `json:"system_prompt_override,omitempty"`
	VisionAssist               ChannelVisionAssistSettings `json:"vision_assist,omitempty"`
	WebSearch                  ChannelWebSearchSettings    `json:"web_search,omitempty"`
}

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

// ChannelVisionAssistSettings 描述目标渠道的视觉辅助识别配置。
type ChannelVisionAssistSettings struct {
	Enabled         bool     `json:"enabled,omitempty"`
	AssistChannelId int      `json:"assist_channel_id,omitempty"`
	AssistModel     string   `json:"assist_model,omitempty"`
	TargetModels    []string `json:"target_models,omitempty"`
	Prompt          string   `json:"prompt,omitempty"`
	CacheTTLSeconds int      `json:"cache_ttl_seconds,omitempty"`
	FailurePolicy   string   `json:"failure_policy,omitempty"`
	StripImage      *bool    `json:"strip_image,omitempty"`
	EndpointMode    string   `json:"endpoint_mode,omitempty"`
	MaxConcurrency  int      `json:"max_concurrency,omitempty"`
	RetryCount      int      `json:"retry_count,omitempty"`
	RetryBackoffMs  int      `json:"retry_backoff_ms,omitempty"`
}

type VertexKeyType string

const (
	VertexKeyTypeJSON   VertexKeyType = "json"
	VertexKeyTypeAPIKey VertexKeyType = "api_key"
)

type AwsKeyType string

const (
	AwsKeyTypeAKSK   AwsKeyType = "ak_sk" // 默认
	AwsKeyTypeApiKey AwsKeyType = "api_key"
)

type ChannelOtherSettings struct {
	AzureResponsesVersion                 string        `json:"azure_responses_version,omitempty"`
	VertexKeyType                         VertexKeyType `json:"vertex_key_type,omitempty"` // "json" or "api_key"
	OpenRouterEnterprise                  *bool         `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery                       bool          `json:"claude_beta_query,omitempty"`         // Claude 渠道是否强制追加 ?beta=true
	AllowServiceTier                      bool          `json:"allow_service_tier,omitempty"`        // 是否允许 service_tier 透传（默认过滤以避免额外计费）
	AllowInferenceGeo                     bool          `json:"allow_inference_geo,omitempty"`       // 是否允许 inference_geo 透传（仅 Claude，默认过滤以满足数据驻留合规
	AllowSpeed                            bool          `json:"allow_speed,omitempty"`               // 是否允许 speed 透传（仅 Claude，默认过滤以避免意外切换推理速度模式）
	AllowSafetyIdentifier                 bool          `json:"allow_safety_identifier,omitempty"`   // 是否允许 safety_identifier 透传（默认过滤以保护用户隐私）
	DisableStore                          bool          `json:"disable_store,omitempty"`             // 是否禁用 store 透传（默认允许透传，禁用后可能导致 Codex 无法使用）
	AllowIncludeObfuscation               bool          `json:"allow_include_obfuscation,omitempty"` // 是否允许 stream_options.include_obfuscation 透传（默认过滤以避免关闭流混淆保护）
	AwsKeyType                            AwsKeyType    `json:"aws_key_type,omitempty"`
	UpstreamModelUpdateCheckEnabled       bool          `json:"upstream_model_update_check_enabled,omitempty"`        // 是否检测上游模型更新
	UpstreamModelUpdateAutoSyncEnabled    bool          `json:"upstream_model_update_auto_sync_enabled,omitempty"`    // 是否自动同步上游模型更新
	UpstreamModelUpdateLastCheckTime      int64         `json:"upstream_model_update_last_check_time,omitempty"`      // 上次检测时间
	UpstreamModelUpdateLastDetectedModels []string      `json:"upstream_model_update_last_detected_models,omitempty"` // 上次检测到的可加入模型
	UpstreamModelUpdateLastRemovedModels  []string      `json:"upstream_model_update_last_removed_models,omitempty"`  // 上次检测到的可删除模型
	UpstreamModelUpdateIgnoredModels      []string      `json:"upstream_model_update_ignored_models,omitempty"`       // 手动忽略的模型
}

func (s *ChannelOtherSettings) IsOpenRouterEnterprise() bool {
	if s == nil || s.OpenRouterEnterprise == nil {
		return false
	}
	return *s.OpenRouterEnterprise
}
