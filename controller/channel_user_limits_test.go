package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type channelUserLimitTestResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		ChannelID   int    `json:"channel_id"`
		Limit       int    `json:"limit"`
		StorageMode string `json:"storage_mode"`
		ResetAt     int64  `json:"reset_at"`
		Page        int    `json:"page"`
		PageSize    int    `json:"page_size"`
		Total       int    `json:"total"`
		Items       []struct {
			UserID             int    `json:"user_id"`
			Username           string `json:"username"`
			DisplayName        string `json:"display_name"`
			UsedQuota          int64  `json:"used_quota"`
			RemainingQuota     int64  `json:"remaining_quota"`
			CurrentConcurrency int    `json:"current_concurrency"`
		} `json:"items"`
	} `json:"data"`
}

func setupChannelUserLimitsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled, previousRedis := common.RedisEnabled, common.RDB
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	common.RedisEnabled = false
	common.RDB = nil
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelUserLimitOverride{}, &model.User{}, &model.Log{}))

	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled, common.RDB = previousRedisEnabled, previousRedis
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func newChannelUserLimitTestContext(method string, target string, body string, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = params
	c.Set("id", 9999)
	c.Set("username", "root-operator")
	return c, recorder
}

func TestChannelUserLimitManagementAPIsReturnUsageAndConcurrency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelUserLimitsTestDB(t)
	dailyLimit := 1000
	concurrencyLimit := 3
	channel := model.Channel{
		Id: 9401, Name: "限制状态渠道", Models: "test-model", Group: "default",
		Status: common.ChannelStatusEnabled, UserDailyQuotaLimit: &dailyLimit, UserConcurrencyLimit: &concurrencyLimit,
	}
	user := model.User{Id: 9402, Username: "limit-user", DisplayName: "Limit User", Password: "password"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, service.SetChannelUserDailyQuota(t.Context(), channel.Id, user.Id, 600))

	dailyContext, dailyRecorder := newChannelUserLimitTestContext(
		http.MethodGet,
		"/api/channel/9401/user-daily-quota?p=1&page_size=20",
		"",
		gin.Params{{Key: "id", Value: "9401"}},
	)
	GetChannelUserDailyQuota(dailyContext)
	var dailyResponse channelUserLimitTestResponse
	require.NoError(t, common.Unmarshal(dailyRecorder.Body.Bytes(), &dailyResponse))
	require.True(t, dailyResponse.Success)
	assert.Equal(t, 9401, dailyResponse.Data.ChannelID)
	assert.Equal(t, 1000, dailyResponse.Data.Limit)
	assert.Equal(t, "memory", dailyResponse.Data.StorageMode)
	assert.Positive(t, dailyResponse.Data.ResetAt)
	assert.Equal(t, 1, dailyResponse.Data.Total)
	require.Len(t, dailyResponse.Data.Items, 1)
	assert.Equal(t, "limit-user", dailyResponse.Data.Items[0].Username)
	assert.Equal(t, int64(600), dailyResponse.Data.Items[0].UsedQuota)
	assert.Equal(t, int64(400), dailyResponse.Data.Items[0].RemainingQuota)

	firstLease, err := service.AcquireChannelUserConcurrency(t.Context(), channel.Id, user.Id, concurrencyLimit, nil)
	require.NoError(t, err)
	secondLease, err := service.AcquireChannelUserConcurrency(t.Context(), channel.Id, user.Id, concurrencyLimit, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = firstLease.Release(t.Context())
		_ = secondLease.Release(t.Context())
	})

	concurrencyContext, concurrencyRecorder := newChannelUserLimitTestContext(
		http.MethodGet,
		"/api/channel/9401/user-concurrency?p=1&page_size=20",
		"",
		gin.Params{{Key: "id", Value: "9401"}},
	)
	GetChannelUserConcurrency(concurrencyContext)
	var concurrencyResponse channelUserLimitTestResponse
	require.NoError(t, common.Unmarshal(concurrencyRecorder.Body.Bytes(), &concurrencyResponse))
	require.True(t, concurrencyResponse.Success)
	require.Len(t, concurrencyResponse.Data.Items, 1)
	assert.Equal(t, 2, concurrencyResponse.Data.Items[0].CurrentConcurrency)
}

func TestSetChannelUserDailyQuotaUsesTargetValueIncludingZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelUserLimitsTestDB(t)
	dailyLimit := 1000
	channel := model.Channel{
		Id: 9501, Name: "个人调整渠道", Models: "test-model", Group: "default",
		Status: common.ChannelStatusEnabled, UserDailyQuotaLimit: &dailyLimit,
	}
	user := model.User{Id: 9502, Username: "adjust-user", Password: "password"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&user).Error)

	setContext, setRecorder := newChannelUserLimitTestContext(
		http.MethodPut,
		"/api/channel/9501/user-daily-quota/9502",
		`{"used_quota":300}`,
		gin.Params{{Key: "id", Value: "9501"}, {Key: "user_id", Value: "9502"}},
	)
	SetChannelUserDailyQuota(setContext)
	assert.Contains(t, setRecorder.Body.String(), `"success":true`)
	usedQuota, err := service.CheckChannelUserDailyQuota(t.Context(), channel.Id, user.Id, dailyLimit)
	require.NoError(t, err)
	assert.Equal(t, int64(300), usedQuota)

	clearContext, clearRecorder := newChannelUserLimitTestContext(
		http.MethodPut,
		"/api/channel/9501/user-daily-quota/9502",
		`{"used_quota":0}`,
		gin.Params{{Key: "id", Value: "9501"}, {Key: "user_id", Value: "9502"}},
	)
	SetChannelUserDailyQuota(clearContext)
	assert.Contains(t, clearRecorder.Body.String(), `"success":true`)
	usedQuota, err = service.CheckChannelUserDailyQuota(t.Context(), channel.Id, user.Id, dailyLimit)
	require.NoError(t, err)
	assert.Zero(t, usedQuota)
}

func TestChannelUserLimitOverrideSupportsUserWithoutUsageHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelUserLimitsTestDB(t)
	concurrencyLimit := 2
	dailyLimit := 500_000
	weeklyLimit := 2_000_000
	channel := model.Channel{
		Id: 9551, Name: "提前提额渠道", Models: "test-model", Group: "default",
		Status: common.ChannelStatusEnabled, UserConcurrencyLimit: &concurrencyLimit,
		UserDailyQuotaLimit: &dailyLimit, UserWeeklyQuotaLimit: &weeklyLimit,
	}
	user := model.User{Id: 9552, Username: "future-user", DisplayName: "Future User", Password: "password"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&user).Error)

	setContext, setRecorder := newChannelUserLimitTestContext(
		http.MethodPut,
		"/api/channel/9551/user-limit-overrides/9552",
		`{"user_concurrency_limit":4,"user_daily_quota_limit":1000000,"user_weekly_quota_limit":4000000,"expires_at":0}`,
		gin.Params{{Key: "id", Value: "9551"}, {Key: "user_id", Value: "9552"}},
	)
	SetChannelUserLimitOverride(setContext)
	assert.Contains(t, setRecorder.Body.String(), `"success":true`)

	statusContext, statusRecorder := newChannelUserLimitTestContext(
		http.MethodGet,
		"/api/channel/9551/user-limit-status/9552",
		"",
		gin.Params{{Key: "id", Value: "9551"}, {Key: "user_id", Value: "9552"}},
	)
	GetChannelUserLimitStatus(statusContext)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			OverrideActive bool `json:"override_active"`
			Concurrency    struct {
				EffectiveLimit int   `json:"effective_limit"`
				Current        int64 `json:"current"`
			} `json:"concurrency"`
			DailyQuota struct {
				EffectiveLimit int   `json:"effective_limit"`
				Current        int64 `json:"current"`
			} `json:"daily_quota"`
			WeeklyQuota struct {
				EffectiveLimit int   `json:"effective_limit"`
				Current        int64 `json:"current"`
			} `json:"weekly_quota"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(statusRecorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.True(t, response.Data.OverrideActive)
	assert.Equal(t, 4, response.Data.Concurrency.EffectiveLimit)
	assert.Zero(t, response.Data.Concurrency.Current)
	assert.Equal(t, 1_000_000, response.Data.DailyQuota.EffectiveLimit)
	assert.Zero(t, response.Data.DailyQuota.Current)
	assert.Equal(t, 4_000_000, response.Data.WeeklyQuota.EffectiveLimit)
	assert.Zero(t, response.Data.WeeklyQuota.Current)

	searchContext, searchRecorder := newChannelUserLimitTestContext(
		http.MethodGet,
		"/api/channel/9551/user-limit-users?keyword=future&p=1&page_size=20",
		"",
		gin.Params{{Key: "id", Value: "9551"}},
	)
	SearchChannelUserLimitUsers(searchContext)
	assert.Contains(t, searchRecorder.Body.String(), `"username":"future-user"`)
}

func TestSetChannelUserWeeklyQuotaUsesTargetValueIncludingZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupChannelUserLimitsTestDB(t)
	weeklyLimit := 2_000
	channel := model.Channel{
		Id: 9561, Name: "每周额度调整渠道", Models: "test-model", Group: "default",
		Status: common.ChannelStatusEnabled, UserWeeklyQuotaLimit: &weeklyLimit,
	}
	user := model.User{Id: 9562, Username: "weekly-user", Password: "password"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&user).Error)

	setContext, setRecorder := newChannelUserLimitTestContext(
		http.MethodPut,
		"/api/channel/9561/user-weekly-quota/9562",
		`{"used_quota":300}`,
		gin.Params{{Key: "id", Value: "9561"}, {Key: "user_id", Value: "9562"}},
	)
	SetChannelUserWeeklyQuota(setContext)
	assert.Contains(t, setRecorder.Body.String(), `"success":true`)
	usedQuota, _, _, err := service.GetChannelUserWeeklyQuotaUsage(t.Context(), channel.Id, user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(300), usedQuota)

	clearContext, clearRecorder := newChannelUserLimitTestContext(
		http.MethodPut,
		"/api/channel/9561/user-weekly-quota/9562",
		`{"used_quota":0}`,
		gin.Params{{Key: "id", Value: "9561"}, {Key: "user_id", Value: "9562"}},
	)
	SetChannelUserWeeklyQuota(clearContext)
	assert.Contains(t, clearRecorder.Body.String(), `"success":true`)
	usedQuota, _, _, err = service.GetChannelUserWeeklyQuotaUsage(t.Context(), channel.Id, user.Id)
	require.NoError(t, err)
	assert.Zero(t, usedQuota)
}

func TestGetChannelUserLimitPageRejectsInvalidPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, target := range []string{
		"/api/channel/1/user-daily-quota?p=-1&page_size=20",
		"/api/channel/1/user-daily-quota?p=1&page_size=-1",
	} {
		c, recorder := newChannelUserLimitTestContext(http.MethodGet, target, "", nil)
		pageInfo, ok := getChannelUserLimitPage(c)
		assert.False(t, ok)
		assert.Nil(t, pageInfo)
		assert.Contains(t, recorder.Body.String(), `"success":false`)
	}
}

func TestChannelUserLimitManagementAPIsHideStorageErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	db := setupChannelUserLimitsTestDB(t)
	dailyLimit := 1000
	channel := model.Channel{
		Id: 9601, Name: "故障脱敏渠道", Models: "test-model", Group: "default",
		Status: common.ChannelStatusEnabled, UserDailyQuotaLimit: &dailyLimit,
	}
	user := model.User{Id: 9602, Username: "storage-error-user", Password: "password"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&user).Error)

	redisServer, err := miniredis.Run()
	require.NoError(t, err)
	redisAddress := redisServer.Addr()
	redisServer.Close()
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddress})
	t.Cleanup(func() {
		_ = redisClient.Close()
	})
	common.RedisEnabled = true
	common.RDB = redisClient

	testCases := []struct {
		name    string
		method  string
		target  string
		body    string
		params  gin.Params
		handler func(*gin.Context)
	}{
		{
			name:    "每日额度列表",
			method:  http.MethodGet,
			target:  "/api/channel/9601/user-daily-quota?p=1&page_size=20",
			params:  gin.Params{{Key: "id", Value: "9601"}},
			handler: GetChannelUserDailyQuota,
		},
		{
			name:    "当前并发列表",
			method:  http.MethodGet,
			target:  "/api/channel/9601/user-concurrency?p=1&page_size=20",
			params:  gin.Params{{Key: "id", Value: "9601"}},
			handler: GetChannelUserConcurrency,
		},
		{
			name:    "个人额度调整",
			method:  http.MethodPut,
			target:  "/api/channel/9601/user-daily-quota/9602",
			body:    `{"used_quota":300}`,
			params:  gin.Params{{Key: "id", Value: "9601"}, {Key: "user_id", Value: "9602"}},
			handler: SetChannelUserDailyQuota,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			c, recorder := newChannelUserLimitTestContext(
				testCase.method,
				testCase.target,
				testCase.body,
				testCase.params,
			)
			c.Request.Header.Set("Accept-Language", "en")
			testCase.handler(c)

			var response channelUserLimitTestResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
			assert.Equal(t, "User limit status is temporarily unavailable, please try again later", response.Message)
			assert.NotContains(t, recorder.Body.String(), redisAddress)
			assert.NotContains(t, recorder.Body.String(), "dial tcp")
			assert.NotContains(t, recorder.Body.String(), "Redis")
		})
	}
}
