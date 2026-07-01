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
