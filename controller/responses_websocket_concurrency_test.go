package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

type responsesWebSocketConcurrencyRequest struct {
	conn    *websocket.Conn
	payload []byte
}

type responsesWebSocketConcurrencyFixture struct {
	db       *gorm.DB
	channel  *model.Channel
	userID   int
	proxy    *httptest.Server
	requests chan responsesWebSocketConcurrencyRequest
	clients  []*websocket.Conn
}

// newResponsesWebSocketConcurrencyFixture 使用真实连接和租约存储控制每轮上游终态。
func newResponsesWebSocketConcurrencyFixture(t *testing.T) *responsesWebSocketConcurrencyFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := setupChannelUserLimitsTestDB(t)
	savedCache, savedLog, savedExport := common.MemoryCacheEnabled, common.LogConsumeEnabled, common.DataExportEnabled
	savedRetry, savedErrorLog := common.RetryTimes, constant.ErrorLogEnabled
	quotaSetting := operation_setting.GetQuotaSetting()
	savedPreconsume := quotaSetting.EnableFreeModelPreConsume
	savedRatios := ratio_setting.ModelRatio2JSONString()
	common.MemoryCacheEnabled, common.LogConsumeEnabled, common.DataExportEnabled = false, false, false
	common.RetryTimes, constant.ErrorLogEnabled = 0, false
	quotaSetting.EnableFreeModelPreConsume = false
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"ws-concurrency-model":0,"ws-paid-model":1}`))
	t.Cleanup(func() {
		require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 2*time.Second, time.Millisecond)
		common.MemoryCacheEnabled, common.LogConsumeEnabled, common.DataExportEnabled = savedCache, savedLog, savedExport
		common.RetryTimes, constant.ErrorLogEnabled = savedRetry, savedErrorLog
		quotaSetting.EnableFreeModelPreConsume = savedPreconsume
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
	})

	f := &responsesWebSocketConcurrencyFixture{db: db, userID: 9826, requests: make(chan responsesWebSocketConcurrencyRequest, 8)}
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("测试上游升级失败: %v", err)
			return
		}
		defer conn.Close()
		for {
			_, payload, readErr := conn.ReadMessage()
			if readErr != nil {
				return
			}
			f.requests <- responsesWebSocketConcurrencyRequest{conn: conn, payload: payload}
		}
	}))
	t.Cleanup(upstream.Close)
	baseURL := upstream.URL
	f.channel = &model.Channel{
		Id: 9852, Type: constant.ChannelTypeOpenAI, Name: "请求级并发测试渠道", Key: "test-key",
		Status: common.ChannelStatusEnabled, BaseURL: &baseURL, Models: "ws-concurrency-model", Group: "default",
		UserConcurrencyLimit: common.GetPointer(1), AutoBan: common.GetPointer(0),
	}
	f.channel.SetSetting(dto.ChannelSettings{ResponsesCompactPassthroughEnabled: true})
	require.NoError(t, db.Create(f.channel).Error)
	require.NoError(t, db.Create(&model.User{Id: f.userID, Username: "ws-concurrency-user"}).Error)
	var handlers sync.WaitGroup
	engine := gin.New()
	engine.GET("/v1/responses", func(c *gin.Context) {
		handlers.Add(1)
		defer handlers.Done()
		common.SetContextKey(c, constant.ContextKeyUserId, f.userID)
		common.SetContextKey(c, constant.ContextKeyUserQuota, common.QuotaRemindThreshold+1)
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, fmt.Sprint(f.channel.Id))
		RelayResponsesWebSocket(c)
	})
	f.proxy = httptest.NewServer(engine)
	t.Cleanup(func() {
		for _, conn := range f.clients {
			_ = conn.Close()
		}
		f.proxy.Close()
		done := make(chan struct{})
		go func() { handlers.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("测试代理未在连接关闭后退出")
		}
	})
	return f
}

// dial 创建同一用户的新客户端连接，并在测试结束时关闭。
func (f *responsesWebSocketConcurrencyFixture) dial(t *testing.T) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(responsesWebSocketServerURL(f.proxy), http.Header{"X-Codex-Beta-Features": {"remote_compaction_v2"}})
	require.NoError(t, err)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	f.clients = append(f.clients, conn)
	return conn
}

// nextRequest 等待实际上游收到请求，作为租约已获取的同步边界。
func (f *responsesWebSocketConcurrencyFixture) nextRequest(t *testing.T) responsesWebSocketConcurrencyRequest {
	t.Helper()
	select {
	case request := <-f.requests:
		return request
	case <-time.After(5 * time.Second):
		t.Fatal("上游未收到预期请求")
		return responsesWebSocketConcurrencyRequest{}
	}
}

// awaitUsage 等待终态处理或异常清理完成后的可观察并发状态。
func (f *responsesWebSocketConcurrencyFixture) awaitUsage(t *testing.T, channelID int, want int) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		usage, _, err := service.GetChannelUserConcurrencyUsage(t.Context(), channelID, f.userID)
		assert.NoError(c, err)
		assert.Equal(c, want, usage)
	}, time.Second, time.Millisecond)
}

// TestResponsesWebSocketConcurrencyReleasesBetweenTurns 验证终态释放名额后仍能复用同一上游续接。
// @param t 测试上下文。
// @return 无返回值。
func TestResponsesWebSocketConcurrencyReleasesBetweenTurns(t *testing.T) {
	for _, compact := range []bool{false, true} {
		t.Run(fmt.Sprintf("compact=%t", compact), func(t *testing.T) {
			f := newResponsesWebSocketConcurrencyFixture(t)
			client := f.dial(t)
			input := `[{"role":"user","content":"测试"}]`
			if compact {
				input = `[{"type":"compaction_trigger"}]`
			}
			first := []byte(fmt.Sprintf(`{"type":"response.create","model":"ws-concurrency-model","input":%s}`, input))
			require.NoError(t, client.WriteMessage(websocket.TextMessage, first))
			request := f.nextRequest(t)
			f.awaitUsage(t, f.channel.Id, 1)
			completed := []byte(`{"type":"response.completed","response":{"id":"resp_first","usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`)
			require.NoError(t, request.conn.WriteMessage(websocket.TextMessage, completed))
			_, response, err := client.ReadMessage()
			require.NoError(t, err)
			assert.JSONEq(t, string(completed), string(response))
			f.awaitUsage(t, f.channel.Id, 0)

			second := []byte(fmt.Sprintf(`{"type":"response.create","model":"ws-concurrency-model","previous_response_id":"resp_first","input":%s}`, input))
			require.NoError(t, client.WriteMessage(websocket.TextMessage, second))
			next := f.nextRequest(t)
			assert.Same(t, request.conn, next.conn)
			assert.Equal(t, "resp_first", gjson.GetBytes(next.payload, "previous_response_id").String())
			f.awaitUsage(t, f.channel.Id, 1)
			require.NoError(t, next.conn.WriteMessage(websocket.TextMessage, completed))
			_, _, err = client.ReadMessage()
			require.NoError(t, err)
			f.awaitUsage(t, f.channel.Id, 0)
		})
	}
}

// TestResponsesWebSocketConcurrencyLimitsActiveRequests 验证空闲连接让出名额，复用时重新限流。
// @param t 测试上下文。
// @return 无返回值。
func TestResponsesWebSocketConcurrencyLimitsActiveRequests(t *testing.T) {
	f := newResponsesWebSocketConcurrencyFixture(t)
	firstClient := f.dial(t)
	payload := []byte(`{"type":"response.create","model":"ws-concurrency-model","input":[]}`)
	require.NoError(t, firstClient.WriteMessage(websocket.TextMessage, payload))
	first := f.nextRequest(t)
	require.NoError(t, first.conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`)))
	_, _, err := firstClient.ReadMessage()
	require.NoError(t, err)
	f.awaitUsage(t, f.channel.Id, 0)

	secondClient := f.dial(t)
	require.NoError(t, secondClient.WriteMessage(websocket.TextMessage, payload))
	second := f.nextRequest(t)
	f.awaitUsage(t, f.channel.Id, 1)
	// 付费模型在额度不足时应先命中并发拒绝，验证检查发生在预扣之前。
	require.NoError(t, firstClient.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-paid-model","input":[]}`)))
	_, rejection, err := firstClient.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, int64(429), gjson.GetBytes(rejection, "status").Int())
	assert.Equal(t, "channel_user_concurrency_exceeded", gjson.GetBytes(rejection, "error.code").String())
	select {
	case unexpected := <-f.requests:
		t.Fatalf("被拒绝的请求进入上游: %s", gjson.GetBytes(unexpected.payload, "type").String())
	default:
	}
	f.awaitUsage(t, f.channel.Id, 1)
	require.NoError(t, secondClient.Close())
	f.awaitUsage(t, f.channel.Id, 0)
	_ = second.conn.Close()
}

// TestResponsesWebSocketConcurrencyReleasesTerminalErrors 验证普通及 Compact 的失败终态释放名额并保留连接。
// @param t 测试上下文。
// @return 无返回值。
func TestResponsesWebSocketConcurrencyReleasesTerminalErrors(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, terminal := range []string{"response.failed", "response.incomplete", "response.cancelled", "response.error", "error", "response.completed"} {
			t.Run(fmt.Sprintf("compact=%t/%s", compact, terminal), func(t *testing.T) {
				f := newResponsesWebSocketConcurrencyFixture(t)
				client := f.dial(t)
				input := `[]`
				if compact {
					input = `[{"type":"compaction_trigger"}]`
				}
				payload := []byte(fmt.Sprintf(`{"type":"response.create","model":"ws-concurrency-model","input":%s}`, input))
				require.NoError(t, client.WriteMessage(websocket.TextMessage, payload))
				request := f.nextRequest(t)
				f.awaitUsage(t, f.channel.Id, 1)
				// completed 缺少 usage 走退款终态，不能留下并发名额。
				event := []byte(fmt.Sprintf(`{"type":%q}`, terminal))
				require.NoError(t, request.conn.WriteMessage(websocket.TextMessage, event))
				_, received, err := client.ReadMessage()
				require.NoError(t, err)
				assert.JSONEq(t, string(event), string(received))
				f.awaitUsage(t, f.channel.Id, 0)
				require.NoError(t, client.WriteMessage(websocket.TextMessage, payload))
				next := f.nextRequest(t)
				assert.Same(t, request.conn, next.conn)
				f.awaitUsage(t, f.channel.Id, 1)
			})
		}
	}
}

// TestResponsesWebSocketConcurrencyReleasesOnDisconnect 验证两端断开都能释放活动名额。
// @param t 测试上下文。
// @return 无返回值。
func TestResponsesWebSocketConcurrencyReleasesOnDisconnect(t *testing.T) {
	for _, side := range []string{"client", "upstream"} {
		t.Run(side, func(t *testing.T) {
			f := newResponsesWebSocketConcurrencyFixture(t)
			client := f.dial(t)
			require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-concurrency-model","input":[]}`)))
			request := f.nextRequest(t)
			f.awaitUsage(t, f.channel.Id, 1)
			if side == "client" {
				require.NoError(t, client.Close())
			} else {
				require.NoError(t, request.conn.Close())
				_, _, err := client.ReadMessage()
				// 上游异常断开会先转发错误封套，随后关闭客户端连接。
				if err == nil {
					_, _, err = client.ReadMessage()
				}
				require.Error(t, err)
			}
			f.awaitUsage(t, f.channel.Id, 0)
		})
	}
}

// TestResponsesWebSocketConcurrencyReleasesPreparationFailure 验证后续轮次校验失败释放已获取的名额。
// @param t 测试上下文。
// @return 无返回值。
func TestResponsesWebSocketConcurrencyReleasesPreparationFailure(t *testing.T) {
	f := newResponsesWebSocketConcurrencyFixture(t)
	client := f.dial(t)
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-concurrency-model","input":[]}`)))
	first := f.nextRequest(t)
	require.NoError(t, first.conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`)))
	_, _, err := client.ReadMessage()
	require.NoError(t, err)
	f.awaitUsage(t, f.channel.Id, 0)
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-concurrency-model","input":[],"max_output_tokens":18446744073686646784}`)))
	_, rejection, err := client.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, int64(400), gjson.GetBytes(rejection, "status").Int())
	_, _, err = client.ReadMessage()
	require.Error(t, err)
	f.awaitUsage(t, f.channel.Id, 0)
	select {
	case <-f.requests:
		t.Fatal("非法请求进入上游")
	default:
	}
}

// TestResponsesWebSocketConcurrencyRefreshesParentBetweenTurns 验证回合内冻结父桶，下一回合读取新的关联。
// @param t 测试上下文。
// @return 无返回值。
func TestResponsesWebSocketConcurrencyRefreshesParentBetweenTurns(t *testing.T) {
	f := newResponsesWebSocketConcurrencyFixture(t)
	parent := &model.Channel{Id: 9853, Name: "请求并发父渠道", UserConcurrencyLimit: common.GetPointer(1)}
	require.NoError(t, f.db.Create(parent).Error)
	relation := &model.ChannelLimitParent{ChannelId: f.channel.Id, ParentChannelId: parent.Id, UpdatedBy: 1}
	require.NoError(t, service.SaveChannelLimitParent(t.Context(), relation))
	client := f.dial(t)
	payload := []byte(`{"type":"response.create","model":"ws-concurrency-model","input":[]}`)
	require.NoError(t, client.WriteMessage(websocket.TextMessage, payload))
	first := f.nextRequest(t)
	f.awaitUsage(t, parent.Id, 1)
	relation.ParentChannelId = 0
	require.NoError(t, service.SaveChannelLimitParent(t.Context(), relation))
	f.awaitUsage(t, parent.Id, 1)
	f.awaitUsage(t, f.channel.Id, 0)
	require.NoError(t, first.conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`)))
	_, _, err := client.ReadMessage()
	require.NoError(t, err)
	f.awaitUsage(t, parent.Id, 0)
	require.NoError(t, client.WriteMessage(websocket.TextMessage, payload))
	next := f.nextRequest(t)
	assert.Same(t, first.conn, next.conn)
	f.awaitUsage(t, f.channel.Id, 1)
	f.awaitUsage(t, parent.Id, 0)
}

// TestResponsesWebSocketConcurrencyRefreshesLimitsBetweenTurns 验证个人覆盖和渠道默认提额对后续回合生效。
// @param t 测试上下文。
// @return 无返回值。
func TestResponsesWebSocketConcurrencyRefreshesLimitsBetweenTurns(t *testing.T) {
	for _, source := range []string{"override", "channel"} {
		t.Run(source, func(t *testing.T) {
			f := newResponsesWebSocketConcurrencyFixture(t)
			client := f.dial(t)
			payload := []byte(`{"type":"response.create","model":"ws-concurrency-model","input":[]}`)
			require.NoError(t, client.WriteMessage(websocket.TextMessage, payload))
			first := f.nextRequest(t)
			require.NoError(t, first.conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`)))
			_, _, err := client.ReadMessage()
			require.NoError(t, err)
			f.awaitUsage(t, f.channel.Id, 0)
			other, err := service.AcquireChannelUserConcurrency(t.Context(), f.channel.Id, f.userID, 1, nil)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, other.Release(t.Context())) })
			if source == "override" {
				require.NoError(t, service.ReplaceChannelUserLimitOverride(t.Context(), f.channel, f.userID, service.ChannelUserLimitOverrideInput{UserConcurrencyLimit: common.GetPointer(2)}, 1))
				t.Cleanup(func() {
					require.NoError(t, service.DeleteChannelUserLimitOverride(t.Context(), f.channel.Id, f.userID))
				})
			} else {
				require.NoError(t, f.db.Model(f.channel).Update("user_concurrency_limit", 2).Error)
			}
			require.NoError(t, client.WriteMessage(websocket.TextMessage, payload))
			next := f.nextRequest(t)
			assert.Same(t, first.conn, next.conn)
			f.awaitUsage(t, f.channel.Id, 2)
		})
	}
}
