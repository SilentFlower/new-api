package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// useResponsesWebSocketFirstFrameTimeout 隔离真实连接测试使用的站点等待配置。
// @param t 测试上下文。
// @param seconds 站点设置的整数秒数字符串。
// @return 无返回值。
func useResponsesWebSocketFirstFrameTimeout(t *testing.T, seconds string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{constant.ResponsesWebSocketFirstFrameTimeoutOptionKey: seconds}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
	})
}

// newResponsesWebSocketFirstFrameServer 提供真实首帧入口和可等待的请求结束信号。
// @param t 测试上下文。
// @param prepare 可选的鉴权与用户上下文初始化回调。
// @return 测试服务及每个连接退出时发送一次的结束信号。
func newResponsesWebSocketFirstFrameServer(t *testing.T, prepare gin.HandlerFunc) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	done := make(chan struct{}, 2)
	engine := gin.New()
	engine.GET("/v1/responses", func(c *gin.Context) {
		defer func() { done <- struct{}{} }()
		c.Set(common.RequestIdKey, "first-frame-test-request")
		if prepare != nil {
			prepare(c)
		}
		RelayResponsesWebSocket(c)
	})
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return server, done
}

// dialResponsesWebSocketFirstFrame 连接真实入口，并保证恢复全局状态前结束请求。
// @param t 测试上下文。
// @param server 客户端连接的测试服务。
// @param done 请求结束信号。
// @return 配置了读取保护期限的客户端连接。
func dialResponsesWebSocketFirstFrame(t *testing.T, server *httptest.Server, done <-chan struct{}) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(responsesWebSocketServerURL(server), nil)
	require.NoError(t, err)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	t.Cleanup(func() {
		_ = conn.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("首帧测试连接未结束，不能安全恢复共享状态")
		}
	})
	return conn
}

// TestResponsesWebSocketFirstFrameFailureCategories 保护输入、超限和关闭的实际对外语义。
// @param t 测试上下文。
// @return 无返回值。
func TestResponsesWebSocketFirstFrameFailureCategories(t *testing.T) {
	useResponsesWebSocketFirstFrameTimeout(t, "2")
	previousLimit := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() { constant.MaxRequestBodyMB = previousLimit })
	for _, tc := range []struct {
		name    string
		payload string
		close   int
		invalid bool
	}{
		{name: "非法JSON仍拒绝输入", payload: "{invalid", close: websocket.ClosePolicyViolation, invalid: true},
		{name: "正常关闭不追加业务错误", close: websocket.CloseNormalClosure},
		{name: "离开连接不追加业务错误", close: websocket.CloseGoingAway},
		{name: "协议错误保留底层关闭码", close: websocket.CloseProtocolError},
		{name: "超限报文保留底层关闭码", close: websocket.CloseMessageTooBig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, done := newResponsesWebSocketFirstFrameServer(t, nil)
			conn := dialResponsesWebSocketFirstFrame(t, server, done)
			switch tc.close {
			case websocket.CloseProtocolError:
				// 客户端帧未掩码，触发 Gorilla 的真实协议错误出口。
				_, err := conn.UnderlyingConn().Write([]byte{0x81, 0x00})
				require.NoError(t, err)
			case websocket.CloseMessageTooBig:
				// 服务端可在读完报文头后提前关闭，客户端写入也可能因此失败。
				_ = conn.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", (1<<20)+1)))
			case websocket.CloseNormalClosure, websocket.CloseGoingAway:
				require.NoError(t, conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(tc.close, ""), time.Now().Add(time.Second)))
			default:
				require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(tc.payload)))
			}
			_, payload, err := conn.ReadMessage()
			if tc.invalid {
				require.NoError(t, err)
				assert.Equal(t, int64(http.StatusBadRequest), gjson.GetBytes(payload, "status").Int())
				assert.Equal(t, "invalid_request", gjson.GetBytes(payload, "error.code").String())
				_, _, err = conn.ReadMessage()
			}
			assert.True(t, websocket.IsCloseError(err, tc.close), "失败类别对应的关闭信号不正确：%v", err)
		})
	}
}

// TestResponsesWebSocketFirstFrameSettingKeepsExistingDeadline 验证新设置只改变后续连接。
// @param t 测试上下文。
// @return 无返回值。
func TestResponsesWebSocketFirstFrameSettingKeepsExistingDeadline(t *testing.T) {
	useResponsesWebSocketFirstFrameTimeout(t, "2")
	server, done := newResponsesWebSocketFirstFrameServer(t, nil)
	oldConn := dialResponsesWebSocketFirstFrame(t, server, done)
	pong := make(chan struct{}, 1)
	oldPayload := make(chan []byte, 1)
	oldReadError := make(chan error, 1)
	oldConn.SetPongHandler(func(string) error { pong <- struct{}{}; return nil })
	go func() {
		_, payload, err := oldConn.ReadMessage()
		oldPayload <- payload
		oldReadError <- err
	}()
	// Pong 证明服务端已进入读取阶段，避免仅凭 Dial 返回推断期限已经冻结。
	require.NoError(t, oldConn.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)))
	select {
	case <-pong:
	case <-time.After(5 * time.Second):
		t.Fatal("旧连接未进入首帧读取")
	}
	common.OptionMapRWMutex.Lock()
	common.OptionMap[constant.ResponsesWebSocketFirstFrameTimeoutOptionKey] = "1"
	common.OptionMapRWMutex.Unlock()
	newConn := dialResponsesWebSocketFirstFrame(t, server, done)
	_, payload, err := newConn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, int64(http.StatusRequestTimeout), gjson.GetBytes(payload, "status").Int())
	assert.Contains(t, gjson.GetBytes(payload, "error.message").String(), "first-frame-test-request")

	// 新连接先超时后，旧连接仍可发送首帧并进入模型校验。
	require.NoError(t, oldConn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create"}`)))
	select {
	case payload := <-oldPayload:
		require.NoError(t, <-oldReadError)
		assert.Equal(t, int64(http.StatusBadRequest), gjson.GetBytes(payload, "status").Int())
		assert.Contains(t, gjson.GetBytes(payload, "error.message").String(), "model is required")
	case <-time.After(5 * time.Second):
		t.Fatal("旧连接未保留建立时的首帧期限")
	}
}

// TestResponsesWebSocketFirstFrameReconnectRecovery 验证超时零上游、重连完整请求和单次记账。
// @param t 测试上下文。
// @return 无返回值。
func TestResponsesWebSocketFirstFrameReconnectRecovery(t *testing.T) {
	require.NoError(t, i18n.Init())
	useResponsesWebSocketFirstFrameTimeout(t, "1")
	savedCache, savedLog, savedExport := common.MemoryCacheEnabled, common.LogConsumeEnabled, common.DataExportEnabled
	quotaSetting := operation_setting.GetQuotaSetting()
	savedPreconsume := quotaSetting.EnableFreeModelPreConsume
	savedRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		common.MemoryCacheEnabled, common.LogConsumeEnabled, common.DataExportEnabled = savedCache, savedLog, savedExport
		quotaSetting.EnableFreeModelPreConsume = savedPreconsume
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
	})
	db := setupChannelUserLimitsTestDB(t)
	t.Cleanup(func() {
		require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 2*time.Second, time.Millisecond)
	})
	common.MemoryCacheEnabled, common.LogConsumeEnabled, common.DataExportEnabled = false, true, false
	quotaSetting.EnableFreeModelPreConsume = false
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"ws-first-frame-model":0}`))
	upstream, frames, _ := newResponsesWebSocketRecoveryUpstream(t, "resp_first_frame_recovered")
	baseURL := upstream.URL
	channel := &model.Channel{Id: 9731, Type: constant.ChannelTypeOpenAI, Name: "首帧恢复测试", Status: common.ChannelStatusEnabled, Key: "test-key", BaseURL: &baseURL, Models: "ws-first-frame-model", Group: "default", UserConcurrencyLimit: common.GetPointer(1)}
	require.NoError(t, db.Create(channel).Error)
	const userID = 9732
	require.NoError(t, db.Create(&model.User{Id: userID, Username: "ws-first-frame-user", Quota: 10000}).Error)
	server, done := newResponsesWebSocketFirstFrameServer(t, func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUserId, userID)
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
		common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, channel.Id)
	})
	first := dialResponsesWebSocketFirstFrame(t, server, done)
	_, payload, err := first.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "responses_websocket_first_frame_timeout", gjson.GetBytes(payload, "error.code").String())
	_, _, err = first.ReadMessage()
	require.True(t, websocket.IsCloseError(err, websocket.CloseTryAgainLater))
	assert.Empty(t, frames)
	var beforeCount int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&beforeCount).Error)
	assert.Zero(t, beforeCount)
	var user model.User
	require.NoError(t, db.First(&user, userID).Error)
	assert.Equal(t, 10000, user.Quota)
	assert.Zero(t, user.UsedQuota)

	replay := dialResponsesWebSocketFirstFrame(t, server, done)
	require.NoError(t, replay.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-first-frame-model","input":[{"role":"user","content":"完整请求"}]}`)))
	_, payload, err = replay.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "resp_first_frame_recovered", gjson.GetBytes(payload, "response.id").String())
	select {
	case forwarded := <-frames:
		assert.Equal(t, "完整请求", gjson.GetBytes(forwarded, "input.0.content").String())
	case <-time.After(5 * time.Second):
		t.Fatal("完整首帧没有到达测试上游")
	}
	require.NoError(t, replay.Close())
	require.Eventually(t, func() bool {
		usage, _, err := service.GetChannelUserConcurrencyUsage(t.Context(), channel.Id, userID)
		return err == nil && usage == 0
	}, 2*time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 2*time.Second, time.Millisecond)
	var logs []model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, channel.Id, logs[0].ChannelId)
	assert.Empty(t, frames)
}
