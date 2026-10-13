package channel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoRequestResponsesReasoningHeaderWaitsForUpstreamBeforeCommitting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	setting := operation_setting.GetGeneralSetting()
	oldEnabled, oldSeconds := setting.PingIntervalEnabled, setting.PingIntervalSeconds
	setting.PingIntervalEnabled, setting.PingIntervalSeconds = true, 1
	t.Cleanup(func() { setting.PingIntervalEnabled, setting.PingIntervalSeconds = oldEnabled, oldSeconds })

	releaseUpstream := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseUpstream) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-releaseUpstream:
			w.Header().Set("X-Reasoning-Included", "false")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(upstream.Close)
	t.Cleanup(release)
	router := gin.New()
	router.POST("/v1/responses", func(c *gin.Context) {
		info := &relaycommon.RelayInfo{
			IsStream: true, RelayMode: relayconstant.RelayModeResponses, RelayFormat: types.RelayFormatOpenAIResponses,
			ChannelMeta: &relaycommon.ChannelMeta{
				ApiType: constant.APITypeOpenAI, ChannelSetting: dto.ChannelSettings{ResponsesReasoningIncludedEnabled: true},
			},
		}
		helper.PrepareResponsesReasoningHeader(c, info)
		req, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader(`{}`))
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		resp, err := doRequest(c, req, info, true)
		if err != nil {
			c.Status(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		helper.SupplementResponsesReasoningHeader(c, info, resp)
		c.Header("X-Reasoning-Included", resp.Header.Get("X-Reasoning-Included"))
		_, _ = io.Copy(c.Writer, resp.Body)
	})
	downstream := httptest.NewServer(router)
	t.Cleanup(downstream.Close)
	t.Cleanup(release)
	responses := make(chan *http.Response, 1)
	errors := make(chan error, 1)
	go func() {
		resp, err := downstream.Client().Post(downstream.URL+"/v1/responses", "application/json", strings.NewReader(`{}`))
		if err != nil {
			errors <- err
			return
		}
		responses <- resp
	}()

	// 上游响应头阻塞跨过一个真实保活周期，验证客户端不会先收到无标识的响应。
	timer := time.NewTimer(1500 * time.Millisecond)
	defer timer.Stop()
	select {
	case resp := <-responses:
		_ = resp.Body.Close()
		t.Fatal("上游尚未返回时提前提交了响应头")
	case err := <-errors:
		t.Fatal(err)
	case <-timer.C:
		release()
	}
	select {
	case resp := <-responses:
		defer resp.Body.Close()
		assert.Equal(t, "false", resp.Header.Get("X-Reasoning-Included"))
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "data: [DONE]\n\n", string(body))
	case err := <-errors:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("未收到最终响应")
	}
}
