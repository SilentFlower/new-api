package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	appdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelLimitFallbackUsesIndependentRequestAndTargetPrice(t *testing.T) {
	previous, err := common.Marshal(ratio_setting.GetModelRatioCopy())
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-4o-mini":2,"gpt-4o":5}`))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(previous))) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	service.InitHttpClient()
	for i, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", path, stream), func(t *testing.T) {
				db := setupChannelUserLimitsTestDB(t)
				// 等待真实退款和指标任务结束，随后才恢复全局配置和关闭测试数据库。
				defer func() {
					require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 2*time.Second, time.Millisecond)
				}()
				require.NoError(t, db.AutoMigrate(&model.Ability{}))
				require.NoError(t, db.Create(&model.User{Id: 77, Username: "fallback-user", Quota: 1000000}).Error)
				oldLog, oldBatch := common.LogConsumeEnabled, common.BatchUpdateEnabled
				common.LogConsumeEnabled, common.BatchUpdateEnabled = true, false
				t.Cleanup(func() { common.LogConsumeEnabled, common.BatchUpdateEnabled = oldLog, oldBatch })
				var requestMu sync.Mutex
				var sent []string
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, readErr := io.ReadAll(r.Body)
					if readErr != nil {
						w.WriteHeader(500)
						return
					}
					requestMu.Lock()
					sent = append(sent, r.Header.Get("Authorization")+" "+r.URL.Path+" "+string(body))
					requestMu.Unlock()
					contentType, response := channelLimitFallbackUpstreamResponse(path, stream)
					w.Header().Set("Content-Type", contentType)
					_, _ = io.WriteString(w, response)
				}))
				t.Cleanup(upstream.Close)
				oldCache := common.MemoryCacheEnabled
				common.MemoryCacheEnabled = false
				t.Cleanup(func() { common.MemoryCacheEnabled = oldCache })
				id := 51000 + i*10
				if stream {
					id++
				}
				mapping, setting := `{"gpt-4o-mini":"gpt-4o"}`, `{"use_upstream_model_for_billing":true}`
				source := &model.Channel{Id: id, Name: "来源", Type: constant.ChannelTypeNewAPI, Status: common.ChannelStatusEnabled, Key: "test-source", Group: "default", Models: "gpt-4o-mini"}
				target := &model.Channel{Id: id + 100, Name: "目标", Type: constant.ChannelTypeNewAPI, Status: common.ChannelStatusEnabled, Key: "test-target", Group: "default", Models: "gpt-4o-mini", ModelMapping: &mapping, Setting: &setting}
				source.BaseURL, target.BaseURL = &upstream.URL, &upstream.URL
				require.NoError(t, db.Create(source).Error)
				require.NoError(t, db.Create(target).Error)
				require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gpt-4o-mini", ChannelId: target.Id, Enabled: true}).Error)
				_, err := service.SaveChannelPeriodPolicy(context.Background(), source.Id, appdto.ChannelPeriodPolicyInput{Config: appdto.ChannelPeriodPolicyConfig{SchemaVersion: 1, PoolDailyQuotaLimit: 1, Fallback: appdto.ChannelLimitFallback{Enabled: true, ChannelID: target.Id, Model: "gpt-4o-mini"}}}, 1)
				require.NoError(t, err)
				require.NoError(t, service.RecordChannelUserQuotaUsage(context.Background(), source.Id, 77, 1))
				body := fmt.Sprintf(`{"model":"gpt-4o-mini","stream":%t,"messages":[{"role":"user","content":"你好"}],"max_tokens":10}`, stream)
				var request dto.Request = &dto.GeneralOpenAIRequest{}
				format, mode := types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions
				if path == "/v1/messages" {
					request = &dto.ClaudeRequest{}
					format = types.RelayFormatClaude
				}
				if path == "/v1/responses" {
					request = &dto.OpenAIResponsesRequest{}
					format = types.RelayFormatOpenAIResponses
					mode = relayconstant.RelayModeResponses
					body = fmt.Sprintf(`{"model":"gpt-4o-mini","stream":%t,"input":"你好","max_output_tokens":10}`, stream)
				}
				require.NoError(t, common.Unmarshal([]byte(body), request))
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", path, strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				t.Cleanup(func() { common.CleanupBodyStorage(c) })
				common.SetContextKey(c, constant.ContextKeyUserId, 77)
				common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
				require.Nil(t, middleware.SetupContextForSelectedChannel(c, source, "gpt-4o-mini"))
				info, err := relaycommon.GenRelayInfo(c, format, request, nil)
				require.NoError(t, err)
				info.RelayMode, info.IsStream, info.RequestURLPath = mode, stream, path
				info.UserId, info.UsingGroup, info.UserGroup, info.TokenGroup = 77, "default", "default", "default"
				info.IsPlayground, info.ForcePreConsume = true, true
				info.UserSetting.BillingPreference = "wallet_only"
				chosen, apiErr := prepareChannelLimitFallback(c, info, source, request)
				require.Nil(t, apiErr, "%v", apiErr)
				assert.Equal(t, target.Id, chosen.Id)
				require.NotNil(t, info.LimitFallback)
				assert.Equal(t, "pool", info.LimitFallback.Scope)
				require.Nil(t, relay.PrepareRequestForSelectedChannel(c, info))
				assert.Equal(t, "gpt-4o-mini", info.OriginModelName)
				assert.Equal(t, "gpt-4o", info.UpstreamModelName)
				originalJSON, err := common.Marshal(request)
				require.NoError(t, err)
				assert.Contains(t, string(originalJSON), `"model":"gpt-4o-mini"`)
				require.Nil(t, prepareMainRelayBilling(c, info))
				assert.Equal(t, "gpt-4o", info.BillingModelName())
				require.NotNil(t, info.Billing)
				assert.Positive(t, info.Billing.GetPreConsumedQuota())
				switch format {
				case types.RelayFormatClaude:
					apiErr = relay.ClaudeHelper(c, info)
				case types.RelayFormatOpenAIResponses:
					apiErr = relay.ResponsesHelper(c, info)
				default:
					apiErr = relay.TextHelper(c, info)
				}
				require.Nil(t, apiErr, "%v", apiErr)
				requestMu.Lock()
				requests := append([]string(nil), sent...)
				requestMu.Unlock()
				require.Len(t, requests, 1)
				assert.Contains(t, requests[0], "Bearer test-target")
				assert.Contains(t, requests[0], `"model":"gpt-4o"`)
				var logs []model.Log
				require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
				require.Len(t, logs, 1)
				assert.Equal(t, target.Id, logs[0].ChannelId)
				assert.Equal(t, "gpt-4o", logs[0].ModelName)
				assert.Contains(t, logs[0].Other, "limit_fallback")
				var user model.User
				require.NoError(t, db.First(&user, 77).Error)
				assert.Positive(t, logs[0].Quota)
				assert.Equal(t, 1000000-logs[0].Quota, user.Quota)
				// 同一资金会话再次结算不能重复扣费。
				require.NoError(t, info.Billing.Settle(logs[0].Quota))
				require.NoError(t, db.First(&user, 77).Error)
				assert.Equal(t, 1000000-logs[0].Quota, user.Quota)
				targetStatus, statusErr := service.GetChannelPeriodStatus(c, target, 77)
				require.NoError(t, statusErr)
				assert.Equal(t, int64(logs[0].Quota), targetStatus.Metrics[2].Used)
				// 只改变目标账单快照不能污染来源累计。
				used, _, _, err := service.GetChannelUserDailyQuotaUsage(c, source.Id, 77)
				require.NoError(t, err)
				assert.Equal(t, int64(1), used)
			})
		}
	}
}

func TestChannelLimitFallbackRejectsUpstreamState(t *testing.T) {
	for _, body := range []string{
		`{"previous_response_id":"resp_1"}`, `{"input":[{"type":"item_reference","id":"item_1"}]}`,
		`{"messages":[{"content":[{"type":"document","source":{"file_id":"file_1"}}]}]}`,
		`{"conversation":{"id":"conv_1"}}`, `{"container":"container_1"}`,
	} {
		assert.False(t, service.ChannelLimitFallbackRequestPortable([]byte(body)), body)
	}
	assert.True(t, service.ChannelLimitFallbackRequestPortable([]byte(`{"messages":[{"role":"user","content":"请解释 previous_response_id"}]}`)))
}

// 模拟三种协议的完整成功响应，流式用显式终止事件结束。
func channelLimitFallbackUpstreamResponse(path string, stream bool) (string, string) {
	if !stream {
		switch path {
		case "/v1/messages":
			return "application/json", `{"id":"msg_1","type":"message","role":"assistant","model":"gpt-4o","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`
		case "/v1/responses":
			return "application/json", `{"id":"resp_1","object":"response","model":"gpt-4o","status":"completed","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}`
		default:
			return "application/json", `{"id":"chatcmpl_1","object":"chat.completion","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`
		}
	}
	switch path {
	case "/v1/messages":
		return "text/event-stream", "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"gpt-4o\",\"content\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	case "/v1/responses":
		return "text/event-stream", "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\",\"output_index\":0,\"content_index\":0}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"gpt-4o\",\"output\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}}\n\n"
	default:
		return "text/event-stream", "data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n\ndata: [DONE]\n\n"
	}
}
