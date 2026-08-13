package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupRelayErrorLogTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalRedisEnabled := common.RedisEnabled

	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.User{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		common.RedisEnabled = originalRedisEnabled
	})

	return db
}

func TestRecordRelayErrorLogIncludesVisionAssistFailureFields(t *testing.T) {
	db := setupRelayErrorLogTestDB(t)
	originalErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = false
	t.Cleanup(func() {
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages?beta=true", nil)
	c.Set("id", 7)
	c.Set("username", "tester")
	c.Set("token_name", "test-token")
	c.Set("original_model", "claude-opus-4-8")
	c.Set("token_id", 11)
	c.Set("group", "test")
	c.Set("channel_id", 22)
	c.Set("channel_name", "main-channel")
	c.Set("channel_type", constant.ChannelTypeAnthropic)
	c.Set(common.RequestIdKey, "request-vision-assist-failed")
	c.Set("use_channel", []string{"22"})
	common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now().Add(-2*time.Second))
	common.SetContextKey(c, constant.ContextKeyLogOther, map[string]interface{}{
		"vision_assist_applied":        false,
		"vision_assist_failure_reason": "assist_call_failed",
		"vision_assist_last_error":     "upstream error: do request failed",
	})

	err := types.NewOpenAIError(errors.New("upstream error: do request failed"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	recordRelayErrorLog(c, err)

	var logs []model.Log
	require.NoError(t, db.Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, model.LogTypeError, logs[0].Type)
	assert.Equal(t, "request-vision-assist-failed", logs[0].RequestId)
	assert.Equal(t, "claude-opus-4-8", logs[0].ModelName)
	other, parseErr := common.StrToMap(logs[0].Other)
	require.NoError(t, parseErr)
	assert.Equal(t, false, other["vision_assist_applied"])
	assert.Equal(t, "assist_call_failed", other["vision_assist_failure_reason"])
	assert.Equal(t, "upstream error: do request failed", other["vision_assist_last_error"])
	assert.Equal(t, "/v1/messages", other["request_path"])
}

func TestRecordRelayErrorLogRespectsGlobalSwitchForNormalErrors(t *testing.T) {
	db := setupRelayErrorLogTestDB(t)
	originalErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = false
	t.Cleanup(func() {
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	err := types.NewOpenAIError(errors.New("upstream error: do request failed"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)

	recordRelayErrorLog(c, err)

	var count int64
	require.NoError(t, db.Model(&model.Log{}).Count(&count).Error)
	assert.EqualValues(t, 0, count)
}

func TestResponsesWebSocketBusinessErrorPersistsCompactAudit(t *testing.T) {
	db := setupRelayErrorLogTestDB(t)
	originalErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses?api_key=client-secret", nil)
	c.Set("id", 8)
	c.Set("username", "ws-tester")
	c.Set("token_name", "ws-token")
	c.Set("original_model", "gpt-5-openai-compact")
	c.Set("token_id", 12)
	c.Set("group", "default")
	c.Set("channel_id", 23)
	c.Set("channel_name", "ws-channel")
	c.Set("channel_type", constant.ChannelTypeOpenAI)
	c.Set(common.RequestIdKey, "request-responses-ws-failed")
	c.Set(common.UpstreamRequestIdKey, "standard-upstream-request-id")
	c.Set("use_channel", []string{"23"})
	common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
	turn := &responsesWebSocketTurn{
		info: &relaycommon.RelayInfo{
			ResponsesCompactMode:   relayconstant.ResponsesCompactModeV2WebSocket,
			RequestURLPath:         "/v1/responses",
			UpstreamRequestURLPath: "https://upstream.example/v1/responses?credential=secret",
			ChannelMeta: &relaycommon.ChannelMeta{
				ChannelId:   23,
				ChannelType: constant.ChannelTypeOpenAI,
			},
		},
	}
	service.SetResponsesCompactAudit(c, turn.info, "response.failed")
	channel := &model.Channel{Id: 23, Type: constant.ChannelTypeOpenAI, Name: "ws-channel", AutoBan: common.GetPointer(0)}
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message: "upstream overloaded",
		Type:    "server_error",
		Code:    "server_is_overloaded",
	}, http.StatusServiceUnavailable)

	processResponsesWebSocketChannelError(c, turn, channel, apiErr, "response.failed")

	var logs []model.Log
	require.NoError(t, db.Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, "standard-upstream-request-id", logs[0].UpstreamRequestId)
	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	audit, ok := adminInfo["responses_compact"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, string(relayconstant.ResponsesCompactModeV2WebSocket), audit["mode"])
	assert.Equal(t, "/v1/responses", audit["inbound_path"])
	assert.Equal(t, "/v1/responses", audit["upstream_path"])
	assert.Equal(t, "response.failed", audit["outcome"])
	assert.Equal(t, "standard-upstream-request-id", audit["upstream_request_id"])
	assert.NotContains(t, logs[0].Other, "secret")
}
