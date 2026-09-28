package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWorkflowAccountStatusRequiresRootRole(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet,
		"/api/token/workflow/status?username=%E4%B8%AD%E6%96%87%E5%B7%A5%E4%BD%9C%E6%B5%81&workflow_id=workflow-id", nil)
	context.Set("role", common.RoleCommonUser)
	GetWorkflowAccountStatusHandler(context)
	require.Contains(t, recorder.Body.String(), `"success":false`)
	require.NotContains(t, recorder.Body.String(), `"tokens"`)
}
