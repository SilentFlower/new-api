package controller

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

type tokenLeakScanTaskPayload struct {
	TokenID int `json:"token_id,omitempty"`
}

type tokenLeakScanHandler struct{}

// Type 返回定时泄露扫描任务类型。
func (tokenLeakScanHandler) Type() string { return model.SystemTaskTypeTokenLeakScan }

// Enabled 判断当前是否允许调度新的定时扫描任务。
func (tokenLeakScanHandler) Enabled() bool {
	if !operation_setting.GetTokenLeakScanSetting().Enabled {
		return false
	}
	if service.ValidateTokenLeakScanConfiguration() != nil {
		return false
	}
	active, err := service.HasActiveTokenLeakScanTask()
	return err == nil && !active
}

// Interval 返回定时泄露扫描的调度间隔。
func (tokenLeakScanHandler) Interval() time.Duration {
	hours := operation_setting.GetTokenLeakScanSetting().IntervalHours
	return time.Duration(hours) * time.Hour
}

// NewPayload 返回定时全量扫描的空任务参数。
func (tokenLeakScanHandler) NewPayload() any { return tokenLeakScanTaskPayload{} }

// Run 执行已领取的定时泄露扫描任务。
func (tokenLeakScanHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	runTokenLeakScanSystemTask(ctx, task, runnerID)
}

type tokenLeakScanManualHandler struct{}

// Type 返回手动泄露扫描任务类型。
func (tokenLeakScanManualHandler) Type() string { return model.SystemTaskTypeTokenLeakScanManual }

// Run 执行已领取的手动泄露扫描任务。
func (tokenLeakScanManualHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	runTokenLeakScanSystemTask(ctx, task, runnerID)
}

func runTokenLeakScanSystemTask(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := tokenLeakScanTaskPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, errors.New("payload_invalid"))
		return
	}
	summary, err := service.RunTokenLeakScan(ctx, payload.TokenID, service.NewSystemTaskProgressReporter(task, runnerID))
	if errors.Is(err, service.ErrTokenLeakScanDisabled) {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
		return
	}
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, summary, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

// GetTokenLeakScanStatus 返回 root 管理端的泄露扫描配置、队列与任务概览。
//
// @param c Gin 请求上下文。
// @return 无。
func GetTokenLeakScanStatus(c *gin.Context) {
	status, err := service.GetTokenLeakScanStatus()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, status)
}

// GetTokenLeakScanFindings 分页返回 root 可见的公开泄露位置。
//
// @param c Gin 请求上下文。
// @return 无。
func GetTokenLeakScanFindings(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	status := c.Query("status")
	if status != "" && status != model.TokenLeakFindingStatusOpen && status != model.TokenLeakFindingStatusMitigated {
		common.ApiErrorMsg(c, "无效的泄露状态")
		return
	}
	findings, err := service.ListTokenLeakFindingViews(status, page, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, findings)
}

// CreateTokenLeakScanTask 创建或复用一个手动全量/单 token 扫描任务。
//
// @param c Gin 请求上下文。
// @return 无。
func CreateTokenLeakScanTask(c *gin.Context) {
	payload := tokenLeakScanTaskPayload{}
	if err := common.DecodeJson(c.Request.Body, &payload); err != nil {
		common.ApiErrorMsg(c, "无效的扫描参数")
		return
	}
	task, created, err := service.StartTokenLeakScanTask(payload.TokenID)
	if err != nil {
		common.ApiErrorMsg(c, tokenLeakScanErrorMessage(err))
		return
	}
	common.ApiSuccess(c, gin.H{"task": task.ToResponse(), "created": created})
}

// DisableTokenLeakFindingToken 禁用泄露位置对应令牌并记录管理审计。
//
// @param c Gin 请求上下文。
// @return 无。
func DisableTokenLeakFindingToken(c *gin.Context) {
	findingID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || findingID <= 0 {
		common.ApiErrorMsg(c, "无效的泄露记录 ID")
		return
	}
	tokenID, userID, err := service.DisableTokenLeakFindingToken(findingID)
	if err != nil {
		common.ApiErrorMsg(c, tokenLeakScanErrorMessage(err))
		return
	}
	recordManageAuditFor(c, userID, "token_leak.disable", map[string]interface{}{
		"finding_id": findingID,
		"token_id":   tokenID,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"token_id": tokenID,
			"status":   common.TokenStatusDisabled,
		},
	})
}

func tokenLeakScanErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	switch err.Error() {
	case service.ErrTokenLeakScanDisabled.Error():
		return "GitHub Key 泄露扫描尚未启用"
	case "github_token_missing":
		return "未配置 GitHub 扫描凭据"
	case "scan_secret_invalid":
		return "未配置至少 32 字节的扫描 HMAC 密钥"
	case "token_id_invalid":
		return "无效的 Token ID"
	case "token_not_found":
		return "Token 不存在或已删除"
	case "finding_id_invalid", "finding_not_found":
		return "泄露记录不存在"
	default:
		return "Key 泄露扫描操作失败"
	}
}
