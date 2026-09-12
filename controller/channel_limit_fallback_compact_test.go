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
