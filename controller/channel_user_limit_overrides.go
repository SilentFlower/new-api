package controller

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type channelUserLimitMetric struct {
	BaseLimit      int    `json:"base_limit"`
	OverrideLimit  *int   `json:"override_limit,omitempty"`
	EffectiveLimit int    `json:"effective_limit"`
	Current        int64  `json:"current"`
	Remaining      int64  `json:"remaining"`
	ResetAt        int64  `json:"reset_at,omitempty"`
	StorageMode    string `json:"storage_mode"`
}

type channelUserLimitStatusResponse struct {
	ChannelID         int                    `json:"channel_id"`
	User              model.UserLimitSummary `json:"user"`
	Concurrency       channelUserLimitMetric `json:"concurrency"`
	DailyQuota        channelUserLimitMetric `json:"daily_quota"`
	WeeklyQuota       channelUserLimitMetric `json:"weekly_quota"`
	OverrideActive    bool                   `json:"override_active"`
	OverrideExpiresAt int64                  `json:"override_expires_at"`
}

type channelUserLimitOverrideItem struct {
	User                      model.UserLimitSummary `json:"user"`
	UserConcurrencyLimit      *int                   `json:"user_concurrency_limit,omitempty"`
	UserDailyQuotaLimit       *int                   `json:"user_daily_quota_limit,omitempty"`
	UserWeeklyQuotaLimit      *int                   `json:"user_weekly_quota_limit,omitempty"`
	EffectiveConcurrencyLimit int                    `json:"effective_concurrency_limit"`
	EffectiveDailyQuotaLimit  int                    `json:"effective_daily_quota_limit"`
	EffectiveWeeklyQuotaLimit int                    `json:"effective_weekly_quota_limit"`
	ExpiresAt                 int64                  `json:"expires_at"`
}

// SearchChannelUserLimitUsers 搜索可提前配置个人覆盖的用户。
//
// @param c Gin 请求上下文。
// @return 无。
func SearchChannelUserLimitUsers(c *gin.Context) {
	if _, ok := getChannelForUserLimit(c); !ok {
		return
	}
	pageInfo, ok := getChannelUserLimitPage(c)
	if !ok {
		return
	}
	users, total, err := model.SearchUserLimitSummaries(c.Query("keyword"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		logger.LogError(c.Request.Context(), "搜索渠道个人限制用户失败: "+common.LocalLogPreview(err.Error()))
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		return
	}
	common.ApiSuccess(c, gin.H{
		"page":      pageInfo.GetPage(),
		"page_size": pageInfo.GetPageSize(),
		"total":     total,
		"items":     users,
	})
}

// GetChannelUserLimitStatus 获取指定用户的并发、日限和周限统一状态。
//
// @param c Gin 请求上下文。
// @return 无。
func GetChannelUserLimitStatus(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	userID, ok := getChannelUserLimitUserID(c)
	if !ok {
		return
	}
	status, err := buildChannelUserLimitStatus(c, channel, userID)
	if err != nil {
		respondChannelUserLimitServiceError(c, "get_user_limit_status", err)
		return
	}
	common.ApiSuccess(c, status)
}

// GetChannelUserLimitOverrides 分页获取指定渠道当前有效的个人覆盖。
//
// @param c Gin 请求上下文。
// @return 无。
func GetChannelUserLimitOverrides(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	pageInfo, ok := getChannelUserLimitPage(c)
	if !ok {
		return
	}
	overrides, total, err := model.ListActiveChannelUserLimitOverrides(channel.Id, common.GetTimestamp(), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		respondChannelUserLimitServiceError(c, "list_user_limit_overrides", err)
		return
	}
	userIDs := make([]int, len(overrides))
	for index, override := range overrides {
		userIDs[index] = override.UserId
	}
	users, err := model.GetUserLimitSummaries(userIDs)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		return
	}
	userMap := make(map[int]model.UserLimitSummary, len(users))
	for _, user := range users {
		userMap[user.ID] = user
	}
	items := make([]channelUserLimitOverrideItem, 0, len(overrides))
	for _, override := range overrides {
		limits, resolveErr := service.ResolveChannelUserEffectiveLimits(c.Request.Context(), channel, override.UserId)
		if resolveErr != nil {
			respondChannelUserLimitServiceError(c, "resolve_user_limit_override", resolveErr)
			return
		}
		items = append(items, channelUserLimitOverrideItem{
			User:                      userMap[override.UserId],
			UserConcurrencyLimit:      override.UserConcurrencyLimit,
			UserDailyQuotaLimit:       override.UserDailyQuotaLimit,
			UserWeeklyQuotaLimit:      override.UserWeeklyQuotaLimit,
			EffectiveConcurrencyLimit: limits.EffectiveConcurrency,
			EffectiveDailyQuotaLimit:  limits.EffectiveDailyQuota,
			EffectiveWeeklyQuotaLimit: limits.EffectiveWeeklyQuota,
			ExpiresAt:                 override.ExpiresAt,
		})
	}
	common.ApiSuccess(c, gin.H{
		"channel_id": channel.Id,
		"page":       pageInfo.GetPage(),
		"page_size":  pageInfo.GetPageSize(),
		"total":      total,
		"items":      items,
	})
}

// SetChannelUserLimitOverride 整条替换指定用户的个人覆盖。
//
// @param c Gin 请求上下文。
// @return 无。
func SetChannelUserLimitOverride(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	userID, ok := getChannelUserLimitUserID(c)
	if !ok {
		return
	}
	if !channelUserLimitUserExists(c, userID) {
		return
	}
	var input service.ChannelUserLimitOverrideInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	updatedBy := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	if err := service.ReplaceChannelUserLimitOverride(c.Request.Context(), channel, userID, input, updatedBy); err != nil {
		if errors.Is(err, service.ErrInvalidChannelUserLimitOverride) {
			common.ApiError(c, err)
		} else {
			respondChannelUserLimitServiceError(c, "set_user_limit_override", err)
		}
		return
	}
	recordManageAudit(c, "channel.user_limit_override_upsert", map[string]interface{}{
		"channel_id":              channel.Id,
		"user_id":                 userID,
		"user_concurrency_limit":  input.UserConcurrencyLimit,
		"user_daily_quota_limit":  input.UserDailyQuotaLimit,
		"user_weekly_quota_limit": input.UserWeeklyQuotaLimit,
		"expires_at":              input.ExpiresAt,
	})
	status, err := buildChannelUserLimitStatus(c, channel, userID)
	if err != nil {
		respondChannelUserLimitServiceError(c, "get_user_limit_status_after_set", err)
		return
	}
	common.ApiSuccess(c, status)
}

// DeleteChannelUserLimitOverride 撤销指定用户的个人覆盖。
//
// @param c Gin 请求上下文。
// @return 无。
func DeleteChannelUserLimitOverride(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	userID, ok := getChannelUserLimitUserID(c)
	if !ok {
		return
	}
	if err := service.DeleteChannelUserLimitOverride(c.Request.Context(), channel.Id, userID); err != nil {
		respondChannelUserLimitServiceError(c, "delete_user_limit_override", err)
		return
	}
	recordManageAudit(c, "channel.user_limit_override_delete", map[string]interface{}{
		"channel_id": channel.Id,
		"user_id":    userID,
	})
	common.ApiSuccess(c, gin.H{"channel_id": channel.Id, "user_id": userID})
}

func buildChannelUserLimitStatus(c *gin.Context, channel *model.Channel, userID int) (*channelUserLimitStatusResponse, error) {
	users, err := model.GetUserLimitSummaries([]int{userID})
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("用户不存在")
	}
	limits, err := service.ResolveChannelUserEffectiveLimits(c.Request.Context(), channel, userID)
	if err != nil {
		return nil, err
	}
	dailyUsed, dailyResetAt, dailyStorageMode, err := service.GetChannelUserDailyQuotaUsage(c.Request.Context(), channel.Id, userID)
	if err != nil {
		return nil, err
	}
	weeklyUsed, weeklyResetAt, weeklyStorageMode, err := service.GetChannelUserWeeklyQuotaUsage(c.Request.Context(), channel.Id, userID)
	if err != nil {
		return nil, err
	}
	concurrency, concurrencyStorageMode, err := service.GetChannelUserConcurrencyUsage(c.Request.Context(), channel.Id, userID)
	if err != nil {
		return nil, err
	}
	return &channelUserLimitStatusResponse{
		ChannelID: channel.Id,
		User:      users[0],
		Concurrency: channelUserLimitMetric{
			BaseLimit:      limits.BaseConcurrency,
			OverrideLimit:  limits.OverrideConcurrency,
			EffectiveLimit: limits.EffectiveConcurrency,
			Current:        int64(concurrency),
			Remaining:      remainingChannelUserLimit(limits.EffectiveConcurrency, int64(concurrency)),
			StorageMode:    concurrencyStorageMode,
		},
		DailyQuota: channelUserLimitMetric{
			BaseLimit:      limits.BaseDailyQuota,
			OverrideLimit:  limits.OverrideDailyQuota,
			EffectiveLimit: limits.EffectiveDailyQuota,
			Current:        dailyUsed,
			Remaining:      remainingChannelUserLimit(limits.EffectiveDailyQuota, dailyUsed),
			ResetAt:        dailyResetAt,
			StorageMode:    dailyStorageMode,
		},
		WeeklyQuota: channelUserLimitMetric{
			BaseLimit:      limits.BaseWeeklyQuota,
			OverrideLimit:  limits.OverrideWeeklyQuota,
			EffectiveLimit: limits.EffectiveWeeklyQuota,
			Current:        weeklyUsed,
			Remaining:      remainingChannelUserLimit(limits.EffectiveWeeklyQuota, weeklyUsed),
			ResetAt:        weeklyResetAt,
			StorageMode:    weeklyStorageMode,
		},
		OverrideActive:    limits.Active,
		OverrideExpiresAt: limits.ExpiresAt,
	}, nil
}

func remainingChannelUserLimit(limit int, current int64) int64 {
	if limit <= 0 {
		return 0
	}
	remaining := int64(limit) - current
	if remaining < 0 {
		return 0
	}
	return remaining
}

func getChannelUserLimitUserID(c *gin.Context) (int, bool) {
	userID, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || userID <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidId)
		return 0, false
	}
	return userID, true
}

func channelUserLimitUserExists(c *gin.Context, userID int) bool {
	users, err := model.GetUserLimitSummaries([]int{userID})
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		return false
	}
	if len(users) == 0 {
		common.ApiErrorI18n(c, i18n.MsgUserNotExists)
		return false
	}
	return true
}
