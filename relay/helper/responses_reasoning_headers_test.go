package helper_test

import (
	"net/http"
	"net/http/httptest"
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

func TestSupplementResponsesReasoningHeaderHonorsChannelAndUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name     string
		enabled  bool
		upstream string
		status   int
		want     string
	}{
		{"旧渠道不补充", false, "", http.StatusOK, ""},
		{"开启后缺失标识补充true", true, "", http.StatusOK, "true"},
		{"保留上游false", true, "false", http.StatusOK, "false"},
		{"保留上游true", true, "true", http.StatusOK, "true"},
		{"保留未知标识", true, "unknown", http.StatusOK, "unknown"},
		{"错误响应不补充", true, "", http.StatusBadRequest, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{
				RelayFormat: types.RelayFormatOpenAIResponses,
				RelayMode:   relayconstant.RelayModeResponses,
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiType:        constant.APITypeOpenAI,
					ChannelSetting: dto.ChannelSettings{ResponsesReasoningIncludedEnabled: tc.enabled},
				},
			}
			resp := &http.Response{StatusCode: tc.status, Header: make(http.Header)}
			if tc.upstream != "" {
				resp.Header.Set("X-Reasoning-Included", tc.upstream)
			}
			helper.SupplementResponsesReasoningHeader(c, info, resp)
			assert.Equal(t, tc.want, resp.Header.Get("X-Reasoning-Included"))
		})
	}
}

func TestResponsesReasoningHeaderExcludesCompactionAndConversions(t *testing.T) {
	for _, tc := range []struct {
		name      string
		apiType   int
		format    types.RelayFormat
		mode      relayconstant.ResponsesCompactMode
		converter string
		want      bool
	}{
		{"原生Codex", constant.APITypeCodex, types.RelayFormatOpenAIResponses, "", "", true},
		{"CompactV2不补充", constant.APITypeOpenAI, types.RelayFormatOpenAIResponses, relayconstant.ResponsesCompactModeV2HTTP, "", false},
		{"Gemini不补充", constant.APITypeGemini, types.RelayFormatOpenAIResponses, "", "", false},
		{"转换后不补充", constant.APITypeOpenAI, types.RelayFormatOpenAI, "", "", false},
		{"高级渠道原生路由", constant.APITypeAdvancedCustom, types.RelayFormatOpenAIResponses, "", "none", true},
		{"高级渠道转换路由", constant.APITypeAdvancedCustom, types.RelayFormatOpenAIResponses, "", "openai_responses_to_openai_chat_completions", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				RelayFormat: types.RelayFormatOpenAIResponses, FinalRequestRelayFormat: tc.format,
				RelayMode: relayconstant.RelayModeResponses, ResponsesCompactMode: tc.mode,
				RequestURLPath: "/v1/responses", OriginModelName: "gpt-6.1-sol",
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiType: tc.apiType, ChannelSetting: dto.ChannelSettings{ResponsesReasoningIncludedEnabled: true},
					ChannelOtherSettings: dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{
						Routes: []dto.AdvancedCustomRoute{{IncomingPath: "/v1/responses", Converter: tc.converter}},
					}},
				},
			}
			assert.Equal(t, tc.want, helper.ShouldSupplementResponsesReasoningHeader(info))
		})
	}
	assert.False(t, helper.ShouldSupplementResponsesReasoningHeader(nil))
}

func TestResponsesReasoningHeaderDoesNotAlterCommittedResponse(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	_, err := c.Writer.Write([]byte(": PING\n\n"))
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAIResponses, RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiType: constant.APITypeOpenAI, ChannelSetting: dto.ChannelSettings{ResponsesReasoningIncludedEnabled: true},
		},
	}
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	helper.SupplementResponsesReasoningHeader(c, info, resp)
	assert.Empty(t, resp.Header.Get("X-Reasoning-Included"))
}
