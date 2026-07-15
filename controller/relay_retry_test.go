package controller

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type billingSettlerStub struct {
	reserved     int
	reserveCalls int
	settled      int
	refunded     bool
}

// Settle 记录测试结算，不执行真实扣费。
// @param actualQuota 实际结算额度。
// @return 始终返回 nil。
func (s *billingSettlerStub) Settle(actualQuota int) error {
	s.settled = actualQuota
	return nil
}

// Refund 记录测试退款，不执行真实退款。
// @param c 当前 Gin 请求上下文。
func (s *billingSettlerStub) Refund(c *gin.Context) {
	s.refunded = true
}

// NeedsRefund 返回测试会话是否存在预扣额度。
// @return 已记录预扣额度时返回 true。
func (s *billingSettlerStub) NeedsRefund() bool {
	return s.reserved > 0
}

// GetPreConsumedQuota 返回测试会话已预扣的额度。
// @return 当前记录的预扣额度。
func (s *billingSettlerStub) GetPreConsumedQuota() int {
	return s.reserved
}

// Reserve 记录目标预扣额度。
// @param targetQuota 目标预扣额度。
// @return 始终返回 nil。
func (s *billingSettlerStub) Reserve(targetQuota int) error {
	s.reserveCalls++
	s.reserved = targetQuota
	return nil
}

func TestShouldRetryAfterKeepAliveCommittedResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	_, err := c.Writer.Write([]byte("\n"))
	require.NoError(t, err)
	relayErr := types.NewError(errors.New("channel unavailable"), types.ErrorCodeChannelNoAvailableKey)

	assert.True(t, shouldRetry(c, relayErr, 1))
}

func TestShouldRetryAlphaSearchUpstreamError(t *testing.T) {
	service.InitHttpClient()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"temporarily unavailable"}}`)
	}))
	t.Cleanup(upstream.Close)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(`{"model":"gpt-5"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() {
		common.CleanupBodyStorage(c)
	})
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5",
		RelayMode:       relayconstant.RelayModeAlphaSearch,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiType:           appconstant.APITypeOpenAI,
			ChannelType:       appconstant.ChannelTypeOpenAI,
			ChannelBaseUrl:    upstream.URL,
			ApiKey:            "upstream-secret",
			UpstreamModelName: "gpt-5",
		},
	}

	apiErr := relay.AlphaSearchHelper(c, info)

	require.NotNil(t, apiErr)
	assert.True(t, shouldRetry(c, apiErr, 1))
}

func TestPrepareAlphaSearchBillingReservesOneWebSearchCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	billing := &billingSettlerStub{}
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5",
		UsingGroup:      "default",
		UserGroup:       "default",
		Billing:         billing,
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}

	apiErr := prepareAlphaSearchBilling(c, info)
	require.Nil(t, apiErr)
	expected := service.ComputeToolCallQuota(service.ToolCallUsage{
		ModelName:         "gpt-5",
		WebSearchCalls:    1,
		WebSearchToolName: service.ToolNameWebSearch,
	}, 1)
	assert.Equal(t, expected.TotalQuota, billing.reserved)
	assert.Equal(t, expected.TotalQuota, info.FinalPreConsumedQuota)
	assert.Equal(t, expected.TotalQuota, info.PriceData.Quota)
	assert.Equal(t, "gpt-5", info.BillingModelName())
	assert.Nil(t, info.QuotaClamp)
	require.NotNil(t, info.ToolCallBilling)
	assert.Equal(t, expected, *info.ToolCallBilling)
}

func TestPrepareAlphaSearchBillingRejectsSaturationBeforeReserve(t *testing.T) {
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"overflow":1e308}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	billing := &billingSettlerStub{reserved: 100}
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5",
		UsingGroup:      "overflow",
		UserGroup:       "default",
		Billing:         billing,
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}

	apiErr := prepareAlphaSearchBilling(c, info)

	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Equal(t, 0, billing.reserveCalls)
	require.NotNil(t, info.QuotaClamp)
	require.NotNil(t, info.ToolCallBilling)
	assert.Equal(t, info.QuotaClamp, info.ToolCallBilling.QuotaClamp)
}

func TestFinalizeMainRelayBillingOnlyRefundsFinalFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	t.Run("重试后成功不退款", func(t *testing.T) {
		billing := &billingSettlerStub{reserved: 100}
		info := &relaycommon.RelayInfo{Billing: billing}

		apiErr := finalizeMainRelayBilling(c, info, true, nil)

		assert.Nil(t, apiErr)
		assert.False(t, billing.refunded)
	})

	t.Run("最终失败退还预扣费", func(t *testing.T) {
		billing := &billingSettlerStub{reserved: 100}
		info := &relaycommon.RelayInfo{Billing: billing}
		expectedErr := types.NewError(errors.New("upstream failed"), types.ErrorCodeDoRequestFailed)

		apiErr := finalizeMainRelayBilling(c, info, true, expectedErr)

		assert.Same(t, expectedErr, apiErr)
		assert.True(t, billing.refunded)
	})
}
