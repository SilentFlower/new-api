package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	appdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChannelLimitFallbackPreservesCompactPassthrough 验证周期策略不会破坏 Compact 透传、基础模型计费和门禁。
// @param t 测试上下文。
func TestChannelLimitFallbackPreservesCompactPassthrough(t *testing.T) {
	previousRatios, err := common.Marshal(ratio_setting.GetModelRatioCopy())
	require.NoError(t, err)
	previousPrices, err := common.Marshal(ratio_setting.GetModelPriceCopy())
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-6-astra":2}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(previousRatios)))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(previousPrices)))
	})
	for modeIndex, mode := range []relayconstant.ResponsesCompactMode{
		relayconstant.ResponsesCompactModeV1Path,
		relayconstant.ResponsesCompactModeV1BodyBridge,
		relayconstant.ResponsesCompactModeV2HTTP,
	} {
		for stateIndex, state := range []string{"正常", "额度耗尽", "能力关闭"} {
			t.Run(string(mode)+"/"+state, func(t *testing.T) {
				db := setupChannelUserLimitsTestDB(t)
				defer func() {
					require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 2*time.Second, time.Millisecond)
				}()
				require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.Token{}))
				oldCache, oldRetry, oldLog, oldBatch := common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled
				common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled = false, 0, true, false
				t.Cleanup(func() {
					common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled = oldCache, oldRetry, oldLog, oldBatch
				})
				service.InitHttpClient()
				path := "/v1/responses"
				body := `{"model":"gpt-6-astra","stream":true,"input":[{"type":"compaction_trigger"}],"future":{"count":0,"enabled":false}}`
				response := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"compact_test\",\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}}\n\n"
				if mode == relayconstant.ResponsesCompactModeV1Path {
					path = "/v1/responses/compact"
					body = `{"model":"gpt-6-astra","input":"测试压缩","future":{"count":0,"enabled":false}}`
					response = `{"id":"compact_test","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}`
				}
				calls := make(chan string, 2)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requestBody, readErr := io.ReadAll(r.Body)
					assert.NoError(t, readErr)
					assert.Equal(t, path, r.URL.Path)
					assert.Equal(t, body, string(requestBody))
					calls <- r.Header.Get("Authorization")
					contentType := "text/event-stream"
					if mode == relayconstant.ResponsesCompactModeV1Path {
						contentType = "application/json"
					}
					w.Header().Set("Content-Type", contentType)
					_, _ = io.WriteString(w, response)
				}))
				defer upstream.Close()
				id := 65000 + modeIndex*100 + stateIndex*10
				settings := fmt.Sprintf(`{"responses_compact_passthrough_enabled":%t}`, state != "能力关闭")
				source := &model.Channel{Id: id, Name: "压缩来源", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "compact-source", BaseURL: &upstream.URL, Group: "default", Models: "gpt-6-astra", Setting: &settings}
				target := &model.Channel{Id: id + 1, Name: "不应调用的备用", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "compact-target", BaseURL: &upstream.URL, Group: "default", Models: "gpt-6-astra"}
				require.NoError(t, db.Create(source).Error)
				require.NoError(t, db.Create(target).Error)
				require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gpt-6-astra", ChannelId: target.Id, Enabled: true}).Error)
				require.NoError(t, db.Create(&model.User{Id: 77, Username: "compact-user", Quota: 1000000}).Error)
				require.NoError(t, db.Create(&model.Token{Id: 78, UserId: 77, Key: "compact-token", RemainQuota: 1000000}).Error)
				limit := int64(1000000)
				if state == "额度耗尽" {
					limit = 1
				}
				config := budgetPoolDailyConfig(limit, target.Id, "gpt-6-astra")
				if state == "正常" {
					config.DefaultOnExceed = appdto.ChannelBudgetAction{Mode: "reject"}
				}
				_, err := service.SaveChannelPeriodPolicy(t.Context(), id, appdto.ChannelPeriodPolicyInput{Config: config}, 1)
				require.NoError(t, err)
				require.NoError(t, service.RecordChannelUserQuotaUsage(t.Context(), id, 77, 1))
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				if mode == relayconstant.ResponsesCompactModeV2HTTP {
					c.Request.Header.Set("x-codex-beta-features", "remote_compaction_v2")
				}
				detected := helper.DetectResponsesCompactMode(http.MethodPost, path, c.Request.Header, []byte(body), helper.ResponsesTransportHTTP)
				require.Equal(t, mode, detected)
				common.SetContextKey(c, constant.ContextKeyResponsesCompactMode, detected)
				defer common.CleanupBodyStorage(c)
				common.SetContextKey(c, constant.ContextKeyUserId, 77)
				common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
				common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
				common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
				common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
				common.SetContextKey(c, constant.ContextKeyTokenId, 78)
				common.SetContextKey(c, constant.ContextKeyTokenKey, "compact-token")
				common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only"})
				require.Nil(t, middleware.SetupContextForSelectedChannel(c, source, "gpt-6-astra"))
				Relay(c, types.RelayFormatOpenAIResponses)

				var user model.User
				require.NoError(t, db.First(&user, 77).Error)
				var logs []model.Log
				require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
				if state == "正常" {
					require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					require.Len(t, calls, 1)
					assert.Equal(t, "Bearer compact-source", <-calls)
					assert.Equal(t, response, recorder.Body.String())
					require.Len(t, logs, 1)
					assert.Equal(t, "gpt-6-astra", logs[0].ModelName)
					assert.Equal(t, id, logs[0].ChannelId)
					assert.Positive(t, logs[0].Quota)
					assert.Equal(t, 1000000-logs[0].Quota, user.Quota)
				} else {
					expectedStatus, expectedCode := http.StatusTooManyRequests, "channel_period_quota_exceeded"
					if state == "能力关闭" {
						expectedStatus, expectedCode = http.StatusServiceUnavailable, "responses_compact_passthrough_disabled"
					}
					assert.Equal(t, expectedStatus, recorder.Code, recorder.Body.String())
					assert.Contains(t, recorder.Body.String(), expectedCode)
					assert.Empty(t, calls)
					assert.Empty(t, logs)
					assert.Equal(t, 1000000, user.Quota)
				}
			})
		}
	}
}

// TestChannelLimitFallbackUsesSameChannelCompactTarget 验证三种 HTTP Compact 模式可在同渠道切换目标模型，并在目标预算耗尽时无副作用拒绝。
// @param t 测试上下文。
func TestChannelLimitFallbackUsesSameChannelCompactTarget(t *testing.T) {
	previousRatios, err := common.Marshal(ratio_setting.GetModelRatioCopy())
	require.NoError(t, err)
	previousPrices, err := common.Marshal(ratio_setting.GetModelPriceCopy())
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-6-astra":5,"gpt-5.6-sol":2}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(previousRatios)))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(previousPrices)))
	})
	for modeIndex, mode := range []relayconstant.ResponsesCompactMode{
		relayconstant.ResponsesCompactModeV1Path,
		relayconstant.ResponsesCompactModeV1BodyBridge,
		relayconstant.ResponsesCompactModeV2HTTP,
	} {
		for stateIndex, targetExhausted := range []bool{false, true} {
			state := "降级成功"
			if targetExhausted {
				state = "目标额度耗尽"
			}
			t.Run(string(mode)+"/"+state, func(t *testing.T) {
				db := setupChannelUserLimitsTestDB(t)
				defer func() {
					require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 2*time.Second, time.Millisecond)
				}()
				require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.Token{}))
				oldCache, oldRetry, oldLog, oldBatch := common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled
				common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled = false, 0, true, false
				t.Cleanup(func() {
					common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled = oldCache, oldRetry, oldLog, oldBatch
				})
				service.InitHttpClient()
				path := "/v1/responses"
				body := `{"model":"gpt-6-astra","stream":true,"input":[{"type":"compaction_trigger"},{"type":"reasoning","encrypted_content":"opaque-codex","summary":[]}],"future":{"model":"nested-original","count":0,"enabled":false}}`
				response := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"compact_test\",\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}}\n\n"
				if mode == relayconstant.ResponsesCompactModeV1Path {
					path = "/v1/responses/compact"
					body = `{"model":"gpt-6-astra","input":"测试压缩","future":{"model":"nested-original","count":0,"enabled":false}}`
					response = `{"id":"compact_test","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}`
				}
				expectedBody := strings.Replace(body, `"model":"gpt-6-astra"`, `"model":"gpt-5.6-sol"`, 1)
				type upstreamCall struct {
					authorization string
					body          string
				}
				calls := make(chan upstreamCall, 1)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requestBody, readErr := io.ReadAll(r.Body)
					assert.NoError(t, readErr)
					assert.Equal(t, path, r.URL.Path)
					calls <- upstreamCall{authorization: r.Header.Get("Authorization"), body: string(requestBody)}
					contentType := "text/event-stream"
					if mode == relayconstant.ResponsesCompactModeV1Path {
						contentType = "application/json"
					}
					w.Header().Set("Content-Type", contentType)
					_, _ = io.WriteString(w, response)
				}))
				defer upstream.Close()
				id := 66000 + modeIndex*100 + stateIndex*10
				settings := `{"responses_compact_passthrough_enabled":true}`
				source := &model.Channel{Id: id, Name: "压缩同渠道", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "compact-source", BaseURL: &upstream.URL, Group: "default", Models: "gpt-6-astra,gpt-5.6-sol", Setting: &settings}
				require.NoError(t, db.Create(source).Error)
				for _, modelName := range []string{"gpt-6-astra", "gpt-5.6-sol"} {
					require.NoError(t, db.Create(&model.Ability{Group: "default", Model: modelName, ChannelId: source.Id, Enabled: true}).Error)
				}
				require.NoError(t, db.Create(&model.User{Id: 77, Username: "compact-user", Quota: 1000000}).Error)
				require.NoError(t, db.Create(&model.Token{Id: 78, UserId: 77, Key: "compact-token", RemainQuota: 1000000}).Error)
				targetLimit := int64(1000000)
				if targetExhausted {
					targetLimit = 1
				}
				config := appdto.ChannelPeriodPolicyConfig{
					SchemaVersion:   2,
					DefaultOnExceed: appdto.ChannelBudgetAction{Mode: "reject"},
					Schedules:       []appdto.ChannelBudgetSchedule{},
					Budgets: []appdto.ChannelBudgetRow{
						{Name: "Astra 日预算", Enabled: true, Scope: "pool", Window: "daily", Models: []string{"gpt-6-astra"}, Limit: 1, OnExceed: appdto.ChannelBudgetAction{Mode: "fallback", ChannelID: 0, Model: "gpt-5.6-sol"}},
						{Name: "Sol 日预算", Enabled: true, Scope: "pool", Window: "daily", Models: []string{"gpt-5.6-sol"}, Limit: targetLimit, OnExceed: appdto.ChannelBudgetAction{Mode: "reject"}},
					},
				}
				view, err := service.SaveChannelPeriodPolicy(t.Context(), source.Id, appdto.ChannelPeriodPolicyInput{Config: config}, 1)
				require.NoError(t, err)
				require.NoError(t, service.RecordChannelUserModelQuotaUsage(t.Context(), source.Id, 77, 1, "gpt-6-astra"))
				if targetExhausted {
					require.NoError(t, service.RecordChannelUserModelQuotaUsage(t.Context(), source.Id, 77, 1, "gpt-5.6-sol"))
				}

				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				if mode == relayconstant.ResponsesCompactModeV2HTTP {
					c.Request.Header.Set("x-codex-beta-features", "remote_compaction_v2")
				}
				detected := helper.DetectResponsesCompactMode(http.MethodPost, path, c.Request.Header, []byte(body), helper.ResponsesTransportHTTP)
				require.Equal(t, mode, detected)
				common.SetContextKey(c, constant.ContextKeyResponsesCompactMode, detected)
				defer common.CleanupBodyStorage(c)
				common.SetContextKey(c, constant.ContextKeyUserId, 77)
				common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
				common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
				common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
				common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
				common.SetContextKey(c, constant.ContextKeyTokenId, 78)
				common.SetContextKey(c, constant.ContextKeyTokenKey, "compact-token")
				common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only"})
				require.Nil(t, middleware.SetupContextForSelectedChannel(c, source, "gpt-6-astra"))
				Relay(c, types.RelayFormatOpenAIResponses)

				var user model.User
				require.NoError(t, db.First(&user, 77).Error)
				var token model.Token
				require.NoError(t, db.First(&token, 78).Error)
				var logs []model.Log
				require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
				astraUsage, err := service.GetChannelBudgetUsage(t.Context(), source.Id, view.Config.Budgets[0].ID, "pool", 0, 20)
				require.NoError(t, err)
				solUsage, err := service.GetChannelBudgetUsage(t.Context(), source.Id, view.Config.Budgets[1].ID, "pool", 0, 20)
				require.NoError(t, err)
				if targetExhausted {
					assert.Equal(t, http.StatusTooManyRequests, recorder.Code, recorder.Body.String())
					assert.Contains(t, recorder.Body.String(), string(types.ErrorCodeChannelPeriodQuotaExceeded))
					assert.Empty(t, calls)
					assert.Empty(t, logs)
					assert.Equal(t, 1000000, user.Quota)
					assert.Equal(t, 1000000, token.RemainQuota)
					assert.Equal(t, int64(1), astraUsage.UsedQuota)
					assert.Equal(t, int64(1), solUsage.UsedQuota)
					return
				}

				require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
				require.Len(t, calls, 1)
				call := <-calls
				assert.Equal(t, "Bearer compact-source", call.authorization)
				assert.Equal(t, expectedBody, call.body)
				assert.Equal(t, response, recorder.Body.String())
				require.Len(t, logs, 1)
				assert.Equal(t, "gpt-5.6-sol", logs[0].ModelName)
				assert.Equal(t, source.Id, logs[0].ChannelId)
				assert.Positive(t, logs[0].Quota)
				assert.Equal(t, 1000000-logs[0].Quota, user.Quota)
				assert.Equal(t, 1000000-logs[0].Quota, token.RemainQuota)
				assert.Equal(t, int64(1), astraUsage.UsedQuota)
				assert.Equal(t, int64(logs[0].Quota), solUsage.UsedQuota)
				var other struct {
					AdminInfo struct {
						Fallback *relaycommon.ChannelLimitFallbackInfo `json:"channel_limit_fallback"`
					} `json:"admin_info"`
				}
				require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
				require.NotNil(t, other.AdminInfo.Fallback)
				assert.Equal(t, "gpt-6-astra", other.AdminInfo.Fallback.OriginalModel)
				assert.Equal(t, "gpt-5.6-sol", other.AdminInfo.Fallback.TargetModel)
				assert.Equal(t, source.Id, other.AdminInfo.Fallback.SourceChannelID)
				assert.Equal(t, source.Id, other.AdminInfo.Fallback.TargetChannelID)
			})
		}
	}
}
