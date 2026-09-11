package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	appdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 验证完整 Relay 路由中的一次降级、拒绝与恢复，不绕开预扣或并发收口。
func TestChannelLimitFallbackFullRelayBoundaries(t *testing.T) {
	previous, err := common.Marshal(ratio_setting.GetModelRatioCopy())
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-4o-mini":2}`))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(previous))) })
	previousPrices, err := common.Marshal(ratio_setting.GetModelPriceCopy())
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"sora-2":0.1,"mj_imagine":0.1}`))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(previousPrices))) })
	for index, scenario := range []struct {
		name        string
		status      int
		targetCalls int
		sourceCalls int
	}{
		{"retry_zero", 200, 1, 0}, {"target_upstream_error_no_third_hop", 500, 1, 0},
		{"source_recovery", 200, 0, 1}, {"target_exhausted", 429, 0, 0},
		{"pinned", 429, 0, 0}, {"token_model_denied", 403, 0, 0}, {"group_denied", 403, 0, 0},
		{"target_disabled", 403, 0, 0}, {"unknown_field_not_dropped", 400, 0, 0},
		{"compact_no_fallback", 429, 0, 0},
		{"disabled_fallback", 429, 0, 0}, {"upstream_started", 429, 0, 0},
		{"opaque_state", 429, 0, 0}, {"tiered_preflight", 429, 0, 0},
		{"websocket_no_fallback", 429, 0, 0}, {"task_no_fallback", 429, 0, 0},
		{"midjourney_no_fallback", 429, 0, 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db := setupChannelUserLimitsTestDB(t)
			// 等待真实退款和指标任务结束，随后才恢复全局配置和关闭测试数据库。
			defer func() {
				require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 2*time.Second, time.Millisecond)
			}()
			require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.Token{}))
			oldCache, oldRetry, oldLog, oldBatch := common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled
			common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled = false, 0, true, false
			t.Cleanup(func() {
				common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled = oldCache, oldRetry, oldLog, oldBatch
			})
			if scenario.name == "tiered_preflight" {
				modes, err := common.Marshal(billing_setting.GetBillingModeCopy())
				require.NoError(t, err)
				expressions, err := common.Marshal(billing_setting.GetBillingExprCopy())
				require.NoError(t, err)
				t.Cleanup(func() {
					require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_mode": string(modes), "billing_setting.billing_expr": string(expressions)}))
				})
				require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_mode": `{"gpt-4o-mini":"tiered_expr"}`, "billing_setting.billing_expr": `{"gpt-4o-mini":"100 / p + c"}`}))
			}
			service.InitHttpClient()
			var sourceCalls, targetCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") == "Bearer source" {
					sourceCalls.Add(1)
				} else {
					targetCalls.Add(1)
				}
				if scenario.name == "target_upstream_error_no_third_hop" {
					w.WriteHeader(500)
					_, _ = w.Write([]byte(`{"error":{"message":"upstream failure","type":"server_error"}}`))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, response := channelLimitFallbackUpstreamResponse("/v1/chat/completions", false)
				_, _ = w.Write([]byte(response))
			}))
			defer upstream.Close()
			id := 60000 + index*10
			concurrency := 1
			source := &model.Channel{Id: id, Name: "来源", Type: constant.ChannelTypeNewAPI, Status: common.ChannelStatusEnabled, Key: "source", BaseURL: &upstream.URL, Group: "default", Models: "gpt-4o-mini", UserConcurrencyLimit: &concurrency}
			target := &model.Channel{Id: id + 1, Name: "备用", Type: constant.ChannelTypeNewAPI, Status: common.ChannelStatusEnabled, Key: "target", BaseURL: &upstream.URL, Group: "default", Models: "gpt-4o-mini", UserConcurrencyLimit: &concurrency}
			if scenario.name == "task_no_fallback" {
				source.Type = constant.ChannelTypeSora
				source.Models = "sora-2"
			}
			if scenario.name == "midjourney_no_fallback" {
				source.Type = constant.ChannelTypeMidjourney
				source.Models = "mj_imagine"
			}
			if scenario.name == "compact_no_fallback" {
				setting := `{"responses_compact_passthrough_enabled":true}`
				source.Setting = &setting
			}
			if scenario.name == "target_disabled" {
				target.Status = common.ChannelStatusManuallyDisabled
			}
			require.NoError(t, db.Create(source).Error)
			require.NoError(t, db.Create(target).Error)
			require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gpt-4o-mini", ChannelId: target.Id, Enabled: scenario.name != "group_denied"}).Error)
			require.NoError(t, db.Create(&model.User{Id: 77, Username: "测试用户", Quota: 1000000}).Error)
			require.NoError(t, db.Create(&model.Token{Id: 78, UserId: 77, Key: "test-token", RemainQuota: 1000000}).Error)
			config := appdto.ChannelPeriodPolicyConfig{SchemaVersion: 1, PoolDailyQuotaLimit: 1, Fallback: appdto.ChannelLimitFallback{Enabled: true, ChannelID: target.Id, Model: "gpt-4o-mini"}}
			if scenario.name == "disabled_fallback" || scenario.name == "tiered_preflight" {
				config.Fallback.Enabled = false
			}
			revision, err := service.SaveChannelPeriodPolicy(t.Context(), source.Id, appdto.ChannelPeriodPolicyInput{Config: config}, 1)
			require.NoError(t, err)
			require.NoError(t, service.RecordChannelUserQuotaUsage(t.Context(), source.Id, 77, 1))
			if scenario.name == "source_recovery" {
				config.PoolDailyQuotaLimit = 1000
				_, err = service.SaveChannelPeriodPolicy(t.Context(), source.Id, appdto.ChannelPeriodPolicyInput{ExpectedRevision: revision.Revision, Config: config}, 1)
				require.NoError(t, err)
			}
			if scenario.name == "target_exhausted" {
				_, err = service.SaveChannelPeriodPolicy(t.Context(), target.Id, appdto.ChannelPeriodPolicyInput{Config: appdto.ChannelPeriodPolicyConfig{SchemaVersion: 1, PoolDailyQuotaLimit: 1, Fallback: appdto.ChannelLimitFallback{Enabled: true, ChannelID: source.Id, Model: "gpt-4o-mini"}}}, 1)
				require.NoError(t, err)
				require.NoError(t, service.RecordChannelUserQuotaUsage(t.Context(), target.Id, 77, 1))
			}
			body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"你好"}],"max_tokens":10}`
			if scenario.name == "opaque_state" {
				body = strings.TrimSuffix(body, "}") + `,"previous_response_id":"opaque"}`
			}
			path := "/v1/chat/completions"
			format := types.RelayFormatOpenAI
			if scenario.name == "unknown_field_not_dropped" {
				body = strings.TrimSuffix(body, "}") + `,"unknown_capability":true}`
			}
			if scenario.name == "compact_no_fallback" {
				path = "/v1/responses/compact"
				format = types.RelayFormatOpenAIResponses
				body = `{"model":"gpt-4o-mini","input":"你好"}`
			}
			switch scenario.name {
			case "websocket_no_fallback":
				path = "/v1/responses"
				body = `{"type":"response.create","model":"gpt-4o-mini","input":"你好","stream":true}`
			case "task_no_fallback":
				path = "/v1/videos"
				body = `{"model":"sora-2","prompt":"测试视频","seconds":"4"}`
			case "midjourney_no_fallback":
				path = "/mj/submit/imagine"
				body = `{"prompt":"测试图片"}`
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", path, strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			defer common.CleanupBodyStorage(c)
			common.SetContextKey(c, constant.ContextKeyUserId, 77)
			common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenId, 78)
			common.SetContextKey(c, constant.ContextKeyTokenKey, "test-token")
			common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only"})
			if scenario.name == "pinned" {
				common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, fmt.Sprint(source.Id))
			}
			if scenario.name == "token_model_denied" {
				common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
				common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"gpt-4o-mini": false})
			}
			require.Nil(t, middleware.SetupContextForSelectedChannel(c, source, source.Models))
			if scenario.name == "upstream_started" {
				c.Set("channel_limit_upstream_started", true)
			}
			switch scenario.name {
			case "websocket_no_fallback":
				c.Request.Method = http.MethodGet
				turn, apiErr := parseResponsesWebSocketTurn(c, websocket.TextMessage, []byte(body))
				require.Nil(t, apiErr)
				_, apiErr = prepareResponsesWebSocketTurnAttempt(c, turn)
				require.NotNil(t, apiErr)
				assert.Equal(t, types.ErrorCodeChannelPeriodQuotaExceeded, apiErr.GetErrorCode())
				assert.Nil(t, turn.info.Billing)
				assert.Nil(t, turn.info.LimitFallback)
				c.Status(apiErr.StatusCode)
				c.Writer.WriteHeaderNow()
			case "task_no_fallback":
				RelayTask(c)
			case "midjourney_no_fallback":
				RelayMidjourney(c)
			default:
				Relay(c, format)
			}
			assert.Equal(t, scenario.status, recorder.Code, recorder.Body.String())
			assert.Equal(t, int32(scenario.targetCalls), targetCalls.Load())
			assert.Equal(t, int32(scenario.sourceCalls), sourceCalls.Load())
			for _, channelID := range []int{source.Id, target.Id} {
				inFlight, _, err := service.GetChannelUserConcurrencyUsage(c, channelID, 77)
				require.NoError(t, err)
				assert.Zero(t, inFlight)
			}
			if scenario.targetCalls+scenario.sourceCalls == 0 {
				var user model.User
				require.NoError(t, db.First(&user, 77).Error)
				assert.Equal(t, 1000000, user.Quota)
				var count int64
				require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&count).Error)
				assert.Zero(t, count)
			}
		})
	}
}

func TestChannelLimitFallbackPreservesToolsAndImages(t *testing.T) {
	original := []byte(`{"model":"original","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/image.png"}}]}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"temperature":0,"max_tokens":10}`)
	assert.True(t, service.ChannelLimitFallbackPreservesRequest(original, []byte(strings.ReplaceAll(string(original), "original", "target"))))
	for _, removed := range []string{`,"temperature":0`, `,"max_tokens":10`, `"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],`} {
		changed := strings.Replace(string(original), removed, "", 1)
		assert.False(t, service.ChannelLimitFallbackPreservesRequest(original, []byte(changed)))
	}
}
