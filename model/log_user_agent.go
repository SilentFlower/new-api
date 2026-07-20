package model

import "github.com/gin-gonic/gin"

func appendRequestUserAgent(c *gin.Context, other map[string]interface{}) map[string]interface{} {
	userAgent := ""
	if c != nil && c.Request != nil {
		userAgent = c.Request.Header.Get("User-Agent")
	}

	adminInfo, hasAdminInfo := other["admin_info"].(map[string]interface{})
	if userAgent == "" {
		// 请求头缺失时移除调用方预置值，避免把非入站 UA 写入审计日志。
		if hasAdminInfo && adminInfo != nil {
			delete(adminInfo, "user_agent")
		}
		return other
	}

	if other == nil {
		other = make(map[string]interface{})
	}
	if !hasAdminInfo || adminInfo == nil {
		adminInfo = make(map[string]interface{})
		other["admin_info"] = adminInfo
	}
	adminInfo["user_agent"] = userAgent
	return other
}
