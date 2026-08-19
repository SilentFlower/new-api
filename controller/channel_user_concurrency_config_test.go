package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateChannelUserConcurrencyLimit(t *testing.T) {
	tests := []struct {
		name    string
		limit   *int
		wantErr bool
	}{
		{name: "历史空值"},
		{name: "不限制", limit: common.GetPointer(0)},
		{name: "正数限制", limit: common.GetPointer(4)},
		{name: "最大限制", limit: common.GetPointer(1000)},
		{name: "负数", limit: common.GetPointer(-1), wantErr: true},
		{name: "超过上限", limit: common.GetPointer(1001), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateChannelUserConcurrencyLimit(test.limit)
			if test.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestNormalizeChannelUserConcurrencyLimitForUpdate(t *testing.T) {
	t.Run("字段缺失时保留空值", func(t *testing.T) {
		channel := &model.Channel{}
		normalizeChannelUserConcurrencyLimitForUpdate(channel, map[string]any{})
		assert.Nil(t, channel.UserConcurrencyLimit)
	})

	t.Run("显式空值归一化为零", func(t *testing.T) {
		channel := &model.Channel{}
		normalizeChannelUserConcurrencyLimitForUpdate(channel, map[string]any{"user_concurrency_limit": nil})
		require.NotNil(t, channel.UserConcurrencyLimit)
		assert.Zero(t, *channel.UserConcurrencyLimit)
	})

	t.Run("显式数值保持不变", func(t *testing.T) {
		channel := &model.Channel{UserConcurrencyLimit: common.GetPointer(4)}
		normalizeChannelUserConcurrencyLimitForUpdate(channel, map[string]any{"user_concurrency_limit": float64(4)})
		require.NotNil(t, channel.UserConcurrencyLimit)
		assert.Equal(t, 4, *channel.UserConcurrencyLimit)
	})
}

func TestChannelUserConcurrencyLimitRejectsDecimalJSON(t *testing.T) {
	var channel PatchChannel
	err := common.Unmarshal([]byte(`{"id":1,"user_concurrency_limit":1.5}`), &channel)
	assert.Error(t, err)
}

func TestChannelUserConcurrencyLimitIsNonSensitive(t *testing.T) {
	origin := &model.Channel{UserConcurrencyLimit: common.GetPointer(0)}
	updated := PatchChannel{Channel: *origin}
	updated.UserConcurrencyLimit = common.GetPointer(4)

	assert.False(t, channelHasSensitiveChanges(&updated, origin, map[string]any{
		"user_concurrency_limit": 4,
	}))
}

func TestValidateChannelUserDailyQuotaLimit(t *testing.T) {
	tests := []struct {
		name    string
		limit   *int
		wantErr bool
	}{
		{name: "历史空值"},
		{name: "不限制", limit: common.GetPointer(0)},
		{name: "正数限制", limit: common.GetPointer(1000)},
		{name: "最大限制", limit: common.GetPointer(common.MaxQuota)},
		{name: "负数", limit: common.GetPointer(-1), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateChannelUserDailyQuotaLimit(test.limit)
			if test.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestNormalizeChannelUserDailyQuotaLimitForUpdate(t *testing.T) {
	t.Run("字段缺失时保留空值", func(t *testing.T) {
		channel := &model.Channel{}
		normalizeChannelUserDailyQuotaLimitForUpdate(channel, map[string]any{})
		assert.Nil(t, channel.UserDailyQuotaLimit)
	})

	t.Run("显式空值归一化为零", func(t *testing.T) {
		channel := &model.Channel{}
		normalizeChannelUserDailyQuotaLimitForUpdate(channel, map[string]any{"user_daily_quota_limit": nil})
		require.NotNil(t, channel.UserDailyQuotaLimit)
		assert.Zero(t, *channel.UserDailyQuotaLimit)
	})
}

func TestChannelUserDailyQuotaLimitRejectsDecimalJSON(t *testing.T) {
	var channel PatchChannel
	err := common.Unmarshal([]byte(`{"id":1,"user_daily_quota_limit":1.5}`), &channel)
	assert.Error(t, err)
}

func TestChannelUserDailyQuotaLimitIsNonSensitive(t *testing.T) {
	origin := &model.Channel{UserDailyQuotaLimit: common.GetPointer(0)}
	updated := PatchChannel{Channel: *origin}
	updated.UserDailyQuotaLimit = common.GetPointer(1000)

	assert.False(t, channelHasSensitiveChanges(&updated, origin, map[string]any{
		"user_daily_quota_limit": 1000,
	}))
}
