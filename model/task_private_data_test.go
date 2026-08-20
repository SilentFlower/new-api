package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskPrivateDataScanIgnoresHistoricalDailyQuotaTrackingFlag(t *testing.T) {
	var privateData TaskPrivateData

	err := privateData.Scan([]byte(`{"billing_source":"wallet","channel_user_daily_quota_tracked":true}`))

	require.NoError(t, err)
	assert.Equal(t, "wallet", privateData.BillingSource)
}
