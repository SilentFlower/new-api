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
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// channelBudgetRelayFixture 提供真实 Relay、钱包、令牌、消费日志与模拟上游的隔离夹具。
type channelBudgetRelayFixture struct {
	db        *gorm.DB
	channel   *model.Channel
	calls     atomic.Int32
	lastModel atomic.Value
}

// newChannelBudgetRelayFixture 创建按模型预算的完整请求链路。
// @param t 测试上下文。
// @param channelID 测试渠道的唯一 ID，避免进程内计数跨用例共享。
// @return 可调用真实 Relay 的夹具。
func newChannelBudgetRelayFixture(t *testing.T, channelID int) *channelBudgetRelayFixture {
	t.Helper()
	f := &channelBudgetRelayFixture{db: setupChannelUserLimitsTestDB(t)}
	require.NoError(t, f.db.AutoMigrate(&model.Ability{}, &model.Token{}))
	oldCache, oldRetry, oldLog, oldBatch := common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled
	common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled = false, 3, true, false
	t.Cleanup(func() {
		common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled = oldCache, oldRetry, oldLog, oldBatch
	})
	for _, pricing := range []struct {
		previous any
		value    string
		update   func(string) error
	}{
		{ratio_setting.GetModelRatioCopy(), `{"gpt-6-astra":5,"gpt-6-mini":2}`, ratio_setting.UpdateModelRatioByJSONString},
		{ratio_setting.GetCompletionRatioCopy(), `{"gpt-6-astra":1,"gpt-6-mini":1}`, ratio_setting.UpdateCompletionRatioByJSONString},
		{ratio_setting.GetModelPriceCopy(), `{}`, ratio_setting.UpdateModelPriceByJSONString},
	} {
		previous, err := common.Marshal(pricing.previous)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, pricing.update(string(previous))) })
		require.NoError(t, pricing.update(pricing.value))
	}
	service.InitHttpClient()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model string `json:"model"`
		}
		if err := common.DecodeJson(r.Body, &request); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.lastModel.Store(request.Model)
		f.calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"chatcmpl_budget","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`, request.Model)
	}))
	t.Cleanup(upstream.Close)
	concurrency := 1
	f.channel = &model.Channel{Id: channelID, Name: "模型预算渠道", Type: constant.ChannelTypeNewAPI, Status: common.ChannelStatusEnabled, Key: "budget-test-key", BaseURL: &upstream.URL, Group: "default", Models: "gpt-6-astra,gpt-6-mini", UserConcurrencyLimit: &concurrency}
	require.NoError(t, f.db.Create(f.channel).Error)
	for _, name := range []string{"gpt-6-astra", "gpt-6-mini"} {
		require.NoError(t, f.db.Create(&model.Ability{Group: "default", Model: name, ChannelId: channelID, Enabled: true}).Error)
	}
	for _, userID := range []int{77, 78} {
		require.NoError(t, f.db.Create(&model.User{Id: userID, Username: fmt.Sprintf("budget-user-%d", userID), AffCode: fmt.Sprintf("b%d", userID), Quota: 1000000, Group: "default"}).Error)
		require.NoError(t, f.db.Create(&model.Token{Id: userID, UserId: userID, Key: fmt.Sprintf("budget-token-%d", userID), RemainQuota: 1000000}).Error)
	}
	// 退款与指标任务必须在恢复全局状态和关闭数据库前结束。
	t.Cleanup(func() {
		require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 2*time.Second, time.Millisecond)
	})
	return f
}

// request 经完整 Relay 执行请求，并验证最终并发租约已释放。
// @param t 测试上下文。
// @param userID 请求用户。
// @param modelName 客户端请求模型。
// @return 实际 HTTP 响应。
func (f *channelBudgetRelayFixture) request(t *testing.T, userID int, modelName string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"你好"}],"max_tokens":10}`, modelName)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	defer common.CleanupBodyStorage(c)
	common.SetContextKey(c, constant.ContextKeyUserId, userID)
	common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
	common.SetContextKey(c, constant.ContextKeyTokenId, userID)
	common.SetContextKey(c, constant.ContextKeyTokenKey, fmt.Sprintf("budget-token-%d", userID))
	common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only"})
	require.Nil(t, middleware.SetupContextForSelectedChannel(c, f.channel, modelName))
	Relay(c, types.RelayFormatOpenAI)
	inFlight, _, err := service.GetChannelUserConcurrencyUsage(c, f.channel.Id, userID)
	require.NoError(t, err)
	assert.Zero(t, inFlight)
	return response
}

// TestChannelLimitFallbackModelBudgetsFullRelay 验证模型日/周预算与整体预算并行约束、个人隔离和拒绝前不扣款。
// @param t 测试上下文。
// @return 无。
func TestChannelLimitFallbackModelBudgetsFullRelay(t *testing.T) {
	for i, scope := range []string{"pool", "user"} {
		for j, window := range []string{"daily", "weekly"} {
			t.Run(scope+"/"+window, func(t *testing.T) {
				f := newChannelBudgetRelayFixture(t, 71000+i*10+j)
				config := appdto.ChannelPeriodPolicyConfig{SchemaVersion: 2, DefaultOnExceed: appdto.ChannelBudgetAction{Mode: "reject"}, Schedules: []appdto.ChannelBudgetSchedule{}, Budgets: []appdto.ChannelBudgetRow{
					{Name: "整体", Enabled: true, Scope: scope, Window: window, Models: []string{}, Limit: 100, OnExceed: appdto.ChannelBudgetAction{Mode: "inherit"}},
					{Name: "Astra", Enabled: true, Scope: scope, Window: window, Models: []string{"gpt-6-astra"}, Limit: 20, OnExceed: appdto.ChannelBudgetAction{Mode: "inherit"}},
				}}
				view, err := service.SaveChannelPeriodPolicy(t.Context(), f.channel.Id, appdto.ChannelPeriodPolicyInput{Config: config}, 1)
				require.NoError(t, err)
				require.NoError(t, service.RecordChannelUserModelQuotaUsage(t.Context(), f.channel.Id, 77, 20, "gpt-6-astra"))
				response := f.request(t, 77, "gpt-6-astra")
				assert.Equal(t, http.StatusTooManyRequests, response.Code, response.Body.String())
				assert.Contains(t, response.Body.String(), string(types.ErrorCodeChannelPeriodQuotaExceeded))
				assert.Zero(t, f.calls.Load())
				if scope == "user" {
					response = f.request(t, 78, "gpt-6-astra")
					require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				}
				response = f.request(t, 77, "gpt-6-mini")
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				assert.Equal(t, "gpt-6-mini", f.lastModel.Load())
				// 将整体计数设到上限后，所有模型都必须拒绝，不能因模型行尚有余额而放行。
				require.NoError(t, service.SetChannelBudgetUsage(t.Context(), f.channel.Id, view.Config.Budgets[0].ID, appdto.ChannelBudgetUsageInput{Scope: scope, UserID: 77, UsedQuota: 100}))
				calls := f.calls.Load()
				for _, name := range []string{"gpt-6-astra", "gpt-6-mini"} {
					response = f.request(t, 77, name)
					assert.Equal(t, http.StatusTooManyRequests, response.Code, response.Body.String())
				}
				assert.Equal(t, calls, f.calls.Load())
				var user model.User
				require.NoError(t, f.db.First(&user, 77).Error)
				assert.Equal(t, 1000000-24, user.Quota)
				var logs []model.Log
				require.NoError(t, f.db.Where("type = ? AND user_id = ?", model.LogTypeConsume, 77).Find(&logs).Error)
				require.Len(t, logs, 1)
				assert.Equal(t, 24, logs[0].Quota)
			})
		}
	}
}

// TestChannelLimitFallbackSameChannelModelBilling 验证同渠道换模型的实际出站、唯一扣费与触发行审计。
// @param t 测试上下文。
// @return 无。
func TestChannelLimitFallbackSameChannelModelBilling(t *testing.T) {
	f := newChannelBudgetRelayFixture(t, 71100)
	config := budgetPoolDailyConfig(600, 0, "")
	config.Budgets = append(config.Budgets,
		appdto.ChannelBudgetRow{Name: "Astra 日预算", Enabled: true, Scope: "pool", Window: "daily", Models: []string{"gpt-6-astra"}, Limit: 500, OnExceed: appdto.ChannelBudgetAction{Mode: "fallback", ChannelID: f.channel.Id, Model: "gpt-6-mini"}},
		appdto.ChannelBudgetRow{Name: "Mini 日预算", Enabled: true, Scope: "pool", Window: "daily", Models: []string{"gpt-6-mini"}, Limit: 500, OnExceed: appdto.ChannelBudgetAction{Mode: "reject"}},
	)
	view, err := service.SaveChannelPeriodPolicy(t.Context(), f.channel.Id, appdto.ChannelPeriodPolicyInput{Config: config}, 1)
	require.NoError(t, err)
	require.NoError(t, service.RecordChannelUserModelQuotaUsage(t.Context(), f.channel.Id, 77, 500, "gpt-6-astra"))
	response := f.request(t, 77, "gpt-6-astra")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, int32(1), f.calls.Load())
	assert.Equal(t, "gpt-6-mini", f.lastModel.Load())
	var logs []model.Log
	require.NoError(t, f.db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, f.channel.Id, logs[0].ChannelId)
	assert.Equal(t, "gpt-6-mini", logs[0].ModelName)
	assert.Equal(t, 24, logs[0].Quota)
	var other struct {
		AdminInfo struct {
			Fallback *relaycommon.ChannelLimitFallbackInfo `json:"channel_limit_fallback"`
		} `json:"admin_info"`
	}
	require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
	require.NotNil(t, other.AdminInfo.Fallback)
	assert.Equal(t, &relaycommon.ChannelLimitFallbackInfo{SourceChannelID: f.channel.Id, TargetChannelID: f.channel.Id, OriginalModel: "gpt-6-astra", TargetModel: "gpt-6-mini", Scope: "pool", Period: "daily", BudgetID: view.Config.Budgets[1].ID, Models: []string{"gpt-6-astra"}}, other.AdminInfo.Fallback)
	var user model.User
	require.NoError(t, f.db.First(&user, 77).Error)
	assert.Equal(t, 1000000-24, user.Quota)
	var token model.Token
	require.NoError(t, f.db.First(&token, 77).Error)
	assert.Equal(t, 1000000-24, token.RemainQuota)
	astraUsage, err := service.GetChannelBudgetUsage(t.Context(), f.channel.Id, view.Config.Budgets[1].ID, "pool", 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(500), astraUsage.UsedQuota)
	miniUsage, err := service.GetChannelBudgetUsage(t.Context(), f.channel.Id, view.Config.Budgets[2].ID, "pool", 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(logs[0].Quota), miniUsage.UsedQuota)
	require.NoError(t, service.SetChannelBudgetUsage(t.Context(), f.channel.Id, view.Config.Budgets[0].ID, appdto.ChannelBudgetUsageInput{Scope: "pool", UsedQuota: 600}))
	response = f.request(t, 77, "gpt-6-astra")
	assert.Equal(t, http.StatusTooManyRequests, response.Code, response.Body.String())
	assert.Equal(t, int32(1), f.calls.Load())
}

// TestChannelLimitFallbackTargetUserBudgets 验证同渠道和跨渠道目标侧个人日/周预算仍拦截，且不会二次降级或扣款。
// @param t 测试上下文。
// @return 无。
func TestChannelLimitFallbackTargetUserBudgets(t *testing.T) {
	for i, sameChannel := range []bool{true, false} {
		for j, window := range []string{"daily", "weekly"} {
			t.Run(fmt.Sprintf("same=%t/%s", sameChannel, window), func(t *testing.T) {
				f := newChannelBudgetRelayFixture(t, 71200+i*10+j*2)
				target := f.channel
				personal := appdto.ChannelBudgetRow{Name: "目标个人额度", Enabled: true, Scope: "user", Window: window, Models: []string{"gpt-6-mini"}, Limit: 20, OnExceed: appdto.ChannelBudgetAction{Mode: "fallback", Model: "gpt-6-astra"}}
				if !sameChannel {
					copy := *f.channel
					copy.Id++
					target = &copy
					require.NoError(t, f.db.Create(target).Error)
					require.NoError(t, f.db.Create(&model.Ability{Group: "default", Model: "gpt-6-mini", ChannelId: target.Id, Enabled: true}).Error)
					personal.Models = []string{}
					personal.OnExceed.ChannelID = f.channel.Id
				}
				config := appdto.ChannelPeriodPolicyConfig{SchemaVersion: 2, DefaultOnExceed: appdto.ChannelBudgetAction{Mode: "reject"}, Schedules: []appdto.ChannelBudgetSchedule{}, Budgets: []appdto.ChannelBudgetRow{
					{Name: "来源 Astra", Enabled: true, Scope: "pool", Window: "daily", Models: []string{"gpt-6-astra"}, Limit: 500, OnExceed: appdto.ChannelBudgetAction{Mode: "fallback", ChannelID: target.Id, Model: "gpt-6-mini"}},
				}}
				if sameChannel {
					config.Budgets = append(config.Budgets, personal)
				} else {
					targetConfig := config
					targetConfig.Budgets = []appdto.ChannelBudgetRow{personal}
					_, err := service.SaveChannelPeriodPolicy(t.Context(), target.Id, appdto.ChannelPeriodPolicyInput{Config: targetConfig}, 1)
					require.NoError(t, err)
				}
				_, err := service.SaveChannelPeriodPolicy(t.Context(), f.channel.Id, appdto.ChannelPeriodPolicyInput{Config: config}, 1)
				require.NoError(t, err)
				require.NoError(t, service.RecordChannelUserModelQuotaUsage(t.Context(), target.Id, 77, 20, "gpt-6-mini"))
				require.NoError(t, service.RecordChannelUserModelQuotaUsage(t.Context(), f.channel.Id, 77, 500, "gpt-6-astra"))
				response := f.request(t, 77, "gpt-6-astra")
				assert.Equal(t, http.StatusTooManyRequests, response.Code, response.Body.String())
				code := types.ErrorCodeChannelPeriodQuotaExceeded
				if !sameChannel && window == "daily" {
					code = types.ErrorCodeChannelUserDailyQuotaExceeded
				}
				if !sameChannel && window == "weekly" {
					code = types.ErrorCodeChannelUserWeeklyQuotaExceeded
				}
				assert.Contains(t, response.Body.String(), string(code))
				assert.Zero(t, f.calls.Load())
				var user model.User
				require.NoError(t, f.db.First(&user, 77).Error)
				assert.Equal(t, 1000000, user.Quota)
				var count int64
				require.NoError(t, f.db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&count).Error)
				assert.Zero(t, count)
			})
		}
	}
}
