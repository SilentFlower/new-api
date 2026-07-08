package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelWebSearchSettingsNormalize(t *testing.T) {
	settings := ChannelWebSearchSettings{
		Enabled:      true,
		Provider:     "ANY_SEARCH",
		APIKey:       " sk-test ",
		MaxResults:   100,
		SearchDepth:  "invalid",
		Freshness:    "decade",
		ContentTypes: []string{"web", "news", "bad", "web"},
	}

	settings.Normalize()

	assert.Equal(t, ChannelWebSearchProviderAnySearch, settings.Provider)
	assert.Equal(t, 20, settings.MaxResults)
	assert.Equal(t, "basic", settings.SearchDepth)
	assert.Empty(t, settings.Freshness)
	assert.Equal(t, []string{"web", "news"}, settings.ContentTypes)
	assert.Equal(t, "sk-test", settings.APIKey)
	assert.True(t, settings.APIKeyConfigured)
	require.NoError(t, settings.ValidateForRelay())
}

func TestChannelWebSearchSettingsValidateForRelayRequiresTavilyKey(t *testing.T) {
	settings := ChannelWebSearchSettings{
		Enabled:  true,
		Provider: ChannelWebSearchProviderTavily,
	}

	err := settings.ValidateForRelay()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "API Key")
}

func TestChannelWebSearchSettingsValidateForRelayAllowsAnySearchWithoutKey(t *testing.T) {
	settings := ChannelWebSearchSettings{
		Enabled:  true,
		Provider: ChannelWebSearchProviderAnySearch,
	}

	require.NoError(t, settings.ValidateForRelay())
}

func TestAdvancedCustomValidateResponsesToChatConverterPath(t *testing.T) {
	valid := &AdvancedCustomConfig{
		Routes: []AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/chat/completions",
				Converter:    AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions,
			},
		},
	}
	require.NoError(t, valid.Validate())

	tests := []struct {
		name         string
		incomingPath string
	}{
		{name: "chat completions", incomingPath: "/v1/chat/completions"},
		{name: "responses compact", incomingPath: "/v1/responses/compact"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &AdvancedCustomConfig{
				Routes: []AdvancedCustomRoute{
					{
						IncomingPath: tt.incomingPath,
						UpstreamPath: "/v1/chat/completions",
						Converter:    AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions,
					},
				},
			}
			err := config.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "converter does not match incoming_path")
		})
	}
}
