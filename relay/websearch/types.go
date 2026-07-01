package websearch

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/dto"
)

const (
	defaultMaxResults = 5
	maxResponseBytes  = 1 << 20
)

// SearchRequest 是发送给 WebSearch 供应商的稳定查询请求。
type SearchRequest struct {
	Query        string
	MaxResults   int
	SearchDepth  string
	Freshness    string
	ContentTypes []string
}

// SearchResult 是不同 WebSearch 供应商归一化后的搜索结果。
type SearchResult struct {
	URL     string `json:"url,omitempty"`
	Title   string `json:"title,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	PageAge string `json:"page_age,omitempty"`
}

// SearchResponse 是 WebSearch 供应商归一化后的响应。
type SearchResponse struct {
	Query   string
	Results []SearchResult
}

// Provider 定义渠道 WebSearch 供应商需要实现的搜索能力。
type Provider interface {
	Name() string
	Search(ctx context.Context, req SearchRequest) (*SearchResponse, error)
}

// NewProvider 根据渠道 WebSearch 配置创建搜索供应商。
func NewProvider(settings dto.ChannelWebSearchSettings, httpClient *http.Client) (Provider, error) {
	settings.Normalize()
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	switch settings.Provider {
	case dto.ChannelWebSearchProviderTavily:
		return newTavilyProvider(settings.APIKey, settings.SearchDepth, httpClient, tavilySearchEndpoint), nil
	case dto.ChannelWebSearchProviderAnySearch:
		return newAnySearchProvider(settings.APIKey, httpClient, anySearchEndpoint), nil
	default:
		return nil, fmt.Errorf("不支持的 web_search provider: %s", settings.Provider)
	}
}

func normalizeSearchRequest(req SearchRequest) SearchRequest {
	req.Query = strings.TrimSpace(req.Query)
	if req.MaxResults <= 0 {
		req.MaxResults = defaultMaxResults
	}
	if req.SearchDepth != "advanced" {
		req.SearchDepth = "basic"
	}
	return req
}

func truncateBodyForError(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 500 {
		return text[:500]
	}
	return text
}
