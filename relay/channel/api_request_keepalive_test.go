package channel

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoRequestSendsStreamPingWhileWaitingForUpstreamHeaders(t *testing.T) {
	service.InitHttpClient()
	gin.SetMode(gin.TestMode)

	setting := operation_setting.GetGeneralSetting()
	oldEnabled := setting.PingIntervalEnabled
	oldSeconds := setting.PingIntervalSeconds
	setting.PingIntervalEnabled = true
	setting.PingIntervalSeconds = 1
	t.Cleanup(func() {
		setting.PingIntervalEnabled = oldEnabled
		setting.PingIntervalSeconds = oldSeconds
	})

	releaseUpstream := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(releaseUpstream)
		})
	})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-releaseUpstream
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(upstream.Close)

	router := gin.New()
	router.POST("/relay", func(c *gin.Context) {
		req, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader("upstream body"))
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		info := &relaycommon.RelayInfo{
			IsStream:    true,
			ChannelMeta: &relaycommon.ChannelMeta{},
		}

		resp, err := doRequest(c, req, info, true)
		if err != nil {
			c.String(http.StatusBadGateway, err.Error())
			return
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		helper.Done(c)
	})
	downstream := httptest.NewServer(router)
	t.Cleanup(downstream.Close)

	responseCh := make(chan *http.Response, 1)
	errorCh := make(chan error, 1)
	go func() {
		resp, err := downstream.Client().Post(downstream.URL+"/relay", "application/json", strings.NewReader(`{}`))
		if err != nil {
			errorCh <- err
			return
		}
		responseCh <- resp
	}()

	var resp *http.Response
	select {
	case err := <-errorCh:
		require.NoError(t, err)
	case resp = <-responseCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for TTFB keepalive")
	}
	defer resp.Body.Close()

	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	reader := bufio.NewReader(resp.Body)
	firstLine, err := reader.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, ": PING\n", firstLine)
	blankLine, err := reader.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "\n", blankLine)

	releaseOnce.Do(func() {
		close(releaseUpstream)
	})
	rest, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Contains(t, string(rest), "data: [DONE]")
}
