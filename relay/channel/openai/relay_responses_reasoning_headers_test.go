package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesReasoningHeaderReachesClientWithoutChangingUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	const responseJSON = `{"id":"resp_test","status":"completed","output":[],"usage":{"input_tokens":100,"output_tokens":7,"total_tokens":107}}`
	for _, stream := range []bool{false, true} {
		for _, upstreamHeader := range []string{"", "false", "true"} {
			name := "JSON/" + upstreamHeader
			if stream {
				name = "SSE/" + upstreamHeader
			}
			t.Run(name, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{}`))
				info := &relaycommon.RelayInfo{
					IsStream: stream, RelayFormat: types.RelayFormatOpenAIResponses, RelayMode: relayconstant.RelayModeResponses,
					ChannelMeta: &relaycommon.ChannelMeta{
						ApiType: constant.APITypeOpenAI, ChannelSetting: dto.ChannelSettings{ResponsesReasoningIncludedEnabled: true},
					},
				}
				body := responseJSON
				if stream {
					body = "data: {\"type\":\"response.completed\",\"response\":" + responseJSON + "}\n\ndata: [DONE]\n\n"
				}
				resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
				if upstreamHeader != "" {
					resp.Header.Set("X-Reasoning-Included", upstreamHeader)
				}
				helper.PrepareResponsesReasoningHeader(c, info)
				helper.SupplementResponsesReasoningHeader(c, info, resp)
				var usage *dto.Usage
				var apiErr *types.NewAPIError
				if stream {
					usage, apiErr = OaiResponsesStreamHandler(c, info, resp)
				} else {
					usage, apiErr = OaiResponsesHandler(c, info, resp)
				}
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				wantHeader := upstreamHeader
				if wantHeader == "" {
					wantHeader = "true"
				}
				assert.Equal(t, wantHeader, recorder.Result().Header.Get("X-Reasoning-Included"))
				assert.Equal(t, 100, usage.PromptTokens)
				assert.Equal(t, 7, usage.CompletionTokens)
				assert.Equal(t, 107, usage.TotalTokens)
				assert.Contains(t, recorder.Body.String(), responseJSON)
			})
		}
	}
}
