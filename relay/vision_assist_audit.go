package relay

import (
	"net/http"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type visionAssistMessageAudit struct {
	captured             bool
	requestID            string
	startedAt            time.Time
	info                 *relaycommon.RelayInfo
	finalizeMessageAudit func(service.MessageAuditFinalizeInput)
}

type visionAssistMessageAuditWriter struct {
	capture  func(service.MessageAuditCaptureInput) bool
	finalize func(service.MessageAuditFinalizeInput)
}

func captureVisionAssistMessageAuditWithWriter(c *gin.Context, parent *relaycommon.RelayInfo, assistInfo *relaycommon.RelayInfo, request dto.Request, writer visionAssistMessageAuditWriter) visionAssistMessageAudit {
	if c == nil || parent == nil || assistInfo == nil || request == nil {
		return visionAssistMessageAudit{}
	}
	if writer.capture == nil || writer.finalize == nil {
		return visionAssistMessageAudit{}
	}
	captured := writer.capture(service.MessageAuditCaptureInput{
		RequestID:        assistInfo.RequestId,
		RequestKind:      service.MessageAuditRequestKindVisionAssist,
		RelatedRequestID: parent.RequestId,
		UserID:           assistInfo.UserId,
		Username:         c.GetString("username"),
		TokenID:          assistInfo.TokenId,
		TokenName:        c.GetString("token_name"),
		ModelName:        assistInfo.OriginModelName,
		RequestPath:      assistInfo.RequestURLPath,
		Protocol:         assistInfo.RelayFormat,
		IsStream:         assistInfo.IsStream,
		Standalone:       true,
		CapturedAt:       assistInfo.StartTime,
		Request:          request,
	})
	return visionAssistMessageAudit{
		captured:             captured,
		requestID:            assistInfo.RequestId,
		startedAt:            assistInfo.StartTime,
		info:                 assistInfo,
		finalizeMessageAudit: writer.finalize,
	}
}

func (audit visionAssistMessageAudit) finalize(apiErr *types.NewAPIError) {
	if !audit.captured || audit.finalizeMessageAudit == nil {
		return
	}
	status := "succeeded"
	errorCode := ""
	httpStatus := http.StatusOK
	if apiErr != nil {
		status = "failed"
		errorCode = string(apiErr.GetErrorCode())
		httpStatus = apiErr.StatusCode
		if httpStatus == 0 {
			httpStatus = http.StatusInternalServerError
		}
	}
	audit.finalizeMessageAudit(service.MessageAuditFinalizeInput{
		RequestID:  audit.requestID,
		ModelName:  service.ConsumeLogModelName(audit.info),
		Status:     status,
		ErrorCode:  errorCode,
		HTTPStatus: httpStatus,
		Duration:   time.Since(audit.startedAt),
	})
}
