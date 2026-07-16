package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newWalletBillingSessionTestContext() *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	return c
}

func walletBillingRelayInfo(userID int, tokenID int, tokenKey string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        tokenKey,
		ForcePreConsume: true,
		IsPlayground:    true,
		RequestId:       "billing-session-test",
		OriginModelName: "gpt-test",
		UserSetting: dto.UserSetting{
			BillingPreference: "wallet_only",
		},
	}
}

func loadWalletBillingQuota(t *testing.T, userID int) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	return user.Quota
}

func TestBillingSessionReserveAndSettleAreIdempotent(t *testing.T) {
	truncate(t)
	seedUser(t, 901, 1000)
	c := newWalletBillingSessionTestContext()
	info := walletBillingRelayInfo(901, 902, "billing-settle-token")

	session, apiErr := NewBillingSession(c, info, 100)
	require.Nil(t, apiErr)
	require.NoError(t, session.Reserve(160))
	require.NoError(t, session.Reserve(160))
	assert.Equal(t, 840, loadWalletBillingQuota(t, 901))

	require.NoError(t, session.Settle(120))
	require.NoError(t, session.Settle(120))
	session.Refund(c)
	assert.Equal(t, 880, loadWalletBillingQuota(t, 901))
	assert.False(t, session.NeedsRefund())
}

func TestBillingSessionRefundsReservedQuotaOnlyOnce(t *testing.T) {
	truncate(t)
	seedUser(t, 903, 1000)
	c := newWalletBillingSessionTestContext()
	info := walletBillingRelayInfo(903, 904, "billing-refund-token")

	session, apiErr := NewBillingSession(c, info, 100)
	require.Nil(t, apiErr)
	require.NoError(t, session.Reserve(160))
	session.Refund(c)
	session.Refund(c)

	require.Eventually(t, func() bool {
		return loadWalletBillingQuota(t, 903) == 1000
	}, time.Second, 10*time.Millisecond)
	assert.False(t, session.NeedsRefund())
}

func TestBillingSessionRejectsInsufficientQuotaWithoutPartialDeduction(t *testing.T) {
	truncate(t)
	seedUser(t, 905, 50)
	c := newWalletBillingSessionTestContext()
	info := walletBillingRelayInfo(905, 906, "billing-insufficient-token")

	session, apiErr := NewBillingSession(c, info, 100)

	require.Nil(t, session)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	assert.Equal(t, 50, loadWalletBillingQuota(t, 905))
}
