package relay

import (
	"encoding/json"
	"testing"

	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesRequestFromCompactionUsesCanonicalFields(t *testing.T) {
	reasoning := &dto.Reasoning{Effort: "high", Summary: "auto"}
	request := &dto.OpenAIResponsesCompactionRequest{
		Model:                "gpt-5",
		Input:                json.RawMessage(`[{"role":"user","content":"hello"}]`),
		Instructions:         json.RawMessage(`"compact carefully"`),
		PreviousResponseID:   "resp_1",
		Tools:                json.RawMessage(`[{"type":"function","name":"lookup"}]`),
		ParallelToolCalls:    json.RawMessage(`false`),
		Reasoning:            reasoning,
		ServiceTier:          "priority",
		PromptCacheKey:       json.RawMessage(`"cache-key"`),
		PromptCacheOptions:   json.RawMessage(`{"scope":"24h"}`),
		PromptCacheRetention: json.RawMessage(`"24h"`),
		Text:                 json.RawMessage(`{"format":{"type":"text"}}`),
	}

	converted := responsesRequestFromCompaction(request)
	require.NotNil(t, converted)
	assert.Equal(t, request.Model, converted.Model)
	assert.JSONEq(t, string(request.Input), string(converted.Input))
	assert.JSONEq(t, string(request.Instructions), string(converted.Instructions))
	assert.Empty(t, converted.PreviousResponseID)
	assert.JSONEq(t, string(request.Tools), string(converted.Tools))
	assert.JSONEq(t, string(request.ParallelToolCalls), string(converted.ParallelToolCalls))
	assert.Same(t, reasoning, converted.Reasoning)
	assert.Equal(t, request.ServiceTier, converted.ServiceTier)
	assert.JSONEq(t, string(request.PromptCacheKey), string(converted.PromptCacheKey))
	assert.Empty(t, converted.PromptCacheOptions)
	assert.Empty(t, converted.PromptCacheRetention)
	assert.JSONEq(t, string(request.Text), string(converted.Text))
}

func TestResponsesRequestForCompactionDropsResponsesOnlyFields(t *testing.T) {
	stream := true
	request := &dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		Input:              json.RawMessage(`[{"type":"compaction_trigger"}]`),
		Instructions:       json.RawMessage(`"compact carefully"`),
		Include:            json.RawMessage(`["reasoning.encrypted_content"]`),
		PreviousResponseID: "resp_1",
		Store:              json.RawMessage(`false`),
		Stream:             &stream,
		ClientMetadata:     json.RawMessage(`{"session":"s1"}`),
		PromptCacheOptions: json.RawMessage(`{"scope":"24h"}`),
	}

	converted := responsesRequestForCompaction(request)

	require.NotNil(t, converted)
	assert.Equal(t, request.Model, converted.Model)
	assert.JSONEq(t, string(request.Input), string(converted.Input))
	assert.Empty(t, converted.Include)
	assert.Empty(t, converted.PreviousResponseID)
	assert.Empty(t, converted.Store)
	assert.Nil(t, converted.Stream)
	assert.Empty(t, converted.ClientMetadata)
	assert.Empty(t, converted.PromptCacheOptions)
}

func TestResponsesCompactRequestURLForSupportedChannels(t *testing.T) {
	tests := []struct {
		name     string
		apiType  int
		info     *relaycommon.RelayInfo
		expected string
	}{
		{
			name:    "OpenAI 渠道",
			apiType: appconstant.APITypeOpenAI,
			info: &relaycommon.RelayInfo{
				RequestURLPath: "/v1/responses/compact",
				RelayMode:      relayconstant.RelayModeResponsesCompact,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:    appconstant.ChannelTypeOpenAI,
					ChannelBaseUrl: "https://openai.example",
				},
			},
			expected: "https://openai.example/v1/responses/compact",
		},
		{
			name:    "Codex 渠道",
			apiType: appconstant.APITypeCodex,
			info: &relaycommon.RelayInfo{
				RequestURLPath: "/v1/responses/compact",
				RelayMode:      relayconstant.RelayModeResponsesCompact,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:    appconstant.ChannelTypeCodex,
					ChannelBaseUrl: "https://chatgpt.com",
				},
			},
			expected: "https://chatgpt.com/backend-api/codex/responses/compact",
		},
		{
			name:    "Azure 渠道",
			apiType: appconstant.APITypeOpenAI,
			info: &relaycommon.RelayInfo{
				RequestURLPath: "/v1/responses/compact",
				RelayMode:      relayconstant.RelayModeResponsesCompact,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:    appconstant.ChannelTypeAzure,
					ChannelBaseUrl: "https://azure.example",
				},
			},
			expected: "https://azure.example/openai/v1/responses/compact?api-version=preview",
		},
		{
			name:    "OpenAI 历史 body bridge",
			apiType: appconstant.APITypeOpenAI,
			info: &relaycommon.RelayInfo{
				RequestURLPath:       "/v1/responses",
				RelayMode:            relayconstant.RelayModeResponses,
				ResponsesCompactMode: relayconstant.ResponsesCompactModeV1BodyBridge,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:    appconstant.ChannelTypeOpenAI,
					ChannelBaseUrl: "https://openai.example",
				},
			},
			expected: "https://openai.example/v1/responses/compact",
		},
		{
			name:    "Codex 历史 body bridge",
			apiType: appconstant.APITypeCodex,
			info: &relaycommon.RelayInfo{
				RequestURLPath:       "/v1/responses",
				RelayMode:            relayconstant.RelayModeResponses,
				ResponsesCompactMode: relayconstant.ResponsesCompactModeV1BodyBridge,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:    appconstant.ChannelTypeCodex,
					ChannelBaseUrl: "https://chatgpt.com",
				},
			},
			expected: "https://chatgpt.com/backend-api/codex/responses/compact",
		},
		{
			name:    "Azure 历史 body bridge",
			apiType: appconstant.APITypeOpenAI,
			info: &relaycommon.RelayInfo{
				RequestURLPath:       "/v1/responses",
				RelayMode:            relayconstant.RelayModeResponses,
				ResponsesCompactMode: relayconstant.ResponsesCompactModeV1BodyBridge,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:    appconstant.ChannelTypeAzure,
					ChannelBaseUrl: "https://azure.example",
				},
			},
			expected: "https://azure.example/openai/v1/responses/compact?api-version=preview",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adaptor := GetAdaptor(test.apiType)
			require.NotNil(t, adaptor)
			adaptor.Init(test.info)

			actual, err := adaptor.GetRequestURL(test.info)

			require.NoError(t, err)
			assert.Equal(t, test.expected, actual)
		})
	}
}
