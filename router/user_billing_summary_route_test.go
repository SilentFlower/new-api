package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserBillingSummaryRouteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.UserSubscription{}))

	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestUserBillingSummaryRouteRequiresAdminAndAllowsAdmin(t *testing.T) {
	db := setupUserBillingSummaryRouteTestDB(t)
	adminToken := "billing-summary-route-admin-token"
	commonToken := "billing-summary-route-common-token"
	users := []model.User{
		{
			Id: 8101, Username: "billing-summary-route-admin", Password: "password",
			Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default",
			AccessToken: &adminToken, AuthVersion: 1, AffCode: "billing-summary-route-admin-aff",
		},
		{
			Id: 8102, Username: "billing-summary-route-common", Password: "password",
			Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
			AccessToken: &commonToken, AuthVersion: 1, AffCode: "billing-summary-route-common-aff",
		},
	}
	require.NoError(t, db.Create(&users).Error)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyAuditLogged, true)
		c.Next()
	})
	SetApiRouter(engine)

	registered := false
	for _, route := range engine.Routes() {
		if route.Method == http.MethodPost && route.Path == "/api/user/billing-summary" {
			registered = true
			break
		}
	}
	require.True(t, registered)

	tests := []struct {
		name       string
		token      string
		wantStatus int
		wantBody   string
	}{
		{name: "未认证请求", wantStatus: http.StatusUnauthorized, wantBody: `"success":false`},
		{name: "普通用户", token: commonToken, wantStatus: http.StatusForbidden, wantBody: "AUTH_INSUFFICIENT_PRIVILEGE"},
		{name: "管理员", token: adminToken, wantStatus: http.StatusOK, wantBody: `"success":true`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/user/billing-summary", strings.NewReader(`{"user_ids":[8102]}`))
			request.Header.Set("Content-Type", "application/json")
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}

			engine.ServeHTTP(recorder, request)

			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.Contains(t, recorder.Body.String(), test.wantBody)
		})
	}
}
