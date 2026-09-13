package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelPeriodTest(t testing.TB, now *time.Time, useRedis bool) *model.Channel {
	t.Helper()
	oldDB, oldRDB, oldEnabled, oldNow := model.DB, common.RDB, common.RedisEnabled, channelPeriodNow
	oldDaily, oldWeekly, oldDailyNow, oldWeeklyNow := channelUserDailyQuotaMemory, channelUserWeeklyQuotaMemory, channelUserDailyQuotaNow, channelUserWeeklyQuotaNow
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.ChannelPeriodPolicy{}, &model.ChannelQuotaTracking{}, &model.ChannelUserPeriodOverride{}, &model.ChannelUserLimitOverride{}, &model.ChannelUserBudgetOverride{}, &model.Channel{}, &model.User{}))
	model.DB = db
	common.RedisEnabled, common.RDB = useRedis, nil
	if useRedis {
		server := miniredis.RunT(t)
		common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
		client := common.RDB
		t.Cleanup(func() { _ = client.Close() })
	}
	channelPeriodNow = func() time.Time { return *now }
	channelUserDailyQuotaNow, channelUserWeeklyQuotaNow = channelPeriodNow, channelPeriodNow
	channelUserDailyQuotaMemory, channelUserWeeklyQuotaMemory = newChannelUserDailyQuotaMemoryStore(), newChannelUserWeeklyQuotaMemoryStore()
	channelPeriodUsageMemory.Lock()
	oldValues := channelPeriodUsageMemory.values
	channelPeriodUsageMemory.values = make(map[string]*channelPeriodUsageBucket)
	channelPeriodUsageMemory.Unlock()
	channelUserLimitOverrideMemoryCache.Lock()
	oldOverrides := channelUserLimitOverrideMemoryCache.values
	channelUserLimitOverrideMemoryCache.values = make(map[string]channelUserLimitOverrideCacheEntry)
	channelUserLimitOverrideMemoryCache.Unlock()
	channelPeriodPolicyCache.Lock()
	oldPolicies := channelPeriodPolicyCache.values
	channelPeriodPolicyCache.values = make(map[string]channelPeriodPolicyCacheEntry)
	channelPeriodPolicyCache.Unlock()
	t.Cleanup(func() {
		model.DB, common.RDB, common.RedisEnabled, channelPeriodNow = oldDB, oldRDB, oldEnabled, oldNow
		channelUserDailyQuotaMemory, channelUserWeeklyQuotaMemory, channelUserDailyQuotaNow, channelUserWeeklyQuotaNow = oldDaily, oldWeekly, oldDailyNow, oldWeeklyNow
		channelPeriodUsageMemory.Lock()
		channelPeriodUsageMemory.values = oldValues
		channelPeriodUsageMemory.Unlock()
		channelUserLimitOverrideMemoryCache.Lock()
		channelUserLimitOverrideMemoryCache.values = oldOverrides
		channelUserLimitOverrideMemoryCache.Unlock()
		channelPeriodPolicyCache.Lock()
		channelPeriodPolicyCache.values = oldPolicies
		channelPeriodPolicyCache.Unlock()
		_ = sqlDB.Close()
	})
	// 旧日/周列用于迁移及未迁移兜底测试，已持久化的 v2 策略不再读取它们。
	daily, weekly := 100, 500
	channel := &model.Channel{Id: 80, Name: "测试渠道", UserDailyQuotaLimit: &daily, UserWeeklyQuotaLimit: &weekly}
	require.NoError(t, db.Create(channel).Error)
	return channel
}

func TestChannelPeriodHolidayQuotaSurvivesDailyReset(t *testing.T) {
	for _, mode := range []string{"memory", "redis"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Date(2031, 9, 15, 9, 0, 0, 0, time.Local)
			channel := setupChannelPeriodTest(t, &now, mode == "redis")
			ctx := context.Background()
			holiday := dateRangeSchedule("new-holiday", "周中放假", "2031-09-15T00:00", "2031-09-20T00:00")
			config := budgetConfig([]dto.ChannelBudgetSchedule{holiday},
				budgetRow("假期个人每日", channelBudgetScopeUser, channelBudgetWindowDaily, "new-holiday", 5),
				budgetRow("假期个人整段", channelBudgetScopeUser, channelBudgetWindowOccurrence, "new-holiday", 15),
				budgetRow("假期池子每日", channelBudgetScopePool, channelBudgetWindowDaily, "new-holiday", 5),
				budgetRow("假期池子整段", channelBudgetScopePool, channelBudgetWindowOccurrence, "new-holiday", 15),
			)
			_, err := SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
			require.NoError(t, err)
			for day := 0; day < 3; day++ {
				require.NoError(t, CheckChannelPeriodLimits(ctx, channel, 7))
				require.NoError(t, RecordChannelUserQuotaUsage(ctx, channel.Id, 7, 5))
				now = now.AddDate(0, 0, 1)
			}
			var blocked *ChannelPeriodBlock
			require.ErrorAs(t, CheckChannelPeriodLimits(ctx, channel, 7), &blocked)
			assert.Equal(t, "occurrence", blocked.Metric.Period)
			assert.Equal(t, int64(15), blocked.Metric.Used)
			require.ErrorAs(t, CheckChannelPeriodLimits(ctx, channel, 8), &blocked)
			assert.Equal(t, "pool", blocked.Metric.Scope)
			// 人工清零旧个人日/周计数不影响整段累计。
			require.NoError(t, SetChannelUserDailyQuota(ctx, channel.Id, 7, 0))
			require.NoError(t, SetChannelUserWeeklyQuota(ctx, channel.Id, 7, 0))
			require.ErrorAs(t, CheckChannelPeriodLimits(ctx, channel, 7), &blocked)
			assert.Equal(t, int64(15), blocked.Metric.Used)
		})
	}
}

func TestChannelPeriodSoftLimitAndAtomicFailure(t *testing.T) {
	for _, mode := range []string{"memory", "redis"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Date(2031, 9, 15, 9, 0, 0, 0, time.Local)
			channel := setupChannelPeriodTest(t, &now, mode == "redis")
			ctx := context.Background()
			_, err := SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{Config: poolDailyConfig(100)}, 1)
			require.NoError(t, err)
			require.NoError(t, RecordChannelUserQuotaUsage(ctx, channel.Id, 7, 80))
			require.NoError(t, CheckChannelPeriodLimits(ctx, channel, 7))
			require.NoError(t, CheckChannelPeriodLimits(ctx, channel, 7))
			require.NoError(t, RecordChannelUserQuotaUsage(ctx, channel.Id, 7, 30))
			require.NoError(t, RecordChannelUserQuotaUsage(ctx, channel.Id, 7, 30))
			require.Error(t, CheckChannelPeriodLimits(ctx, channel, 7))
			for _, quota := range []int{0, -30} {
				require.NoError(t, RecordChannelUserQuotaUsage(ctx, channel.Id, 7, quota))
			}
			key := newChannelBudgetCounter(channel.Id, channelBudgetRow{ChannelBudgetRow: dto.ChannelBudgetRow{ID: legacyPoolDailyBudgetID, Scope: channelBudgetScopePool, Window: channelBudgetWindowDaily}}, channelBudgetResolution{now: now}).poolKey
			if mode == "redis" {
				require.NoError(t, common.RDB.HSet(ctx, key, "__pool", math.MaxInt64).Err())
			} else {
				channelPeriodUsageMemory.values[key].values["__pool"] = math.MaxInt64
			}
			require.Error(t, RecordChannelUserQuotaUsage(ctx, channel.Id, 7, 1))
			daily, _, _, err := GetChannelUserDailyQuotaUsage(ctx, channel.Id, 7)
			require.NoError(t, err)
			assert.Equal(t, int64(140), daily)
			weekly, _, _, err := GetChannelUserWeeklyQuotaUsage(ctx, channel.Id, 7)
			require.NoError(t, err)
			assert.Equal(t, int64(140), weekly)
		})
	}
}

func TestChannelPeriodRulePriorityAndStableIdentity(t *testing.T) {
	now := time.Date(2031, 9, 15, 9, 0, 0, 0, time.Local)
	channel := setupChannelPeriodTest(t, &now, false)
	ctx := context.Background()
	config := budgetConfig([]dto.ChannelBudgetSchedule{
		weeklySchedule("new-week", "跨周区间", 6, "00:00", 3, "00:00"),
		dateRangeSchedule("new-holiday", "放假", "2031-09-15T00:00", "2031-09-16T00:00"),
	},
		budgetRow("跨周个人整段", channelBudgetScopeUser, channelBudgetWindowOccurrence, "new-week", 50),
		budgetRow("放假个人每日", channelBudgetScopeUser, channelBudgetWindowDaily, "new-holiday", 5),
		budgetRow("放假个人整段", channelBudgetScopeUser, channelBudgetWindowOccurrence, "new-holiday", 20),
	)
	view, err := SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
	require.NoError(t, err)
	require.NoError(t, RecordChannelUserQuotaUsage(ctx, channel.Id, 7, 12))
	// 个人只对放假每日行提额，整段约束仍保留。
	require.NoError(t, ReplaceChannelUserBudgetOverride(ctx, channel.Id, 7, view.Config.Budgets[1].ID, dto.ChannelBudgetUserOverrideInput{Limit: 20}, 1))
	status, err := GetChannelPeriodStatus(ctx, channel, 7)
	require.NoError(t, err)
	daily, ok := findMetric(status, "user", "daily", true)
	require.True(t, ok)
	assert.Equal(t, int64(20), daily.Limit)
	assert.Equal(t, "personal", daily.Source.Kind)
	require.NoError(t, CheckChannelPeriodLimits(ctx, channel, 7))
	now = now.AddDate(0, 0, 1)
	status, err = GetChannelPeriodStatus(ctx, channel, 7)
	require.NoError(t, err)
	interval, ok := findMetric(status, "user", "occurrence", true)
	require.True(t, ok)
	assert.Equal(t, int64(12), interval.Used)
	assert.Equal(t, int64(50), interval.Limit)
	view.Config.Schedules[0].Enabled = false
	view, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
	require.NoError(t, err)
	require.NoError(t, RecordChannelUserQuotaUsage(ctx, channel.Id, 7, 3))
	view.Config.Schedules[0].Enabled = true
	view, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
	require.NoError(t, err)
	status, err = GetChannelPeriodStatus(ctx, channel, 7)
	require.NoError(t, err)
	interval, ok = findMetric(status, "user", "occurrence", true)
	require.True(t, ok)
	assert.Equal(t, int64(15), interval.Used)
	view.Config.Schedules[0].StartTime = "01:00"
	_, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
	assert.ErrorIs(t, err, ErrInvalidChannelPeriodPolicy)
	_, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: 0, Config: config}, 1)
	assert.ErrorIs(t, err, model.ErrChannelPeriodPolicyConflict)
}

func TestChannelPeriodRuleOverlapInheritanceAndTimezone(t *testing.T) {
	now := time.Date(2031, 9, 15, 9, 0, 0, 0, time.UTC)
	first := weeklySchedule("new-a", "区间", 6, "21:00", 0, "09:00")
	second := weeklySchedule("new-b", "早间", 0, "08:00", 0, "10:00")
	schedules := []dto.ChannelBudgetSchedule{first, second}
	_, err := NormalizeChannelBudgetConfig(budgetConfig(schedules,
		budgetRow("区间个人每日", channelBudgetScopeUser, channelBudgetWindowDaily, "new-a", 5),
		budgetRow("早间个人每日", channelBudgetScopeUser, channelBudgetWindowDaily, "new-b", 8),
	), defaultChannelBudgetConfig(), now)
	assert.ErrorIs(t, err, ErrInvalidChannelPeriodPolicy)
	config, err := NormalizeChannelBudgetConfig(budgetConfig(schedules,
		budgetRow("区间个人每日", channelBudgetScopeUser, channelBudgetWindowDaily, "new-a", 5),
		budgetRow("早间池子每日", channelBudgetScopePool, channelBudgetWindowDaily, "new-b", 0),
	), defaultChannelBudgetConfig(), now)
	require.NoError(t, err)
	plan := buildChannelBudgetPlan(dto.ChannelPeriodPolicyView{Revision: 1, Config: config})
	res, err := resolveChannelBudgetRows(plan, now.Add(-30*time.Minute), "")
	require.NoError(t, err)
	userDaily, ok := res.effectiveRow(plan, channelBudgetScopeUser+"|"+channelBudgetWindowDaily+"|")
	require.True(t, ok)
	assert.Equal(t, int64(5), userDaily.Limit)
	poolDaily, ok := res.effectiveRow(plan, channelBudgetScopePool+"|"+channelBudgetWindowDaily+"|")
	require.True(t, ok)
	assert.Equal(t, int64(0), poolDaily.Limit)
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	_, err = parseChannelRuleLocalTime("2031-11-02T01:30", loc)
	assert.Error(t, err)
	_, err = parseChannelRuleLocalTime("2031-03-09T02:30", loc)
	assert.Error(t, err)
}

func TestChannelPeriodPersonalExpiryAndStorageFailures(t *testing.T) {
	for _, useRedis := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "redis"}[useRedis], func(t *testing.T) {
			now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
			channel := setupChannelPeriodTest(t, &now, useRedis)
			config := budgetConfig([]dto.ChannelBudgetSchedule{dateRangeSchedule("new-holiday", "假期", "2031-09-16T00:00", "2031-09-20T00:00")},
				budgetRow("池子每日", channelBudgetScopePool, channelBudgetWindowDaily, "", 100),
				budgetRow("假期个人每日", channelBudgetScopeUser, channelBudgetWindowDaily, "new-holiday", 5),
				budgetRow("假期个人整段", channelBudgetScopeUser, channelBudgetWindowOccurrence, "new-holiday", 15),
			)
			view, err := SaveChannelPeriodPolicy(t.Context(), channel.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
			require.NoError(t, err)
			expires := now.Add(time.Hour).Unix()
			require.NoError(t, ReplaceChannelUserBudgetOverride(t.Context(), channel.Id, 7, view.Config.Budgets[1].ID, dto.ChannelBudgetUserOverrideInput{Limit: 20, ExpiresAt: expires}, 1))
			require.NoError(t, ReplaceChannelUserBudgetOverride(t.Context(), channel.Id, 7, view.Config.Budgets[2].ID, dto.ChannelBudgetUserOverrideInput{Limit: 30, ExpiresAt: expires}, 1))
			require.NoError(t, RecordChannelUserQuotaUsage(t.Context(), channel.Id, 7, 16))
			require.NoError(t, CheckChannelPeriodLimits(t.Context(), channel, 7))
			now = now.Add(time.Hour)
			status, err := GetChannelPeriodStatus(t.Context(), channel, 7)
			require.NoError(t, err)
			assert.Equal(t, int64(5), status.Metrics[1].Limit)
			assert.Equal(t, int64(15), status.Metrics[2].Limit)
			assert.True(t, status.Blocked)
			require.NoError(t, model.DB.Migrator().DropTable(&model.ChannelUserBudgetOverride{}))
			_, err = GetChannelPeriodStatus(t.Context(), channel, 7)
			require.Error(t, err)
			assert.Equal(t, 503, ChannelPeriodAPIError(err).StatusCode)
		})
	}
}

func TestChannelPeriodCounterFailureIsVisibleAfterRedisRecovers(t *testing.T) {
	now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
	channel := setupChannelPeriodTest(t, &now, true)
	config := budgetConfig(nil,
		budgetRow("个人每日", channelBudgetScopeUser, channelBudgetWindowDaily, "", 1000),
		budgetRow("个人每周", channelBudgetScopeUser, channelBudgetWindowWeekly, "", 5000),
		budgetRow("池子每日", channelBudgetScopePool, channelBudgetWindowDaily, "", 100),
	)
	_, err := SaveChannelPeriodPolicy(t.Context(), channel.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
	require.NoError(t, err)
	require.NoError(t, RecordChannelUserQuotaUsage(t.Context(), channel.Id, 7, 10))
	client := common.RDB
	common.RDB = nil
	require.Error(t, RecordChannelUserQuotaUsage(t.Context(), channel.Id, 7, 5))
	require.Error(t, CheckChannelPeriodLimits(t.Context(), channel, 7))
	common.RDB = client
	status, err := GetChannelPeriodStatus(t.Context(), channel, 7)
	require.NoError(t, err)
	assert.Equal(t, int64(10), status.Metrics[2].Used)
	assert.Equal(t, "incomplete", status.Metrics[2].Coverage)
	assert.Equal(t, "incomplete", status.Metrics[0].Coverage)
	now = now.AddDate(0, 0, 1)
	status, err = GetChannelPeriodStatus(t.Context(), channel, 7)
	require.NoError(t, err)
	assert.Equal(t, "since_tracking_start", status.Metrics[2].Coverage)
	assert.Equal(t, "incomplete", status.Metrics[1].Coverage)
}

func TestChannelPeriodCorruptCachedConfigurationFailsClosed(t *testing.T) {
	now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
	channel := setupChannelPeriodTest(t, &now, true)
	require.NoError(t, common.RDB.Set(t.Context(), "channel_period_policy:{80}", `{"revision":1,"config":{"schema_version":2,"default_on_exceed":{"mode":"reject","channel_id":0,"model":""},"schedules":[],"budgets":[{"id":"x","name":"x","enabled":true,"scope":"pool","window":"daily","schedule_id":"","models":[],"limit":-1,"on_exceed":{"mode":"inherit","channel_id":0,"model":""},"created_at":1}]}}`, time.Minute).Err())
	_, err := GetChannelPeriodPolicy(t.Context(), channel.Id)
	require.Error(t, err)
	assert.Equal(t, 503, ChannelPeriodAPIError(err).StatusCode)
}

// 保护资金结算失败不能增加软额度这一财务边界。
func TestChannelPeriodQuotaRequiresSuccessfulSettlement(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			now := time.Now()
			channel := setupChannelPeriodTest(t, &now, false)
			oldLog, oldBatch := common.LogConsumeEnabled, common.BatchUpdateEnabled
			common.LogConsumeEnabled, common.BatchUpdateEnabled = false, false
			t.Cleanup(func() { common.LogConsumeEnabled, common.BatchUpdateEnabled = oldLog, oldBatch })
			require.NoError(t, model.DB.Create(&model.User{Id: 7, Username: "结算测试", Quota: 1000}).Error)
			config := budgetConfig(nil,
				budgetRow("个人每日", channelBudgetScopeUser, channelBudgetWindowDaily, "", 100),
				budgetRow("个人每周", channelBudgetScopeUser, channelBudgetWindowWeekly, "", 500),
				budgetRow("池子每日", channelBudgetScopePool, channelBudgetWindowDaily, "", 1000),
			)
			_, err := SaveChannelPeriodPolicy(t.Context(), channel.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
			require.NoError(t, err)
			billing := &textQuotaGuardBillingStub{}
			if fail {
				billing.settleErr = errors.New("模拟资金结算失败")
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{UserId: 7, UserQuota: common.MaxQuota, StartTime: now, Billing: billing,
				ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channel.Id},
				PriceData:   hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}}}
			// 只验证资金与周期额度，冻结计费模型且不生成性能模型指标。
			info.FreezeBillingModelName("test-settlement")
			PostTextConsumeQuota(c, info, &relaydto.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12}, nil)
			status, err := GetChannelPeriodStatus(c, channel, 7)
			require.NoError(t, err)
			expected := int64(12)
			if fail {
				expected = 0
			}
			assert.Equal(t, []int{12}, billing.settledQuotas)
			assert.Equal(t, expected, status.Metrics[0].Used)
			assert.Equal(t, expected, status.Metrics[2].Used)
		})
	}
}

func TestChannelPeriodPolicyLatePublishKeepsLatestRevision(t *testing.T) {
	now := time.Now()
	channel := setupChannelPeriodTest(t, &now, true)
	older, err := SaveChannelPeriodPolicy(t.Context(), channel.Id, dto.ChannelPeriodPolicyInput{Config: poolDailyConfig(100)}, 1)
	require.NoError(t, err)
	next := older.Config
	next.Budgets[0].Limit = 50
	newer, err := SaveChannelPeriodPolicy(t.Context(), channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: older.Revision, Config: next}, 2)
	require.NoError(t, err)
	raw, err := common.Marshal(older)
	require.NoError(t, err)
	// 模拟另一实例已完成较早的数据库提交，但直到新版发布后才恢复缓存写入。
	require.NoError(t, channelPeriodPolicyPublishScript.Run(t.Context(), common.RDB, []string{fmt.Sprintf("channel_period_policy:{%d}", channel.Id)}, raw, older.Revision).Err())
	current, err := GetChannelPeriodPolicy(t.Context(), channel.Id)
	require.NoError(t, err)
	assert.Equal(t, newer.Revision, current.Revision)
	assert.Equal(t, int64(50), current.Config.Budgets[0].Limit)
}

// TestChannelPeriodPolicyLimitAboveInt32 保证池子与整段额度不受 32 位 quota 列上限约束，且仍受 MaxPeriodQuota 兜底。
func TestChannelPeriodPolicyLimitAboveInt32(t *testing.T) {
	now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
	channel := setupChannelPeriodTest(t, &now, false)
	ctx := context.Background()
	// $4500 按默认单价换算后超过 math.MaxInt32，曾被误拒。
	large := int64(4500) * int64(common.QuotaPerUnit)
	require.Greater(t, large, int64(common.MaxQuota))
	config := budgetConfig([]dto.ChannelBudgetSchedule{dateRangeSchedule("new-holiday", "大额假期", "2031-09-16T00:00", "2031-09-20T00:00")},
		budgetRow("池子每日", channelBudgetScopePool, channelBudgetWindowDaily, "", large),
		budgetRow("假期池子整段", channelBudgetScopePool, channelBudgetWindowOccurrence, "new-holiday", large),
	)
	view, err := SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
	require.NoError(t, err)
	assert.Equal(t, large, view.Config.Budgets[0].Limit)
	require.NoError(t, RecordChannelUserQuotaUsage(ctx, channel.Id, 7, common.MaxQuota))
	status, err := GetChannelPeriodStatus(ctx, channel, 7)
	require.NoError(t, err)
	require.Len(t, status.Metrics, 2)
	for _, metric := range status.Metrics {
		assert.Equal(t, large, metric.Limit, metric.Period)
		assert.Equal(t, int64(common.MaxQuota), metric.Used, metric.Period)
		assert.Less(t, metric.Used, metric.Limit, metric.Period)
	}
	next := view.Config
	next.Budgets[0].Limit = common.MaxPeriodQuota + 1
	_, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: next}, 1)
	require.ErrorIs(t, err, ErrInvalidChannelPeriodPolicy)
}

// TestChannelPeriodStatusFallbackEnabledFollowsBlockingRow 保证 fallback_enabled 与请求期一致：
// 拦截时按拦截行的有效动作（行级优先、inherit 回落策略默认）回答，未拦截时才沿用策略默认动作。
func TestChannelPeriodStatusFallbackEnabledFollowsBlockingRow(t *testing.T) {
	fallbackTo := func(channelID int, modelName string) dto.ChannelBudgetAction {
		return dto.ChannelBudgetAction{Mode: channelBudgetActionFallback, ChannelID: channelID, Model: modelName}
	}
	cases := []struct {
		name           string
		defaultAction  dto.ChannelBudgetAction
		rowAction      dto.ChannelBudgetAction
		idleEnabled    bool
		blockedEnabled bool
	}{
		{"row fallback beats default reject", dto.ChannelBudgetAction{Mode: channelBudgetActionReject}, fallbackTo(0, "gpt-6-mini"), false, true},
		{"row reject beats default fallback", fallbackTo(81, "gpt-4o-mini"), dto.ChannelBudgetAction{Mode: channelBudgetActionReject}, true, false},
		{"inherit follows default fallback", fallbackTo(81, "gpt-4o-mini"), dto.ChannelBudgetAction{Mode: channelBudgetActionInherit}, true, true},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
			channel := setupChannelPeriodTest(t, &now, false)
			require.NoError(t, model.DB.Create(&model.Channel{Id: 81, Name: "备用渠道"}).Error)
			overall := budgetRow("池子每日", channelBudgetScopePool, channelBudgetWindowDaily, "", 600)
			modelRow := budgetRow("gpt-6-astra 个人每日", channelBudgetScopeUser, channelBudgetWindowDaily, "", 500)
			modelRow.Models, modelRow.OnExceed = []string{"gpt-6-astra"}, item.rowAction
			config := budgetConfig(nil, overall, modelRow)
			config.DefaultOnExceed = item.defaultAction
			_, err := SaveChannelPeriodPolicy(t.Context(), channel.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
			require.NoError(t, err)
			status, err := GetChannelPeriodStatus(t.Context(), channel, 7)
			require.NoError(t, err)
			assert.False(t, status.Blocked)
			assert.Equal(t, item.idleEnabled, status.FallbackEnabled)
			// 只耗尽按模型的个人行，整体池子行仍有余量，状态必须按真正拦截的那一行回答。
			require.NoError(t, RecordChannelUserModelQuotaUsage(t.Context(), channel.Id, 7, 500, "gpt-6-astra"))
			status, err = GetChannelPeriodStatus(t.Context(), channel, 7)
			require.NoError(t, err)
			assert.True(t, status.Blocked)
			assert.Equal(t, item.blockedEnabled, status.FallbackEnabled)
		})
	}
}
