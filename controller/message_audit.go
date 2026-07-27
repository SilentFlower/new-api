package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// GetMessageAudits 返回 root 可见且不包含正文的消息审计分页列表。
//
// 参数由 Gin 查询字符串提供。
// 返回值使用统一的 success/message/data 管理 API 契约。
func GetMessageAudits(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	userID, _ := strconv.Atoi(c.Query("user_id"))
	tokenID, _ := strconv.Atoi(c.Query("token_id"))
	requests, total, err := service.ListMessageAudits(model.MessageAuditListFilter{
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		UserID:         userID,
		Username:       c.Query("username"),
		TokenID:        tokenID,
		TokenName:      c.Query("token_name"),
		ModelName:      c.Query("model_name"),
		RequestID:      c.Query("request_id"),
		RequestPath:    c.Query("request_path"),
		Status:         c.Query("status"),
		AuditSessionID: c.Query("audit_session_id"),
		Offset:         pageInfo.GetStartIdx(),
		Limit:          pageInfo.GetPageSize(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(requests)
	common.ApiSuccess(c, pageInfo)
}

// GetMessageAuditStatus 返回当前节点的审计配置和异步队列指标。
func GetMessageAuditStatus(c *gin.Context) {
	common.ApiSuccess(c, service.GetMessageAuditStatus())
}

// GetMessageAuditDetail 按请求 ID 解密并返回有序入站消息。
//
// 每次成功或失败尝试都会写管理审计日志，且日志不包含消息正文。
func GetMessageAuditDetail(c *gin.Context) {
	requestID := c.Param("request_id")
	if requestID == "" {
		recordManageAudit(c, "message_audit.detail_view", map[string]interface{}{"request_id": "", "success": false})
		common.ApiErrorMsg(c, "request id is required")
		return
	}
	detail, err := service.GetMessageAuditDetail(requestID)
	recordManageAudit(c, "message_audit.detail_view", map[string]interface{}{
		"request_id": requestID,
		"success":    err == nil,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, detail)
}

// CreateMessageAuditCleanupSystemTask 创建或复用当前的一键清空异步任务。
func CreateMessageAuditCleanupSystemTask(c *gin.Context) {
	task, created, err := service.StartMessageAuditCleanupTask(time.Now().UnixNano())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"task":    task.ToResponse(),
			"created": created,
		},
	})
}
