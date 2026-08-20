package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type subscriptionPlanTestEnvelope struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func performAdminSubscriptionPlanRequest(t *testing.T, method string, path string, body string, handler gin.HandlerFunc) subscriptionPlanTestEnvelope {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if method == http.MethodPut {
		c.Params = gin.Params{{Key: "id", Value: "8101"}}
	}
	handler(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope subscriptionPlanTestEnvelope
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
	return envelope
}

func TestAdminSubscriptionPlanUpsertRejectsOversizedCustomResetPeriod(t *testing.T) {
	db := setupAdminUserBillingSummaryControllerTestDB(t)
	confirmPaymentComplianceForTest(t)
	existing := model.SubscriptionPlan{
		Id: 8101, Title: "原套餐", QuotaResetPeriod: model.SubscriptionResetNever,
	}
	require.NoError(t, db.Create(&existing).Error)

	body, err := common.Marshal(AdminUpsertSubscriptionPlanRequest{Plan: model.SubscriptionPlan{
		Title:                   "超大自定义周期",
		QuotaResetPeriod:        model.SubscriptionResetCustom,
		QuotaResetCustomSeconds: model.MaxSubscriptionResetCustomSeconds + 1,
	}})
	require.NoError(t, err)

	for _, testCase := range []struct {
		name    string
		method  string
		path    string
		handler gin.HandlerFunc
	}{
		{name: "create", method: http.MethodPost, path: "/api/subscription/plan", handler: AdminCreateSubscriptionPlan},
		{name: "update", method: http.MethodPut, path: "/api/subscription/plan/8101", handler: AdminUpdateSubscriptionPlan},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			envelope := performAdminSubscriptionPlanRequest(t, testCase.method, testCase.path, string(body), testCase.handler)
			assert.False(t, envelope.Success)
			assert.Equal(t, "自定义重置周期参数无效", envelope.Message)
		})
	}

	var plans []model.SubscriptionPlan
	require.NoError(t, db.Order("id").Find(&plans).Error)
	require.Len(t, plans, 1)
	assert.Equal(t, "原套餐", plans[0].Title)
	assert.Equal(t, model.SubscriptionResetNever, plans[0].QuotaResetPeriod)
}
