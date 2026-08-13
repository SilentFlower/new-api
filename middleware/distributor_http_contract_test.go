package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	appI18n "github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

func setupDistributorTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := model.DB
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalDatabaseType := common.MainDatabaseType()

	dsn := fmt.Sprintf("file:distributor-contract-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))

	model.DB = db
	common.MemoryCacheEnabled = true
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	model.InitChannelCache()

	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM abilities").Error
		_ = db.Exec("DELETE FROM channels").Error
		model.InitChannelCache()
		model.DB = originalDB
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.SetMainDatabaseType(originalDatabaseType)
		if originalMemoryCacheEnabled && originalDB != nil {
			model.InitChannelCache()
		}
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestDistributePreservesTokenModelAccessHTTPContract(t *testing.T) {
	require.NoError(t, appI18n.Init())
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		body         string
		betaFeatures string
	}{
		{
			name: "ordinary responses",
			body: `{"model":"forbidden-model","input":[]}`,
		},
		{
			name:         "compact v2 uses base model",
			body:         `{"model":"forbidden-model","stream":true,"input":[{"type":"compaction_trigger"}]}`,
			betaFeatures: "remote_compaction_v2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(test.body))
			c.Request.Header.Set("Content-Type", "application/json")
			if test.betaFeatures != "" {
				c.Request.Header.Set("X-Codex-Beta-Features", test.betaFeatures)
			}
			common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
			common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{
				"allowed-model": true,
			})
			t.Cleanup(func() { common.CleanupBodyStorage(c) })

			Distribute()(c)

			assert.True(t, c.IsAborted())
			assert.Equal(t, http.StatusForbidden, recorder.Code)
			assert.Empty(t, gjson.Get(recorder.Body.String(), "error.code").String())
			assert.Contains(t, recorder.Body.String(), "forbidden-model")
		})
	}
}

func TestDistributeSkipsChannelSetupWhenSelectionIsNotRequired(t *testing.T) {
	require.NoError(t, appI18n.Init())
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/mj/task/list-by-condition", nil)
	common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
	common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{
		"": true,
	})

	Distribute()(c)

	assert.False(t, c.IsAborted())
	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestDistributePreservesHTTPErrorCodeContract(t *testing.T) {
	require.NoError(t, appI18n.Init())
	gin.SetMode(gin.TestMode)
	db := setupDistributorTestDatabase(t)

	disabledChannel := &model.Channel{
		Id:     7101,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "disabled-key",
		Status: common.ChannelStatusManuallyDisabled,
		Name:   "disabled-channel",
		Models: "gpt-5",
		Group:  "default",
	}
	noEnabledKeyChannel := &model.Channel{
		Id:     7102,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "disabled-multi-key",
		Status: common.ChannelStatusEnabled,
		Name:   "no-enabled-key-channel",
		Models: "gpt-5",
		Group:  "default",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:         true,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusManuallyDisabled},
		},
	}
	require.NoError(t, db.Create(disabledChannel).Error)
	require.NoError(t, db.Create(noEnabledKeyChannel).Error)

	tests := []struct {
		name           string
		path           string
		body           string
		setupContext   func(c *gin.Context)
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "invalid request",
			path:           "/v1/responses",
			body:           `{`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "model access still applies without selection",
			path: "/mj/task/list-by-condition",
			setupContext: func(c *gin.Context) {
				common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
				common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"allowed-model": true})
			},
			expectedStatus: http.StatusForbidden,
		},
		{
			name: "specified channel disabled",
			path: "/v1/responses",
			body: `{"model":"gpt-5","input":[]}`,
			setupContext: func(c *gin.Context) {
				common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, disabledChannel.Id)
			},
			expectedStatus: http.StatusForbidden,
		},
		{
			name: "no available channel",
			path: "/v1/responses",
			body: `{"model":"gpt-5","input":[]}`,
			setupContext: func(c *gin.Context) {
				common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			},
			expectedStatus: http.StatusServiceUnavailable,
			expectedCode:   string(types.ErrorCodeModelNotFound),
		},
		{
			name: "channel context setup failure",
			path: "/v1/responses",
			body: `{"model":"gpt-5","input":[]}`,
			setupContext: func(c *gin.Context) {
				common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, noEnabledKeyChannel.Id)
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			if test.body != "" {
				c.Request.Header.Set("Content-Type", "application/json")
			}
			if test.setupContext != nil {
				test.setupContext(c)
			}
			t.Cleanup(func() { common.CleanupBodyStorage(c) })

			Distribute()(c)

			assert.True(t, c.IsAborted())
			assert.Equal(t, test.expectedStatus, recorder.Code)
			assert.Equal(t, test.expectedCode, gjson.Get(recorder.Body.String(), "error.code").String())
		})
	}
}
