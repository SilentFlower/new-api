package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	redisserver "github.com/alicebob/miniredis/v2/server"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChannelBudgetAuditReadFailureStopsAdjustment 验证审计前值第二次读取失败时不会修改额度或伪造成功日志。
// @param t 测试上下文。
func TestChannelBudgetAuditReadFailureStopsAdjustment(t *testing.T) {
	db := setupChannelUserLimitsTestDB(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RedisEnabled, common.RDB = true, client
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, db.Create(&model.Channel{Id: 80, Name: "审计渠道"}).Error)
	require.NoError(t, db.Create(&model.User{Id: 77, Username: "审计用户"}).Error)
	config := budgetPoolDailyConfig(100, 0, "")
	config.Budgets[0].Scope, config.Budgets[0].Models = "user", []string{"gpt-6-astra"}
	view, err := service.SaveChannelPeriodPolicy(t.Context(), 80, dto.ChannelPeriodPolicyInput{Config: config}, 1)
	require.NoError(t, err)
	require.NoError(t, service.RecordChannelUserModelQuotaUsage(t.Context(), 80, 77, 40, "gpt-6-astra"))
	reads := 0
	server.Server().SetPreHook(func(peer *redisserver.Peer, command string, args ...string) bool {
		if command == "HGETALL" {
			reads++
			if reads == 2 {
				peer.WriteError("ERR 审计读取故障")
				return true
			}
		}
		return false
	})
	c, recorder := newChannelUserLimitTestContext(http.MethodPut, "/api/channel/80/budgets/"+view.Config.Budgets[0].ID+"/usage", `{"scope":"user","user_id":77,"used_quota":5}`, gin.Params{{Key: "id", Value: "80"}, {Key: "budget_id", Value: view.Config.Budgets[0].ID}})
	SetChannelBudgetUsage(c)
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	server.Server().SetPreHook(nil)
	usage, err := service.GetChannelBudgetUsage(t.Context(), 80, view.Config.Budgets[0].ID, "user", 0, 20)
	require.NoError(t, err)
	require.Len(t, usage.Items, 1)
	assert.Equal(t, int64(40), usage.Items[0].UsedQuota)
	var count int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeManage).Count(&count).Error)
	assert.Zero(t, count)
}

// TestChannelBudgetUsageAndOverrideContract 验证按行的用量列表/调整与个人提额接口的契约、隔离与 400 矩阵。
func TestChannelBudgetUsageAndOverrideContract(t *testing.T) {
	db := setupChannelUserLimitsTestDB(t)
	require.NoError(t, db.Create(&model.Channel{Id: 80, Name: "预算渠道", Status: common.ChannelStatusEnabled, Models: "gpt-6-astra,gpt-6-mini", Group: "default"}).Error)
	require.NoError(t, db.Create(&model.User{Id: 77, Username: "budget-user", DisplayName: "预算用户"}).Error)
	row := func(name, scope, window string, limit int64, models ...string) dto.ChannelBudgetRow {
		if models == nil {
			models = []string{}
		}
		return dto.ChannelBudgetRow{Name: name, Enabled: true, Scope: scope, Window: window, Models: models, Limit: limit, OnExceed: dto.ChannelBudgetAction{Mode: "inherit"}}
	}
	view, err := service.SaveChannelPeriodPolicy(t.Context(), 80, dto.ChannelPeriodPolicyInput{Config: dto.ChannelPeriodPolicyConfig{SchemaVersion: 2, DefaultOnExceed: dto.ChannelBudgetAction{Mode: "reject"}, Schedules: []dto.ChannelBudgetSchedule{}, Budgets: []dto.ChannelBudgetRow{
		row("个人每日", "user", "daily", 100),
		row("池子每日", "pool", "daily", 1000),
		row("gpt-6-astra 每日", "pool", "daily", 500, "gpt-6-astra"),
	}}}, 1)
	require.NoError(t, err)
	userRow, poolRow, modelRow := view.Config.Budgets[0].ID, view.Config.Budgets[1].ID, view.Config.Budgets[2].ID
	require.NoError(t, service.RecordChannelUserModelQuotaUsage(t.Context(), 80, 77, 40, "gpt-6-astra"))
	call := func(method, target, body string, params gin.Params, handler func(*gin.Context)) (int, string) {
		c, recorder := newChannelUserLimitTestContext(method, target, body, params)
		handler(c)
		// 用量与个人提额独立于策略配置，成功或失败的写操作都不能推进策略版本。
		if method != http.MethodGet {
			after, err := service.GetChannelPeriodPolicy(t.Context(), 80)
			require.NoError(t, err)
			assert.Equal(t, view.Revision, after.Revision, target)
		}
		return recorder.Code, recorder.Body.String()
	}
	budgetParams := func(id string) gin.Params { return gin.Params{{Key: "id", Value: "80"}, {Key: "budget_id", Value: id}} }
	var usage struct {
		Success bool                       `json:"success"`
		Data    dto.ChannelBudgetUsageView `json:"data"`
	}
	code, body := call(http.MethodGet, "/api/channel/80/budgets/"+userRow+"/usage?scope=user&p=1&page_size=20", "", budgetParams(userRow), GetChannelBudgetUsage)
	require.Equal(t, http.StatusOK, code, body)
	require.NoError(t, common.Unmarshal([]byte(body), &usage))
	require.Len(t, usage.Data.Items, 1)
	assert.Equal(t, 77, usage.Data.Items[0].UserID)
	assert.Equal(t, "budget-user", usage.Data.Items[0].Username)
	assert.Equal(t, int64(40), usage.Data.Items[0].UsedQuota)
	for _, id := range []string{poolRow, modelRow} {
		code, body = call(http.MethodGet, "/api/channel/80/budgets/"+id+"/usage?scope=pool", "", budgetParams(id), GetChannelBudgetUsage)
		require.Equal(t, http.StatusOK, code, body)
		require.NoError(t, common.Unmarshal([]byte(body), &usage))
		assert.Equal(t, int64(40), usage.Data.UsedQuota, id)
	}
	// 调整模型行池子汇总不影响整体池子与个人计数。
	code, body = call(http.MethodPut, "/api/channel/80/budgets/"+modelRow+"/usage", `{"scope":"pool","user_id":0,"used_quota":5}`, budgetParams(modelRow), SetChannelBudgetUsage)
	require.Equal(t, http.StatusOK, code, body)
	code, body = call(http.MethodGet, "/api/channel/80/budgets/"+modelRow+"/usage?scope=pool", "", budgetParams(modelRow), GetChannelBudgetUsage)
	require.NoError(t, common.Unmarshal([]byte(body), &usage))
	assert.Equal(t, int64(5), usage.Data.UsedQuota)
	code, body = call(http.MethodGet, "/api/channel/80/budgets/"+poolRow+"/usage?scope=pool", "", budgetParams(poolRow), GetChannelBudgetUsage)
	require.NoError(t, common.Unmarshal([]byte(body), &usage))
	assert.Equal(t, int64(40), usage.Data.UsedQuota)
	code, body = call(http.MethodPut, "/api/channel/80/budgets/"+userRow+"/usage", `{"scope":"user","user_id":77,"used_quota":0}`, budgetParams(userRow), SetChannelBudgetUsage)
	require.Equal(t, http.StatusOK, code, body)
	code, body = call(http.MethodGet, "/api/channel/80/budgets/"+userRow+"/usage?scope=user", "", budgetParams(userRow), GetChannelBudgetUsage)
	require.NoError(t, common.Unmarshal([]byte(body), &usage))
	assert.Empty(t, usage.Data.Items)
	for name, item := range map[string]struct{ id, body string }{
		"legacy user out of range": {userRow, fmt.Sprintf(`{"scope":"user","user_id":77,"used_quota":%d}`, int64(common.MaxQuota)+1)},
		"unknown row":              {"missing", `{"scope":"pool","user_id":0,"used_quota":1}`},
		"out of range":             {poolRow, fmt.Sprintf(`{"scope":"pool","user_id":0,"used_quota":%d}`, common.MaxPeriodQuota+1)},
		"invalid scope":            {poolRow, `{"scope":"all","user_id":0,"used_quota":1}`},
		"unknown field":            {poolRow, `{"scope":"pool","user_id":0,"used":1}`},
	} {
		code, body = call(http.MethodPut, "/api/channel/80/budgets/"+item.id+"/usage", item.body, budgetParams(item.id), SetChannelBudgetUsage)
		assert.Equal(t, http.StatusBadRequest, code, name+": "+body)
	}

	overrideParams := func(id string) gin.Params { return append(budgetParams(id), gin.Param{Key: "user_id", Value: "77"}) }
	var status struct {
		Success bool                           `json:"success"`
		Data    channelUserLimitStatusResponse `json:"data"`
	}
	code, body = call(http.MethodPut, "/api/channel/80/budgets/"+userRow+"/user-overrides/77", `{"limit":200,"expires_at":0}`, overrideParams(userRow), SetChannelUserBudgetOverride)
	require.Equal(t, http.StatusOK, code, body)
	require.NoError(t, common.Unmarshal([]byte(body), &status))
	require.NotEmpty(t, status.Data.PeriodLimits.Metrics)
	assert.Equal(t, int64(200), status.Data.PeriodLimits.Metrics[0].Limit)
	assert.Equal(t, int64(100), status.Data.PeriodLimits.Metrics[0].BaseLimit)
	require.NotNil(t, status.Data.PeriodLimits.Metrics[0].OverrideLimit)
	assert.Equal(t, "personal", status.Data.PeriodLimits.Metrics[0].Source.Kind)
	for name, item := range map[string]struct{ id, body string }{
		"pool row":     {poolRow, `{"limit":2000,"expires_at":0}`},
		"not increase": {userRow, `{"limit":100,"expires_at":0}`},
		"expired":      {userRow, `{"limit":300,"expires_at":1}`},
	} {
		code, body = call(http.MethodPut, "/api/channel/80/budgets/"+item.id+"/user-overrides/77", item.body, overrideParams(item.id), SetChannelUserBudgetOverride)
		assert.Equal(t, http.StatusBadRequest, code, name+": "+body)
	}
	var list struct {
		Success bool `json:"success"`
		Data    struct {
			Total int64 `json:"total"`
			Items []struct {
				BudgetID string `json:"budget_id"`
				Limit    int64  `json:"limit"`
			} `json:"items"`
		} `json:"data"`
	}
	// 撤销池子行必须拒绝，不能因为撤销占位机制而写入无效个人覆盖。
	code, body = call(http.MethodDelete, "/api/channel/80/budgets/"+poolRow+"/user-overrides/77", "", overrideParams(poolRow), DeleteChannelUserBudgetOverride)
	assert.Equal(t, http.StatusBadRequest, code, body)
	var poolOverrides int64
	require.NoError(t, model.DB.Model(&model.ChannelUserBudgetOverride{}).Where("channel_id = ? AND budget_id = ?", 80, poolRow).Count(&poolOverrides).Error)
	assert.Zero(t, poolOverrides)
	code, body = call(http.MethodGet, "/api/channel/80/budget-user-overrides?p=1&page_size=20", "", gin.Params{{Key: "id", Value: "80"}}, GetChannelUserBudgetOverrides)
	require.Equal(t, http.StatusOK, code, body)
	require.NoError(t, common.Unmarshal([]byte(body), &list))
	require.Len(t, list.Data.Items, 1)
	assert.Equal(t, userRow, list.Data.Items[0].BudgetID)
	assert.Equal(t, int64(200), list.Data.Items[0].Limit)
	code, body = call(http.MethodPut, "/api/channel/80/budgets/"+userRow+"/user-overrides/77", `{"limit":300,"expires_at":0}`, overrideParams(userRow), SetChannelUserBudgetOverride)
	require.Equal(t, http.StatusOK, code, body)
	code, body = call(http.MethodDelete, "/api/channel/80/budgets/"+userRow+"/user-overrides/77", "", overrideParams(userRow), DeleteChannelUserBudgetOverride)
	require.Equal(t, http.StatusOK, code, body)
	var afterDelete struct {
		Success bool                           `json:"success"`
		Data    channelUserLimitStatusResponse `json:"data"`
	}
	require.NoError(t, common.Unmarshal([]byte(body), &afterDelete))
	assert.Equal(t, int64(100), afterDelete.Data.PeriodLimits.Metrics[0].Limit)
	assert.Nil(t, afterDelete.Data.PeriodLimits.Metrics[0].OverrideLimit)
	// 旧个人日/周提额字段已下线，携带即 400。
	for _, field := range []string{"user_daily_quota_limit", "user_weekly_quota_limit"} {
		code, body = call(http.MethodPut, "/api/channel/80/user-limit-overrides/77", fmt.Sprintf(`{"user_concurrency_limit":4,"%s":1000,"expires_at":0}`, field), gin.Params{{Key: "id", Value: "80"}, {Key: "user_id", Value: "77"}}, SetChannelUserLimitOverride)
		assert.Equal(t, http.StatusBadRequest, code, field+": "+body)
		assert.Contains(t, body, `"success":false`)
	}

	// 从实际持久化的管理日志核对前后值，失败的写请求不得产生成功审计。
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeManage).Order("id ASC").Find(&logs).Error)
	expected := []struct {
		action, budgetID, before, after string
		userID                          int
	}{
		{"channel.budget_usage_set", modelRow, `40`, `5`, 0},
		{"channel.budget_usage_set", userRow, `40`, `0`, 77},
		{"channel.budget_user_override_set", userRow, `null`, `{"limit":200,"expires_at":0}`, 77},
		{"channel.budget_user_override_set", userRow, `{"limit":200,"expires_at":0}`, `{"limit":300,"expires_at":0}`, 77},
		{"channel.budget_user_override_delete", userRow, `{"limit":300,"expires_at":0}`, `null`, 77},
	}
	require.Len(t, logs, len(expected))
	for i, want := range expected {
		var other struct {
			Op struct {
				Action string `json:"action"`
				Params struct {
					ChannelID int             `json:"channel_id"`
					BudgetID  string          `json:"budget_id"`
					UserID    int             `json:"user_id"`
					Before    json.RawMessage `json:"before"`
					After     json.RawMessage `json:"after"`
				} `json:"params"`
			} `json:"op"`
		}
		require.NoError(t, common.UnmarshalJsonStr(logs[i].Other, &other))
		assert.Equal(t, want.action, other.Op.Action)
		assert.Equal(t, 80, other.Op.Params.ChannelID)
		assert.Equal(t, want.budgetID, other.Op.Params.BudgetID)
		assert.Equal(t, want.userID, other.Op.Params.UserID)
		assert.JSONEq(t, want.before, string(other.Op.Params.Before))
		assert.JSONEq(t, want.after, string(other.Op.Params.After))
	}
}
