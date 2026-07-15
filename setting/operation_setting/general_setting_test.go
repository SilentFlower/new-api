package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneralSettingMissingNonStreamKeepAliveDefaultsDisabled(t *testing.T) {
	setting := GeneralSetting{}
	manager := config.NewConfigManager()
	manager.Register("general_setting", &setting)

	err := manager.LoadFromDB(map[string]string{
		"general_setting.ping_interval_enabled": "true",
		"general_setting.ping_interval_seconds": "30",
	})

	require.NoError(t, err)
	assert.True(t, setting.PingIntervalEnabled)
	assert.Equal(t, 30, setting.PingIntervalSeconds)
	assert.False(t, setting.NonStreamKeepAliveEnabled)
}
