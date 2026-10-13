package dto_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSettingsResponsesReasoningHeaderJSONCompatibility(t *testing.T) {
	var legacy dto.ChannelSettings
	require.NoError(t, common.Unmarshal([]byte(`{"pass_through_body_enabled":true}`), &legacy))
	assert.False(t, legacy.ResponsesReasoningIncludedEnabled)
	for _, enabled := range []bool{false, true} {
		data, err := common.Marshal(dto.ChannelSettings{ResponsesReasoningIncludedEnabled: enabled})
		require.NoError(t, err)
		var restored dto.ChannelSettings
		require.NoError(t, common.Unmarshal(data, &restored))
		assert.Equal(t, enabled, restored.ResponsesReasoningIncludedEnabled)
	}
}
