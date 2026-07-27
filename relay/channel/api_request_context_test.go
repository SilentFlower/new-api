package channel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type contextTestAdaptor struct {
	Adaptor
	requestURL string
}

func (a *contextTestAdaptor) GetRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.requestURL, nil
}

func (a *contextTestAdaptor) SetupRequestHeader(_ *gin.Context, _ *http.Header, _ *relaycommon.RelayInfo) error {
	return nil
}

func TestDoRequestUsesGinRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(requestContext)
	upstreamRequest, err := http.NewRequest(http.MethodPost, server.URL, nil)
	require.NoError(t, err)

	_, err = doRequest(c, upstreamRequest, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}, false)

	require.Error(t, err)
	assert.Zero(t, calls.Load())
}

func TestDoWssRequestUsesGinRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)

	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil).WithContext(requestContext)
	adaptor := &contextTestAdaptor{requestURL: "ws" + strings.TrimPrefix(server.URL, "http")}

	_, err := DoWssRequest(adaptor, c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "canceled")
	assert.Zero(t, calls.Load())
}
