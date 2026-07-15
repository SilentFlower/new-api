package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesCompactionHandlerForwardsBodyAndMapsUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	const body = `{"id":"cmp_1","object":"response.compaction","output":[],"usage":{"input_tokens":123,"output_tokens":7,"total_tokens":130,"input_tokens_details":{"cached_tokens":11,"cache_write_tokens":3}}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": {"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}

	usage, apiErr := OaiResponsesCompactionHandler(c, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 123, usage.PromptTokens)
	assert.Equal(t, 7, usage.CompletionTokens)
	assert.Equal(t, 130, usage.TotalTokens)
	assert.Equal(t, 11, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 3, usage.PromptTokensDetails.CacheWriteTokens)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, body, recorder.Body.String())
}
