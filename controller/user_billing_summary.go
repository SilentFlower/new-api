package controller

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetAdminUserBillingSummaries 返回管理员指定用户的批量账务摘要。
//
// @param c Gin 请求上下文。
// @return 无。
func GetAdminUserBillingSummaries(c *gin.Context) {
	var request dto.AdminUserBillingSummaryRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	response, err := service.GetAdminUserBillingSummaries(c.Request.Context(), request)
	if err == nil {
		common.ApiSuccess(c, response)
		return
	}
	switch {
	case errors.Is(err, service.ErrAdminUserBillingSummaryInvalidRequest):
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
	case errors.Is(err, service.ErrAdminUserBillingSummaryBatchTooLarge):
		common.ApiErrorI18n(c, i18n.MsgBatchTooMany, map[string]any{"Max": service.AdminUserBillingSummaryMaxUsers})
	default:
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"批量查询用户账务摘要失败: %s",
			common.LocalLogPreview(err.Error()),
		))
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
	}
}
