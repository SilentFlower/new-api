package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelGetUserConcurrencyLimit(t *testing.T) {
	zero := 0
	positive := 4
	negative := -1

	tests := []struct {
		name     string
		channel  *Channel
		expected int
	}{
		{name: "空渠道", channel: nil, expected: 0},
		{name: "历史空值", channel: &Channel{}, expected: 0},
		{name: "显式零值", channel: &Channel{UserConcurrencyLimit: &zero}, expected: 0},
		{name: "正数限制", channel: &Channel{UserConcurrencyLimit: &positive}, expected: 4},
		{name: "异常负数", channel: &Channel{UserConcurrencyLimit: &negative}, expected: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, test.channel.GetUserConcurrencyLimit())
		})
	}
}

func TestChannelUserConcurrencyLimitPersistsNullPositiveAndZero(t *testing.T) {
	truncateTables(t)
	historical := &Channel{
		Id:     801,
		Name:   "历史渠道",
		Models: "test-model",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, DB.Create(historical).Error)

	var saved Channel
	require.NoError(t, DB.First(&saved, historical.Id).Error)
	assert.Nil(t, saved.UserConcurrencyLimit)
	assert.Zero(t, saved.GetUserConcurrencyLimit())

	positive := 4
	saved.UserConcurrencyLimit = &positive
	require.NoError(t, saved.Update())
	require.NoError(t, DB.First(&saved, historical.Id).Error)
	require.NotNil(t, saved.UserConcurrencyLimit)
	assert.Equal(t, 4, *saved.UserConcurrencyLimit)

	zero := 0
	saved.UserConcurrencyLimit = &zero
	require.NoError(t, saved.Update())
	require.NoError(t, DB.First(&saved, historical.Id).Error)
	require.NotNil(t, saved.UserConcurrencyLimit)
	assert.Zero(t, *saved.UserConcurrencyLimit)
}
