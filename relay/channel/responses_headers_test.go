package channel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestCopyResponsesMetadataHeadersUsesExplicitAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Add("X-Codex-Beta-Features", "feature-a")
	c.Request.Header.Add("X-Codex-Beta-Features", "remote_compaction_v2")
	c.Request.Header.Set("X-Codex-Turn-State", "turn-state")
	c.Request.Header.Set("Session-Id", "session-official")
	c.Request.Header.Set("Session_id", "session-compat")
	c.Request.Header.Set("Thread_id", "thread-compat")
	c.Request.Header.Set("Authorization", "Bearer client-secret")
	c.Request.Header.Set("Cookie", "session=secret")
	c.Request.Header.Set("X-Not-Allowed", "blocked")

	target := http.Header{}
	CopyResponsesMetadataHeaders(c, &target)

	assert.Equal(t, []string{"feature-a", "remote_compaction_v2"}, target.Values("X-Codex-Beta-Features"))
	assert.Equal(t, "turn-state", target.Get("X-Codex-Turn-State"))
	assert.Equal(t, "session-official", target.Get("Session-Id"))
	assert.Equal(t, "session-official", target.Get("Session_id"))
	assert.Equal(t, "thread-compat", target.Get("Thread-Id"))
	assert.Equal(t, "thread-compat", target.Get("Thread_id"))
	assert.Empty(t, target.Get("Authorization"))
	assert.Empty(t, target.Get("Cookie"))
	assert.Empty(t, target.Get("X-Not-Allowed"))
}

func TestCaptureResponsesMetadataHeadersKeepsOnlyTurnState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Request.Header.Set("Authorization", "Bearer client-secret")

	CaptureResponsesMetadataHeaders(c, http.Header{
		"X-Codex-Turn-State":    {"next-state"},
		"X-Codex-Turn-Metadata": {"next-metadata"},
		"Authorization":         {"Bearer upstream-secret"},
	})

	assert.Equal(t, "next-state", c.Request.Header.Get("X-Codex-Turn-State"))
	assert.Equal(t, "next-metadata", c.Request.Header.Get("X-Codex-Turn-Metadata"))
	assert.Equal(t, "Bearer client-secret", c.Request.Header.Get("Authorization"))
}
