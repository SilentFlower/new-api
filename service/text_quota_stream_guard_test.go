package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type textQuotaGuardBillingStub struct {
	settledQuotas   []int
	settleErr       error
	needsRefund     bool
	refundCallCount int
}

// Settle 记录文本计费收口传入的实际结算额度。
// @param actualQuota 实际结算额度。
// @return 测试配置的结算错误。
func (s *textQuotaGuardBillingStub) Settle(actualQuota int) error {
	s.settledQuotas = append(s.settledQuotas, actualQuota)
	return s.settleErr
}

// Refund 记录测试中的退款调用。
// @param c 当前 Gin 请求上下文。
func (s *textQuotaGuardBillingStub) Refund(c *gin.Context) { s.refundCallCount++ }

// NeedsRefund 返回测试会话是否仍有可退款的预扣状态。
// @return 测试配置的退款状态。
func (s *textQuotaGuardBillingStub) NeedsRefund() bool { return s.needsRefund }

// GetPreConsumedQuota 返回测试会话的预扣额度。
// @return 固定返回零。
func (s *textQuotaGuardBillingStub) GetPreConsumedQuota() int { return 0 }

// Reserve 接受测试会话的目标预扣额度。
// @param targetQuota 目标预扣额度。
// @return 始终返回 nil。
func (s *textQuotaGuardBillingStub) Reserve(targetQuota int) error { return nil }

func setupTextQuotaGuardTest(t *testing.T, errorLogEnabled bool, dataExportEnabled bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	originalErrorLogEnabled := constant.ErrorLogEnabled
	originalDataExportEnabled := common.DataExportEnabled
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalBatchUpdateEnabled := common.BatchUpdateEnabled
	originalRedisEnabled := common.RedisEnabled

	constant.ErrorLogEnabled = errorLogEnabled
	common.DataExportEnabled = dataExportEnabled
	common.LogConsumeEnabled = true
	common.BatchUpdateEnabled = false
	common.RedisEnabled = false

	clearTextQuotaGuardTables(t)
	clearTextQuotaGuardCache()
	t.Cleanup(func() {
		clearTextQuotaGuardTables(t)
		clearTextQuotaGuardCache()
		constant.ErrorLogEnabled = originalErrorLogEnabled
		common.DataExportEnabled = originalDataExportEnabled
		common.LogConsumeEnabled = originalLogConsumeEnabled
		common.BatchUpdateEnabled = originalBatchUpdateEnabled
		common.RedisEnabled = originalRedisEnabled
	})
}

func clearTextQuotaGuardTables(t *testing.T) {
	t.Helper()
	for _, table := range []string{"logs", "quota_data", "users", "tokens", "channels"} {
		require.NoError(t, model.DB.Exec("DELETE FROM "+table).Error)
	}
}

func clearTextQuotaGuardCache() {
	model.CacheQuotaDataLock.Lock()
	defer model.CacheQuotaDataLock.Unlock()
	model.CacheQuotaData = make(map[string]*model.QuotaData)
}

func newTextQuotaGuardContext(requestId string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Set("username", "guard-user")
	c.Set("token_name", "guard-token")
	c.Set(common.RequestIdKey, requestId)
	c.Set(common.UpstreamRequestIdKey, "upstream-"+requestId)
	return c
}

func newTextQuotaGuardRelayInfo(userId int, tokenId int, channelId int, billing *textQuotaGuardBillingStub, endReason relaycommon.StreamEndReason) *relaycommon.RelayInfo {
	now := time.Now()
	streamStatus := relaycommon.NewStreamStatus()
	streamStatus.SetEndReason(endReason, nil)
	return &relaycommon.RelayInfo{
		UserId:            userId,
		TokenId:           tokenId,
		UsingGroup:        "default",
		OriginModelName:   "guard-model",
		StartTime:         now,
		FirstResponseTime: now,
		IsStream:          true,
		StreamStatus:      streamStatus,
		Billing:           billing,
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         channelId,
			ChannelType:       constant.ChannelTypeAnthropic,
			UpstreamModelName: "guard-model",
		},
	}
}

func seedTextQuotaGuardData(t *testing.T, userId int, tokenId int, channelId int) {
	t.Helper()
	seedUser(t, userId, 1000000)
	seedToken(t, tokenId, userId, "guard-token-key", 1000000)
	seedChannel(t, channelId)
}

func textQuotaGuardUsage() *dto.Usage {
	return &dto.Usage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
	}
}

func assertTextQuotaGuardUserAndChannelUsed(t *testing.T, userId int, channelId int, expectedQuota int, expectedRequests int) {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.First(&user, userId).Error)
	assert.Equal(t, expectedQuota, user.UsedQuota)
	assert.Equal(t, expectedRequests, user.RequestCount)

	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, channelId).Error)
	assert.Equal(t, int64(expectedQuota), channel.UsedQuota)
}

func assertTextQuotaGuardTokenUnchanged(t *testing.T, tokenId int) {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.First(&token, tokenId).Error)
	assert.Equal(t, 1000000, token.RemainQuota)
	assert.Equal(t, 0, token.UsedQuota)
}

func logsByRequestId(t *testing.T, requestId string) []model.Log {
	t.Helper()
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("request_id = ?", requestId).Order("id asc").Find(&logs).Error)
	return logs
}

func TestPostTextConsumeQuotaSkipsClientGoneLocalUsageBilling(t *testing.T) {
	setupTextQuotaGuardTest(t, true, true)
	userId := 2101
	tokenId := 2102
	channelId := 2103
	seedTextQuotaGuardData(t, userId, tokenId, channelId)

	c := newTextQuotaGuardContext("client-gone-local-skip")
	common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
	billing := &textQuotaGuardBillingStub{}
	info := newTextQuotaGuardRelayInfo(userId, tokenId, channelId, billing, relaycommon.StreamEndReasonClientGone)

	PostTextConsumeQuota(c, info, textQuotaGuardUsage(), nil)

	require.Equal(t, []int{0}, billing.settledQuotas)
	assertTextQuotaGuardUserAndChannelUsed(t, userId, channelId, 0, 0)
	assertTextQuotaGuardTokenUnchanged(t, tokenId)

	logs := logsByRequestId(t, "client-gone-local-skip")
	require.Len(t, logs, 1)
	assert.Equal(t, model.LogTypeError, logs[0].Type)
	assert.Equal(t, 0, logs[0].Quota)
	assert.Equal(t, "guard-model", logs[0].ModelName)

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, clientGoneLocalUsageBillingSkippedReason, adminInfo["billing_skipped_reason"])
	assert.Equal(t, usageBillingPathLocal, adminInfo["usage_billing_path"])
	assert.Equal(t, true, adminInfo["local_count_tokens"])
	assert.Equal(t, float64(15), adminInfo["estimated_quota"])
	streamStatus, ok := other["stream_status"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "error", streamStatus["status"])
	assert.Equal(t, string(relaycommon.StreamEndReasonClientGone), streamStatus["end_reason"])

	model.CacheQuotaDataLock.Lock()
	assert.Len(t, model.CacheQuotaData, 0)
	model.CacheQuotaDataLock.Unlock()
}

func TestPostTextConsumeQuotaClientGoneUpstreamUsageStillConsumes(t *testing.T) {
	setupTextQuotaGuardTest(t, false, false)
	userId := 2111
	tokenId := 2112
	channelId := 2113
	seedTextQuotaGuardData(t, userId, tokenId, channelId)

	c := newTextQuotaGuardContext("client-gone-upstream-consume")
	billing := &textQuotaGuardBillingStub{}
	info := newTextQuotaGuardRelayInfo(userId, tokenId, channelId, billing, relaycommon.StreamEndReasonClientGone)

	PostTextConsumeQuota(c, info, textQuotaGuardUsage(), nil)

	require.Equal(t, []int{15}, billing.settledQuotas)
	assertTextQuotaGuardUserAndChannelUsed(t, userId, channelId, 15, 1)

	logs := logsByRequestId(t, "client-gone-upstream-consume")
	require.Len(t, logs, 1)
	assert.Equal(t, model.LogTypeConsume, logs[0].Type)
	assert.Equal(t, 15, logs[0].Quota)
}

func TestPostTextConsumeQuotaLocalUsageNonClientGoneStillConsumes(t *testing.T) {
	setupTextQuotaGuardTest(t, false, false)
	userId := 2121
	tokenId := 2122
	channelId := 2123
	seedTextQuotaGuardData(t, userId, tokenId, channelId)

	c := newTextQuotaGuardContext("local-non-client-gone-consume")
	common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
	billing := &textQuotaGuardBillingStub{}
	info := newTextQuotaGuardRelayInfo(userId, tokenId, channelId, billing, relaycommon.StreamEndReasonDone)

	PostTextConsumeQuota(c, info, textQuotaGuardUsage(), nil)

	require.Equal(t, []int{15}, billing.settledQuotas)
	assertTextQuotaGuardUserAndChannelUsed(t, userId, channelId, 15, 1)

	logs := logsByRequestId(t, "local-non-client-gone-consume")
	require.Len(t, logs, 1)
	assert.Equal(t, model.LogTypeConsume, logs[0].Type)
	assert.Equal(t, 15, logs[0].Quota)
	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, usageBillingPathLocal, adminInfo["usage_billing_path"])
}

func TestPostTextConsumeQuotaSkipsClientGoneLocalUsageWithoutForcedErrorLog(t *testing.T) {
	setupTextQuotaGuardTest(t, false, false)
	userId := 2131
	tokenId := 2132
	channelId := 2133
	seedTextQuotaGuardData(t, userId, tokenId, channelId)

	c := newTextQuotaGuardContext("client-gone-local-no-error-log")
	common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
	billing := &textQuotaGuardBillingStub{}
	info := newTextQuotaGuardRelayInfo(userId, tokenId, channelId, billing, relaycommon.StreamEndReasonClientGone)

	PostTextConsumeQuota(c, info, textQuotaGuardUsage(), nil)

	require.Equal(t, []int{0}, billing.settledQuotas)
	assertTextQuotaGuardUserAndChannelUsed(t, userId, channelId, 0, 0)
	assertTextQuotaGuardTokenUnchanged(t, tokenId)
	assert.Empty(t, logsByRequestId(t, "client-gone-local-no-error-log"))
}

func TestPostTextConsumeQuotaRefundsClientGoneLocalUsageWhenZeroSettlementFails(t *testing.T) {
	setupTextQuotaGuardTest(t, true, false)
	userId := 2141
	tokenId := 2142
	channelId := 2143
	seedTextQuotaGuardData(t, userId, tokenId, channelId)

	c := newTextQuotaGuardContext("client-gone-local-settlement-refund")
	common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
	billing := &textQuotaGuardBillingStub{
		settleErr:   errors.New("zero settlement failed"),
		needsRefund: true,
	}
	info := newTextQuotaGuardRelayInfo(userId, tokenId, channelId, billing, relaycommon.StreamEndReasonClientGone)

	PostTextConsumeQuota(c, info, textQuotaGuardUsage(), nil)

	require.Equal(t, []int{0}, billing.settledQuotas)
	assert.Equal(t, 1, billing.refundCallCount)
	assertTextQuotaGuardUserAndChannelUsed(t, userId, channelId, 0, 0)
	assertTextQuotaGuardTokenUnchanged(t, tokenId)

	logs := logsByRequestId(t, "client-gone-local-settlement-refund")
	require.Len(t, logs, 1)
	assert.Equal(t, model.LogTypeError, logs[0].Type)
	assert.Contains(t, logs[0].Content, "零额结算失败")
	assert.Contains(t, logs[0].Content, "已触发退款兜底")

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, adminInfo["billing_settlement_failed"])
	assert.Equal(t, true, adminInfo["billing_refund_fallback_triggered"])
}

func TestPostTextConsumeQuotaDoesNotRefundClientGoneLocalUsageAfterFundingSettled(t *testing.T) {
	setupTextQuotaGuardTest(t, true, false)
	userId := 2151
	tokenId := 2152
	channelId := 2153
	seedTextQuotaGuardData(t, userId, tokenId, channelId)

	c := newTextQuotaGuardContext("client-gone-local-settlement-no-refund")
	common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
	billing := &textQuotaGuardBillingStub{
		settleErr:   errors.New("token adjustment failed after funding settled"),
		needsRefund: false,
	}
	info := newTextQuotaGuardRelayInfo(userId, tokenId, channelId, billing, relaycommon.StreamEndReasonClientGone)

	PostTextConsumeQuota(c, info, textQuotaGuardUsage(), nil)

	require.Equal(t, []int{0}, billing.settledQuotas)
	assert.Equal(t, 0, billing.refundCallCount)

	logs := logsByRequestId(t, "client-gone-local-settlement-no-refund")
	require.Len(t, logs, 1)
	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, adminInfo["billing_settlement_failed"])
	assert.Equal(t, false, adminInfo["billing_refund_fallback_triggered"])
}
