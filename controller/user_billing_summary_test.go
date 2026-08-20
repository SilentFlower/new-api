package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type adminUserBillingSummaryEnvelope struct {
	Success bool                                `json:"success"`
	Message string                              `json:"message"`
	Data    dto.AdminUserBillingSummaryResponse `json:"data"`
}

func setupAdminUserBillingSummaryControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	previousTranslateMessage := common.TranslateMessage
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.TranslateMessage = func(_ *gin.Context, key string, args ...map[string]any) string {
		if len(args) > 0 {
			if max, exists := args[0]["Max"]; exists {
				return fmt.Sprintf("%s:%v", key, max)
			}
		}
		return key
	}

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.UserSubscription{}))

	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		common.TranslateMessage = previousTranslateMessage
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func performAdminUserBillingSummaryRequest(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/billing-summary", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	GetAdminUserBillingSummaries(c)
	return recorder
}

func decodeAdminUserBillingSummaryEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) adminUserBillingSummaryEnvelope {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope adminUserBillingSummaryEnvelope
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
	return envelope
}

func TestGetAdminUserBillingSummariesReturnsRemoteFactsWithoutSensitiveFields(t *testing.T) {
	db := setupAdminUserBillingSummaryControllerTestDB(t)
	accessToken := "billing-summary-controller-secret-token"
	user := model.User{
		Id: 7101, Username: "billing-summary-controller", Password: "billing-summary-controller-password",
		Email: "billing-summary-controller@example.com", AccessToken: &accessToken,
		Quota: 900, UsedQuota: 250, Group: "vip", Status: common.UserStatusDisabled,
		AffCode: "billing-summary-controller-aff",
	}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Update("role", common.RoleGuestUser).Error)
	plan := model.SubscriptionPlan{Id: 7201, Title: "Controller 套餐", QuotaResetPeriod: model.SubscriptionResetNever}
	require.NoError(t, db.Create(&plan).Error)
	now := model.GetDBTimestamp()
	require.NoError(t, db.Create(&model.UserSubscription{
		Id: 7301, UserId: user.Id, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 400,
		StartTime: now - 100, EndTime: now + 1000, Status: "active",
	}).Error)

	recorder := performAdminUserBillingSummaryRequest(t, `{"user_ids":[7101],"sort_by":"subscription_remaining","sort_order":"desc"}`)
	envelope := decodeAdminUserBillingSummaryEnvelope(t, recorder)

	require.True(t, envelope.Success)
	require.Len(t, envelope.Data.Items, 1)
	item := envelope.Data.Items[0]
	assert.Equal(t, "ok", item.Status)
	require.NotNil(t, item.RemoteStatus)
	require.NotNil(t, item.RemoteRole)
	assert.Equal(t, common.UserStatusDisabled, *item.RemoteStatus)
	assert.Equal(t, common.RoleGuestUser, *item.RemoteRole)
	assert.Equal(t, 900, item.Wallet.Quota)
	assert.EqualValues(t, 600, item.Subscription.FiniteRemaining)
	assert.NotContains(t, recorder.Body.String(), "billing-summary-controller-password")
	assert.NotContains(t, recorder.Body.String(), accessToken)
	assert.NotContains(t, recorder.Body.String(), "billing-summary-controller@example.com")
	assert.NotContains(t, recorder.Body.String(), "access_token")
}

func TestGetAdminUserBillingSummariesMapsRequestErrors(t *testing.T) {
	setupAdminUserBillingSummaryControllerTestDB(t)

	for _, body := range []string{
		`{"user_ids":`,
		`{"user_ids":[]}`,
		`{"user_ids":[1],"sort_by":"quota"}`,
	} {
		recorder := performAdminUserBillingSummaryRequest(t, body)
		envelope := decodeAdminUserBillingSummaryEnvelope(t, recorder)
		assert.False(t, envelope.Success)
		assert.NotEmpty(t, envelope.Message)
	}

	userIDs := make([]int, service.AdminUserBillingSummaryMaxUsers+1)
	for index := range userIDs {
		userIDs[index] = index + 1
	}
	body, err := common.Marshal(dto.AdminUserBillingSummaryRequest{UserIDs: userIDs})
	require.NoError(t, err)
	recorder := performAdminUserBillingSummaryRequest(t, string(body))
	envelope := decodeAdminUserBillingSummaryEnvelope(t, recorder)
	assert.False(t, envelope.Success)
	assert.Contains(t, envelope.Message, fmt.Sprintf("%d", service.AdminUserBillingSummaryMaxUsers))
}

func TestGetAdminUserBillingSummariesHidesDatabaseErrors(t *testing.T) {
	db := setupAdminUserBillingSummaryControllerTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	recorder := performAdminUserBillingSummaryRequest(t, `{"user_ids":[1]}`)
	envelope := decodeAdminUserBillingSummaryEnvelope(t, recorder)

	assert.False(t, envelope.Success)
	assert.NotEmpty(t, envelope.Message)
	assert.NotContains(t, strings.ToLower(recorder.Body.String()), "closed")
}
