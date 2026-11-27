package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type billingSettlerStub struct {
	reserved     int
	reserveCalls int
	settled      int
	settleCalls  int
	refunded     bool
}

// Settle 记录测试结算，不执行真实扣费。
// @param actualQuota 实际结算额度。
// @return 始终返回 nil。
func (s *billingSettlerStub) Settle(actualQuota int) error {
	s.settled = actualQuota
	s.settleCalls++
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

	assert.True(t, service.ShouldRetryRelayError(c, relayErr, 1))
}

func TestInitialRelayRequestErrorClassifiesUnexpectedEOF(t *testing.T) {
	apiErr := initialRelayRequestError(fmt.Errorf("client upload failed: %w", io.ErrUnexpectedEOF))

	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeReadRequestBodyFailed, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr))
}

func TestInitialRelayRequestErrorClassifiesOversizedBody(t *testing.T) {
	apiErr := initialRelayRequestError(common.ErrRequestBodyTooLarge)

	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeReadRequestBodyFailed, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusRequestEntityTooLarge, apiErr.StatusCode)
	assert.True(t, types.IsSkipRetryError(apiErr))
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
	assert.True(t, service.ShouldRetryRelayError(c, apiErr, 1))
}

func TestPrepareAlphaSearchBillingReservesOneWebSearchCall(t *testing.T) {
	db := setupChannelUserLimitsTestDB(t)
	require.NoError(t, db.Create(&model.Channel{Id: 1, Name: "搜索计费测试渠道"}).Error)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	common.SetContextKey(c, appconstant.ContextKeyChannelId, 1)
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
	assert.Equal(t, "gpt-5", info.GetBillingModelName())
	assert.Nil(t, info.QuotaClamp)
	require.NotNil(t, info.ToolCallBilling)
	assert.Equal(t, expected, *info.ToolCallBilling)
}

func TestClaudeLocalSummaryLargeHistorySettlesOnceAsZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelUserLimitsTestDB(t)
	service.InitHttpClient()
	previousCountTokens, previousTimeout := appconstant.CountToken, appconstant.StreamingTimeout
	previousLogConsume, previousExport := common.LogConsumeEnabled, common.DataExportEnabled
	appconstant.CountToken, appconstant.StreamingTimeout = true, 30
	common.LogConsumeEnabled, common.DataExportEnabled = true, false
	t.Cleanup(func() {
		appconstant.CountToken, appconstant.StreamingTimeout = previousCountTokens, previousTimeout
		common.LogConsumeEnabled, common.DataExportEnabled = previousLogConsume, previousExport
	})
	const modelName = "gemini-local-summary-fixture"
	previousModelRatios := ratio_setting.ModelRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gemini-local-summary-fixture":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousModelRatios))
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/messages", r.URL.Path)
		length, err := io.Copy(io.Discard, r.Body)
		assert.NoError(t, err)
		assert.Greater(t, length, int64(600000))
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Flower2API-Local-Response", "agy_agent_summary")
		_, _ = io.WriteString(w, `data: {"type":"message_start","message":{"id":"msg_fixture","model":"gemini-local-summary-fixture","usage":{"input_tokens":0,"output_tokens":0}}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Reading sample file"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":0,"output_tokens":0}}

data: {"type":"message_stop"}

`)
	}))
	t.Cleanup(upstream.Close)

	const channelID, userID = 9510, 9511
	channel := model.Channel{Id: channelID, Name: "本地计费测试渠道"}
	channel.SetSetting(dto.ChannelSettings{TrustFlower2APILocalResponse: true})
	require.NoError(t, db.Create(&channel).Error)
	var stored model.Channel
	require.NoError(t, db.First(&stored, channelID).Error)
	require.True(t, stored.GetSetting().TrustFlower2APILocalResponse)
	require.False(t, (&model.Channel{}).GetSetting().TrustFlower2APILocalResponse)
	require.NoError(t, db.Create(&model.User{Id: userID, Username: "local-summary-fixture"}).Error)
	requestBody, err := common.Marshal(map[string]any{
		"model": modelName, "stream": true, "max_tokens": 30,
		"messages": []map[string]any{
			{"role": "assistant", "content": []map[string]any{{"type": "tool_use", "id": "sample", "name": "lookup", "input": map[string]any{}}}},
			{"role": "user", "content": []map[string]any{{"type": "tool_result", "tool_use_id": "sample", "content": strings.Repeat("example 12345 data\n", 40000)}}},
		},
	})
	require.NoError(t, err)
	request := &dto.ClaudeRequest{}
	require.NoError(t, common.Unmarshal(requestBody, request))
	c, recorder := newChannelUserLimitTestContext(http.MethodPost, "/v1/messages", string(requestBody), nil)
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	c.Set(common.RequestIdKey, "agy-local-summary-full-history")
	common.SetContextKey(c, appconstant.ContextKeyChannelType, appconstant.ChannelTypeAnthropic)
	common.SetContextKey(c, appconstant.ContextKeyChannelId, channelID)
	common.SetContextKey(c, appconstant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(c, appconstant.ContextKeyChannelKey, "fixture-upstream-key")
	common.SetContextKey(c, appconstant.ContextKeyChannelSetting, stored.GetSetting())
	common.SetContextKey(c, appconstant.ContextKeyOriginalModel, modelName)
	billing := &billingSettlerStub{settled: -1}
	info := &relaycommon.RelayInfo{
		Request: request, OriginModelName: modelName, RelayFormat: types.RelayFormatClaude,
		UserId: userID, UsingGroup: "default", Billing: billing, StartTime: time.Now(), IsStream: true,
	}
	info.InitChannelMeta(c)
	require.Nil(t, prepareMainRelayBilling(c, info))
	assert.Greater(t, info.GetEstimatePromptTokens(), 200000)
	assert.Positive(t, billing.reserved)
	require.Nil(t, relay.ClaudeHelper(c, info))
	assert.Zero(t, billing.settled)
	assert.Equal(t, 1, billing.settleCalls)
	assert.Contains(t, recorder.Body.String(), "Reading sample file")
	assert.Contains(t, recorder.Body.String(), "event: message_stop")
	var logs []model.Log
	require.NoError(t, db.Where("request_id = ?", "agy-local-summary-full-history").Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Zero(t, logs[0].PromptTokens)
	assert.Zero(t, logs[0].Quota)
	assert.Contains(t, logs[0].Other, `"local_response":"agy_agent_summary"`)
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

func TestResponsesCompactV2RetryUsesBaseSelectionAndEachChannelsOwnModelMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	selectionModel := "gpt-5"
	info := &relaycommon.RelayInfo{
		RelayMode:            relayconstant.RelayModeResponses,
		ResponsesCompactMode: relayconstant.ResponsesCompactModeV2HTTP,
		OriginModelName:      selectionModel,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: selectionModel,
		},
	}

	c.Set("model_mapping", `{"gpt-5":"gpt-5.1"}`)
	firstRequest := &dto.OpenAIResponsesRequest{Model: "gpt-5"}
	require.NoError(t, relayhelper.ModelMappedHelper(c, info, firstRequest))
	require.Equal(t, "gpt-5.1", info.UpstreamModelName)
	require.Equal(t, ratio_setting.WithCompactModelSuffix("gpt-5.1"), info.OriginModelName)

	resetMainRelayAttemptFields(info, selectionModel)
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: selectionModel}
	c.Set("model_mapping", `{"gpt-5":"gpt-5.2"}`)
	secondRequest := &dto.OpenAIResponsesRequest{Model: "gpt-5"}
	require.NoError(t, relayhelper.ModelMappedHelper(c, info, secondRequest))

	assert.Equal(t, "gpt-5.2", info.UpstreamModelName)
	assert.Equal(t, "gpt-5.2", secondRequest.Model)
	assert.Equal(t, ratio_setting.WithCompactModelSuffix("gpt-5.2"), info.OriginModelName)
}

func (s *billingSettlerStub) ReserveBeforeSubmit(quota int) error { return s.Reserve(quota) }
