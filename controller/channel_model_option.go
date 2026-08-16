package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetChannelModelOptions 返回管理端可选的启用渠道和模型，不包含敏感字段。
//
// @param c Gin 请求上下文。
// @return 通过 HTTP 响应返回精简渠道列表或数据库错误。
func GetChannelModelOptions(c *gin.Context) {
	options, err := model.ListEnabledChannelModelOptions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, options)
}
