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
