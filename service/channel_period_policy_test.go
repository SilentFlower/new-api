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

func setupChannelPeriodTest(t *testing.T, now *time.Time, useRedis bool) *model.Channel {
	t.Helper()
	oldDB, oldRDB, oldEnabled, oldNow := model.DB, common.RDB, common.RedisEnabled, channelPeriodNow
	oldDaily, oldWeekly, oldDailyNow, oldWeeklyNow := channelUserDailyQuotaMemory, channelUserWeeklyQuotaMemory, channelUserDailyQuotaNow, channelUserWeeklyQuotaNow
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.ChannelPeriodPolicy{}, &model.ChannelQuotaTracking{}, &model.ChannelUserPeriodOverride{}, &model.ChannelUserLimitOverride{}, &model.Channel{}, &model.User{}))
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
			five, fifteen := int64(5), int64(15)
			input := dto.ChannelPeriodPolicyInput{Config: dto.ChannelPeriodPolicyConfig{SchemaVersion: 1, Rules: []dto.ChannelPeriodRule{{Name: "周中放假", Enabled: true, Kind: "date_range", StartLocal: "2031-09-15T00:00", EndLocal: "2031-09-20T00:00", UserDailyQuotaLimit: &five, UserPeriodQuotaLimit: &fifteen, PoolDailyQuotaLimit: &five, PoolPeriodQuotaLimit: &fifteen}}}}
			_, err := SaveChannelPeriodPolicy(ctx, channel.Id, input, 1)
			require.NoError(t, err)
			for day := 0; day < 3; day++ {
				require.NoError(t, CheckChannelPeriodLimits(ctx, channel, 7))
				require.NoError(t, RecordChannelUserQuotaUsage(ctx, channel.Id, 7, 5))
				now = now.AddDate(0, 0, 1)
			}
			var blocked *ChannelPeriodBlock
			require.ErrorAs(t, CheckChannelPeriodLimits(ctx, channel, 7), &blocked)
			assert.Equal(t, "custom", blocked.Metric.Period)
			assert.Equal(t, int64(15), blocked.Metric.Used)
			require.ErrorAs(t, CheckChannelPeriodLimits(ctx, channel, 8), &blocked)
			assert.Equal(t, "pool", blocked.Metric.Scope)
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
			_, err := SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{Config: dto.ChannelPeriodPolicyConfig{SchemaVersion: 1, PoolDailyQuotaLimit: 100}}, 1)
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
			key := channelPeriodCounters(channel.Id, now, nil)[0].key
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
	five, twenty, fifty := int64(5), int64(20), int64(50)
	personalDaily := 20
	config := dto.ChannelPeriodPolicyConfig{SchemaVersion: 1, Rules: []dto.ChannelPeriodRule{
		{Name: "跨周区间", Kind: "weekly", Enabled: true, StartWeekday: 6, StartTime: "00:00", EndWeekday: 3, EndTime: "00:00", UserPeriodQuotaLimit: &fifty},
		{Name: "放假", Kind: "date_range", Enabled: true, StartLocal: "2031-09-15T00:00", EndLocal: "2031-09-16T00:00", UserDailyQuotaLimit: &five, UserPeriodQuotaLimit: &twenty},
	}}
	view, err := SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{Config: config}, 1)
	require.NoError(t, err)
	require.NoError(t, RecordChannelUserQuotaUsage(ctx, channel.Id, 7, 12))
	// 个人只覆盖填写的每日指标，整段及池子约束仍保留。
	require.NoError(t, ReplaceChannelUserLimitOverride(ctx, channel, 7, ChannelUserLimitOverrideInput{UserDailyQuotaLimit: &personalDaily}, 1))
	limits, err := ResolveChannelUserEffectiveLimits(ctx, channel, 7)
	require.NoError(t, err)
	assert.Equal(t, 20, limits.EffectiveDailyQuota)
	require.NoError(t, CheckChannelPeriodLimits(ctx, channel, 7))
	now = now.AddDate(0, 0, 1)
	status, err := GetChannelPeriodStatus(ctx, channel, 7)
	require.NoError(t, err)
	var interval dto.ChannelPeriodMetric
	for _, metric := range status.Metrics {
		if metric.Scope == "user" && metric.Period == "custom" && metric.Enforced {
			interval = metric
		}
	}
	assert.Equal(t, int64(12), interval.Used)
	assert.Equal(t, int64(50), interval.Limit)
	view.Config.Rules[0].Enabled = false
	view, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
	require.NoError(t, err)
	require.NoError(t, RecordChannelUserQuotaUsage(ctx, channel.Id, 7, 3))
	view.Config.Rules[0].Enabled = true
	view, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
	require.NoError(t, err)
	status, err = GetChannelPeriodStatus(ctx, channel, 7)
	require.NoError(t, err)
	for _, metric := range status.Metrics {
		if metric.Scope == "user" && metric.Period == "custom" {
			assert.Equal(t, int64(15), metric.Used)
		}
	}
	view.Config.Rules[0].StartTime = "01:00"
	_, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: view.Config}, 1)
	assert.ErrorIs(t, err, ErrInvalidChannelPeriodPolicy)
	_, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: 0, Config: config}, 1)
	assert.ErrorIs(t, err, model.ErrChannelPeriodPolicyConflict)
}

func TestChannelPeriodRuleOverlapInheritanceAndTimezone(t *testing.T) {
	now := time.Date(2031, 9, 15, 9, 0, 0, 0, time.UTC)
	five, zero := int64(5), int64(0)
	base := dto.ChannelPeriodRule{Name: "区间", Kind: "weekly", Enabled: true, StartWeekday: 6, StartTime: "21:00", EndWeekday: 0, EndTime: "09:00", UserDailyQuotaLimit: &five}
	other := base
	other.StartWeekday = 0
	other.StartTime = "08:00"
	other.EndTime = "10:00"
	_, err := NormalizeChannelPeriodConfig(dto.ChannelPeriodPolicyConfig{SchemaVersion: 1, Rules: []dto.ChannelPeriodRule{base, other}}, dto.ChannelPeriodPolicyConfig{}, now)
	assert.ErrorIs(t, err, ErrInvalidChannelPeriodPolicy)
	other.UserDailyQuotaLimit = nil
	other.PoolDailyQuotaLimit = &zero
	config, err := NormalizeChannelPeriodConfig(dto.ChannelPeriodPolicyConfig{SchemaVersion: 1, Rules: []dto.ChannelPeriodRule{base, other}}, dto.ChannelPeriodPolicyConfig{}, now)
	require.NoError(t, err)
	limits, _, _, _, err := resolveChannelPeriodSources(config, 100, now.Add(-30*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, int64(5), limits["user_daily"])
	assert.Equal(t, int64(0), limits["pool_daily"])
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
			five, fifteen := int64(5), int64(15)
			view, err := SaveChannelPeriodPolicy(t.Context(), channel.Id, dto.ChannelPeriodPolicyInput{Config: dto.ChannelPeriodPolicyConfig{SchemaVersion: 1, PoolDailyQuotaLimit: 100, Rules: []dto.ChannelPeriodRule{{Name: "假期", Enabled: true, Kind: "date_range", StartLocal: "2031-09-16T00:00", EndLocal: "2031-09-20T00:00", UserDailyQuotaLimit: &five, UserPeriodQuotaLimit: &fifteen}}}}, 1)
			require.NoError(t, err)
			personal := 20
			expires := now.Add(time.Hour).Unix()
			require.NoError(t, ReplaceChannelUserLimitOverride(t.Context(), channel, 7, ChannelUserLimitOverrideInput{UserDailyQuotaLimit: &personal, ExpiresAt: expires}, 1))
			require.NoError(t, ReplaceChannelUserPeriodOverride(t.Context(), channel.Id, 7, view.Config.Rules[0].ID, dto.ChannelUserPeriodOverrideInput{UserPeriodQuotaLimit: 30, ExpiresAt: expires}, 1))
			require.NoError(t, RecordChannelUserQuotaUsage(t.Context(), channel.Id, 7, 16))
			require.NoError(t, CheckChannelPeriodLimits(t.Context(), channel, 7))
			now = now.Add(time.Hour)
			status, err := GetChannelPeriodStatus(t.Context(), channel, 7)
			require.NoError(t, err)
			assert.Equal(t, int64(5), status.Metrics[0].Limit)
			assert.Equal(t, int64(15), status.Metrics[4].Limit)
			assert.True(t, status.Blocked)
			require.NoError(t, model.DB.Migrator().DropTable(&model.ChannelUserLimitOverride{}))
			_, err = GetChannelPeriodStatus(t.Context(), channel, 7)
			require.Error(t, err)
			assert.Equal(t, 503, ChannelPeriodAPIError(err).StatusCode)
		})
	}
}

func TestChannelPeriodCounterFailureIsVisibleAfterRedisRecovers(t *testing.T) {
	now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
	channel := setupChannelPeriodTest(t, &now, true)
	_, err := SaveChannelPeriodPolicy(t.Context(), channel.Id, dto.ChannelPeriodPolicyInput{Config: dto.ChannelPeriodPolicyConfig{SchemaVersion: 1, PoolDailyQuotaLimit: 100}}, 1)
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
	assert.Equal(t, "incomplete", status.Metrics[3].Coverage)
}

func TestChannelPeriodCorruptCachedConfigurationFailsClosed(t *testing.T) {
	now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
	channel := setupChannelPeriodTest(t, &now, true)
	require.NoError(t, common.RDB.Set(t.Context(), "channel_period_policy:{80}", `{"revision":1,"config":{"schema_version":1,"pool_daily_quota_limit":-1,"rules":[]}}`, time.Minute).Err())
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
	older, err := SaveChannelPeriodPolicy(t.Context(), channel.Id, dto.ChannelPeriodPolicyInput{Config: dto.ChannelPeriodPolicyConfig{SchemaVersion: 1, PoolDailyQuotaLimit: 100}}, 1)
	require.NoError(t, err)
	newer, err := SaveChannelPeriodPolicy(t.Context(), channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: older.Revision, Config: dto.ChannelPeriodPolicyConfig{SchemaVersion: 1, PoolDailyQuotaLimit: 50}}, 2)
	require.NoError(t, err)
	raw, err := common.Marshal(older)
	require.NoError(t, err)
	// 模拟另一实例已完成较早的数据库提交，但直到新版发布后才恢复缓存写入。
	require.NoError(t, channelPeriodPolicyPublishScript.Run(t.Context(), common.RDB, []string{fmt.Sprintf("channel_period_policy:{%d}", channel.Id)}, raw, older.Revision).Err())
	current, err := GetChannelPeriodPolicy(t.Context(), channel.Id)
	require.NoError(t, err)
	assert.Equal(t, newer.Revision, current.Revision)
	assert.Equal(t, int64(50), current.Config.PoolDailyQuotaLimit)
}

// TestChannelPeriodPolicyLimitAboveInt32 保证池子与规则额度不受 32 位 quota 列上限约束，且仍受 MaxPeriodQuota 兜底。
func TestChannelPeriodPolicyLimitAboveInt32(t *testing.T) {
	now := time.Date(2031, 9, 16, 10, 0, 0, 0, time.Local)
	channel := setupChannelPeriodTest(t, &now, false)
	ctx := context.Background()
	// $4500 按默认单价换算后超过 math.MaxInt32，曾被误拒。
	large := int64(4500) * int64(common.QuotaPerUnit)
	require.Greater(t, large, int64(common.MaxQuota))
	view, err := SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{Config: dto.ChannelPeriodPolicyConfig{SchemaVersion: 1, PoolDailyQuotaLimit: large, Rules: []dto.ChannelPeriodRule{{Name: "大额假期", Enabled: true, Kind: "date_range", StartLocal: "2031-09-16T00:00", EndLocal: "2031-09-20T00:00", PoolPeriodQuotaLimit: &large}}}}, 1)
	require.NoError(t, err)
	assert.Equal(t, large, view.Config.PoolDailyQuotaLimit)
	require.NoError(t, RecordChannelUserQuotaUsage(ctx, channel.Id, 7, common.MaxQuota))
	status, err := GetChannelPeriodStatus(ctx, channel, 7)
	require.NoError(t, err)
	checked := 0
	for _, metric := range status.Metrics {
		if metric.Scope != "pool" || metric.Period == "weekly" {
			continue
		}
		checked++
		assert.Equal(t, large, metric.Limit, metric.Period)
		assert.Equal(t, int64(common.MaxQuota), metric.Used, metric.Period)
		assert.Less(t, metric.Used, metric.Limit, metric.Period)
	}
	assert.Equal(t, 2, checked)
	_, err = SaveChannelPeriodPolicy(ctx, channel.Id, dto.ChannelPeriodPolicyInput{ExpectedRevision: view.Revision, Config: dto.ChannelPeriodPolicyConfig{SchemaVersion: 1, PoolDailyQuotaLimit: common.MaxPeriodQuota + 1}}, 1)
	require.ErrorIs(t, err, ErrInvalidChannelPeriodPolicy)
}
