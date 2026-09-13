package controller

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetChannelBudgetUsageSummary 一次返回渠道全部已保存预算行的当前窗口用量摘要，供预算表与用量页签单次加载。
// @param c 管理员请求上下文。
// @return 无，写入摘要视图。
func GetChannelBudgetUsageSummary(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	view, err := service.GetChannelBudgetUsageSummary(c, channel)
	if err != nil {
		respondChannelPeriodPolicyError(c, err, false)
		return
	}
	userIDs := make([]int, 0, len(view.Items))
	for _, item := range view.Items {
		if item.TopUser != nil {
			userIDs = append(userIDs, item.TopUser.UserID)
		}
	}
	users, err := model.GetUserLimitSummaries(userIDs)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("查询预算用量摘要用户失败: channel_id=%d error=%s", channel.Id, common.LocalLogPreview(err.Error())))
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		return
	}
	summaries := make(map[int]model.UserLimitSummary, len(users))
	for _, user := range users {
		summaries[user.ID] = user
	}
	for _, item := range view.Items {
		if item.TopUser != nil {
			item.TopUser.Username, item.TopUser.DisplayName = summaries[item.TopUser.UserID].Username, summaries[item.TopUser.UserID].DisplayName
		}
	}
	common.ApiSuccess(c, view)
}
