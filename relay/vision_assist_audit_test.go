package relay

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCaptureVisionAssistMessageAuditUsesPreparedProtocolRequest(t *testing.T) {
	tests := []struct {
		name     string
		protocol types.RelayFormat
		path     string
		request  dto.Request
	}{
		{
			name:     "OpenAI Chat",
			protocol: types.RelayFormatOpenAI,
			path:     "/v1/chat/completions",
			request:  &dto.GeneralOpenAIRequest{Model: "vision-chat"},
		},
		{
			name:     "OpenAI Responses",
			protocol: types.RelayFormatOpenAIResponses,
			path:     "/v1/responses",
			request:  &dto.OpenAIResponsesRequest{Model: "vision-responses"},
		},
		{
			name:     "Claude Messages",
			protocol: types.RelayFormatClaude,
			path:     "/v1/messages",
			request:  &dto.ClaudeRequest{Model: "vision-claude"},
		},
		{
			name:     "Gemini Native",
			protocol: types.RelayFormatGemini,
			path:     "/v1beta/models/vision-gemini:generateContent",
			request: &dto.GeminiChatRequest{Contents: []dto.GeminiChatContent{{
				Role:  "user",
				Parts: []dto.GeminiPart{{Text: "描述图片"}},
			}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("username", "audit-user")
			c.Set("token_name", "audit-token")
			startedAt := time.Now().Add(-time.Second)
			parent := &relaycommon.RelayInfo{RequestId: "main-request"}
			assistInfo := &relaycommon.RelayInfo{
				RequestId:       "assist-request",
				UserId:          21,
				TokenId:         22,
				OriginModelName: "vision-origin",
				RequestURLPath:  test.path,
				RelayFormat:     test.protocol,
				StartTime:       startedAt,
			}
			assistInfo.FreezeBillingModelName("vision-mapped")
			var captureInput service.MessageAuditCaptureInput
			var finalizeInput service.MessageAuditFinalizeInput
			writer := visionAssistMessageAuditWriter{
				capture: func(input service.MessageAuditCaptureInput) bool {
					captureInput = input
					return true
				},
				finalize: func(input service.MessageAuditFinalizeInput) {
					finalizeInput = input
				},
			}

			audit := captureVisionAssistMessageAuditWithWriter(c, parent, assistInfo, test.request, writer)

			require.True(t, audit.captured)
			assert.Equal(t, "assist-request", captureInput.RequestID)
			assert.Equal(t, service.MessageAuditRequestKindVisionAssist, captureInput.RequestKind)
			assert.Equal(t, "main-request", captureInput.RelatedRequestID)
			assert.Equal(t, "audit-user", captureInput.Username)
			assert.Equal(t, "audit-token", captureInput.TokenName)
			assert.Equal(t, test.path, captureInput.RequestPath)
			assert.Equal(t, test.protocol, captureInput.Protocol)
			assert.True(t, captureInput.Standalone)
			assert.Equal(t, test.request, captureInput.Request)
			assert.Equal(t, startedAt, captureInput.CapturedAt)

			audit.finalize(nil)
			assert.Equal(t, "assist-request", finalizeInput.RequestID)
			assert.Equal(t, "vision-mapped", finalizeInput.ModelName)
			assert.Equal(t, "succeeded", finalizeInput.Status)
			assert.Equal(t, http.StatusOK, finalizeInput.HTTPStatus)
			assert.GreaterOrEqual(t, finalizeInput.Duration, time.Second)
		})
	}
}

func TestVisionAssistMessageAuditFinalizesFailureAndSkipsUnavailableCapture(t *testing.T) {
	assistInfo := &relaycommon.RelayInfo{OriginModelName: "vision-model"}
	assistInfo.FreezeBillingModelName("vision-model")
	var failureInput service.MessageAuditFinalizeInput
	audit := visionAssistMessageAudit{
		captured:  true,
		requestID: "failed-assist-request",
		startedAt: time.Now(),
		info:      assistInfo,
		finalizeMessageAudit: func(input service.MessageAuditFinalizeInput) {
			failureInput = input
		},
	}
	apiErr := types.NewError(
		errors.New("upstream failed"),
		types.ErrorCodeDoRequestFailed,
		types.ErrOptionWithStatusCode(http.StatusBadGateway),
	)

	audit.finalize(apiErr)

	assert.Equal(t, "failed", failureInput.Status)
	assert.Equal(t, string(types.ErrorCodeDoRequestFailed), failureInput.ErrorCode)
	assert.Equal(t, http.StatusBadGateway, failureInput.HTTPStatus)

	finalizeCalls := 0
	unavailable := visionAssistMessageAudit{
		captured: false,
		finalizeMessageAudit: func(service.MessageAuditFinalizeInput) {
			finalizeCalls++
		},
	}
	unavailable.finalize(nil)
	assert.Zero(t, finalizeCalls)
}
