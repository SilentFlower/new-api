package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/websearch"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldHandleChatWebSearchEmulation(t *testing.T) {
	assert.False(t, shouldHandleChatWebSearchEmulation(nil))
	assert.False(t, shouldHandleChatWebSearchEmulation(&relaycommon.RelayInfo{}))

	disabled := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{
				WebSearch: dto.ChannelWebSearchSettings{Enabled: false},
			},
		},
	}
	assert.False(t, shouldHandleChatWebSearchEmulation(disabled))

	enabled := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{
				WebSearch: dto.ChannelWebSearchSettings{Enabled: true},
			},
		},
	}
	assert.True(t, shouldHandleChatWebSearchEmulation(enabled))
}

func TestHandleChatWebSearchEmulationRejectsEmptyQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{
				WebSearch: dto.ChannelWebSearchSettings{Enabled: true},
			},
		},
	}
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "assistant", Content: "没有查询"}},
	}

	newAPIError := handleChatWebSearchEmulation(context, info, request)

	require.NotNil(t, newAPIError)
	assert.Equal(t, http.StatusBadRequest, newAPIError.StatusCode)
	assert.Contains(t, newAPIError.Error(), "最后一条 user 消息")
}

func TestExecuteChannelWebSearchMapsProviderFailureToBadGateway(t *testing.T) {
	gin.SetMode(gin.TestMode)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodConnect, r.Method)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{
				Proxy: proxy.URL,
				WebSearch: dto.ChannelWebSearchSettings{
					Enabled:  true,
					Provider: dto.ChannelWebSearchProviderTavily,
					APIKey:   "secret-web-search-key",
				},
			},
		},
	}

	response, newAPIError := executeChannelWebSearch(context, info, "query")

	assert.Nil(t, response)
	require.NotNil(t, newAPIError)
	assert.Equal(t, http.StatusBadGateway, newAPIError.StatusCode)
	assert.False(t, strings.Contains(newAPIError.Error(), "secret-web-search-key"))
}

func TestWriteChatWebSearchStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name         string
		includeUsage bool
	}{
		{name: "包含 usage", includeUsage: true},
		{name: "省略 usage", includeUsage: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			usage := websearch.BuildChatUsage(8, 4)

			err := writeChatWebSearchStream(context, "chatcmpl-test", 123, "gpt-test", "query", []websearch.SearchResult{
				{URL: "https://example.com", Title: "Example", Snippet: "Snippet"},
			}, usage, tt.includeUsage)
			require.NoError(t, err)

			body := recorder.Body.String()
			assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
			assert.Contains(t, body, `"role":"assistant"`)
			assert.Contains(t, body, `https://example.com`)
			assert.Contains(t, body, `"finish_reason":"stop"`)
			assert.Contains(t, body, `data: [DONE]`)
			if tt.includeUsage {
				assert.Contains(t, body, `"prompt_tokens":8`)
			} else {
				assert.NotContains(t, body, `"prompt_tokens":8`)
			}
		})
	}
}
