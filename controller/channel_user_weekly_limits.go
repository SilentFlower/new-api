package controller

import (
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type channelUserWeeklyQuotaItem struct {
	UserID            int    `json:"user_id"`
	Username          string `json:"username"`
	DisplayName       string `json:"display_name"`
	UsedQuota         int64  `json:"used_quota"`
	BaseLimit         int    `json:"base_limit"`
	OverrideLimit     *int   `json:"override_limit,omitempty"`
	OverrideExpiresAt int64  `json:"override_expires_at"`
	Limit             int    `json:"limit"`
	RemainingQuota    int64  `json:"remaining_quota"`
}

// GetChannelUserWeeklyQuota 获取指定渠道当前自然周的用户额度使用情况。
//
// @param c Gin 请求上下文。
// @return 无。
func GetChannelUserWeeklyQuota(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	pageInfo, ok := getChannelUserLimitPage(c)
	if !ok {
		return
	}
	usage, resetAt, storageMode, err := service.ListChannelUserWeeklyQuota(c.Request.Context(), channel.Id)
	if err != nil {
		respondChannelUserLimitServiceError(c, "list_weekly_quota", err)
		return
	}
	start, end := channelUserLimitPageRange(pageInfo, len(usage))
	pageUsage := usage[start:end]
	userIDs := make([]int, len(pageUsage))
	for index, item := range pageUsage {
		userIDs[index] = item.UserID
	}
	users, err := model.GetUserLimitSummaries(userIDs)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("查询渠道每周额度用户摘要失败: channel_id=%d error=%s", channel.Id, common.LocalLogPreview(err.Error())))
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		return
	}
	userMap := make(map[int]model.UserLimitSummary, len(users))
	for _, user := range users {
		userMap[user.ID] = user
	}
	items := make([]channelUserWeeklyQuotaItem, 0, len(pageUsage))
	for _, item := range pageUsage {
		limits, resolveErr := service.ResolveChannelUserEffectiveLimits(c.Request.Context(), channel, item.UserID)
		if resolveErr != nil {
			respondChannelUserLimitServiceError(c, "resolve_weekly_quota_override", resolveErr)
			return
		}
		items = append(items, channelUserWeeklyQuotaItem{
			UserID:            item.UserID,
			Username:          userMap[item.UserID].Username,
			DisplayName:       userMap[item.UserID].DisplayName,
			UsedQuota:         item.UsedQuota,
			BaseLimit:         limits.BaseWeeklyQuota,
			OverrideLimit:     limits.OverrideWeeklyQuota,
			OverrideExpiresAt: limits.ExpiresAt,
			Limit:             limits.EffectiveWeeklyQuota,
			RemainingQuota:    remainingChannelUserLimit(limits.EffectiveWeeklyQuota, item.UsedQuota),
		})
	}
	common.ApiSuccess(c, channelUserLimitListResponse{
		ChannelID:   channel.Id,
		Limit:       channel.GetUserWeeklyQuotaLimit(),
		StorageMode: storageMode,
		ResetAt:     resetAt,
		Page:        pageInfo.GetPage(),
		PageSize:    pageInfo.GetPageSize(),
		Total:       len(usage),
		Items:       items,
	})
}

// SetChannelUserWeeklyQuota 设置指定用户当前自然周的已使用额度。
//
// @param c Gin 请求上下文。
// @return 无。
func SetChannelUserWeeklyQuota(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	userID, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || userID <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidId)
		return
	}
	var request setChannelUserDailyQuotaRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.UsedQuota == nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if *request.UsedQuota < 0 || *request.UsedQuota > common.MaxQuota {
		common.ApiErrorI18n(c, i18n.MsgQuotaExceedMax)
		return
	}
	if !channelUserLimitUserExists(c, userID) {
		return
	}
	if err := service.SetChannelUserWeeklyQuota(c.Request.Context(), channel.Id, userID, *request.UsedQuota); err != nil {
		respondChannelUserLimitServiceError(c, "set_weekly_quota", err)
		return
	}
	recordManageAudit(c, "channel.user_weekly_quota_set", map[string]interface{}{
		"channel_id": channel.Id,
		"user_id":    userID,
		"used_quota": *request.UsedQuota,
	})
	common.ApiSuccess(c, gin.H{"channel_id": channel.Id, "user_id": userID, "used_quota": *request.UsedQuota})
}
