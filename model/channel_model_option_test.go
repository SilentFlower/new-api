package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListEnabledChannelModelOptionsExcludesDisabledChannelsAndSecrets(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&Channel{Id: 8101, Name: "视觉渠道", Key: "secret-key", Status: common.ChannelStatusEnabled, Models: "vision-b,vision-a"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: 8102, Name: "停用渠道", Key: "disabled-secret", Status: common.ChannelStatusManuallyDisabled, Models: "disabled-model"}).Error)

	options, err := ListEnabledChannelModelOptions()
	require.NoError(t, err)
	require.Len(t, options, 1)
	assert.Equal(t, 8101, options[0].ID)
	assert.Equal(t, "视觉渠道", options[0].Name)
	assert.Equal(t, []string{"vision-b", "vision-a"}, options[0].Models)
	payload, err := common.Marshal(options)
	require.NoError(t, err)
	assert.NotContains(t, string(payload), "secret-key")
}
