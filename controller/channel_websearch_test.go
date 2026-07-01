package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeChannelForResponseMasksWebSearchAPIKey(t *testing.T) {
	setting := `{"custom":true,"web_search":{"enabled":true,"provider":"tavily","api_key":"secret-key","max_results":5}}`
	channel := &model.Channel{Setting: &setting}

	sanitized := sanitizeChannelForResponse(channel)

	require.NotNil(t, sanitized)
	require.NotNil(t, channel.Setting)
	require.NotNil(t, sanitized.Setting)
	assert.Contains(t, *channel.Setting, "secret-key")
	assert.NotContains(t, *sanitized.Setting, "secret-key")

	var record map[string]any
	require.NoError(t, common.Unmarshal([]byte(*sanitized.Setting), &record))
	assert.Equal(t, true, record["custom"])
	webSearch, ok := record["web_search"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, webSearch["api_key_configured"])
	assert.NotContains(t, webSearch, "api_key")
}

func TestMergeChannelWebSearchAPIKeyInheritsOriginKey(t *testing.T) {
	originSetting := `{"web_search":{"enabled":true,"provider":"tavily","api_key":"old-key","max_results":5}}`
	updateSetting := `{"web_search":{"enabled":true,"provider":"tavily","max_results":3}}`
	origin := &model.Channel{Setting: &originSetting}
	update := &model.Channel{Setting: &updateSetting}

	require.NoError(t, mergeChannelWebSearchAPIKey(update, origin))

	var record map[string]any
	require.NoError(t, common.Unmarshal([]byte(*update.Setting), &record))
	webSearch, ok := record["web_search"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "old-key", webSearch["api_key"])
	assert.NotContains(t, webSearch, "api_key_configured")
	assert.NotContains(t, webSearch, "clear_api_key")
}

func TestMergeChannelWebSearchAPIKeyClearsWhenDisabled(t *testing.T) {
	originSetting := `{"web_search":{"enabled":true,"provider":"tavily","api_key":"old-key","max_results":5}}`
	updateSetting := `{"web_search":{"enabled":false,"provider":"tavily","clear_api_key":true}}`
	origin := &model.Channel{Setting: &originSetting}
	update := &model.Channel{Setting: &updateSetting}

	require.NoError(t, mergeChannelWebSearchAPIKey(update, origin))

	var record map[string]any
	require.NoError(t, common.Unmarshal([]byte(*update.Setting), &record))
	webSearch, ok := record["web_search"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, webSearch, "api_key")
	assert.NotContains(t, webSearch, "clear_api_key")
}
