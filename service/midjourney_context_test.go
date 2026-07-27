package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoMidjourneyHttpRequestUsesGinRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	InitHttpClient()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"description":"ok"}`))
	}))
	t.Cleanup(server.Close)

	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/mj/task/1/fetch", nil).WithContext(requestContext)

	_, _, err := DoMidjourneyHttpRequest(c, time.Second, server.URL)

	require.Error(t, err)
	assert.Contains(t, err.Error(), context.Canceled.Error())
	assert.Zero(t, calls.Load())
}
