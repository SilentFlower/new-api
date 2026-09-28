package model

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestGetWorkflowAccountStatusDoesNotExposeKey(t *testing.T) {
	t.Cleanup(func() {
		DB.Exec("DELETE FROM tokens")
		DB.Exec("DELETE FROM users")
	})
	user := User{Username: "中文工作流", DisplayName: "中文工作流", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 123}
	require.NoError(t, DB.Create(&user).Error)
	token := Token{UserId: user.Id, Key: "secret-workflow-key", Name: "workflow-original-id",
		Status: common.TokenStatusEnabled, UnlimitedQuota: true, Group: "default"}
	require.NoError(t, DB.Create(&token).Error)

	status, err := GetWorkflowAccountStatus(user.Username, token.Name)
	require.NoError(t, err)
	require.Equal(t, user.Id, status.User.ID)
	require.Equal(t, token.Id, status.Tokens[0].ID)
	require.True(t, status.Tokens[0].EffectiveStateMatches)
	payload, err := json.Marshal(status)
	require.NoError(t, err)
	require.NotContains(t, string(payload), token.Key)
	require.NotContains(t, string(payload), "password")

	missing, err := GetWorkflowAccountStatus("不存在的工作流", token.Name)
	require.NoError(t, err)
	require.Nil(t, missing.User)
	require.Empty(t, missing.Tokens)
}

func TestGetWorkflowAccountStatusRejectsStaleTokenCache(t *testing.T) {
	t.Cleanup(func() {
		DB.Exec("DELETE FROM tokens")
		DB.Exec("DELETE FROM users")
	})
	useUserCacheMiniRedis(t)
	user := User{Username: "缓存归属校验", DisplayName: "缓存归属校验", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(&user).Error)
	token := Token{UserId: user.Id, Key: "workflow-stale-cache-key", Name: "workflow-stale-cache",
		Status: common.TokenStatusEnabled, UnlimitedQuota: true, Group: "default", ExpiredTime: -1}
	require.NoError(t, DB.Create(&token).Error)
	stale := token
	stale.UserId = user.Id + 100
	require.NoError(t, cacheSetToken(stale))

	status, err := GetWorkflowAccountStatus(user.Username, token.Name)
	require.NoError(t, err)
	require.False(t, status.Tokens[0].EffectiveStateMatches)

	require.NoError(t, cacheSetToken(token))
	status, err = GetWorkflowAccountStatus(user.Username, token.Name)
	require.NoError(t, err)
	require.True(t, status.Tokens[0].EffectiveStateMatches)
}
