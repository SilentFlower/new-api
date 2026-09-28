package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestValidateMigrateTargetsPreservesLegacyAndAcceptsZeroQuota(t *testing.T) {
	legacy, err := validateMigrateTargets([]int{1}, nil)
	require.NoError(t, err)
	require.Nil(t, legacy)

	zero := 0
	targets, err := validateMigrateTargets([]int{2}, []migrateTokenTarget{{TokenId: 2, Username: "标书生成工作流", UserQuota: &zero}})
	require.NoError(t, err)
	require.Equal(t, "标书生成工作流", targets[2].Username)
	require.Equal(t, 0, *targets[2].UserQuota)
}

func TestValidateMigrateTargetsRejectsIncompleteOrAmbiguousBatch(t *testing.T) {
	quota := 100
	target := migrateTokenTarget{TokenId: 1, Username: "工作流一", UserQuota: &quota}
	cases := []struct {
		name    string
		ids     []int
		targets []migrateTokenTarget
	}{
		{"缺少目标", []int{1}, []migrateTokenTarget{}},
		{"重复令牌", []int{1, 1}, []migrateTokenTarget{target, target}},
		{"目标不匹配", []int{2}, []migrateTokenTarget{target}},
		{"重复用户名", []int{1, 2}, []migrateTokenTarget{target, {TokenId: 2, Username: "工作流一", UserQuota: &quota}}},
		{"缺少额度", []int{1}, []migrateTokenTarget{{TokenId: 1, Username: "工作流一"}}},
		{"额度为负", []int{1}, []migrateTokenTarget{{TokenId: 1, Username: "工作流一", UserQuota: new(int)}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "额度为负" {
				*tc.targets[0].UserQuota = -1
			}
			_, err := validateMigrateTargets(tc.ids, tc.targets)
			require.Error(t, err)
		})
	}
	max := int(1000000000 * common.QuotaPerUnit)
	over := max + 1
	_, err := validateMigrateTargets([]int{1}, []migrateTokenTarget{{TokenId: 1, Username: "工作流一", UserQuota: &over}})
	require.ErrorContains(t, err, "user_quota")
}

func TestMigrateSingleTokenInTxUsesExplicitNameAndZeroQuota(t *testing.T) {
	db := setupTokenMigrateControllerTestDB(t)
	src := &model.User{Username: "root-explicit", Password: "placeholder", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "explicit-root-aff"}
	require.NoError(t, db.Create(src).Error)
	token := &model.Token{UserId: src.Id, Key: "explicit-token-key", Name: "workflow-original-id", Status: common.TokenStatusEnabled, UnlimitedQuota: true, Group: "default"}
	require.NoError(t, db.Create(token).Error)
	zero := 0
	target := &migrateTokenTarget{TokenId: token.Id, Username: "中文工作流", UserQuota: &zero}

	username, userID, err := migrateSingleTokenInTx(nil, src, token, map[string]bool{}, target)
	require.NoError(t, err)
	require.Equal(t, target.Username, username)
	var user model.User
	require.NoError(t, db.First(&user, userID).Error)
	require.Equal(t, 0, user.Quota)
	require.Equal(t, common.RoleCommonUser, user.Role)
	require.True(t, user.GetSetting().RecordIpLog)
	var moved model.Token
	require.NoError(t, db.First(&moved, token.Id).Error)
	require.Equal(t, userID, moved.UserId)
	require.Equal(t, token.Key, moved.Key)
	require.Equal(t, token.Name, moved.Name)
}

func TestMigrateSingleTokenInTxConflictKeepsOriginalOwner(t *testing.T) {
	db := setupTokenMigrateControllerTestDB(t)
	src := &model.User{Username: "root-conflict", Password: "placeholder", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AffCode: "conflict-root-aff"}
	existing := &model.User{Username: "已占用工作流", Password: "placeholder", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "conflict-existing-aff"}
	require.NoError(t, db.Create(src).Error)
	require.NoError(t, db.Create(existing).Error)
	token := &model.Token{UserId: src.Id, Key: "conflict-token-key", Name: "conflict-workflow", Status: common.TokenStatusEnabled}
	require.NoError(t, db.Create(token).Error)
	quota := 100
	_, _, err := migrateSingleTokenInTx(nil, src, token, map[string]bool{}, &migrateTokenTarget{TokenId: token.Id, Username: existing.Username, UserQuota: &quota})
	require.ErrorContains(t, err, "已存在")
	var actual model.Token
	require.NoError(t, db.First(&actual, token.Id).Error)
	require.Equal(t, src.Id, actual.UserId)
	var users int64
	require.NoError(t, db.Model(&model.User{}).Count(&users).Error)
	require.EqualValues(t, 2, users)
}
