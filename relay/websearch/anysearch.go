package websearch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
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
	// AnySearch 实际常以 Markdown 文本返回结果列表；先按该格式拆成逐条结果，
	// 否则整段文本会退化成一条没有 URL 的结果。
	if results := parseAnySearchMarkdownResults(text); len(results) > 0 {
		return results
	}
	return []SearchResult{fallbackAnySearchTextResult(text)}
}

var (
	// anySearchMarkdownTitlePattern 匹配 "### 1. 标题" 这类编号标题行。
	anySearchMarkdownTitlePattern = regexp.MustCompile(`^#{1,6}\s*\d+[.)]\s+(.+?)\s*$`)
	// anySearchMarkdownURLPattern 匹配 "- **URL**: https://..." 这类 URL 行。
	anySearchMarkdownURLPattern = regexp.MustCompile(`(?i)^-?\s*\*{0,2}url\*{0,2}\s*[:：]\s*(\S+)`)
)

// anySearchMarkdownDateMarker 是摘要行尾部日期的分隔标记，例如 "… date: Sep 2, 2026"。
const anySearchMarkdownDateMarker = " date: "

// parseAnySearchMarkdownResults 解析 AnySearch MCP 文本形态的结果列表：
//
//	## Search Results (5 results, 2012ms)
//	### 1. 标题
//	- **URL**: https://example.com/a
//	- 摘要 … date: Sep 2, 2026
//
// 标题行开启一条结果，URL 行填充 URL，其余列表行拼入摘要并把尾部 "date:" 提取为
// PageAge；标题行之前的内容（如 "## Search Results" 头）忽略。文本不含编号标题时返回 nil。
func parseAnySearchMarkdownResults(text string) []SearchResult {
	var results []SearchResult
	var current *SearchResult
	flush := func() {
		if current != nil && (current.URL != "" || current.Title != "") {
			current.Snippet = limitSnippet(current.Snippet)
			results = append(results, *current)
		}
		current = nil
	}
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if match := anySearchMarkdownTitlePattern.FindStringSubmatch(line); match != nil {
			flush()
			current = &SearchResult{Title: strings.TrimSpace(match[1])}
			continue
		}
		if current == nil {
			continue
		}
		if match := anySearchMarkdownURLPattern.FindStringSubmatch(line); match != nil {
			current.URL = strings.TrimSpace(match[1])
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if body == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(body), "date:") {
			current.PageAge = strings.TrimSpace(body[len("date:"):])
			continue
		}
		if idx := strings.LastIndex(body, anySearchMarkdownDateMarker); idx >= 0 {
			current.PageAge = strings.TrimSpace(body[idx+len(anySearchMarkdownDateMarker):])
			body = strings.TrimSpace(body[:idx])
		}
		if body == "" {
			continue
		}
		if current.Snippet != "" {
			current.Snippet += " "
		}
		current.Snippet += body
	}
	flush()
	return results
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
