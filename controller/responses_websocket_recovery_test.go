package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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

// newResponsesWebSocketRecoveryUpstream 提供可观察完整请求与连接释放的上游
func newResponsesWebSocketRecoveryUpstream(t *testing.T, responseID string) (*httptest.Server, <-chan []byte, <-chan struct{}) {
	t.Helper()
	frames := make(chan []byte, 4)
	closed := make(chan struct{})
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(closed)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("测试上游升级失败: %v", err)
			return
		}
		defer conn.Close()
		for {
			messageType, payload, readErr := conn.ReadMessage()
			if readErr != nil {
				return
			}
			frames <- payload
			completed := []byte(fmt.Sprintf(`{"type":"response.completed","response":{"id":%q,"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}`, responseID))
			if writeErr := conn.WriteMessage(messageType, completed); writeErr != nil {
				t.Errorf("测试上游写入终态失败: %v", writeErr)
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	return server, frames, closed
}

// TestResponsesWebSocketChannelChangeRecovery 验证分组变更后的重选、续接恢复与权限边界
// @param t 测试上下文
// @return 无返回值
func TestResponsesWebSocketChannelChangeRecovery(t *testing.T) {
	require.NoError(t, i18n.Init())
	cases := []struct {
		name        string
		previousID  string
		model       string
		alternative bool
		errorCode   string
		errorStatus int64
		replay      bool
	}{
		{name: "完整请求重选渠道", model: "ws-recovery-model", alternative: true},
		{name: "增量请求恢复完整历史", previousID: "resp_primary", model: "ws-recovery-model", alternative: true, errorCode: "previous_response_not_found", errorStatus: 400, replay: true},
		{name: "没有可用渠道", model: "ws-recovery-model", errorCode: "model_not_found", errorStatus: 503},
		{name: "模型权限不可绕过", model: "ws-forbidden-model", alternative: true, errorCode: "access_denied", errorStatus: 403},
	}
	for _, memoryCache := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("cache=%t/%s", memoryCache, tc.name), func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				savedCache, savedLog, savedExport := common.MemoryCacheEnabled, common.LogConsumeEnabled, common.DataExportEnabled
				quotaSetting := operation_setting.GetQuotaSetting()
				savedPreconsume := quotaSetting.EnableFreeModelPreConsume
				savedRatios := ratio_setting.ModelRatio2JSONString()
				t.Cleanup(func() {
					common.MemoryCacheEnabled, common.LogConsumeEnabled, common.DataExportEnabled = savedCache, savedLog, savedExport
					quotaSetting.EnableFreeModelPreConsume = savedPreconsume
					require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
					if model.DB != nil {
						model.InitChannelCache()
					}
				})
				db := setupChannelUserLimitsTestDB(t)
				require.NoError(t, db.AutoMigrate(&model.Ability{}))
				common.MemoryCacheEnabled, common.LogConsumeEnabled, common.DataExportEnabled = memoryCache, true, false
				quotaSetting.EnableFreeModelPreConsume = false
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"ws-recovery-model":0}`))

				primaryServer, primaryFrames, primaryClosed := newResponsesWebSocketRecoveryUpstream(t, "resp_primary")
				alternativeServer, alternativeFrames, _ := newResponsesWebSocketRecoveryUpstream(t, "resp_alternative")
				primaryURL, alternativeURL := primaryServer.URL, alternativeServer.URL
				parent := &model.Channel{Id: 9710, Name: "共享并发限制", Models: "ws-parent-model", Group: "default", UserConcurrencyLimit: common.GetPointer(1)}
				primary := &model.Channel{Id: 9711, Type: constant.ChannelTypeOpenAI, Name: "原渠道", Status: common.ChannelStatusEnabled, Key: "test-primary-key", BaseURL: &primaryURL, Models: "ws-recovery-model", Group: "default"}
				alternative := &model.Channel{Id: 9712, Type: constant.ChannelTypeOpenAI, Name: "替代渠道", Status: common.ChannelStatusEnabled, Key: "test-alternative-key", BaseURL: &alternativeURL, Models: "ws-recovery-model", Group: "replacement"}
				for _, channel := range []*model.Channel{parent, primary, alternative} {
					require.NoError(t, db.Create(channel).Error)
				}
				require.NoError(t, db.Create(&model.Ability{Group: parent.Group, Model: parent.Models, ChannelId: parent.Id, Enabled: true}).Error)
				for _, channel := range []*model.Channel{primary, alternative} {
					require.NoError(t, db.Create(&model.Ability{Group: channel.Group, Model: "ws-recovery-model", ChannelId: channel.Id, Enabled: true}).Error)
					require.NoError(t, service.SaveChannelLimitParent(t.Context(), &model.ChannelLimitParent{ChannelId: channel.Id, ParentChannelId: parent.Id, UpdatedBy: 1}))
				}
				const userID = 9713
				require.NoError(t, db.Create(&model.User{Id: userID, Username: "ws-recovery-user"}).Error)
				model.InitChannelCache()

				handlersDone := make(chan struct{}, 2)
				engine := gin.New()
				engine.GET("/v1/responses", func(c *gin.Context) {
					defer func() { handlersDone <- struct{}{} }()
					common.SetContextKey(c, constant.ContextKeyUserId, userID)
					common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
					common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
					common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
					common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
					common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"ws-recovery-model": true})
					RelayResponsesWebSocket(c)
				})
				proxyServer := httptest.NewServer(engine)
				t.Cleanup(proxyServer.Close)
				var clientConn *websocket.Conn
				pendingHandlers := 0
				t.Cleanup(func() {
					if clientConn != nil {
						_ = clientConn.Close()
					}
					for pendingHandlers > 0 {
						select {
						case <-handlersDone:
							pendingHandlers--
						case <-time.After(5 * time.Second):
							t.Error("WebSocket 请求未在清理期限内结束")
							return
						}
					}
					require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 2*time.Second, time.Millisecond)
				})
				var err error
				clientConn, _, err = websocket.DefaultDialer.Dial(responsesWebSocketServerURL(proxyServer), nil)
				require.NoError(t, err)
				pendingHandlers++
				require.NoError(t, clientConn.SetReadDeadline(time.Now().Add(5*time.Second)))
				fullRequest := []byte(`{"type":"response.create","model":"ws-recovery-model","input":[{"role":"user","content":"完整历史"}],"future_field":{"keep":true}}`)
				require.NoError(t, clientConn.WriteMessage(websocket.TextMessage, fullRequest))
				_, firstResponse, err := clientConn.ReadMessage()
				require.NoError(t, err)
				require.Equal(t, "resp_primary", gjson.GetBytes(firstResponse, "response.id").String())
				<-primaryFrames

				// 模拟管理员移动分组后发布能力缓存，原连接仍持有旧渠道与共享父租约
				require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", primary.Id).Update("group", "moved").Error)
				require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", primary.Id).Update("group", "moved").Error)
				if tc.alternative {
					require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", alternative.Id).Update("group", "default").Error)
					require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", alternative.Id).Update("group", "default").Error)
				}
				model.InitChannelCache()
				history := []map[string]any{{"role": "user", "content": "完整历史"}, {"role": "user", "content": "后续输入"}}
				nextBody := map[string]any{
					"type": "response.create", "model": tc.model, "input": history, "future_field": map[string]any{"keep": true},
				}
				replayRequest, err := common.Marshal(nextBody)
				require.NoError(t, err)
				if tc.previousID != "" {
					nextBody["previous_response_id"] = tc.previousID
					nextBody["input"] = history[1:]
				}
				nextRequest, err := common.Marshal(nextBody)
				require.NoError(t, err)
				require.NoError(t, clientConn.WriteMessage(websocket.TextMessage, nextRequest))
				_, nextResponse, err := clientConn.ReadMessage()
				require.NoError(t, err)
				if tc.errorCode != "" {
					assert.Equal(t, "error", gjson.GetBytes(nextResponse, "type").String())
					assert.Equal(t, tc.errorCode, gjson.GetBytes(nextResponse, "error.code").String())
					assert.Equal(t, tc.errorStatus, gjson.GetBytes(nextResponse, "status").Int())
					if tc.replay {
						assert.Equal(t, "previous_response_id", gjson.GetBytes(nextResponse, "error.param").String())
						assert.Equal(t, "invalid_request_error", gjson.GetBytes(nextResponse, "error.type").String())
					}
					_, _, closeErr := clientConn.ReadMessage()
					require.True(t, websocket.IsCloseError(closeErr, websocket.ClosePolicyViolation, websocket.CloseInternalServerErr), "错误响应后连接未关闭: %v", closeErr)
					_ = clientConn.Close()
					clientConn = nil
					<-handlersDone
					pendingHandlers--
				}
				if tc.replay {
					// 对照 Codex 的恢复行为，新连接清除旧 response ID 并重新提交完整历史
					clientConn, _, err = websocket.DefaultDialer.Dial(responsesWebSocketServerURL(proxyServer), nil)
					require.NoError(t, err)
					pendingHandlers++
					require.NoError(t, clientConn.SetReadDeadline(time.Now().Add(5*time.Second)))
					require.NoError(t, clientConn.WriteMessage(websocket.TextMessage, replayRequest))
					_, nextResponse, err = clientConn.ReadMessage()
					require.NoError(t, err)
				}
				expectedLogs := 1
				if tc.errorCode == "" || tc.replay {
					require.Equal(t, "resp_alternative", gjson.GetBytes(nextResponse, "response.id").String())
					forwarded := <-alternativeFrames
					assert.Empty(t, gjson.GetBytes(forwarded, "previous_response_id").String())
					assert.True(t, gjson.GetBytes(forwarded, "future_field.keep").Bool())
					assert.Len(t, gjson.GetBytes(forwarded, "input").Array(), 2)
					assert.Equal(t, "完整历史", gjson.GetBytes(forwarded, "input.0.content").String())
					assert.Equal(t, "后续输入", gjson.GetBytes(forwarded, "input.1.content").String())
					usage, _, usageErr := service.GetChannelUserConcurrencyUsage(t.Context(), parent.Id, userID)
					require.NoError(t, usageErr)
					assert.Equal(t, 1, usage)
					expectedLogs++
					_ = clientConn.Close()
					clientConn = nil
					<-handlersDone
					pendingHandlers--
				}
				select {
				case <-primaryClosed:
				case <-time.After(5 * time.Second):
					t.Fatal("原渠道连接未释放")
				}
				assert.Empty(t, primaryFrames)
				assert.Empty(t, alternativeFrames)
				usage, _, err := service.GetChannelUserConcurrencyUsage(t.Context(), parent.Id, userID)
				require.NoError(t, err)
				assert.Zero(t, usage)
				var logs []model.Log
				require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Order("id asc").Find(&logs).Error)
				require.Len(t, logs, expectedLogs)
				assert.Equal(t, primary.Id, logs[0].ChannelId)
				if expectedLogs == 2 {
					assert.Equal(t, alternative.Id, logs[1].ChannelId)
				}
			})
		}
	}
}
