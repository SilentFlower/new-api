package websearch

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPureClaudeWebSearchRequest(t *testing.T) {
	tests := []struct {
		name    string
		tools   any
		want    bool
		message string
	}{
		{
			name: "单个 web_search 类型",
			tools: []any{
				map[string]any{"type": "web_search_20250305", "name": "web_search"},
			},
			want: true,
		},
		{
			name: "单个 google_search 名称",
			tools: []any{
				map[string]any{"name": "google_search"},
			},
			want: true,
		},
		{
			name: "混合工具不模拟",
			tools: []any{
				map[string]any{"type": "web_search_20250305"},
				map[string]any{"name": "get_weather", "input_schema": map[string]any{"type": "object"}},
			},
			want: false,
		},
		{
			name: "普通工具不模拟",
			tools: []any{
				map[string]any{"name": "get_weather"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &dto.ClaudeRequest{Tools: tt.tools}
			assert.Equal(t, tt.want, IsPureClaudeWebSearchRequest(req))
		})
	}
}

func TestExtractClaudeWebSearchQuery(t *testing.T) {
	req := &dto.ClaudeRequest{
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "old"},
			{
				Role: "user",
				Content: []any{
					map[string]any{"type": "text", "text": " latest tavily docs "},
				},
			},
		},
	}

	assert.Equal(t, "latest tavily docs", ExtractClaudeWebSearchQuery(req))
}

func TestBuildClaudeWebSearchResponse(t *testing.T) {
	resp := BuildClaudeWebSearchResponse("msg_test", "srvtoolu_test", "claude-test", "query", []SearchResult{
		{URL: "https://example.com", Title: "Example", Snippet: "Snippet"},
	}, 10, 2)

	require.NotNil(t, resp)
	assert.Equal(t, "message", resp.Type)
	assert.Equal(t, "end_turn", resp.StopReason)
	require.Len(t, resp.Content, 3)
	assert.Equal(t, "server_tool_use", resp.Content[0].Type)
	assert.Equal(t, "web_search_tool_result", resp.Content[1].Type)
	require.NotNil(t, resp.Usage)
	require.NotNil(t, resp.Usage.ServerToolUse)
	assert.Equal(t, 1, resp.Usage.ServerToolUse.WebSearchRequests)
}

func TestClaudeWebSearchHelpersDoNotMutateRequest(t *testing.T) {
	req := &dto.ClaudeRequest{
		Model: "claude-test",
		Tools: []any{
			map[string]any{"type": "web_search_20250305", "name": "web_search"},
		},
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []any{
					map[string]any{"type": "text", "text": "stable request body"},
				},
			},
		},
	}
	before, err := common.Marshal(req)
	require.NoError(t, err)

	query := ExtractClaudeWebSearchQuery(req)
	_ = BuildClaudeWebSearchResponse("msg_test", "srvtoolu_test", "claude-test", query, []SearchResult{
		{URL: "https://example.com", Title: "Example", Snippet: "Snippet"},
	}, 10, 2)
	after, err := common.Marshal(req)
	require.NoError(t, err)

	assert.JSONEq(t, string(before), string(after))
}
