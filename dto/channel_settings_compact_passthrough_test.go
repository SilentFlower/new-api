package dto_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSettingsResponsesCompactPassthroughJSONCompatibility(t *testing.T) {
	var legacy dto.ChannelSettings
	require.NoError(t, common.Unmarshal([]byte(`{"pass_through_body_enabled":true}`), &legacy))
	assert.False(t, legacy.ResponsesCompactPassthroughEnabled)

	enabled := dto.ChannelSettings{ResponsesCompactPassthroughEnabled: true}
	data, err := common.Marshal(enabled)
	require.NoError(t, err)
	var encoded map[string]any
	require.NoError(t, common.Unmarshal(data, &encoded))
	assert.Equal(t, true, encoded["responses_compact_passthrough_enabled"])

	var restored dto.ChannelSettings
	require.NoError(t, common.Unmarshal(data, &restored))
	assert.True(t, restored.ResponsesCompactPassthroughEnabled)
}
