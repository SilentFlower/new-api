package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetChannelBudgetUsage 返回某条预算行当前窗口的池子汇总或分页用户用量。
// @param c 管理员请求上下文；query 含 scope、page、page_size。
// @return 无，写入用量视图。
func GetChannelBudgetUsage(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	pageInfo, ok := getChannelUserLimitPage(c)
	if !ok {
		return
	}
	scope := c.DefaultQuery("scope", "user")
	view, err := service.GetChannelBudgetUsage(c, channel.Id, c.Param("budget_id"), scope, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		respondChannelPeriodPolicyError(c, err, false)
		return
	}
	userIDs := make([]int, len(view.Items))
	for i, item := range view.Items {
		userIDs[i] = item.UserID
	}
	users, err := model.GetUserLimitSummaries(userIDs)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("查询预算用量用户摘要失败: channel_id=%d error=%s", channel.Id, common.LocalLogPreview(err.Error())))
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		return
	}
	summaries := make(map[int]model.UserLimitSummary, len(users))
	for _, user := range users {
		summaries[user.ID] = user
	}
	for i := range view.Items {
		view.Items[i].Username, view.Items[i].DisplayName = summaries[view.Items[i].UserID].Username, summaries[view.Items[i].UserID].DisplayName
	}
	view.Page, view.PageSize = pageInfo.GetPage(), pageInfo.GetPageSize()
	common.ApiSuccess(c, view)
}

// SetChannelBudgetUsage 直接设置某条预算行当前窗口的已用额度；不改策略与其他行。
// @param c 管理员请求上下文；body 为 ChannelBudgetUsageInput。
// @return 无，写入调整结果。
func SetChannelBudgetUsage(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	var input dto.ChannelBudgetUsageInput
	if !bindChannelPeriodJSON(c, &input) {
		return
	}
	if input.Scope == "user" && !channelUserLimitUserExists(c, input.UserID) {
		return
	}
	budgetID := c.Param("budget_id")
	before, err := service.GetChannelBudgetUsage(c, channel.Id, budgetID, input.Scope, 0, 0)
	if err != nil {
		respondChannelPeriodPolicyError(c, err, false)
		return
	}
	beforeUsed := before.UsedQuota
	if input.Scope == "user" {
		beforeUsed, err = channelBudgetUserUsage(c, channel.Id, budgetID, input.UserID)
		if err != nil {
			respondChannelPeriodPolicyError(c, err, false)
			return
		}
	}
	if err := service.SetChannelBudgetUsage(c, channel.Id, budgetID, input); err != nil {
		respondChannelPeriodPolicyError(c, err, false)
		return
	}
	recordManageAudit(c, "channel.budget_usage_set", map[string]interface{}{"channel_id": channel.Id, "budget_id": budgetID, "scope": input.Scope, "user_id": input.UserID, "before": beforeUsed, "after": input.UsedQuota})
	common.ApiSuccess(c, gin.H{"channel_id": channel.Id, "budget_id": budgetID, "scope": input.Scope, "user_id": input.UserID, "used_quota": input.UsedQuota})
}

// channelBudgetUserUsage 读取审计所需的调整前用户已用额度，读取故障必须阻止后续调整。
func channelBudgetUserUsage(c *gin.Context, channelID int, budgetID string, userID int) (int64, error) {
	view, err := service.GetChannelBudgetUsage(c, channelID, budgetID, "user", 0, 1<<30)
	if err != nil {
		return 0, err
	}
	for _, item := range view.Items {
		if item.UserID == userID {
			return item.UsedQuota, nil
		}
	}
	return 0, nil
}

// SetChannelUserBudgetOverride 保存用户对某条个人预算行的提额，并回读完整有效状态。
// @param c 管理员请求上下文；body 为 ChannelBudgetUserOverrideInput。
// @return 无，写入统一状态。
func SetChannelUserBudgetOverride(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	userID, ok := getChannelUserLimitUserID(c)
	if !ok || !channelUserLimitUserExists(c, userID) {
		return
	}
	var input dto.ChannelBudgetUserOverrideInput
	if !bindChannelPeriodJSON(c, &input) {
		return
	}
	budgetID := c.Param("budget_id")
	previous, err := model.GetChannelUserBudgetOverride(c, channel.Id, userID, budgetID)
	if err != nil {
		respondChannelPeriodPolicyError(c, err, false)
		return
	}
	var before *dto.ChannelBudgetUserOverrideInput
	if previous != nil {
		before = &dto.ChannelBudgetUserOverrideInput{Limit: previous.QuotaLimit, ExpiresAt: previous.ExpiresAt}
	}
	if err := service.ReplaceChannelUserBudgetOverride(c, channel.Id, userID, budgetID, input, common.GetContextKeyInt(c, constant.ContextKeyUserId)); err != nil {
		respondChannelPeriodPolicyError(c, err, false)
		return
	}
	recordManageAudit(c, "channel.budget_user_override_set", map[string]interface{}{"channel_id": channel.Id, "user_id": userID, "budget_id": budgetID, "limit": input.Limit, "expires_at": input.ExpiresAt, "before": before, "after": input})
	status, err := buildChannelUserLimitStatus(c, channel, userID)
	if err != nil {
		respondChannelPeriodPolicyError(c, err, true)
		return
	}
	common.ApiSuccess(c, status)
}

// DeleteChannelUserBudgetOverride 撤销用户对某条预算行的提额，其他行不受影响。
// @param c 管理员请求上下文。
// @return 无，写入统一状态。
func DeleteChannelUserBudgetOverride(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	userID, ok := getChannelUserLimitUserID(c)
	if !ok || !channelUserLimitUserExists(c, userID) {
		return
	}
	budgetID := c.Param("budget_id")
	previous, err := model.GetChannelUserBudgetOverride(c, channel.Id, userID, budgetID)
	if err != nil {
		respondChannelPeriodPolicyError(c, err, false)
		return
	}
	var before *dto.ChannelBudgetUserOverrideInput
	if previous != nil {
		before = &dto.ChannelBudgetUserOverrideInput{Limit: previous.QuotaLimit, ExpiresAt: previous.ExpiresAt}
	}
	if err := service.DeleteChannelUserBudgetOverride(c, channel.Id, userID, budgetID); err != nil {
		respondChannelPeriodPolicyError(c, err, false)
		return
	}
	recordManageAudit(c, "channel.budget_user_override_delete", map[string]interface{}{"channel_id": channel.Id, "user_id": userID, "budget_id": budgetID, "before": before, "after": nil})
	status, err := buildChannelUserLimitStatus(c, channel, userID)
	if err != nil {
		respondChannelPeriodPolicyError(c, err, true)
		return
	}
	common.ApiSuccess(c, status)
}

// channelUserBudgetOverrideItem 是管理端行级提额列表项。
type channelUserBudgetOverrideItem struct {
	User       model.UserLimitSummary `json:"user"`
	BudgetID   string                 `json:"budget_id"`
	BudgetName string                 `json:"budget_name"`
	BaseLimit  int64                  `json:"base_limit"`
	Limit      int64                  `json:"limit"`
	ExpiresAt  int64                  `json:"expires_at"`
}

// GetChannelUserBudgetOverrides 分页返回渠道内当前有效的行级提额。
// @param c 管理员请求上下文。
// @return 无，写入分页列表。
func GetChannelUserBudgetOverrides(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	pageInfo, ok := getChannelUserLimitPage(c)
	if !ok {
		return
	}
	view, err := service.GetChannelPeriodPolicy(c, channel.Id)
	if err != nil {
		respondChannelPeriodPolicyError(c, err, false)
		return
	}
	rows := make(map[string]dto.ChannelBudgetRow, len(view.Config.Budgets))
	for _, row := range view.Config.Budgets {
		rows[row.ID] = row
	}
	overrides, total, err := model.ListActiveChannelUserBudgetOverridesPage(c, channel.Id, common.GetTimestamp(), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		respondChannelUserLimitServiceError(c, "list_user_budget_overrides", err)
		return
	}
	userIDs := make([]int, len(overrides))
	for i, item := range overrides {
		userIDs[i] = item.UserId
	}
	users, err := model.GetUserLimitSummaries(userIDs)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		return
	}
	summaries := make(map[int]model.UserLimitSummary, len(users))
	for _, user := range users {
		summaries[user.ID] = user
	}
	items := make([]channelUserBudgetOverrideItem, 0, len(overrides))
	for _, item := range overrides {
		row := rows[item.BudgetId]
		items = append(items, channelUserBudgetOverrideItem{User: summaries[item.UserId], BudgetID: item.BudgetId, BudgetName: row.Name, BaseLimit: row.Limit, Limit: item.QuotaLimit, ExpiresAt: item.ExpiresAt})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"channel_id": channel.Id, "page": pageInfo.GetPage(), "page_size": pageInfo.GetPageSize(), "total": total, "items": items}})
}
