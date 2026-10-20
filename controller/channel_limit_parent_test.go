package controller

import (
	"net/http"
	"net/http/httptest"
	"os"
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
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type channelLimitParentRelayFixture struct {
	db     *gorm.DB
	parent *model.Channel
	child  *model.Channel
	calls  atomic.Int32
	key    atomic.Value
}

// setupChannelLimitParentRelayFixture 初始化真实管理、资金与模拟上游，测试结束前等待后台日志任务收口。
func setupChannelLimitParentRelayFixture(t *testing.T) *channelLimitParentRelayFixture {
	t.Helper()
	f := &channelLimitParentRelayFixture{db: setupChannelUserLimitsTestDB(t)}
	require.NoError(t, f.db.AutoMigrate(&model.Ability{}, &model.Token{}))
	oldCache, oldRetry, oldLog, oldBatch := common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled
	common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled = false, 3, true, false
	previous, err := common.Marshal(ratio_setting.GetModelRatioCopy())
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-4o-mini":2}`))
	t.Cleanup(func() {
		require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 2*time.Second, time.Millisecond)
		common.MemoryCacheEnabled, common.RetryTimes, common.LogConsumeEnabled, common.BatchUpdateEnabled = oldCache, oldRetry, oldLog, oldBatch
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(previous)))
	})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		f.key.Store(r.Header.Get("Authorization"))
		contentType, body := channelLimitFallbackUpstreamResponse("/v1/chat/completions", false)
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(upstream.Close)
	service.InitHttpClient()
	f.parent = &model.Channel{Id: 79001, Type: constant.ChannelTypeNewAPI, Name: "父渠道", Key: "parent-key", BaseURL: &upstream.URL, Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-4o-mini", UserConcurrencyLimit: common.GetPointer(4)}
	f.child = &model.Channel{Id: 79002, Type: constant.ChannelTypeNewAPI, Name: "子渠道", Key: "child-key", BaseURL: &upstream.URL, Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-4o-mini", UserConcurrencyLimit: common.GetPointer(1)}
	for _, channel := range []*model.Channel{f.parent, f.child} {
		require.NoError(t, f.db.Create(channel).Error)
		require.NoError(t, f.db.Create(&model.Ability{Group: "default", Model: "gpt-4o-mini", ChannelId: channel.Id, Enabled: true}).Error)
	}
	require.NoError(t, f.db.Create(&model.User{Id: 77, Username: "parent-user", Quota: 1000000}).Error)
	require.NoError(t, f.db.Create(&model.Token{Id: 78, UserId: 77, Key: "parent-token", RemainQuota: 1000000}).Error)
	require.NoError(t, service.SaveChannelLimitParent(t.Context(), &model.ChannelLimitParent{ChannelId: f.child.Id, ParentChannelId: f.parent.Id, UpdatedBy: 1}))
	return f
}

// request 构造经过实际选渠上下文初始化的请求，不替换 Relay 或记账实现。
func (f *channelLimitParentRelayFixture) request(t *testing.T, channel *model.Channel) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"你好"}],"max_tokens":10}`))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	common.SetContextKey(c, constant.ContextKeyUserId, 77)
	common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(c, constant.ContextKeyTokenGroup, "default")
	common.SetContextKey(c, constant.ContextKeyTokenId, 78)
	common.SetContextKey(c, constant.ContextKeyTokenKey, "parent-token")
	common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only"})
	require.Nil(t, middleware.SetupContextForSelectedChannel(c, channel, "gpt-4o-mini"))
	return c, recorder
}

func TestChannelLimitParentFullRelayRejectsFifthRequestWithoutRetryOrDebit(t *testing.T) {
	f := setupChannelLimitParentRelayFixture(t)
	for _, channel := range []*model.Channel{f.parent, f.parent, f.child, f.child} {
		c, _ := f.request(t, channel)
		guard, apiErr := acquireChannelUserConcurrencyGuard(c)
		require.Nil(t, apiErr)
		t.Cleanup(func() { require.Nil(t, finishChannelUserConcurrencyGuard(c, guard, nil)) })
	}
	c, recorder := f.request(t, f.child)
	Relay(c, types.RelayFormatOpenAI)
	assert.Equal(t, http.StatusTooManyRequests, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), string(types.ErrorCodeChannelUserConcurrencyExceeded))
	assert.Zero(t, f.calls.Load())
	var user model.User
	require.NoError(t, f.db.First(&user, 77).Error)
	assert.Equal(t, 1000000, user.Quota)
	var count int64
	require.NoError(t, f.db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&count).Error)
	assert.Zero(t, count)
	for _, channel := range []*model.Channel{f.parent, f.child} {
		stored, err := model.GetChannelById(channel.Id, false)
		require.NoError(t, err)
		assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	}
}

func TestChannelLimitParentFullRelayKeepsPhysicalChannelAndSharesUsage(t *testing.T) {
	f := setupChannelLimitParentRelayFixture(t)
	c, recorder := f.request(t, f.child)
	Relay(c, types.RelayFormatOpenAI)
	assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.EqualValues(t, 1, f.calls.Load())
	assert.Equal(t, "Bearer child-key", f.key.Load())
	var logs []model.Log
	require.NoError(t, f.db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, f.child.Id, logs[0].ChannelId)
	assert.Positive(t, logs[0].Quota)
	used, _, _, err := service.GetChannelUserDailyQuotaUsage(t.Context(), f.parent.Id, 77)
	require.NoError(t, err)
	assert.EqualValues(t, logs[0].Quota, used)
	childUsed, _, _, err := service.GetChannelUserDailyQuotaUsage(t.Context(), f.child.Id, 77)
	require.NoError(t, err)
	assert.Zero(t, childUsed)
}

func TestChannelLimitParentFullRelaySharedBudgetCannotFallbackToChild(t *testing.T) {
	f := setupChannelLimitParentRelayFixture(t)
	_, err := service.SaveChannelPeriodPolicy(t.Context(), f.parent.Id, appdto.ChannelPeriodPolicyInput{Config: budgetPoolDailyConfig(1, f.child.Id, "gpt-4o-mini")}, 1)
	require.NoError(t, err)
	require.NoError(t, service.RecordChannelUserQuotaUsage(t.Context(), f.parent.Id, 77, 1))
	for _, channel := range []*model.Channel{f.parent, f.child} {
		c, recorder := f.request(t, channel)
		Relay(c, types.RelayFormatOpenAI)
		assert.Equal(t, http.StatusTooManyRequests, recorder.Code, recorder.Body.String())
	}
	assert.Zero(t, f.calls.Load())
	var user model.User
	require.NoError(t, f.db.First(&user, 77).Error)
	assert.Equal(t, 1000000, user.Quota)
}

func TestChannelLimitParentManagementKeepsChildIdentityAndRejectsWrites(t *testing.T) {
	f := setupChannelLimitParentRelayFixture(t)
	params := gin.Params{{Key: "id", Value: "79002"}, {Key: "user_id", Value: "77"}, {Key: "budget_id", Value: "test-budget"}}
	c, recorder := newChannelUserLimitTestContext(http.MethodGet, "/api/channel/79002/user-limit-status/77", "", params)
	GetChannelUserLimitStatus(c)
	var response struct {
		Success bool                           `json:"success"`
		Data    channelUserLimitStatusResponse `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success, recorder.Body.String())
	assert.Equal(t, f.child.Id, response.Data.ChannelID)
	assert.Equal(t, 4, response.Data.Concurrency.BaseLimit)
	for _, endpoint := range []struct {
		method  string
		handler gin.HandlerFunc
	}{
		{http.MethodPut, SetChannelUserLimitOverride}, {http.MethodDelete, DeleteChannelUserLimitOverride},
		{http.MethodPut, SetChannelPeriodPolicy}, {http.MethodPut, SetChannelBudgetUsage},
		{http.MethodPut, SetChannelUserBudgetOverride}, {http.MethodDelete, DeleteChannelUserBudgetOverride},
	} {
		c, recorder := newChannelUserLimitTestContext(endpoint.method, "/api/channel/79002/test", `{}`, params)
		endpoint.handler(c)
		assert.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
		assert.Contains(t, recorder.Body.String(), "channel_limit_parent_read_only")
	}
	c, recorder = newChannelUserLimitTestContext(http.MethodPut, "/api/channel/", `{"id":79002,"user_concurrency_limit":2}`, nil)
	UpdateChannel(c)
	assert.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	parent, err := model.GetChannelById(f.parent.Id, false)
	require.NoError(t, err)
	child, err := model.GetChannelById(f.child.Id, false)
	require.NoError(t, err)
	assert.Equal(t, 4, parent.GetUserConcurrencyLimit())
	assert.Equal(t, 1, child.GetUserConcurrencyLimit())
	c, recorder = newChannelUserLimitTestContext(http.MethodPut, "/api/channel/", `{"id":79001,"user_concurrency_limit":5}`, nil)
	UpdateChannel(c)
	var saved struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &saved))
	assert.True(t, saved.Success, recorder.Body.String())
	c, recorder = newChannelUserLimitTestContext(http.MethodGet, "/api/channel/79002/limit-parent", "", params)
	GetChannelLimitParent(c)
	var inherited struct {
		Data appdto.ChannelLimitParentView `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &inherited))
	assert.Equal(t, 5, inherited.Data.ConcurrencyLimit)
}

// TestChannelLimitParentWriteNeverTargetsCachedParent 防止跨实例解绑后旧缓存反向写父配置。
func TestChannelLimitParentWriteNeverTargetsCachedParent(t *testing.T) {
	f := setupChannelLimitParentRelayFixture(t)
	owner, err := service.ResolveChannelLimitParent(t.Context(), f.child)
	require.NoError(t, err)
	require.Equal(t, f.parent.Id, owner.Id)
	relation, err := model.GetChannelLimitParent(t.Context(), f.child.Id)
	require.NoError(t, err)
	relation.ParentChannelId = 0
	// 模拟其他实例保存权威关系，本节点的旧关系缓存在短 TTL 内仍然存在。
	require.NoError(t, model.ReplaceChannelLimitParent(t.Context(), &relation))
	params := gin.Params{{Key: "id", Value: "79002"}, {Key: "user_id", Value: "77"}}
	c, recorder := newChannelUserLimitTestContext(http.MethodPut, "/api/channel/79002/user-limit-overrides/77", `{"user_concurrency_limit":6,"expires_at":0}`, params)
	SetChannelUserLimitOverride(c)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, recorder.Body.String())
	parentLimits, err := service.ResolveChannelUserEffectiveLimits(t.Context(), f.parent, 77)
	require.NoError(t, err)
	childLimits, err := service.ResolveChannelUserEffectiveLimits(t.Context(), f.child, 77)
	require.NoError(t, err)
	assert.Equal(t, 4, parentLimits.EffectiveConcurrency)
	assert.Equal(t, 6, childLimits.EffectiveConcurrency)
}

func TestChannelLimitParentAiFundContract(t *testing.T) {
	f := setupChannelLimitParentRelayFixture(t)
	config := appdto.ChannelPeriodPolicyConfig{SchemaVersion: 2, DefaultOnExceed: appdto.ChannelBudgetAction{Mode: "reject"}, Schedules: []appdto.ChannelBudgetSchedule{}, Budgets: []appdto.ChannelBudgetRow{
		{ID: "new-shared", Name: "共享个人每日", Enabled: true, Scope: "user", Window: "daily", Models: []string{}, Limit: 1000000, OnExceed: appdto.ChannelBudgetAction{Mode: "inherit"}},
	}}
	policy, err := service.SaveChannelPeriodPolicy(t.Context(), f.parent.Id, appdto.ChannelPeriodPolicyInput{Config: config}, 1)
	require.NoError(t, err)
	c, recorder := f.request(t, f.child)
	Relay(c, types.RelayFormatOpenAI)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	fixture := make(map[string]any)
	params := gin.Params{{Key: "id", Value: "79001"}, {Key: "user_id", Value: "77"}}
	params = append(params, gin.Param{Key: "budget_id", Value: policy.Config.Budgets[0].ID})
	for _, item := range []struct {
		name    string
		suffix  string
		handler gin.HandlerFunc
	}{
		{"channel", "", GetChannel},
		{"status", "/user-limit-status/77", GetChannelUserLimitStatus},
		{"policy", "/period-policy", GetChannelPeriodPolicy},
		{"concurrency", "/user-concurrency?p=1&page_size=20", GetChannelUserConcurrency},
		{"usage_summary", "/budgets/usage-summary", GetChannelBudgetUsageSummary},
		{"usage", "/budgets/" + policy.Config.Budgets[0].ID + "/usage?scope=user&p=1&page_size=20", GetChannelBudgetUsage},
		{"overrides", "/user-limit-overrides?p=1&page_size=20", GetChannelUserLimitOverrides},
		{"budget_overrides", "/budget-user-overrides?p=1&page_size=20", GetChannelUserBudgetOverrides},
	} {
		c, recorder := newChannelUserLimitTestContext(http.MethodGet, "/api/channel/79001"+item.suffix, "", params)
		item.handler(c)
		var response struct {
			Success bool `json:"success"`
			Data    any  `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success, recorder.Body.String())
		if item.name == "channel" {
			value, ok := response.Data.(map[string]any)
			require.True(t, ok)
			response.Data = map[string]any{"id": value["id"], "name": value["name"], "status": value["status"], "type": value["type"], "user_concurrency_limit": value["user_concurrency_limit"]}
		}
		fixture[item.name] = response.Data
	}
	policyInput, err := common.Marshal(appdto.ChannelPeriodPolicyInput{ExpectedRevision: policy.Revision, Config: policy.Config})
	require.NoError(t, err)
	for _, action := range []struct {
		name    string
		path    string
		body    string
		handler gin.HandlerFunc
	}{
		{"concurrency_update", "/api/channel/", `{"id":79001,"user_concurrency_limit":5}`, UpdateChannel},
		{"override_update", "/api/channel/79001/user-limit-overrides/77", `{"user_concurrency_limit":7,"expires_at":0}`, SetChannelUserLimitOverride},
		{"budget_override_update", "/api/channel/79001/budgets/" + policy.Config.Budgets[0].ID + "/user-overrides/77", `{"limit":2000000,"expires_at":0}`, SetChannelUserBudgetOverride},
		{"policy_update", "/api/channel/79001/period-policy", string(policyInput), SetChannelPeriodPolicy},
	} {
		c, recorder := newChannelUserLimitTestContext(http.MethodPut, action.path, action.body, params)
		action.handler(c)
		var response struct {
			Success bool `json:"success"`
			Data    any  `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success, recorder.Body.String())
		var requestBody any
		require.NoError(t, common.UnmarshalJsonStr(action.body, &requestBody))
		if action.name == "concurrency_update" {
			response.Data = nil
		}
		fixture[action.name] = map[string]any{"path": action.path, "body": requestBody, "response": response.Data}
	}
	encoded, err := common.Marshal(fixture)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "child-key")
	assert.NotContains(t, string(encoded), "parent-key")
	if output := os.Getenv("CHANNEL_LIMIT_PARENT_CONTRACT_OUTPUT"); output != "" {
		require.NoError(t, os.WriteFile(output, append(encoded, '\n'), 0600))
	}
}

func TestChannelLimitParentManagementLinksAndUnlinksWithRevision(t *testing.T) {
	db := setupChannelUserLimitsTestDB(t)
	parent := &model.Channel{Id: 79201, Name: "父", Key: "parent-secret", UserConcurrencyLimit: common.GetPointer(4)}
	child := &model.Channel{Id: 79202, Name: "子", Key: "child-secret", UserConcurrencyLimit: common.GetPointer(1)}
	require.NoError(t, db.Create(parent).Error)
	require.NoError(t, db.Create(child).Error)
	params := gin.Params{{Key: "id", Value: "79202"}}
	for _, step := range []struct {
		body     string
		parent   int
		revision int
		limit    int
	}{
		{`{"expected_revision":0,"parent_channel_id":79201}`, 79201, 1, 4},
		{`{"expected_revision":1,"parent_channel_id":0}`, 0, 2, 1},
	} {
		c, recorder := newChannelUserLimitTestContext(http.MethodPut, "/api/channel/79202/limit-parent", step.body, params)
		SetChannelLimitParent(c)
		assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		var response struct {
			Success bool                          `json:"success"`
			Data    appdto.ChannelLimitParentView `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		assert.True(t, response.Success, recorder.Body.String())
		assert.Equal(t, child.Id, response.Data.ChannelID)
		assert.Equal(t, step.parent, response.Data.ParentChannelID)
		assert.Equal(t, step.revision, response.Data.Revision)
		assert.Equal(t, step.limit, response.Data.ConcurrencyLimit)
		assert.Equal(t, step.parent > 0, response.Data.ReadOnly)
		assert.NotContains(t, recorder.Body.String(), "secret")
	}
	c, recorder := newChannelUserLimitTestContext(http.MethodPut, "/api/channel/79202/limit-parent", `{"expected_revision":0,"parent_channel_id":79201}`, params)
	SetChannelLimitParent(c)
	assert.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	for _, body := range []string{
		`{"expected_revision":2,"parent_channel_id":79202}`,
		`{"expected_revision":2,"parent_channel_id":0,"unexpected":true}`,
		`{"expected_revision":2}`,
		`{"expected_revision":2,"parent_channel_id":0.5}`,
	} {
		c, recorder := newChannelUserLimitTestContext(http.MethodPut, "/api/channel/79202/limit-parent", body, params)
		SetChannelLimitParent(c)
		assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
	current, err := model.GetChannelLimitParent(t.Context(), child.Id)
	require.NoError(t, err)
	assert.Equal(t, 2, current.Revision)
	assert.Zero(t, current.ParentChannelId)
}
