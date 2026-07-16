package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type responsesWebSocketBillingStub struct {
	settled []int
	refunds int
}

func newResponsesWebSocketScriptServer(events [][]byte, closeCode int) (*httptest.Server, <-chan error) {
	errors := make(chan error, 4)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			errors <- err
			return
		}
		defer conn.Close()
		messageType, _, err := conn.ReadMessage()
		if err != nil {
			errors <- err
			return
		}
		for _, event := range events {
			if err := conn.WriteMessage(messageType, event); err != nil {
				errors <- err
				return
			}
		}
		if closeCode != 0 {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(closeCode, "script complete"), time.Now().Add(time.Second))
		}
	}))
	return server, errors
}

func newResponsesWebSocketProxyServer(initialUpstreamURL string, initialChannel *model.Channel, billing *responsesWebSocketBillingStub, proxyResults chan<- error) *httptest.Server {
	engine := gin.New()
	engine.GET("/v1/responses", func(c *gin.Context) {
		clientConn, err := responsesWebSocketUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			proxyResults <- err
			return
		}
		defer clientConn.Close()
		messageType, payload, err := clientConn.ReadMessage()
		if err != nil {
			proxyResults <- err
			return
		}
		turn, apiErr := parseResponsesWebSocketTurn(c, messageType, payload)
		if apiErr != nil {
			proxyResults <- apiErr
			return
		}
		turn.info = &relaycommon.RelayInfo{
			Billing:              billing,
			OriginModelName:      turn.selectionModel,
			ResponsesCompactMode: turn.compactMode,
			IsStream:             true,
			StartTime:            time.Now(),
			ChannelMeta: &relaycommon.ChannelMeta{
				ChannelId:         initialChannel.Id,
				ChannelType:       initialChannel.Type,
				UpstreamModelName: turn.baseModel,
			},
			ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
				BuiltInTools: map[string]*relaycommon.BuildInToolInfo{},
			},
		}
		common.SetContextKey(c, constant.ContextKeyChannelId, initialChannel.Id)
		common.SetContextKey(c, constant.ContextKeyChannelName, initialChannel.Name)
		common.SetContextKey(c, constant.ContextKeyChannelType, initialChannel.Type)
		common.SetContextKey(c, constant.ContextKeyChannelKey, "test-key")
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())

		upstreamConn, _, err := websocket.DefaultDialer.Dial(initialUpstreamURL, nil)
		if err != nil {
			proxyResults <- err
			return
		}
		defer upstreamConn.Close()
		if err := upstreamConn.WriteMessage(messageType, payload); err != nil {
			proxyResults <- err
			return
		}
		proxyErr := proxyResponsesWebSocket(c, clientConn, upstreamConn, initialChannel, turn)
		if proxyErr != nil {
			proxyResults <- proxyErr
			return
		}
		proxyResults <- nil
	})
	return httptest.NewServer(engine)
}

func responsesWebSocketServerURL(server *httptest.Server) string {
	return "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/responses"
}

func assertNoResponsesWebSocketServerError(t *testing.T, errors <-chan error) {
	t.Helper()
	select {
	case err := <-errors:
		require.NoError(t, err)
	default:
	}
}

// Settle 记录 Responses WebSocket turn 的结算额度。
// @param actualQuota 实际结算额度。
// @return 始终返回 nil。
func (s *responsesWebSocketBillingStub) Settle(actualQuota int) error {
	s.settled = append(s.settled, actualQuota)
	return nil
}

// Refund 记录 Responses WebSocket turn 的退款调用。
// @param c 当前 Gin 请求上下文。
func (s *responsesWebSocketBillingStub) Refund(c *gin.Context) {
	s.refunds++
}

// NeedsRefund 判断测试计费会话是否仍需退款。
// @return 未结算且未退款时返回 true。
func (s *responsesWebSocketBillingStub) NeedsRefund() bool {
	return len(s.settled) == 0 && s.refunds == 0
}

// GetPreConsumedQuota 返回测试计费会话的预扣额度。
// @return 固定返回 0。
func (s *responsesWebSocketBillingStub) GetPreConsumedQuota() int {
	return 0
}

// Reserve 接受测试 turn 的目标预扣额度。
// @param targetQuota 目标预扣额度。
// @return 始终返回 nil。
func (s *responsesWebSocketBillingStub) Reserve(targetQuota int) error {
	return nil
}

func TestParseResponsesWebSocketTurnDetectsCompactV2(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Request.Header.Set("X-Codex-Beta-Features", "other, remote_compaction_v2")
	payload := []byte(`{"type":"response.create","model":"gpt-5","stream":true,"input":[{"type":"compaction_trigger"}]}`)

	turn, apiErr := parseResponsesWebSocketTurn(c, websocket.TextMessage, payload)

	require.Nil(t, apiErr)
	require.NotNil(t, turn)
	assert.Equal(t, "gpt-5", turn.baseModel)
	assert.Equal(t, "gpt-5", turn.selectionModel)
	assert.Equal(t, relayconstant.ResponsesCompactModeV2WebSocket, turn.compactMode)
}

func TestResponsesWebSocketBusinessErrorUsesSemanticRetryStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	tests := []struct {
		name           string
		payload        string
		expectedCode   types.ErrorCode
		expectedStatus int
		retryable      bool
	}{
		{
			name:           "server overloaded",
			payload:        `{"type":"response.failed","response":{"error":{"type":"server_error","code":"server_is_overloaded","message":"retry later"}}}`,
			expectedCode:   "server_is_overloaded",
			expectedStatus: http.StatusServiceUnavailable,
			retryable:      true,
		},
		{
			name:           "context length exceeded",
			payload:        `{"type":"response.failed","response":{"error":{"type":"invalid_request_error","code":"context_length_exceeded","message":"context too large"}}}`,
			expectedCode:   "context_length_exceeded",
			expectedStatus: http.StatusBadRequest,
			retryable:      false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			apiErr, eventType := responsesWebSocketUpstreamBusinessError([]byte(test.payload))

			require.NotNil(t, apiErr)
			assert.Equal(t, "response.failed", eventType)
			assert.Equal(t, test.expectedCode, apiErr.GetErrorCode())
			assert.Equal(t, test.expectedStatus, apiErr.StatusCode)
			assert.Equal(t, test.retryable, shouldRetry(c, apiErr, 1))
		})
	}
}

func TestHandleResponsesWebSocketUpstreamEventTracksCompactOutputAndFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	turn := &responsesWebSocketTurn{
		compactMode: relayconstant.ResponsesCompactModeV2WebSocket,
		info: &relaycommon.RelayInfo{
			ResponsesCompactMode: relayconstant.ResponsesCompactModeV2WebSocket,
			ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
				BuiltInTools: map[string]*relaycommon.BuildInToolInfo{},
			},
		},
	}

	terminal := handleResponsesWebSocketUpstreamEvent(c, turn, nil, []byte(`{"type":"response.output_item.done","item":{"type":"compaction"}}`))
	assert.False(t, terminal)
	assert.Equal(t, 1, turn.info.ResponsesUsageInfo.OutputItemDoneCount)
	assert.Equal(t, 1, turn.info.ResponsesUsageInfo.CompactionOutputItemCount)

	terminal = handleResponsesWebSocketUpstreamEvent(c, turn, nil, []byte(`{"type":"response.failed","response":{"status":"failed"}}`))
	assert.True(t, terminal)
}

func TestHandleResponsesWebSocketUpstreamEventSettlesSuccessTerminal(t *testing.T) {
	oldLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = oldLogConsumeEnabled
	})

	for _, eventType := range []string{"response.completed", "response.done"} {
		t.Run(eventType, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
			billing := &responsesWebSocketBillingStub{}
			turn := &responsesWebSocketTurn{
				info: &relaycommon.RelayInfo{
					Billing:         billing,
					ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 1},
					OriginModelName: "test-model",
					StartTime:       time.Now(),
					ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
						BuiltInTools: map[string]*relaycommon.BuildInToolInfo{},
					},
				},
			}
			payload := []byte(`{"type":"` + eventType + `","response":{"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`)

			terminal := handleResponsesWebSocketUpstreamEvent(c, turn, nil, payload)

			require.True(t, terminal)
			require.Equal(t, []int{0}, billing.settled)
			require.Zero(t, billing.refunds)
		})
	}
}

func TestHandleResponsesWebSocketUpstreamEventRefundsFailureTerminals(t *testing.T) {
	for _, eventType := range []string{
		"response.failed",
		"response.incomplete",
		"response.cancelled",
		"response.canceled",
		"response.error",
		"error",
	} {
		t.Run(eventType, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
			billing := &responsesWebSocketBillingStub{}
			turn := &responsesWebSocketTurn{
				info: &relaycommon.RelayInfo{Billing: billing},
			}

			terminal := handleResponsesWebSocketUpstreamEvent(c, turn, nil, []byte(`{"type":"`+eventType+`"}`))

			require.True(t, terminal)
			require.Equal(t, 1, billing.refunds)
			require.Empty(t, billing.settled)
		})
	}
}

func TestProxyResponsesWebSocketRetriesFirstBusinessErrorBeforeDownstreamWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldRetryTimes := common.RetryTimes
	oldLogConsumeEnabled := common.LogConsumeEnabled
	oldConnector := responsesWebSocketTurnConnector
	common.RetryTimes = 1
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		common.RetryTimes = oldRetryTimes
		common.LogConsumeEnabled = oldLogConsumeEnabled
		responsesWebSocketTurnConnector = oldConnector
	})

	primaryEvents := [][]byte{
		[]byte(`{"type":"response.failed","response":{"error":{"type":"server_error","code":"server_is_overloaded","message":"retry later"}}}`),
	}
	primaryServer, primaryErrors := newResponsesWebSocketScriptServer(primaryEvents, websocket.CloseNormalClosure)
	defer primaryServer.Close()
	retryEvents := [][]byte{
		[]byte(`{"type":"response.completed","response":{"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`),
	}
	retryServer, retryErrors := newResponsesWebSocketScriptServer(retryEvents, websocket.CloseNormalClosure)
	defer retryServer.Close()

	primaryChannel := &model.Channel{Id: 201, Type: constant.ChannelTypeOpenAI, Name: "primary", AutoBan: common.GetPointer(0)}
	retryChannel := &model.Channel{Id: 202, Type: constant.ChannelTypeOpenAI, Name: "retry", AutoBan: common.GetPointer(0)}
	connectorCalls := 0
	responsesWebSocketTurnConnector = func(c *gin.Context, turn *responsesWebSocketTurn, _ *model.Channel, startRetry int) (*websocket.Conn, *model.Channel, *types.NewAPIError) {
		connectorCalls++
		turn.retryIndex = startRetry
		turn.info.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: retryChannel.Id, ChannelType: retryChannel.Type, UpstreamModelName: turn.baseModel}
		common.SetContextKey(c, constant.ContextKeyChannelId, retryChannel.Id)
		common.SetContextKey(c, constant.ContextKeyChannelName, retryChannel.Name)
		common.SetContextKey(c, constant.ContextKeyChannelType, retryChannel.Type)
		conn, _, err := websocket.DefaultDialer.Dial(responsesWebSocketServerURL(retryServer), nil)
		if err != nil {
			return nil, nil, types.NewErrorWithStatusCode(err, types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
		}
		if err := conn.WriteMessage(turn.messageType, turn.rawPayload); err != nil {
			_ = conn.Close()
			return nil, nil, types.NewErrorWithStatusCode(err, types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
		}
		return conn, retryChannel, nil
	}

	billing := &responsesWebSocketBillingStub{}
	proxyResults := make(chan error, 1)
	proxyServer := newResponsesWebSocketProxyServer(responsesWebSocketServerURL(primaryServer), primaryChannel, billing, proxyResults)
	defer proxyServer.Close()
	clientConn, _, err := websocket.DefaultDialer.Dial(responsesWebSocketServerURL(proxyServer), nil)
	require.NoError(t, err)
	defer clientConn.Close()
	_ = clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	require.NoError(t, clientConn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-model","stream":true,"input":[]}`)))

	_, payload, err := clientConn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "response.completed", gjson.GetBytes(payload, "type").String())
	_, _, closeErr := clientConn.ReadMessage()
	require.True(t, websocket.IsCloseError(closeErr, websocket.CloseNormalClosure), "unexpected close error: %v", closeErr)
	require.NoError(t, <-proxyResults)
	assert.Equal(t, 1, connectorCalls)
	assert.Equal(t, []int{0}, billing.settled)
	assert.Zero(t, billing.refunds)
	assertNoResponsesWebSocketServerError(t, primaryErrors)
	assertNoResponsesWebSocketServerError(t, retryErrors)
}

func TestProxyResponsesWebSocketDoesNotRetryAfterBusinessOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldRetryTimes := common.RetryTimes
	oldLogConsumeEnabled := common.LogConsumeEnabled
	oldConnector := responsesWebSocketTurnConnector
	common.RetryTimes = 1
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		common.RetryTimes = oldRetryTimes
		common.LogConsumeEnabled = oldLogConsumeEnabled
		responsesWebSocketTurnConnector = oldConnector
	})

	events := [][]byte{
		[]byte(`{"type":"response.output_text.delta","delta":"partial"}`),
		[]byte(`{"type":"response.failed","response":{"error":{"type":"server_error","code":"server_is_overloaded","message":"retry later"}}}`),
	}
	upstreamServer, upstreamErrors := newResponsesWebSocketScriptServer(events, websocket.CloseNormalClosure)
	defer upstreamServer.Close()
	channel := &model.Channel{Id: 203, Type: constant.ChannelTypeOpenAI, Name: "primary", AutoBan: common.GetPointer(0)}
	connectorCalls := 0
	responsesWebSocketTurnConnector = func(_ *gin.Context, _ *responsesWebSocketTurn, _ *model.Channel, _ int) (*websocket.Conn, *model.Channel, *types.NewAPIError) {
		connectorCalls++
		return nil, nil, types.NewErrorWithStatusCode(fmt.Errorf("unexpected retry"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	}

	billing := &responsesWebSocketBillingStub{}
	proxyResults := make(chan error, 1)
	proxyServer := newResponsesWebSocketProxyServer(responsesWebSocketServerURL(upstreamServer), channel, billing, proxyResults)
	defer proxyServer.Close()
	clientConn, _, err := websocket.DefaultDialer.Dial(responsesWebSocketServerURL(proxyServer), nil)
	require.NoError(t, err)
	defer clientConn.Close()
	_ = clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	require.NoError(t, clientConn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-model","stream":true,"input":[]}`)))

	_, firstPayload, err := clientConn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "response.output_text.delta", gjson.GetBytes(firstPayload, "type").String())
	_, secondPayload, err := clientConn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "response.failed", gjson.GetBytes(secondPayload, "type").String())
	_, _, closeErr := clientConn.ReadMessage()
	require.True(t, websocket.IsCloseError(closeErr, websocket.CloseNormalClosure), "unexpected close error: %v", closeErr)
	require.NoError(t, <-proxyResults)
	assert.Zero(t, connectorCalls)
	assert.Equal(t, 1, billing.refunds)
	assertNoResponsesWebSocketServerError(t, upstreamErrors)
}

func TestProxyResponsesWebSocketForwardsCancelPongAndCloseCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = oldLogConsumeEnabled })

	upstreamFrames := make(chan []byte, 1)
	upstreamErrors := make(chan error, 4)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			upstreamErrors <- err
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			upstreamErrors <- err
			return
		}
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			upstreamErrors <- err
			return
		}
		upstreamFrames <- append([]byte(nil), payload...)
		if err := conn.WriteMessage(messageType, []byte(`{"type":"response.cancelled","response":{"status":"cancelled"}}`)); err != nil {
			upstreamErrors <- err
			return
		}
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4001, "cancelled"), time.Now().Add(time.Second))
	}))
	defer upstreamServer.Close()

	channel := &model.Channel{Id: 204, Type: constant.ChannelTypeOpenAI, Name: "cancel", AutoBan: common.GetPointer(0)}
	billing := &responsesWebSocketBillingStub{}
	proxyResults := make(chan error, 1)
	proxyServer := newResponsesWebSocketProxyServer("ws"+strings.TrimPrefix(upstreamServer.URL, "http"), channel, billing, proxyResults)
	defer proxyServer.Close()
	clientConn, _, err := websocket.DefaultDialer.Dial(responsesWebSocketServerURL(proxyServer), nil)
	require.NoError(t, err)
	defer clientConn.Close()
	_ = clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	pong := make(chan string, 1)
	clientConn.SetPongHandler(func(appData string) error {
		pong <- appData
		return nil
	})
	require.NoError(t, clientConn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-model","stream":true,"input":[]}`)))
	require.NoError(t, clientConn.WriteControl(websocket.PingMessage, []byte("probe"), time.Now().Add(time.Second)))
	require.NoError(t, clientConn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.cancel"}`)))

	_, payload, err := clientConn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "response.cancelled", gjson.GetBytes(payload, "type").String())
	select {
	case pongPayload := <-pong:
		assert.Equal(t, "probe", pongPayload)
	default:
		t.Fatal("未收到 Responses WebSocket Pong")
	}
	_, _, closeErr := clientConn.ReadMessage()
	require.True(t, websocket.IsCloseError(closeErr, 4001), "unexpected close error: %v", closeErr)
	require.NoError(t, <-proxyResults)
	assert.Equal(t, "response.cancel", gjson.GetBytes(<-upstreamFrames, "type").String())
	assert.Equal(t, 1, billing.refunds)
	assertNoResponsesWebSocketServerError(t, upstreamErrors)
}

func TestProxyResponsesWebSocketSupportsOrdinaryCompactAndOrdinaryTurns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	quotaSetting := operation_setting.GetQuotaSetting()
	oldFreeModelPreConsume := quotaSetting.EnableFreeModelPreConsume
	quotaSetting.EnableFreeModelPreConsume = false
	savedModelRatios := ratio_setting.ModelRatio2JSONString()
	modelRatios, err := common.Marshal(map[string]float64{
		"ws-model-a": 0,
		ratio_setting.WithCompactModelSuffix("ws-model-b"): 0,
		"ws-model-c": 0,
	})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(modelRatios)))
	t.Cleanup(func() {
		common.LogConsumeEnabled = oldLogConsumeEnabled
		quotaSetting.EnableFreeModelPreConsume = oldFreeModelPreConsume
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModelRatios))
	})

	upstreamFrames := make(chan []byte, 3)
	upstreamErrors := make(chan error, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, upgradeErr := upgrader.Upgrade(w, r, nil)
		if upgradeErr != nil {
			upstreamErrors <- upgradeErr
			return
		}
		defer conn.Close()

		for turnIndex := 0; turnIndex < 3; turnIndex++ {
			messageType, payload, readErr := conn.ReadMessage()
			if readErr != nil {
				upstreamErrors <- readErr
				return
			}
			upstreamFrames <- append([]byte(nil), payload...)
			if turnIndex == 1 {
				itemDone := []byte(`{"type":"response.output_item.done","item":{"type":"compaction","encrypted_content":"opaque"}}`)
				if writeErr := conn.WriteMessage(messageType, itemDone); writeErr != nil {
					upstreamErrors <- writeErr
					return
				}
			}
			completed := []byte(fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_%d","usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`, turnIndex+1))
			if writeErr := conn.WriteMessage(messageType, completed); writeErr != nil {
				upstreamErrors <- writeErr
				return
			}
		}
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"), time.Now().Add(time.Second))
	}))
	defer upstreamServer.Close()

	baseURL := upstreamServer.URL
	channel := &model.Channel{
		Id:      101,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "upstream-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "responses-ws-test",
		BaseURL: &baseURL,
		Models:  "ws-model-a,ws-model-b,ws-model-c",
		Group:   "default",
	}
	proxyResults := make(chan error, 1)
	logOtherResults := make(chan map[string]interface{}, 1)
	engine := gin.New()
	engine.GET("/v1/responses", func(c *gin.Context) {
		clientConn, upgradeErr := responsesWebSocketUpgrader.Upgrade(c.Writer, c.Request, nil)
		if upgradeErr != nil {
			proxyResults <- upgradeErr
			return
		}
		defer clientConn.Close()

		messageType, firstPayload, readErr := clientConn.ReadMessage()
		if readErr != nil {
			proxyResults <- readErr
			return
		}
		turn, apiErr := parseResponsesWebSocketTurn(c, messageType, firstPayload)
		if apiErr != nil {
			proxyResults <- apiErr
			return
		}
		common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, fmt.Sprint(channel.Id))
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		if apiErr := middleware.SetupContextForSelectedChannel(c, channel, turn.selectionModel); apiErr != nil {
			proxyResults <- apiErr
			return
		}
		common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
		upstreamConn, selectedChannel, apiErr := connectResponsesWebSocketTurn(c, turn, channel, 0)
		if apiErr != nil {
			proxyResults <- apiErr
			return
		}
		defer upstreamConn.Close()
		proxyErr := proxyResponsesWebSocket(c, clientConn, upstreamConn, selectedChannel, turn)
		logOther, _ := common.GetContextKeyType[map[string]interface{}](c, constant.ContextKeyLogOther)
		logOtherResults <- logOther
		if proxyErr != nil {
			proxyResults <- proxyErr
			return
		}
		proxyResults <- nil
	})
	proxyServer := httptest.NewServer(engine)
	defer proxyServer.Close()

	proxyURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http") + "/v1/responses"
	header := http.Header{"X-Codex-Beta-Features": {"remote_compaction_v2"}}
	clientConn, _, err := websocket.DefaultDialer.Dial(proxyURL, header)
	require.NoError(t, err)
	defer clientConn.Close()
	_ = clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))

	firstTurn := []byte(`{"type":"response.create","model":"ws-model-a","stream":true,"input":[{"type":"message","role":"user"}],"future_field":{"keep":true}}`)
	require.NoError(t, clientConn.WriteMessage(websocket.TextMessage, firstTurn))
	_, firstCompleted, err := clientConn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(firstCompleted, "type").String())

	secondTurn := []byte(`{"type":"response.create","model":"ws-model-b","stream":true,"input":[{"type":"compaction_trigger"}],"client_metadata":{"turn":2}}`)
	require.NoError(t, clientConn.WriteMessage(websocket.TextMessage, secondTurn))
	_, itemDone, err := clientConn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "compaction", gjson.GetBytes(itemDone, "item.type").String())
	_, secondCompleted, err := clientConn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(secondCompleted, "type").String())

	thirdTurn := []byte(`{"type":"response.create","model":"ws-model-c","stream":true,"input":[{"type":"message","role":"user"}]}`)
	require.NoError(t, clientConn.WriteMessage(websocket.TextMessage, thirdTurn))
	_, thirdCompleted, err := clientConn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(thirdCompleted, "type").String())

	_, _, closeErr := clientConn.ReadMessage()
	require.True(t, websocket.IsCloseError(closeErr, websocket.CloseNormalClosure), "unexpected close error: %v", closeErr)

	firstUpstream := <-upstreamFrames
	secondUpstream := <-upstreamFrames
	thirdUpstream := <-upstreamFrames
	require.Equal(t, "ws-model-a", gjson.GetBytes(firstUpstream, "model").String())
	require.True(t, gjson.GetBytes(firstUpstream, "future_field.keep").Bool())
	require.Equal(t, "ws-model-b", gjson.GetBytes(secondUpstream, "model").String())
	require.Equal(t, "compaction_trigger", gjson.GetBytes(secondUpstream, "input.0.type").String())
	require.Equal(t, int64(2), gjson.GetBytes(secondUpstream, "client_metadata.turn").Int())
	require.Equal(t, "ws-model-c", gjson.GetBytes(thirdUpstream, "model").String())

	logOther := <-logOtherResults
	adminInfo, _ := logOther["admin_info"].(map[string]interface{})
	assert.NotContains(t, adminInfo, "responses_compact")

	select {
	case upstreamErr := <-upstreamErrors:
		require.NoError(t, upstreamErr)
	default:
	}
	require.NoError(t, <-proxyResults)
}

func TestPrepareResponsesWebSocketTurnAttemptRejectsAdvancedCustomConverter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyChannelId, 102)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeAdvancedCustom)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://upstream.example")
	common.SetContextKey(c, constant.ContextKeyChannelKey, "upstream-key")
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/chat/completions",
				Converter:    "openai_responses_to_openai_chat_completions",
			},
		}},
	})
	turn, apiErr := parseResponsesWebSocketTurn(c, websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-model-a","stream":true,"input":[]}`))
	require.Nil(t, apiErr)

	_, apiErr = prepareResponsesWebSocketTurnAttempt(c, turn)

	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeChannelModelMappedError, apiErr.GetErrorCode())
}

func TestPrepareResponsesWebSocketTurnAttemptRejectsMaxOutputTokensOverflow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	payload := []byte(`{"type":"response.create","model":"ws-model","stream":true,"input":[],"max_output_tokens":2147483648}`)
	turn, apiErr := parseResponsesWebSocketTurn(c, websocket.TextMessage, payload)
	require.Nil(t, apiErr)

	_, apiErr = prepareResponsesWebSocketTurnAttempt(c, turn)

	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeInvalidRequest, apiErr.GetErrorCode())
	assert.Contains(t, apiErr.Error(), "max_output_tokens is invalid")
}

func TestApplyConvertedResponsesWebSocketRequestPreservesUnknownFields(t *testing.T) {
	maxTokens := uint(0)
	request := &dto.OpenAIResponsesRequest{
		Model:           "gpt-5.1",
		Instructions:    json.RawMessage(`"system"`),
		Store:           json.RawMessage(`false`),
		MaxOutputTokens: &maxTokens,
		Reasoning:       &dto.Reasoning{Effort: "high"},
	}
	original := []byte(`{"type":"response.create","model":"gpt-5","input":[{"type":"future_item","opaque":{"value":1}}],"future_field":{"enabled":false},"temperature":0}`)

	result, err := applyConvertedResponsesWebSocketRequest(original, request)

	require.NoError(t, err)
	assert.Equal(t, "response.create", gjson.GetBytes(result, "type").String())
	assert.Equal(t, "gpt-5.1", gjson.GetBytes(result, "model").String())
	assert.True(t, gjson.GetBytes(result, "stream").Bool())
	assert.Equal(t, "future_item", gjson.GetBytes(result, "input.0.type").String())
	assert.False(t, gjson.GetBytes(result, "future_field.enabled").Bool())
	assert.Equal(t, uint64(0), gjson.GetBytes(result, "max_output_tokens").Uint())
	assert.False(t, gjson.GetBytes(result, "store").Bool())
	assert.Equal(t, "high", gjson.GetBytes(result, "reasoning.effort").String())
	assert.False(t, gjson.GetBytes(result, "temperature").Exists())
}
