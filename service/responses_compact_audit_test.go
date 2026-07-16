package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type responsesCompactAuditBillingStub struct {
	settled int
}

// Settle 记录消费日志触发的结算次数。
// @param actualQuota 实际结算额度。
// @return 始终返回 nil。
func (s *responsesCompactAuditBillingStub) Settle(actualQuota int) error {
	s.settled++
	return nil
}

// Refund 忽略测试中的退款调用。
// @param c 当前 Gin 请求上下文。
func (s *responsesCompactAuditBillingStub) Refund(c *gin.Context) {}

// NeedsRefund 返回测试会话无需退款。
// @return 始终返回 false。
func (s *responsesCompactAuditBillingStub) NeedsRefund() bool { return false }

// GetPreConsumedQuota 返回测试会话的预扣额度。
// @return 固定返回零。
func (s *responsesCompactAuditBillingStub) GetPreConsumedQuota() int { return 0 }

// Reserve 接受测试会话的目标预扣额度。
// @param targetQuota 目标预扣额度。
// @return 始终返回 nil。
func (s *responsesCompactAuditBillingStub) Reserve(targetQuota int) error { return nil }

func TestResponsesCompactAuditMergesIntoAdminInfoWithoutSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses?api_key=client-secret", nil)
	common.SetContextKey(c, constant.ContextKeyChannelName, "compact-upstream")
	c.Set(common.UpstreamRequestIdKey, "upstream-request-id")
	info := &relaycommon.RelayInfo{
		ResponsesCompactMode:   relayconstant.ResponsesCompactModeV2WebSocket,
		RequestURLPath:         "/v1/responses?client=secret",
		UpstreamRequestURLPath: "https://upstream.example/v1/responses?api_key=upstream-secret",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   17,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}

	SetResponsesCompactAudit(c, info, "completed")
	other := map[string]interface{}{
		"admin_info": map[string]interface{}{"use_channel": []string{"17"}},
	}
	MergeContextLogOther(c, other)

	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, []string{"17"}, adminInfo["use_channel"])
	audit, ok := adminInfo[responsesCompactAuditKey].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, string(relayconstant.ResponsesCompactModeV2WebSocket), audit["mode"])
	assert.Equal(t, "/v1/responses", audit["inbound_path"])
	assert.Equal(t, "/v1/responses", audit["upstream_path"])
	assert.Equal(t, 17, audit["channel_id"])
	assert.Equal(t, "compact-upstream", audit["channel_name"])
	assert.Equal(t, "completed", audit["outcome"])
	assert.Equal(t, "upstream-request-id", audit["upstream_request_id"])
	assert.NotContains(t, common.MapToJsonStr(other), "secret")
}

func TestClearResponsesCompactAuditPreservesOtherLogFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyLogOther, map[string]interface{}{
		"vision_assist_failure_reason": "timeout",
		"admin_info": map[string]interface{}{
			responsesCompactAuditKey: map[string]interface{}{"mode": "v2_websocket"},
			"quota_saturation":       map[string]interface{}{"kind": "overflow"},
		},
	})

	ClearResponsesCompactAudit(c)

	logOther, ok := common.GetContextKeyType[map[string]interface{}](c, constant.ContextKeyLogOther)
	require.True(t, ok)
	assert.Equal(t, "timeout", logOther["vision_assist_failure_reason"])
	adminInfo, ok := logOther["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.NotContains(t, adminInfo, responsesCompactAuditKey)
	assert.Contains(t, adminInfo, "quota_saturation")
}

func TestResponsesCompactAuditPersistsInConsumeLog(t *testing.T) {
	truncate(t)
	seedUser(t, 911, 1000)
	seedChannel(t, 912)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses?api_key=client-secret", nil)
	c.Set("username", "compact-user")
	c.Set("token_name", "compact-token")
	common.SetContextKey(c, constant.ContextKeyChannelName, "compact-channel")
	c.Set(common.RequestIdKey, "compact-consume-request")
	c.Set(common.UpstreamRequestIdKey, "standard-upstream-request-id")
	now := time.Now()
	billing := &responsesCompactAuditBillingStub{}
	info := &relaycommon.RelayInfo{
		UserId:                 911,
		TokenId:                913,
		UsingGroup:             "default",
		OriginModelName:        "compact-audit-model",
		ResponsesCompactMode:   relayconstant.ResponsesCompactModeV2HTTP,
		RequestURLPath:         "/v1/responses",
		UpstreamRequestURLPath: "https://upstream.example/v1/responses?api_key=upstream-secret",
		StartTime:              now,
		FirstResponseTime:      now,
		IsStream:               true,
		Billing:                billing,
		PriceData: types.PriceData{
			ModelRatio: 0,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         912,
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "compact-audit-model",
		},
	}
	SetResponsesCompactAudit(c, info, "completed")

	PostTextConsumeQuota(c, info, &dto.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}, nil)

	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("request_id = ?", "compact-consume-request").Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, 1, billing.settled)
	assert.Equal(t, "standard-upstream-request-id", logs[0].UpstreamRequestId)
	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	audit, ok := adminInfo[responsesCompactAuditKey].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "completed", audit["outcome"])
	assert.Equal(t, "/v1/responses", audit["upstream_path"])
	assert.NotContains(t, logs[0].Other, "secret")
}
