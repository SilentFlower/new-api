package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChannelGetUserWeeklyQuotaLimit 验证历史空值、显式零值和异常负值都按不限处理。
func TestChannelGetUserWeeklyQuotaLimit(t *testing.T) {
	zero := 0
	positive := 7000
	negative := -1
	tests := []struct {
		name     string
		channel  *Channel
		expected int
	}{
		{name: "空渠道", channel: nil, expected: 0},
		{name: "历史空值", channel: &Channel{}, expected: 0},
		{name: "显式零值", channel: &Channel{UserWeeklyQuotaLimit: &zero}, expected: 0},
		{name: "正数限制", channel: &Channel{UserWeeklyQuotaLimit: &positive}, expected: 7000},
		{name: "异常负数", channel: &Channel{UserWeeklyQuotaLimit: &negative}, expected: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, test.channel.GetUserWeeklyQuotaLimit())
		})
	}
}

// TestChannelUserLimitOverrideReplaceAndExpiry 验证覆盖整条替换、唯一性与到期回落。
func TestChannelUserLimitOverrideReplaceAndExpiry(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	daily := 2000
	first := &ChannelUserLimitOverride{
		ChannelId:           81,
		UserId:              123,
		UserDailyQuotaLimit: &daily,
		ExpiresAt:           now + 3600,
		UpdatedBy:           1,
	}
	require.NoError(t, ReplaceChannelUserLimitOverride(first))

	saved, err := GetActiveChannelUserLimitOverride(81, 123, now)
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, 2000, *saved.UserDailyQuotaLimit)

	weekly := 9000
	require.NoError(t, ReplaceChannelUserLimitOverride(&ChannelUserLimitOverride{
		ChannelId:            81,
		UserId:               123,
		UserWeeklyQuotaLimit: &weekly,
		ExpiresAt:            now + 7200,
		UpdatedBy:            2,
	}))
	saved, err = GetActiveChannelUserLimitOverride(81, 123, now)
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Nil(t, saved.UserDailyQuotaLimit)
	assert.Equal(t, 9000, *saved.UserWeeklyQuotaLimit)

	expired, err := GetActiveChannelUserLimitOverride(81, 123, now+7201)
	require.NoError(t, err)
	assert.Nil(t, expired)
}
