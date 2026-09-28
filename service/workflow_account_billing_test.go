package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// TestWorkflowAccountsChargeSeparateWallets 验证共用渠道下两枚令牌分别扣自己的用户余额。
func TestWorkflowAccountsChargeSeparateWallets(t *testing.T) {
	truncate(t)
	accounts := []struct {
		username   string
		workflowID string
		key        string
		quota      int
		charge     int
	}{
		{"物料检索", "material-search", "workflow-a-test-key", 100, 15},
		{"类目审核", "category-audit", "workflow-b-test-key", 200, 25},
	}
	users := make([]model.User, 0, len(accounts))
	for _, account := range accounts {
		user := model.User{Username: account.username, DisplayName: account.username,
			Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
			Group: "default", Quota: account.quota, AffCode: account.workflowID}
		require.NoError(t, model.DB.Create(&user).Error)
		token := model.Token{UserId: user.Id, Key: account.key, Name: account.workflowID,
			Status: common.TokenStatusEnabled, UnlimitedQuota: true, Group: "default"}
		require.NoError(t, model.DB.Create(&token).Error)
		users = append(users, user)
	}

	for index, account := range accounts {
		token, err := model.GetTokenByKey(account.key, true)
		require.NoError(t, err)
		require.Equal(t, users[index].Id, token.UserId)
		require.Equal(t, account.workflowID, token.Name)
		info := &relaycommon.RelayInfo{UserId: token.UserId, TokenId: token.Id,
			TokenKey: account.key, TokenUnlimited: true, BillingSource: BillingSourceWallet}
		require.NoError(t, PostConsumeQuota(info, account.charge, 0, false))
		for userIndex, user := range users {
			quota, err := model.GetUserQuota(user.Id, true)
			require.NoError(t, err)
			expected := accounts[userIndex].quota
			if userIndex <= index {
				expected -= accounts[userIndex].charge
			}
			require.Equal(t, expected, quota)
		}
	}
}
