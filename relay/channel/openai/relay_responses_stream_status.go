package openai

import (
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
)

// responsesStreamStatusObserver 区分 Responses 业务终态与 SSE 传输结束标记。
// 状态仅由串行事件回调更新，扫描器退出后再检查完整性。
type responsesStreamStatusObserver struct {
	completed bool
	failed    bool
}

// observe 记录明确的业务终态，避免先到达的传输结束标记掩盖失败。
func (o *responsesStreamStatusObserver) observe(status *relaycommon.StreamStatus, event dto.ResponsesStreamResponse) {
	var failure string
	switch event.Type {
	case "response.failed", "response.incomplete", "response.cancelled", "response.canceled", "response.error", "error":
		failure = "上游 Responses 流异常终态：" + event.Type
	case "response.completed", "response.done":
		if event.Response == nil || !relaycommon.IsNonBillableResponsesStatus(event.Response.Status) {
			o.completed = true
			return
		}
		failure = "上游 Responses 完成事件包含异常状态：" + event.Type
	default:
		return
	}
	if o.failed {
		return
	}
	o.failed = true
	// 扫描器和回调可能争抢 SetEndReason，业务异常必须独立保存在错误记录中。
	status.RecordError(failure)
}

// finalize 在扫描回调全部退出后补充缺失终态，保留既有取消或读取错误的归因。
func (o *responsesStreamStatusObserver) finalize(c *gin.Context, info *relaycommon.RelayInfo) {
	if o.completed || o.failed || info == nil || info.StreamStatus == nil {
		return
	}
	status := info.StreamStatus
	if !status.IsNormalEnd() || status.HasErrors() {
		return
	}
	const message = "上游 Responses 流已结束，但缺少 response.completed 或 response.done 完成事件"
	status.RecordError(message)
	logger.LogWarn(c, message)
}
