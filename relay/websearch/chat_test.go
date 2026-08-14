package websearch

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPureChatWebSearchRequest(t *testing.T) {
	tests := []struct {
		name     string
		request  *dto.GeneralOpenAIRequest
		expected bool
	}{
		{
			name:     "空请求不模拟",
			request:  nil,
			expected: false,
		},
		{
			name:     "缺少 web_search_options 不模拟",
			request:  &dto.GeneralOpenAIRequest{},
			expected: false,
		},
		{
			name: "只有 web_search_options 时模拟",
			request: &dto.GeneralOpenAIRequest{
				WebSearchOptions: &dto.WebSearchOptions{},
			},
			expected: true,
		},
		{
			name: "空 tools 和空 functions 仍模拟",
			request: &dto.GeneralOpenAIRequest{
				WebSearchOptions: &dto.WebSearchOptions{},
				Tools:            []dto.ToolCallRequest{},
				Functions:        []byte(`[]`),
			},
			expected: true,
		},
		{
			name: "null functions 仍模拟",
			request: &dto.GeneralOpenAIRequest{
				WebSearchOptions: &dto.WebSearchOptions{},
				Functions:        []byte(`null`),
			},
			expected: true,
		},
		{
			name: "存在 tools 时透传",
			request: &dto.GeneralOpenAIRequest{
				WebSearchOptions: &dto.WebSearchOptions{},
				Tools: []dto.ToolCallRequest{
					{Type: "function"},
				},
			},
			expected: false,
		},
		{
			name: "存在 legacy functions 时透传",
			request: &dto.GeneralOpenAIRequest{
				WebSearchOptions: &dto.WebSearchOptions{},
				Functions:        []byte(`[{}]`),
			},
			expected: false,
		},
		{
			name: "异常 functions 不误判为纯搜索",
			request: &dto.GeneralOpenAIRequest{
				WebSearchOptions: &dto.WebSearchOptions{},
				Functions:        []byte(`{}`),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsPureChatWebSearchRequest(tt.request))
		})
	}
}

func TestExtractChatWebSearchQuery(t *testing.T) {
	tests := []struct {
		name     string
		request  *dto.GeneralOpenAIRequest
		expected string
	}{
		{
			name: "提取最后一条 user 字符串消息",
			request: &dto.GeneralOpenAIRequest{
				Messages: []dto.Message{
					{Role: "user", Content: "old query"},
					{Role: "assistant", Content: "old answer"},
					{Role: "user", Content: " latest Tavily docs "},
				},
			},
			expected: "latest Tavily docs",
		},
		{
			name: "提取文本块并忽略非文本块",
			request: &dto.GeneralOpenAIRequest{
				Messages: []dto.Message{
					{
						Role: "user",
						Content: []dto.MediaContent{
							{Type: dto.ContentTypeText, Text: "latest "},
							{Type: dto.ContentTypeImageURL, ImageUrl: map[string]any{"url": "https://example.com/image.png"}},
							{Type: dto.ContentTypeText, Text: "docs"},
						},
					},
				},
			},
			expected: "latest docs",
		},
		{
			name: "最后一条不是 user 时不回看历史",
			request: &dto.GeneralOpenAIRequest{
				Messages: []dto.Message{
					{Role: "user", Content: "query"},
					{Role: "assistant", Content: "answer"},
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ExtractChatWebSearchQuery(tt.request))
		})
	}
}

func TestBuildChatWebSearchResponse(t *testing.T) {
	response := BuildChatWebSearchResponse("chatcmpl-test", 123, "gpt-test", "query", []SearchResult{
		{URL: "https://example.com", Title: "Example", Snippet: "Snippet"},
	}, 10, 5)

	require.NotNil(t, response)
	assert.Equal(t, "chat.completion", response.Object)
	assert.Equal(t, "gpt-test", response.Model)
	require.Len(t, response.Choices, 1)
	assert.Equal(t, "assistant", response.Choices[0].Message.Role)
	assert.Equal(t, "stop", response.Choices[0].FinishReason)
	assert.Contains(t, response.Choices[0].Message.StringContent(), "https://example.com")
	assert.Equal(t, 10, response.Usage.PromptTokens)
	assert.Equal(t, 5, response.Usage.CompletionTokens)
	assert.Equal(t, 15, response.Usage.TotalTokens)
	assert.Empty(t, response.Usage.UsageSemantic)
}

func TestChatWebSearchHelpersDoNotMutateRequest(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model:            "gpt-test",
		WebSearchOptions: &dto.WebSearchOptions{},
		Messages: []dto.Message{
			{
				Role: "user",
				Content: []any{
					map[string]any{"type": "text", "text": "stable request body"},
				},
			},
		},
	}
	before, err := common.Marshal(request)
	require.NoError(t, err)

	query := ExtractChatWebSearchQuery(request)
	_ = BuildChatWebSearchResponse("chatcmpl-test", 123, "gpt-test", query, []SearchResult{
		{URL: "https://example.com", Title: "Example", Snippet: "Snippet"},
	}, 10, 5)
	after, err := common.Marshal(request)
	require.NoError(t, err)

	assert.JSONEq(t, string(before), string(after))
}
