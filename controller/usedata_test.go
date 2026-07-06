package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func newDashboardQueryTestContext(target string) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return ctx
}

func TestParseDashboardQueryValuesRepeatedAndBracket(t *testing.T) {
	ctx := newDashboardQueryTestContext("/api/data/export?token_names=alpha&token_names=beta&token_names[]=%20beta%20&token_names[]=&token_name=legacy")

	values := parseDashboardTokenNames(ctx)

	assert.Equal(t, []string{"alpha", "beta"}, values)
}

func TestParseDashboardQueryValuesLegacyFallback(t *testing.T) {
	ctx := newDashboardQueryTestContext("/api/data/export?group=%20vip%20")

	values := parseDashboardGroups(ctx)

	assert.Equal(t, []string{"vip"}, values)
}

func TestParseDashboardQueryValuesNewEmptyDoesNotFallback(t *testing.T) {
	ctx := newDashboardQueryTestContext("/api/data/export?groups=&groups=%20&group=legacy")

	values := parseDashboardGroups(ctx)

	assert.Empty(t, values)
}
