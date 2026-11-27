package model

import "github.com/gin-gonic/gin"

// requestLogOtherWithUserAgent 将真实入站 User-Agent 仅保存到管理员日志字段。
func requestLogOtherWithUserAgent(c *gin.Context, other *LogOther) *LogOther {
	if c == nil || c.Request == nil || c.Request.UserAgent() == "" {
		if other != nil {
			delete(other.adminInfo, "user_agent")
		}
		return other
	}
	if other == nil {
		other = NewLogOther()
	}
	other.SetAdmin("user_agent", c.Request.UserAgent())
	return other
}
