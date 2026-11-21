package service

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostTextConsumeQuotaReturnsAgentSummaryReservation(t *testing.T) {
	const (
		userID         = 2301
		tokenID        = 2302
		channelID      = 2303
		subscriptionID = 2304
		preConsumed    = 5000
	)
	for _, test := range []struct {
		name         string
		fixedPrice   bool
		subscription bool
	}{
		{"按次钱包", true, false},
		{"按量钱包", false, false},
		{"阶梯计费订阅", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			setupTextQuotaGuardTest(t, true, false)
			seedTextQuotaGuardData(t, userID, tokenID, channelID)
			requestID := "agy-summary-" + test.name
			ctx := newTextQuotaGuardContext(requestID)
			info := newTextQuotaGuardRelayInfo(userID, tokenID, channelID, nil, relaycommon.StreamEndReasonEOF)
			info.RequestId = requestID
			info.TokenKey = "guard-token-key"
			info.ForcePreConsume = true
			info.SetEstimatePromptTokens(230753)
			info.UserSetting.BillingPreference = "wallet_only"
			info.PriceData.UsePrice = test.fixedPrice
			if test.fixedPrice {
				info.PriceData.ModelPrice = 0.01
			}
			var subscription model.UserSubscription
			if test.subscription {
				info.UserSetting.BillingPreference = "subscription_only"
				info.TieredBillingSnapshot = makeSnapshot(`tier("base", 10000 + p * 2)`, 1, 230753, 0)
				require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionPlan{}, &model.SubscriptionPreConsumeRecord{}))
				plan := model.SubscriptionPlan{Title: "本地标签计费测试"}
				require.NoError(t, model.DB.Create(&plan).Error)
				seedSubscription(t, subscriptionID, userID, 1000000, 100)
				require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", subscriptionID).Update("plan_id", plan.Id).Error)
				t.Cleanup(func() {
					require.NoError(t, model.DB.Where("request_id = ?", requestID).Delete(&model.SubscriptionPreConsumeRecord{}).Error)
					require.NoError(t, model.DB.Delete(&model.UserSubscription{}, subscriptionID).Error)
					require.NoError(t, model.DB.Delete(&plan).Error)
				})
			}

			session, apiErr := NewBillingSession(ctx, info, preConsumed)
			require.Nil(t, apiErr)
			info.Billing = session
			require.Equal(t, preConsumed, session.GetPreConsumedQuota())
			var token model.Token
			require.NoError(t, model.DB.First(&token, tokenID).Error)
			assert.Equal(t, 1000000-preConsumed, token.RemainQuota)
			if test.subscription {
				require.NoError(t, model.DB.First(&subscription, subscriptionID).Error)
				assert.EqualValues(t, 100+preConsumed, subscription.AmountUsed)
			} else {
				assert.Equal(t, 1000000-preConsumed, loadWalletBillingQuota(t, userID))
			}

			PostTextConsumeQuota(ctx, info, &dto.Usage{LocalResponse: dto.LocalResponseAGYAgentSummary}, nil)
			require.NoError(t, session.Settle(0))
			assert.False(t, session.NeedsRefund())
			assert.Equal(t, 1000000, loadWalletBillingQuota(t, userID))
			if test.subscription {
				require.NoError(t, model.DB.First(&subscription, subscriptionID).Error)
				assert.EqualValues(t, 100, subscription.AmountUsed)
			}
			assertTextQuotaGuardTokenUnchanged(t, tokenID)
			assertTextQuotaGuardUserAndChannelUsed(t, userID, channelID, 0, 0)
			logs := logsByRequestId(t, requestID)
			require.Len(t, logs, 1)
			assert.Equal(t, model.LogTypeConsume, logs[0].Type)
			assert.Equal(t, "upstream-"+requestID, logs[0].UpstreamRequestId)
			assert.Zero(t, logs[0].PromptTokens)
			assert.Zero(t, logs[0].CompletionTokens)
			assert.Zero(t, logs[0].Quota)
			var other map[string]any
			require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
			assert.Equal(t, dto.LocalResponseAGYAgentSummary, other["local_response"])
			assert.Zero(t, other["cache_tokens"])
			assert.Equal(t, info.BillingSource, other["billing_source"])
		})
	}
}

func TestPostTextConsumeQuotaAgentSummarySettlementFailure(t *testing.T) {
	for _, canRefund := range []bool{true, false} {
		name := "资金阶段已提交"
		if canRefund {
			name = "资金仍可退回"
		}
		t.Run(name, func(t *testing.T) {
			setupTextQuotaGuardTest(t, true, false)
			seedTextQuotaGuardData(t, 2301, 2302, 2303)
			ctx := newTextQuotaGuardContext("agy-summary-settlement-failed")
			billing := &textQuotaGuardBillingStub{settleErr: errors.New("结算故障样本"), needsRefund: canRefund}
			info := newTextQuotaGuardRelayInfo(2301, 2302, 2303, billing, relaycommon.StreamEndReasonEOF)

			PostTextConsumeQuota(ctx, info, &dto.Usage{LocalResponse: dto.LocalResponseAGYAgentSummary}, nil)

			assert.Equal(t, []int{0}, billing.settledQuotas)
			if canRefund {
				assert.Equal(t, 1, billing.refundCallCount)
			} else {
				assert.Zero(t, billing.refundCallCount)
			}
			logs := logsByRequestId(t, "agy-summary-settlement-failed")
			require.Len(t, logs, 1)
			assert.Equal(t, model.LogTypeError, logs[0].Type)
			assert.Zero(t, logs[0].Quota)
			var other map[string]any
			require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
			adminInfo, ok := other["admin_info"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, true, adminInfo["billing_settlement_failed"])
			assert.Equal(t, canRefund, adminInfo["billing_refund_fallback_triggered"])
		})
	}
}
