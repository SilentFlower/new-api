package logger

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestSensitiveContentLogsAreSuppressedForGinContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	previousWriter := gin.DefaultWriter
	previousErrorWriter := gin.DefaultErrorWriter
	previousDebugEnabled := common.DebugEnabled
	gin.DefaultWriter = &output
	gin.DefaultErrorWriter = &output
	common.DebugEnabled = true
	t.Cleanup(func() {
		gin.DefaultWriter = previousWriter
		gin.DefaultErrorWriter = previousErrorWriter
		common.DebugEnabled = previousDebugEnabled
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	SuppressSensitiveContentLogs(ctx)
	LogInfo(ctx, "sensitive prompt")
	LogWarn(ctx, "sensitive tool result")
	LogError(ctx, "sensitive error body")
	LogDebug(ctx, "sensitive response: %s", "secret")

	assert.True(t, SensitiveContentLogsSuppressed(ctx))
	assert.Empty(t, output.String())
}
