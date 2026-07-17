package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newResponsesCompactPassthroughWebSocketTestContext(t *testing.T, enabled bool) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyChannelId, 301)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://upstream.example")
	common.SetContextKey(c, constant.ContextKeyChannelKey, "upstream-secret")
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, `{"gpt-5.6-sol":"mapped-model"}`)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{ResponsesCompactPassthroughEnabled: enabled})
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{})
	return c
}

func TestPrepareResponsesCompactPassthroughWebSocketTurnRejectsDisabledSelectedChannel(t *testing.T) {
	c := newResponsesCompactPassthroughWebSocketTestContext(t, false)
	turn := &responsesWebSocketTurn{
		messageType:    websocket.TextMessage,
		rawPayload:     []byte(`{"type":"response.create","model":"gpt-5.6-sol","stream":true,"input":[{"type":"compaction_trigger"}]}`),
		baseModel:      "gpt-5.6-sol",
		selectionModel: "gpt-5.6-sol",
		compactMode:    relayconstant.ResponsesCompactModeV2WebSocket,
	}

	payload, apiErr := prepareResponsesCompactPassthroughWebSocketTurn(c, turn)

	require.NotNil(t, apiErr)
	assert.Nil(t, payload)
	assert.Equal(t, types.ErrorCode("responses_compact_passthrough_disabled"), apiErr.GetErrorCode())
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.False(t, types.IsChannelError(apiErr))
	assert.False(t, types.IsRecordErrorLog(apiErr))
	assert.False(t, shouldRetry(c, apiErr, 3))
	require.NotNil(t, turn.info)
	assert.Nil(t, turn.info.Billing)
}

func TestPrepareResponsesCompactPassthroughWebSocketTurnPreservesRawFrameAndBaseModel(t *testing.T) {
	c := newResponsesCompactPassthroughWebSocketTestContext(t, true)
	quotaSetting := operation_setting.GetQuotaSetting()
	oldFreeModelPreConsume := quotaSetting.EnableFreeModelPreConsume
	quotaSetting.EnableFreeModelPreConsume = false
	savedModelRatios := ratio_setting.ModelRatio2JSONString()
	modelRatios, err := common.Marshal(map[string]float64{"gpt-5.6-sol": 0})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(modelRatios)))
	t.Cleanup(func() {
		quotaSetting.EnableFreeModelPreConsume = oldFreeModelPreConsume
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModelRatios))
	})
	rawPayload := []byte(`{"type":"response.create","model":"gpt-5.6-sol","stream":true,"input":[{"type":"compaction_trigger"}],"enabled":false,"future":{"count":0}}`)
	turn := &responsesWebSocketTurn{
		messageType:    websocket.TextMessage,
		rawPayload:     rawPayload,
		baseModel:      "gpt-5.6-sol",
		selectionModel: "gpt-5.6-sol",
		compactMode:    relayconstant.ResponsesCompactModeV2WebSocket,
	}

	payload, apiErr := prepareResponsesCompactPassthroughWebSocketTurn(c, turn)

	require.Nil(t, apiErr)
	assert.Equal(t, rawPayload, payload)
	require.NotNil(t, turn.info)
	assert.Equal(t, "gpt-5.6-sol", turn.info.OriginModelName)
	assert.Equal(t, "gpt-5.6-sol", turn.info.UpstreamModelName)
	assert.Equal(t, "gpt-5.6-sol", turn.info.BillingModelName())
	assert.False(t, turn.info.IsModelMapped)
	assert.NotContains(t, string(payload), "mapped-model")
	assert.NotContains(t, string(payload), "openai-compact")
}

func TestHandleResponsesCompactPassthroughWebSocketUpstreamEventRefundsInvalidUsage(t *testing.T) {
	oldLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = oldLogConsumeEnabled
	})

	c := newResponsesCompactPassthroughWebSocketTestContext(t, true)
	billing := &responsesWebSocketBillingStub{}
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-5.6-sol")
	info := relaycommon.GenRelayInfoResponses(c, &dto.OpenAIResponsesRequest{Model: "gpt-5.6-sol"})
	info.Billing = billing
	info.ResponsesCompactMode = relayconstant.ResponsesCompactModeV2WebSocket
	turn := &responsesWebSocketTurn{
		compactMode: relayconstant.ResponsesCompactModeV2WebSocket,
		info:        info,
	}
	payload := []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":2}}}`)

	terminal := handleResponsesWebSocketUpstreamEvent(c, turn, nil, payload)

	assert.True(t, terminal)
	assert.Equal(t, 1, billing.refunds)
	assert.Empty(t, billing.settled)
	assert.True(t, info.HasSendResponse())
}
