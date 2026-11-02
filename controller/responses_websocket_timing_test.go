package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResponsesWebSocketFirstResponseTiming 验证首事件建立计时，后续帧和成功结算保持同一日志首响应时间。
// @param t 当前测试上下文。
func TestResponsesWebSocketFirstResponseTiming(t *testing.T) {
	previousLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = previousLogConsumeEnabled })

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	startedAt := time.Now().Add(-2 * time.Second)
	common.SetContextKey(c, constant.ContextKeyRequestStartTime, startedAt)
	stream := true
	// 使用生产初始化入口，避免手写 RelayInfo 遗漏私有的首次响应状态。
	info := relaycommon.GenRelayInfoResponses(c, &dto.OpenAIResponsesRequest{Model: "ws-timing-model", Stream: &stream})
	info.ChannelMeta = &relaycommon.ChannelMeta{}
	billing := &responsesWebSocketBillingStub{}
	info.Billing = billing
	turn := &responsesWebSocketTurn{info: info}
	assert.False(t, info.HasSendResponse())
	assert.Equal(t, float64(-1000), service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 0, 1)["frt"])

	assert.False(t, handleResponsesWebSocketUpstreamEvent(c, turn, nil, []byte(`{"type":"response.created","response":{"id":"resp_timing","status":"in_progress"}}`)))
	firstResponseAt := info.FirstResponseTime
	assert.True(t, info.HasSendResponse())
	assert.GreaterOrEqual(t, firstResponseAt.Sub(startedAt), 2*time.Second)
	firstOther := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 0, 1)
	firstResponseMilliseconds, ok := firstOther["frt"].(float64)
	require.True(t, ok)
	assert.GreaterOrEqual(t, firstResponseMilliseconds, float64(2000))
	assert.Equal(t, float64(firstResponseAt.UnixMilli()-startedAt.UnixMilli()), firstResponseMilliseconds)

	assert.False(t, handleResponsesWebSocketUpstreamEvent(c, turn, nil, []byte(`{"type":"response.output_text.delta","delta":"你好"}`)))
	assert.Equal(t, firstResponseAt, info.FirstResponseTime)
	assert.True(t, handleResponsesWebSocketUpstreamEvent(c, turn, nil, []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`)))
	assert.Equal(t, firstResponseAt, info.FirstResponseTime)
	assert.Equal(t, firstResponseMilliseconds, service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 0, 1)["frt"])
	assert.Equal(t, []int{0}, billing.settled)
	assert.Zero(t, billing.refunds)
}

// TestResponsesWebSocketFirstResponseTimingResetsForNextTurn 验证同一连接上下文的下一轮重新初始化并独立记录首响应。
// @param t 当前测试上下文。
func TestResponsesWebSocketFirstResponseTimingResetsForNextTurn(t *testing.T) {
	c := newResponsesCompactPassthroughWebSocketTestContext(t, false)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	quotaSetting := operation_setting.GetQuotaSetting()
	previousFreeModelPreConsume := quotaSetting.EnableFreeModelPreConsume
	quotaSetting.EnableFreeModelPreConsume = false
	savedModelRatios := ratio_setting.ModelRatio2JSONString()
	modelRatios, err := common.Marshal(map[string]float64{"ws-timing-model": 0})
	require.NoError(t, err)
	t.Cleanup(func() {
		quotaSetting.EnableFreeModelPreConsume = previousFreeModelPreConsume
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModelRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(modelRatios)))
	payload := []byte(`{"type":"response.create","model":"ws-timing-model","stream":true,"input":[{"role":"user","content":"你好"}]}`)

	firstTurn, apiErr := parseResponsesWebSocketTurn(c, websocket.TextMessage, payload)
	require.Nil(t, apiErr)
	_, apiErr = prepareResponsesWebSocketTurnAttempt(c, firstTurn)
	require.Nil(t, apiErr)
	require.NotNil(t, firstTurn.info)
	firstTurn.info.Billing = &responsesWebSocketBillingStub{}
	assert.False(t, firstTurn.info.HasSendResponse())
	assert.False(t, handleResponsesWebSocketUpstreamEvent(c, firstTurn, nil, []byte(`{"type":"response.created"}`)))
	assert.True(t, firstTurn.info.HasSendResponse())
	firstResponseAt := firstTurn.info.FirstResponseTime
	assert.True(t, handleResponsesWebSocketUpstreamEvent(c, firstTurn, nil, []byte(`{"type":"response.cancelled"}`)))

	secondTurn, apiErr := parseResponsesWebSocketTurn(c, websocket.TextMessage, payload)
	require.Nil(t, apiErr)
	_, apiErr = prepareResponsesWebSocketTurnAttempt(c, secondTurn)
	require.Nil(t, apiErr)
	require.NotNil(t, secondTurn.info)
	secondTurn.info.Billing = &responsesWebSocketBillingStub{}
	assert.NotSame(t, firstTurn.info, secondTurn.info)
	assert.False(t, secondTurn.info.StartTime.Before(firstResponseAt))
	assert.False(t, secondTurn.info.HasSendResponse())
	assert.Equal(t, float64(-1000), service.GenerateTextOtherInfo(c, secondTurn.info, 1, 1, 1, 0, 1, 0, 1)["frt"])

	assert.False(t, handleResponsesWebSocketUpstreamEvent(c, secondTurn, nil, []byte(`{"type":"response.created"}`)))
	assert.True(t, secondTurn.info.HasSendResponse())
	assert.False(t, secondTurn.info.FirstResponseTime.Before(secondTurn.info.StartTime))
	assert.Equal(t, float64(secondTurn.info.FirstResponseTime.UnixMilli()-secondTurn.info.StartTime.UnixMilli()), service.GenerateTextOtherInfo(c, secondTurn.info, 1, 1, 1, 0, 1, 0, 1)["frt"])
	assert.Equal(t, firstResponseAt, firstTurn.info.FirstResponseTime)
	assert.True(t, handleResponsesWebSocketUpstreamEvent(c, secondTurn, nil, []byte(`{"type":"response.cancelled"}`)))
}

// TestResponsesWebSocketFirstResponseTimingIgnoresControlFrames 验证上游仅发送 Ping/Pong 后关闭时仍没有模型首响应。
// @param t 当前测试上下文。
func TestResponsesWebSocketFirstResponseTimingIgnoresControlFrames(t *testing.T) {
	previousRetryTimes, previousErrorLogEnabled := common.RetryTimes, constant.ErrorLogEnabled
	common.RetryTimes, constant.ErrorLogEnabled = 0, false
	t.Cleanup(func() {
		common.RetryTimes, constant.ErrorLogEnabled = previousRetryTimes, previousErrorLogEnabled
	})
	gin.SetMode(gin.TestMode)
	upstreamErrors := make(chan error, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			upstreamErrors <- err
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, _, err := conn.ReadMessage(); err != nil {
			upstreamErrors <- err
			return
		}
		// 等代理答复 Pong 再关闭，保证控制帧已被消费，避免用等待时间猜测处理进度。
		conn.SetPongHandler(func(string) error {
			return conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"), time.Now().Add(time.Second))
		})
		for _, messageType := range []int{websocket.PingMessage, websocket.PongMessage} {
			if err := conn.WriteControl(messageType, []byte("timing-probe"), time.Now().Add(time.Second)); err != nil {
				upstreamErrors <- err
				return
			}
		}
		if _, _, err := conn.ReadMessage(); !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
			upstreamErrors <- err
		}
	}))
	defer upstreamServer.Close()

	proxyResults := make(chan error, 1)
	observedInfo := make(chan *relaycommon.RelayInfo, 1)
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
		common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now().Add(-2*time.Second))
		stream := true
		info := relaycommon.GenRelayInfoResponses(c, &dto.OpenAIResponsesRequest{Model: "ws-timing-model", Stream: &stream})
		info.ChannelMeta = &relaycommon.ChannelMeta{}
		turn := &responsesWebSocketTurn{info: info}
		upstreamConn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(upstreamServer.URL, "http"), nil)
		if err != nil {
			proxyResults <- err
			return
		}
		defer upstreamConn.Close()
		if err := upstreamConn.WriteMessage(messageType, payload); err != nil {
			proxyResults <- err
			return
		}
		apiErr := proxyResponsesWebSocket(c, clientConn, upstreamConn, nil, turn)
		observedInfo <- info
		if apiErr != nil {
			proxyResults <- apiErr
			return
		}
		proxyResults <- nil
	})
	proxyServer := httptest.NewServer(engine)
	defer proxyServer.Close()
	clientConn, _, err := websocket.DefaultDialer.Dial(responsesWebSocketServerURL(proxyServer), nil)
	require.NoError(t, err)
	defer clientConn.Close()
	require.NoError(t, clientConn.SetReadDeadline(time.Now().Add(5*time.Second)))
	require.NoError(t, clientConn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-timing-model"}`)))
	_, _, closeErr := clientConn.ReadMessage()
	require.True(t, websocket.IsCloseError(closeErr, websocket.CloseNormalClosure), "非预期关闭：%v", closeErr)
	require.NoError(t, <-proxyResults)
	info := <-observedInfo
	assert.False(t, info.HasSendResponse())
	assert.Equal(t, info.StartTime.Add(-time.Second), info.FirstResponseTime)
	assertNoResponsesWebSocketServerError(t, upstreamErrors)
}
