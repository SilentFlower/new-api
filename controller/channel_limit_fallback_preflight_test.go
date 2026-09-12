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
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChannelLimitFallbackPreflightCompatibility 用隔离数据库和上游验证普通请求参数及目标路由的兼容性。
// @param t 测试上下文。
func TestChannelLimitFallbackPreflightCompatibility(t *testing.T) {
	for index, scenario := range []struct {
		name          string
		responses     bool
		budget        string
		withPolicy    bool
		fallback      bool
		directTarget  bool
		toolParameter string
	}{
		{name: "Responses别名参数", responses: true, budget: "1024"},
		{name: "Responses显式零预算", responses: true, budget: "0"},
		{name: "Responses未传预算", responses: true},
		{name: "Responses启用降级但未超限", responses: true, budget: "1024", withPolicy: true},
		{name: "直接请求自定义目标", directTarget: true},
		{name: "降级到自定义目标", fallback: true},
		{name: "工具参数名为prompt", fallback: true, toolParameter: "prompt"},
		{name: "工具参数名为file_id", fallback: true, toolParameter: "file_id"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db := setupChannelUserLimitsTestDB(t)
			defer func() {
				require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 2*time.Second, time.Millisecond)
			}()
			require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.Token{}))
			oldCache, oldRetry, oldLog, oldBatch := common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled
			common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled = false, 0, true, false
			oldRatios, err := common.Marshal(ratio_setting.GetModelRatioCopy())
			require.NoError(t, err)
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"alias-model":1,"qwen3-32b":1,"gpt-4o-mini":1,"gpt-4o":1}`))
			t.Cleanup(func() {
				common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled = oldCache, oldRetry, oldLog, oldBatch
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(oldRatios)))
			})
			service.InitHttpClient()
			path, format := "/v1/chat/completions", types.RelayFormatOpenAI
			requestModel := "gpt-4o-mini"
			if scenario.responses {
				path, format, requestModel = "/v1/responses", types.RelayFormatOpenAIResponses, "alias-model"
			}
			if scenario.directTarget {
				requestModel = "gpt-4o"
			}
			expectedPath, expectedKey := path, "audit-source"
			if scenario.directTarget || scenario.fallback {
				expectedPath, expectedKey = "/v1/target/chat", "audit-target"
			}
			if scenario.toolParameter != "" {
				expectedPath = path
			}
			calls := make(chan string, 4)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				calls <- string(body)
				assert.Equal(t, expectedPath, r.URL.Path)
				assert.Equal(t, "Bearer "+expectedKey, r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				_, response := channelLimitFallbackUpstreamResponse(path, false)
				_, _ = io.WriteString(w, response)
			}))
			defer upstream.Close()
			id := 82000 + index*10
			source := &model.Channel{Id: id, Name: "审查来源", Type: constant.ChannelTypeNewAPI, Status: common.ChannelStatusEnabled, Key: "audit-source", BaseURL: &upstream.URL, Group: "default", Models: requestModel}
			target := &model.Channel{Id: id + 1, Name: "审查目标", Type: constant.ChannelTypeAdvancedCustom, Status: common.ChannelStatusEnabled, Key: "audit-target", BaseURL: &upstream.URL, Group: "default", Models: "gpt-4o"}
			settings, err := common.Marshal(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{IncomingPath: "/v1/chat/completions", UpstreamPath: "/v1/target/chat", Converter: "none", Models: []string{"gpt-4o"}}}}})
			require.NoError(t, err)
			target.OtherSettings = string(settings)
			if scenario.toolParameter != "" {
				target.Type = constant.ChannelTypeNewAPI
				target.OtherSettings = ""
			}
			if scenario.responses {
				mapping := `{"alias-model":"qwen3-32b"}`
				source.ModelMapping = &mapping
			}
			require.NoError(t, db.Create(source).Error)
			require.NoError(t, db.Create(target).Error)
			require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gpt-4o", ChannelId: target.Id, Enabled: true}).Error)
			require.NoError(t, db.Create(&model.User{Id: 77, Username: "audit-user", Quota: 1000000}).Error)
			require.NoError(t, db.Create(&model.Token{Id: 78, UserId: 77, Key: "audit-token", RemainQuota: 1000000}).Error)
			selected := source
			if scenario.directTarget {
				selected = target
			}
			if scenario.fallback || scenario.withPolicy {
				_, err := service.SaveChannelPeriodPolicy(t.Context(), source.Id, appdto.ChannelPeriodPolicyInput{Config: budgetPoolDailyConfig(1, target.Id, "gpt-4o")}, 1)
				require.NoError(t, err)
				if scenario.fallback {
					require.NoError(t, service.RecordChannelUserQuotaUsage(t.Context(), source.Id, 77, 1))
				}
			}
			body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"审查"}],"max_tokens":10}`, requestModel)
			if scenario.responses {
				body = `{"model":"alias-model","input":"审查","max_output_tokens":10}`
				if scenario.budget != "" {
					body = strings.TrimSuffix(body, "}") + `,"thinking_budget":` + scenario.budget + `}`
				}
			}
			if scenario.toolParameter != "" {
				body = strings.TrimSuffix(body, "}") + fmt.Sprintf(`,"tools":[{"type":"function","function":{"name":"render","parameters":{"type":"object","properties":{%q:{"type":"string"}}}}}]}`, scenario.toolParameter)
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			defer common.CleanupBodyStorage(c)
			common.SetContextKey(c, constant.ContextKeyUserId, 77)
			common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
			common.SetContextKey(c, constant.ContextKeyTokenId, 78)
			common.SetContextKey(c, constant.ContextKeyTokenKey, "audit-token")
			common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only"})
			require.Nil(t, middleware.SetupContextForSelectedChannel(c, selected, requestModel))
			Relay(c, format)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Len(t, calls, 1)
			var sent map[string]any
			require.NoError(t, common.Unmarshal([]byte(<-calls), &sent))
			if scenario.responses {
				assert.Equal(t, "qwen3-32b", sent["model"])
				if scenario.budget == "" {
					assert.NotContains(t, sent, "thinking_budget")
				} else {
					var expected any
					require.NoError(t, common.Unmarshal([]byte(scenario.budget), &expected))
					assert.Equal(t, expected, sent["thinking_budget"], "显式参数必须到达映射后的模型")
				}
			} else {
				assert.Equal(t, "gpt-4o", sent["model"])
			}
			if scenario.toolParameter != "" {
				var original map[string]any
				require.NoError(t, common.Unmarshal([]byte(body), &original))
				assert.Equal(t, original["tools"], sent["tools"])
			}
			if scenario.fallback || scenario.directTarget {
				var logs []model.Log
				require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
				require.Len(t, logs, 1)
				assert.Equal(t, target.Id, logs[0].ChannelId)
				assert.Equal(t, "gpt-4o", logs[0].ModelName)
				assert.Positive(t, logs[0].Quota)
				var user model.User
				require.NoError(t, db.First(&user, 77).Error)
				assert.Equal(t, 1000000-logs[0].Quota, user.Quota)
				if scenario.fallback {
					assert.Contains(t, logs[0].Other, `"original_model":"gpt-4o-mini"`)
				}
			}
		})
	}
}
