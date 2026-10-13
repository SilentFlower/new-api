package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareResponsesReasoningHeaderStopsUncommittedJSONKeepAlive(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ticks := make(chan time.Time)
	stop := startNonStreamKeepAlive(c, ticks, func() {})
	t.Cleanup(stop)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAIResponses, RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiType: constant.APITypeOpenAI, ChannelSetting: dto.ChannelSettings{ResponsesReasoningIncludedEnabled: true},
		},
	}
	PrepareResponsesReasoningHeader(c, info)
	// 停止函数等待保活协程退出，验证响应头仍可由后续成功响应决定。
	stop()
	assert.False(t, c.Writer.Written())
	resp := &http.Response{StatusCode: http.StatusOK}
	SupplementResponsesReasoningHeader(c, info, resp)
	require.Equal(t, "true", resp.Header.Get("X-Reasoning-Included"))
}
