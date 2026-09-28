package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetWorkflowAccountStatusHandler 查询工作流用户与同名令牌的非敏感状态。
// @param c 已通过 RootAuth 的 Gin 请求上下文。
func GetWorkflowAccountStatusHandler(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		common.ApiErrorI18n(c, i18n.MsgTokenMigrateForbidden)
		return
	}
	username := c.Query("username")
	workflowID := c.Query("workflow_id")
	if model.ValidateMigrationUsername(username) != nil || workflowID == "" || len([]rune(workflowID)) > model.TokenNameMaxLength {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	status, err := model.GetWorkflowAccountStatus(username, workflowID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, status)
}
