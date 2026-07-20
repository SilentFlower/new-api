package model

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupRequestUserAgentLogTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	originalDB := DB
	originalLogDB := LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalRedisEnabled := common.RedisEnabled
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalDataExportEnabled := common.DataExportEnabled
	originalGinMode := gin.Mode()

	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.LogConsumeEnabled = true
	common.DataExportEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	require.NoError(t, db.AutoMigrate(&Log{}, &User{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		DB = originalDB
		LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		common.RedisEnabled = originalRedisEnabled
		common.LogConsumeEnabled = originalLogConsumeEnabled
		common.DataExportEnabled = originalDataExportEnabled
		gin.SetMode(originalGinMode)
	})

	return db
}

// TestRequestLogsPreserveOriginalUserAgent 验证消费和错误日志都以入站请求头为唯一 UA 来源。
func TestRequestLogsPreserveOriginalUserAgent(t *testing.T) {
	tests := []struct {
		name     string
		logType  int
		recordFn func(c *gin.Context, other map[string]interface{})
	}{
		{
			name:    "消费日志",
			logType: LogTypeConsume,
			recordFn: func(c *gin.Context, other map[string]interface{}) {
				RecordConsumeLog(c, 1, RecordConsumeLogParams{
					ChannelId: 2,
					ModelName: "gpt-test",
					TokenName: "test-token",
					Other:     other,
				})
			},
		},
		{
			name:    "错误日志",
			logType: LogTypeError,
			recordFn: func(c *gin.Context, other map[string]interface{}) {
				RecordErrorLog(c, 1, 2, "gpt-test", "test-token", "upstream error", 3, 1, false, "default", other)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupRequestUserAgentLogTestDB(t)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			const originalUserAgent = "SourceSDK/7.3 (linux; x86_64) raw-token/ABC+123"
			c.Request.Header.Set("User-Agent", originalUserAgent)
			c.Set("username", "tester")
			c.Set(common.RequestIdKey, "request-user-agent")

			other := map[string]interface{}{
				"admin_info": map[string]interface{}{
					"existing":   "kept",
					"user_agent": "untrusted-value",
				},
			}
			tt.recordFn(c, other)

			var log Log
			require.NoError(t, db.First(&log).Error)
			assert.Equal(t, tt.logType, log.Type)
			parsed, err := common.StrToMap(log.Other)
			require.NoError(t, err)
			adminInfo, ok := parsed["admin_info"].(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, "kept", adminInfo["existing"])
			assert.Equal(t, originalUserAgent, adminInfo["user_agent"])
		})
	}
}

// TestRecordConsumeLogOmitsEmptyUserAgent 验证空 UA 不落库且消费日志仍能正常写入。
func TestRecordConsumeLogOmitsEmptyUserAgent(t *testing.T) {
	db := setupRequestUserAgentLogTestDB(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("username", "tester")

	RecordConsumeLog(c, 1, RecordConsumeLogParams{
		Other: map[string]interface{}{
			"admin_info": map[string]interface{}{
				"existing":   "kept",
				"user_agent": "untrusted-value",
			},
		},
	})

	var log Log
	require.NoError(t, db.First(&log).Error)
	parsed, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	adminInfo, ok := parsed["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "kept", adminInfo["existing"])
	assert.NotContains(t, adminInfo, "user_agent")
}

// TestAppendRequestUserAgentHandlesMissingRequest 验证缺少请求对象时不会保留伪造 UA 或触发异常。
func TestAppendRequestUserAgentHandlesMissingRequest(t *testing.T) {
	tests := []struct {
		name string
		ctx  *gin.Context
	}{
		{name: "空上下文"},
		{name: "空请求对象", ctx: &gin.Context{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			other := map[string]interface{}{
				"admin_info": map[string]interface{}{
					"existing":   "kept",
					"user_agent": "untrusted-value",
				},
			}

			result := appendRequestUserAgent(tt.ctx, other)

			adminInfo, ok := result["admin_info"].(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, "kept", adminInfo["existing"])
			assert.NotContains(t, adminInfo, "user_agent")
		})
	}
}

// TestFormatUserLogsStripsRequestUserAgent 验证普通用户日志响应会移除管理员专属 UA。
func TestFormatUserLogsStripsRequestUserAgent(t *testing.T) {
	logs := []*Log{{
		Other: common.MapToJsonStr(map[string]interface{}{
			"model_price": 0.004,
			"admin_info": map[string]interface{}{
				"user_agent": "SourceSDK/7.3",
			},
		}),
	}}

	formatUserLogs(logs, 0)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, parsed, "admin_info")
	assert.Contains(t, parsed, "model_price")
}
