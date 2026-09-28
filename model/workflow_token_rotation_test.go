package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestWorkflowTokenRotationKeepsAccountAndDisablesOldToken(t *testing.T) {
	t.Cleanup(func() {
		DB.Exec("DELETE FROM tokens")
		DB.Exec("DELETE FROM users")
	})
	user := &User{Username: "工作流账号", DisplayName: "工作流账号", Password: "placeholder", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "rotation-aff"}
	user.SetSetting(dto.UserSetting{RecordIpLog: true})
	require.NoError(t, DB.Create(user).Error)
	old := &Token{UserId: user.Id, Key: "rotation-old-key", Name: "workflow-original", Group: "default", Status: common.TokenStatusEnabled, UnlimitedQuota: true, ExpiredTime: -1}
	require.NoError(t, DB.Create(old).Error)

	replacement, err := IssueWorkflowReplacementToken(user.Id, old.Id, old.Name)
	require.NoError(t, err)
	require.NotEqual(t, old.Key, replacement.Key)
	require.Equal(t, old.UserId, replacement.UserId)
	require.Equal(t, old.Name, replacement.Name)
	require.Equal(t, old.Group, replacement.Group)
	require.True(t, replacement.UnlimitedQuota)
	repeated, err := IssueWorkflowReplacementToken(user.Id, old.Id, old.Name)
	require.NoError(t, err)
	require.Equal(t, replacement.Id, repeated.Id)
	require.Equal(t, replacement.Key, repeated.Key)
	var active int64
	require.NoError(t, DB.Model(&Token{}).Where("user_id = ? AND name = ? AND status = ?",
		user.Id, old.Name, common.TokenStatusEnabled).Count(&active).Error)
	require.EqualValues(t, 2, active)

	require.NoError(t, RetireWorkflowToken(user.Id, old.Id, replacement.Id))
	var stored Token
	require.NoError(t, DB.First(&stored, old.Id).Error)
	require.Equal(t, common.TokenStatusDisabled, stored.Status)
	require.NoError(t, RetireWorkflowToken(user.Id, old.Id, replacement.Id))
}

func TestWorkflowTokenRotationRejectsCrossAccountReplacement(t *testing.T) {
	t.Cleanup(func() {
		DB.Exec("DELETE FROM tokens")
		DB.Exec("DELETE FROM users")
	})
	first := &User{Username: "工作流一", Password: "placeholder", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "rotation-first"}
	second := &User{Username: "工作流二", Password: "placeholder", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "rotation-second"}
	first.SetSetting(dto.UserSetting{RecordIpLog: true})
	second.SetSetting(dto.UserSetting{RecordIpLog: true})
	require.NoError(t, DB.Create(first).Error)
	require.NoError(t, DB.Create(second).Error)
	old := &Token{UserId: first.Id, Key: "rotation-first-key", Name: "workflow-one", Status: common.TokenStatusEnabled, UnlimitedQuota: true}
	other := &Token{UserId: second.Id, Key: "rotation-second-key", Name: "workflow-one", Status: common.TokenStatusEnabled, UnlimitedQuota: true}
	require.NoError(t, DB.Create(old).Error)
	require.NoError(t, DB.Create(other).Error)

	require.Error(t, RetireWorkflowToken(first.Id, old.Id, other.Id))
	var stored Token
	require.NoError(t, DB.First(&stored, old.Id).Error)
	require.Equal(t, common.TokenStatusEnabled, stored.Status)
	_, err := IssueWorkflowReplacementToken(first.Id, other.Id, old.Name)
	require.Error(t, err)
}

func TestWorkflowTokenRotationRejectsRelaxedReplacementPolicy(t *testing.T) {
	t.Cleanup(func() {
		DB.Exec("DELETE FROM tokens")
		DB.Exec("DELETE FROM users")
	})
	user := &User{Username: "工作流受限账号", Password: "placeholder", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AffCode: "rotation-limits"}
	user.SetSetting(dto.UserSetting{RecordIpLog: true})
	require.NoError(t, DB.Create(user).Error)
	allowedIP := "192.0.2.1"
	old := &Token{UserId: user.Id, Key: "rotation-limited-old", Name: "workflow-limited", Group: "default",
		Status: common.TokenStatusEnabled, UnlimitedQuota: true, ExpiredTime: -1,
		ModelLimitsEnabled: true, ModelLimits: "model-a", AllowIps: &allowedIP}
	require.NoError(t, DB.Create(old).Error)
	relaxed := &Token{UserId: user.Id, Key: "rotation-relaxed-new", Name: old.Name, Group: old.Group,
		Status: common.TokenStatusEnabled, UnlimitedQuota: true, ExpiredTime: old.ExpiredTime}
	require.NoError(t, DB.Create(relaxed).Error)

	_, err := IssueWorkflowReplacementToken(user.Id, old.Id, old.Name)
	require.Error(t, err)
	require.Error(t, RetireWorkflowToken(user.Id, old.Id, relaxed.Id))
	var stored Token
	require.NoError(t, DB.First(&stored, old.Id).Error)
	require.Equal(t, common.TokenStatusEnabled, stored.Status)
}
