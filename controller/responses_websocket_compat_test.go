package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	appdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
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

func responsesWebSocketServerURL(server *httptest.Server) string {
	return "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/responses"
}

// serveResponsesWSTestSession 在每轮注入夹具身份，运行实际的 WebSocket 会话处理。
func serveResponsesWSTestSession(c *gin.Context) {
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	relay.ResponsesWebSocketHelper(c, ws, func(req *http.Request, id string, handle func(*gin.Context) *types.NewAPIError) *types.NewAPIError {
		turn, _ := gin.CreateTestContext(httptest.NewRecorder())
		turn.Request = req
		for key, v := range c.Keys {
			turn.Set(key, v)
		}
		constraints := service.GetChannelConstraints(c)
		common.SetContextKey(turn, constant.ContextKeyChannelConstraints, &appdto.ChannelConstraints{Pins: slices.Clone(constraints.Pins), Filters: slices.Clone(constraints.Filters)})
		turn.Set(common.RequestIdKey, id)
		defer common.CleanupBodyStorage(turn)
		return handle(turn)
	})
}

func TestProxyResponsesWebSocketSupportsOrdinaryCompactAndOrdinaryTurns(t *testing.T) {
	db := setupChannelUserLimitsTestDB(t)
	gin.SetMode(gin.TestMode)
	// 真实回合会异步记录性能指标，必须在后续用例切换全局缓存状态前收口。
	defer func() {
		require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 2*time.Second, time.Millisecond)
	}()
	oldLogConsumeEnabled := common.LogConsumeEnabled
	oldDataExportEnabled := common.DataExportEnabled
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.LogConsumeEnabled = true
	common.DataExportEnabled = false
	common.MemoryCacheEnabled = false
	quotaSetting := operation_setting.GetQuotaSetting()
	oldFreeModelPreConsume := quotaSetting.EnableFreeModelPreConsume
	quotaSetting.EnableFreeModelPreConsume = false
	savedModelRatios := ratio_setting.ModelRatio2JSONString()
	modelRatios, err := common.Marshal(map[string]float64{
		"ws-model-a": 0,
		"ws-model-b": 0,
		"ws-model-c": 0,
	})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(modelRatios)))
	t.Cleanup(func() {
		common.LogConsumeEnabled = oldLogConsumeEnabled
		common.DataExportEnabled = oldDataExportEnabled
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
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
	channel.SetSetting(dto.ChannelSettings{ResponsesCompactPassthroughEnabled: true, ResponsesWebSocketEnabled: true})
	parent := &model.Channel{Id: 100, Name: "WebSocket 限制父渠道", UserConcurrencyLimit: common.GetPointer(4)}
	require.NoError(t, db.Create(parent).Error)
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&model.User{Id: 787, Username: "ws-timing-user"}).Error)
	relation := &model.ChannelLimitParent{ChannelId: channel.Id, ParentChannelId: parent.Id, UpdatedBy: 1}
	require.NoError(t, service.SaveChannelLimitParent(t.Context(), relation))
	proxyResults := make(chan error, 1)
	engine := gin.New()
	engine.GET("/v1/responses", func(c *gin.Context) {
		service.GetChannelConstraints(c).AddPin(appdto.ChannelPin{ChannelId: channel.Id, Source: appdto.PinSourceToken})
		common.SetContextKey(c, constant.ContextKeyUserId, 787)
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		serveResponsesWSTestSession(c)
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
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		parentUsage, _, usageErr := service.GetChannelUserConcurrencyUsage(t.Context(), parent.Id, 787)
		assert.NoError(collect, usageErr)
		assert.Zero(collect, parentUsage)
	}, time.Second, time.Millisecond)
	// 本轮已释放父渠道名额，解绑后的下一轮读取新的限制归属。
	relation.ParentChannelId = 0
	require.NoError(t, service.SaveChannelLimitParent(t.Context(), relation))
	secondTurn := []byte(`{"type":"response.create","model":"ws-model-b","stream":true,"input":[{"type":"compaction_trigger"}],"client_metadata":{"turn":2}}`)
	require.NoError(t, clientConn.WriteMessage(websocket.TextMessage, secondTurn))
	_, itemDone, err := clientConn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "compaction", gjson.GetBytes(itemDone, "item.type").String())
	_, secondCompleted, err := clientConn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(secondCompleted, "type").String())
	parentUsage, _, err := service.GetChannelUserConcurrencyUsage(t.Context(), parent.Id, 787)
	require.NoError(t, err)
	assert.Zero(t, parentUsage)
	childUsage, _, err := service.GetChannelUserConcurrencyUsage(t.Context(), channel.Id, 787)
	require.NoError(t, err)
	assert.Zero(t, childUsage)

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

	select {
	case upstreamErr := <-upstreamErrors:
		require.NoError(t, upstreamErr)
	default:
	}
	require.NoError(t, <-proxyResults)
	// 核对真实连接中每轮落库的首响应，防止只验证合成状态而遗漏普通与 Compact 回合交替。
	var logs []model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Order("id asc").Find(&logs).Error)
	require.Len(t, logs, 3)
	for index, log := range logs {
		var other map[string]interface{}
		require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
		admin, _ := other["admin_info"].(map[string]any)
		if index != 1 {
			assert.NotContains(t, admin, "responses_compact")
		} else {
			assert.Contains(t, admin, "responses_compact")
		}
		frt, ok := other["frt"].(float64)
		require.True(t, ok)
		assert.GreaterOrEqual(t, frt, float64(0), "模型 %s 的首响应漏记", log.ModelName)
		assert.Zero(t, log.Quota)
		assert.Zero(t, log.PromptTokens)
		assert.Zero(t, log.CompletionTokens)
	}
	parentUsage, _, err = service.GetChannelUserConcurrencyUsage(t.Context(), parent.Id, 787)
	require.NoError(t, err)
	assert.Zero(t, parentUsage)
}
