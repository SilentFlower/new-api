package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type issueWorkflowTokenRequest struct {
	UserID     int    `json:"user_id"`
	OldTokenID int    `json:"old_token_id"`
	WorkflowID string `json:"workflow_id"`
}

type retireWorkflowTokenRequest struct {
	UserID             int `json:"user_id"`
	OldTokenID         int `json:"old_token_id"`
	ReplacementTokenID int `json:"replacement_token_id"`
}

// IssueWorkflowToken 为已迁移的工作流用户签发一枚替代令牌。
// @param c 已通过 RootAuth 的 Gin 请求上下文。
func IssueWorkflowToken(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		common.ApiErrorI18n(c, i18n.MsgTokenMigrateForbidden)
		return
	}
	var request issueWorkflowTokenRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	token, err := model.IssueWorkflowReplacementToken(request.UserID, request.OldTokenID, request.WorkflowID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"token_id": token.Id, "key": token.GetFullKey()})
}

// RetireWorkflowTokenHandler 在 Secret 切换后停用旧工作流令牌。
// @param c 已通过 RootAuth 的 Gin 请求上下文。
func RetireWorkflowTokenHandler(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		common.ApiErrorI18n(c, i18n.MsgTokenMigrateForbidden)
		return
	}
	var request retireWorkflowTokenRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := model.RetireWorkflowToken(request.UserID, request.OldTokenID, request.ReplacementTokenID); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"retired_token_id": request.OldTokenID})
}
