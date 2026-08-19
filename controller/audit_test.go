package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAuditContentENRendersChannelUserDailyQuotaAdjustment(t *testing.T) {
	content := auditContentEN("channel.user_daily_quota_set", map[string]interface{}{
		"channel_id": 1201,
		"user_id":    1202,
		"used_quota": 300,
	})

	assert.Equal(t, "Set daily used quota for user 1202 on channel 1201 to 300", content)
}
