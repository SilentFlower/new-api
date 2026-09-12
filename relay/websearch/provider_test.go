package websearch

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTavilyProviderSearchUsesBearerHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer secret-key", r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.NotContains(t, string(body), "secret-key")
		var payload tavilyRequest
		require.NoError(t, common.Unmarshal(body, &payload))
		assert.Equal(t, "golang", payload.Query)
		assert.Equal(t, 3, payload.MaxResults)
		assert.Equal(t, "advanced", payload.SearchDepth)
		_, _ = w.Write([]byte(`{"results":[{"url":"https://example.com","title":"Example","content":"Snippet"}]}`))
	}))
	defer server.Close()

	provider := newTavilyProvider("secret-key", "advanced", server.Client(), server.URL)
	resp, err := provider.Search(context.Background(), SearchRequest{Query: "golang", MaxResults: 3})

	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, "https://example.com", resp.Results[0].URL)
	assert.Equal(t, "Example", resp.Results[0].Title)
	assert.Equal(t, "Snippet", resp.Results[0].Snippet)
}

func TestNormalizeAnySearchResponseFromMCPTextJSON(t *testing.T) {
	body := []byte(`{"result":{"content":[{"type":"text","text":"[{\"url\":\"https://example.com\",\"title\":\"Example\",\"snippet\":\"Snippet\"}]"}]}}`)

	resp, err := NormalizeAnySearchResponse("query", body)

	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, "https://example.com", resp.Results[0].URL)
	assert.Equal(t, "Example", resp.Results[0].Title)
	assert.Equal(t, "Snippet", resp.Results[0].Snippet)
}

func TestNormalizeAnySearchResponseFromMCPMarkdownText(t *testing.T) {
	markdown := "## Search Results (2 results, 2012ms)\n\n" +
		"### 1. 中国气象局举行2026年9月新闻发布会\n" +
		"- **URL**: http://www.scio.gov.cn/xwfb/t20260904_1006845.html\n" +
		"- 我国的台风数量具有偏多的特点。 ... date: Sep 2, 2026\n\n" +
		"### 2. 9月将有2～3个台风生成可能影响我国 - 新闻\n" +
		"- **URL**: https://news.sciencenet.cn/htmlnews/2026/9/570826.shtm\n" +
		"- 今年已有7个台风登陆我国。\n" +
		"- date: 5 days ago\n"
	body, err := common.Marshal(map[string]any{
		"result": map[string]any{"content": []map[string]any{{"type": "text", "text": markdown}}},
	})
	require.NoError(t, err)

	resp, err := NormalizeAnySearchResponse("台风", body)

	require.NoError(t, err)
	require.Len(t, resp.Results, 2)
	assert.Equal(t, "http://www.scio.gov.cn/xwfb/t20260904_1006845.html", resp.Results[0].URL)
	assert.Equal(t, "中国气象局举行2026年9月新闻发布会", resp.Results[0].Title)
	assert.Equal(t, "我国的台风数量具有偏多的特点。 ...", resp.Results[0].Snippet)
	assert.Equal(t, "Sep 2, 2026", resp.Results[0].PageAge)
	assert.Equal(t, "https://news.sciencenet.cn/htmlnews/2026/9/570826.shtm", resp.Results[1].URL)
	assert.Equal(t, "今年已有7个台风登陆我国。", resp.Results[1].Snippet)
	assert.Equal(t, "5 days ago", resp.Results[1].PageAge)

	assert.Nil(t, parseAnySearchMarkdownResults("No numbered headings\n- **URL**: https://example.com"))
	plain, err := NormalizeAnySearchResponse("q", []byte(`{"result":{"content":[{"type":"text","text":"just some prose"}]}}`))
	require.NoError(t, err)
	require.Len(t, plain.Results, 1)
	assert.Equal(t, "just some prose", plain.Results[0].Title)
	assert.Empty(t, plain.Results[0].URL)
}

func TestAnySearchProviderSearchRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer any-key", r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.False(t, strings.Contains(string(body), "any-key"))
		var payload anySearchRequest
		require.NoError(t, common.Unmarshal(body, &payload))
		assert.Equal(t, "tools/call", payload.Method)
		assert.Equal(t, "search", payload.Params.Name)
		assert.Equal(t, "golang", payload.Params.Arguments["query"])
		assert.Equal(t, float64(4), payload.Params.Arguments["max_results"])
		assert.Equal(t, "day", payload.Params.Arguments["freshness"])
		_, _ = w.Write([]byte(`{"result":{"results":[{"url":"https://example.com","title":"Example","content":"Snippet"}]}}`))
	}))
	defer server.Close()

	provider := newAnySearchProvider("any-key", server.Client(), server.URL)
	resp, err := provider.Search(context.Background(), SearchRequest{
		Query:        "golang",
		MaxResults:   4,
		Freshness:    "day",
		ContentTypes: []string{"web"},
	})

	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, "https://example.com", resp.Results[0].URL)
}

func TestAnySearchProviderSearchAllowsEmptyAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"result":{"results":[{"url":"https://example.com","title":"Example","content":"Snippet"}]}}`))
	}))
	defer server.Close()

	provider := newAnySearchProvider("", server.Client(), server.URL)
	resp, err := provider.Search(context.Background(), SearchRequest{Query: "golang"})

	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, "https://example.com", resp.Results[0].URL)
}
