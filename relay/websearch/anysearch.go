package websearch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const anySearchEndpoint = "https://api.anysearch.com/mcp"

type anySearchProvider struct {
	apiKey     string
	httpClient *http.Client
	endpoint   string
}

func newAnySearchProvider(apiKey string, httpClient *http.Client, endpoint string) *anySearchProvider {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &anySearchProvider{apiKey: apiKey, httpClient: httpClient, endpoint: endpoint}
}

func (p *anySearchProvider) Name() string {
	return "anysearch"
}

func (p *anySearchProvider) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	req = normalizeSearchRequest(req)
	args := map[string]any{
		"query":       req.Query,
		"max_results": req.MaxResults,
	}
	if req.Freshness != "" {
		args["freshness"] = req.Freshness
	}
	if len(req.ContentTypes) > 0 {
		args["content_types"] = req.ContentTypes
	}
	payload := anySearchRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: anySearchRequestParams{
			Name:      "search",
			Arguments: args,
		},
	}
	bodyBytes, err := common.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("anysearch: 编码请求失败: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("anysearch: 构造请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(p.apiKey) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anysearch: 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("anysearch: 读取响应失败: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("anysearch: 响应过大")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("anysearch: HTTP %d: %s", resp.StatusCode, truncateBodyForError(body))
	}
	return NormalizeAnySearchResponse(req.Query, body)
}

// NormalizeAnySearchResponse 将 AnySearch MCP JSON-RPC 响应归一化为内部搜索结果。
func NormalizeAnySearchResponse(query string, body []byte) (*SearchResponse, error) {
	var raw anySearchResponse
	if err := common.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("anysearch: 解析响应失败: %w", err)
	}
	if raw.Error != nil {
		errBytes, _ := common.Marshal(raw.Error)
		return nil, fmt.Errorf("anysearch: API 错误: %s", truncateBodyForError(errBytes))
	}
	results := collectAnySearchResults(raw.Result)
	if len(results) == 0 {
		for _, text := range extractAnySearchTextBlocks(raw.Result) {
			results = append(results, collectAnySearchResultsFromText(text)...)
		}
	}
	if len(results) == 0 {
		if text := strings.TrimSpace(fmt.Sprintf("%v", raw.Result)); text != "" && text != "<nil>" {
			results = append(results, fallbackAnySearchTextResult(text))
		}
	}
	return &SearchResponse{Query: query, Results: results}, nil
}

func collectAnySearchResultsFromText(text string) []SearchResult {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var parsed any
	if err := common.Unmarshal([]byte(text), &parsed); err == nil {
		if results := collectAnySearchResults(parsed); len(results) > 0 {
			return results
		}
	}
	return []SearchResult{fallbackAnySearchTextResult(text)}
}

func collectAnySearchResults(value any) []SearchResult {
	results := make([]SearchResult, 0)
	collectAnySearchResultsInto(value, &results)
	return results
}

func collectAnySearchResultsInto(value any, results *[]SearchResult) {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			collectAnySearchResultsInto(item, results)
		}
	case map[string]any:
		if result, ok := anySearchResultFromMap(v); ok {
			*results = append(*results, result)
			return
		}
		for _, key := range []string{"results", "items", "data", "list"} {
			if nested, ok := v[key]; ok {
				collectAnySearchResultsInto(nested, results)
			}
		}
	}
}

func anySearchResultFromMap(item map[string]any) (SearchResult, bool) {
	result := SearchResult{
		URL:     firstStringValue(item, "url", "link", "href"),
		Title:   firstStringValue(item, "title", "name"),
		Snippet: firstStringValue(item, "snippet", "content", "description", "page_content", "text"),
		PageAge: firstStringValue(item, "page_age", "age", "date"),
	}
	result.Snippet = limitSnippet(result.Snippet)
	if result.URL == "" && result.Title == "" && result.Snippet == "" {
		return SearchResult{}, false
	}
	if result.Title == "" {
		result.Title = result.URL
	}
	return result, true
}

func extractAnySearchTextBlocks(value any) []string {
	root, ok := value.(map[string]any)
	if !ok {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return []string{text}
		}
		return nil
	}
	content, ok := root["content"].([]any)
	if !ok {
		return nil
	}
	texts := make([]string, 0, len(content))
	for _, item := range content {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if firstStringValue(block, "type") != "text" {
			continue
		}
		if text := strings.TrimSpace(firstStringValue(block, "text")); text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

func fallbackAnySearchTextResult(text string) SearchResult {
	title := "AnySearch result"
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.Trim(line, "#*- "))
		if line != "" {
			title = limitSnippet(line)
			break
		}
	}
	return SearchResult{Title: title, Snippet: limitSnippet(text)}
}

func firstStringValue(item map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := item[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			return strings.TrimSpace(typed)
		case float64:
			return strconv.FormatFloat(typed, 'f', -1, 64)
		case float32:
			return strconv.FormatFloat(float64(typed), 'f', -1, 32)
		case int:
			return strconv.Itoa(typed)
		case int64:
			return strconv.FormatInt(typed, 10)
		case bool:
			return strconv.FormatBool(typed)
		default:
			continue
		}
	}
	return ""
}

func limitSnippet(text string) string {
	const maxRunes = 4000
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes])
}

type anySearchRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      int                    `json:"id"`
	Method  string                 `json:"method"`
	Params  anySearchRequestParams `json:"params"`
}

type anySearchRequestParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type anySearchResponse struct {
	Result any `json:"result"`
	Error  any `json:"error,omitempty"`
}
