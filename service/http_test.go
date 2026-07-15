package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type committedKeepAliveWriter struct {
	gin.ResponseWriter
	written bool
}

func (w *committedKeepAliveWriter) BeginFinalResponse() {}

func (w *committedKeepAliveWriter) NonStreamKeepAliveWritten() bool {
	return w.written
}

func TestIOCopyBytesGracefullyPreservesNormalResponseMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusCreated,
		Header: http.Header{
			"Content-Type":  []string{"application/json"},
			"X-Upstream-Id": []string{"upstream-1"},
		},
	}

	IOCopyBytesGracefully(c, resp, []byte(`{"ok":true}`))

	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.Equal(t, "11", recorder.Header().Get("Content-Length"))
	assert.Equal(t, "upstream-1", recorder.Header().Get("X-Upstream-Id"))
	assert.JSONEq(t, `{"ok":true}`, recorder.Body.String())
}

func TestIOCopyBytesGracefullySkipsLateMetadataAfterKeepAlive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	recorder.Header().Set("Content-Type", "application/json")
	_, _ = recorder.Write([]byte("\n"))
	c, _ := gin.CreateTestContext(recorder)
	c.Writer = &committedKeepAliveWriter{ResponseWriter: c.Writer, written: true}
	resp := &http.Response{
		StatusCode: http.StatusCreated,
		Header: http.Header{
			common.RequestIdKey: []string{"upstream-request"},
			"X-Upstream-Id":     []string{"late-upstream"},
		},
	}

	IOCopyBytesGracefully(c, resp, []byte(`{"ok":true}`))

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Empty(t, recorder.Header().Get("Content-Length"))
	assert.Empty(t, recorder.Header().Get("X-Upstream-Id"))
	assert.Empty(t, recorder.Header().Get(common.RequestIdKey))
	assert.Equal(t, "upstream-request", c.GetString(common.UpstreamRequestIdKey))
	assert.JSONEq(t, `{"ok":true}`, recorder.Body.String())
}
