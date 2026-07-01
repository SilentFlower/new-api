package websearch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
)

const tavilySearchEndpoint = "https://api.tavily.com/search"

type tavilyProvider struct {
	apiKey      string
	searchDepth string
	httpClient  *http.Client
	endpoint    string
}

func newTavilyProvider(apiKey string, searchDepth string, httpClient *http.Client, endpoint string) *tavilyProvider {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if searchDepth != "advanced" {
		searchDepth = "basic"
	}
	return &tavilyProvider{
		apiKey:      apiKey,
		searchDepth: searchDepth,
		httpClient:  httpClient,
		endpoint:    endpoint,
	}
}

func (p *tavilyProvider) Name() string {
	return "tavily"
}

func (p *tavilyProvider) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	req = normalizeSearchRequest(req)
	payload := tavilyRequest{
		Query:       req.Query,
		MaxResults:  req.MaxResults,
		SearchDepth: p.searchDepth,
	}
	bodyBytes, err := common.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("tavily: 编码请求失败: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("tavily: 构造请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("tavily: 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("tavily: 读取响应失败: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("tavily: 响应过大")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("tavily: HTTP %d: %s", resp.StatusCode, truncateBodyForError(body))
	}
	return NormalizeTavilyResponse(req.Query, body)
}

// NormalizeTavilyResponse 将 Tavily Search API 响应归一化为内部搜索结果。
func NormalizeTavilyResponse(query string, body []byte) (*SearchResponse, error) {
	var raw tavilyResponse
	if err := common.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("tavily: 解析响应失败: %w", err)
	}
	results := make([]SearchResult, 0, len(raw.Results))
	for _, item := range raw.Results {
		results = append(results, SearchResult{
			URL:     item.URL,
			Title:   item.Title,
			Snippet: item.Content,
		})
	}
	return &SearchResponse{Query: query, Results: results}, nil
}

type tavilyRequest struct {
	Query       string `json:"query"`
	MaxResults  int    `json:"max_results"`
	SearchDepth string `json:"search_depth"`
}

type tavilyResponse struct {
	Results []tavilyResult `json:"results"`
}

type tavilyResult struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Content string `json:"content"`
}
