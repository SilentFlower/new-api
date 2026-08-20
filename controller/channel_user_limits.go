package controller

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type channelUserDailyQuotaItem struct {
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

type channelUserConcurrencyItem struct {
	UserID             int    `json:"user_id"`
	Username           string `json:"username"`
	DisplayName        string `json:"display_name"`
	CurrentConcurrency int    `json:"current_concurrency"`
	BaseLimit          int    `json:"base_limit"`
	OverrideLimit      *int   `json:"override_limit,omitempty"`
	OverrideExpiresAt  int64  `json:"override_expires_at"`
	Limit              int    `json:"limit"`
}

type channelUserLimitListResponse struct {
	ChannelID   int         `json:"channel_id"`
	Limit       int         `json:"limit"`
	StorageMode string      `json:"storage_mode"`
	ResetAt     int64       `json:"reset_at,omitempty"`
	Page        int         `json:"page"`
	PageSize    int         `json:"page_size"`
	Total       int         `json:"total"`
	Items       interface{} `json:"items"`
}

type setChannelUserDailyQuotaRequest struct {
	UsedQuota *int `json:"used_quota"`
}

// GetChannelUserDailyQuota 获取指定渠道当前自然日的用户额度使用情况。
//
// @param c Gin 请求上下文。
// @return 无。
func GetChannelUserDailyQuota(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	pageInfo, ok := getChannelUserLimitPage(c)
	if !ok {
		return
	}
	usage, resetAt, storageMode, err := service.ListChannelUserDailyQuota(c.Request.Context(), channel.Id)
	if err != nil {
		respondChannelUserLimitServiceError(c, "list_daily_quota", err)
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
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"查询渠道每日额度用户摘要失败: channel_id=%d error=%s",
			channel.Id,
			common.LocalLogPreview(err.Error()),
		))
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		return
	}
	userMap := make(map[int]model.UserLimitSummary, len(users))
	for _, user := range users {
		userMap[user.ID] = user
	}
	limit := channel.GetUserDailyQuotaLimit()
	items := make([]channelUserDailyQuotaItem, 0, len(pageUsage))
	for _, item := range pageUsage {
		limits, err := service.ResolveChannelUserEffectiveLimits(c.Request.Context(), channel, item.UserID)
		if err != nil {
			respondChannelUserLimitServiceError(c, "resolve_daily_quota_override", err)
			return
		}
		remaining := int64(limits.EffectiveDailyQuota) - item.UsedQuota
		if remaining < 0 {
			remaining = 0
		}
		user := userMap[item.UserID]
		items = append(items, channelUserDailyQuotaItem{
			UserID:            item.UserID,
			Username:          user.Username,
			DisplayName:       user.DisplayName,
			UsedQuota:         item.UsedQuota,
			BaseLimit:         limits.BaseDailyQuota,
			OverrideLimit:     limits.OverrideDailyQuota,
			OverrideExpiresAt: limits.ExpiresAt,
			Limit:             limits.EffectiveDailyQuota,
			RemainingQuota:    remaining,
		})
	}
	common.ApiSuccess(c, channelUserLimitListResponse{
		ChannelID:   channel.Id,
		Limit:       limit,
		StorageMode: storageMode,
		ResetAt:     resetAt,
		Page:        pageInfo.GetPage(),
		PageSize:    pageInfo.GetPageSize(),
		Total:       len(usage),
		Items:       items,
	})
}

// SetChannelUserDailyQuota 设置指定用户当前自然日的已使用额度。
//
// @param c Gin 请求上下文。
// @return 无。
func SetChannelUserDailyQuota(c *gin.Context) {
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
	users, err := model.GetUserLimitSummaries([]int{userID})
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"查询渠道额度调整用户失败: channel_id=%d user_id=%d error=%s",
			channel.Id,
			userID,
			common.LocalLogPreview(err.Error()),
		))
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		return
	}
	if len(users) == 0 {
		common.ApiErrorI18n(c, i18n.MsgUserNotExists)
		return
	}
	if err := service.SetChannelUserDailyQuota(c.Request.Context(), channel.Id, userID, *request.UsedQuota); err != nil {
		respondChannelUserLimitServiceError(c, "set_daily_quota", err)
		return
	}
	recordManageAudit(c, "channel.user_daily_quota_set", map[string]interface{}{
		"channel_id": channel.Id,
		"user_id":    userID,
		"used_quota": *request.UsedQuota,
	})
	common.ApiSuccess(c, gin.H{
		"channel_id": channel.Id,
		"user_id":    userID,
		"used_quota": *request.UsedQuota,
	})
}

// GetChannelUserConcurrency 获取指定渠道当前有效的用户并发数量。
//
// @param c Gin 请求上下文。
// @return 无。
func GetChannelUserConcurrency(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	pageInfo, ok := getChannelUserLimitPage(c)
	if !ok {
		return
	}
	usage, storageMode, err := service.ListChannelUserConcurrency(c.Request.Context(), channel.Id)
	if err != nil {
		respondChannelUserLimitServiceError(c, "list_concurrency", err)
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
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"查询渠道并发用户摘要失败: channel_id=%d error=%s",
			channel.Id,
			common.LocalLogPreview(err.Error()),
		))
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		return
	}
	userMap := make(map[int]model.UserLimitSummary, len(users))
	for _, user := range users {
		userMap[user.ID] = user
	}
	limit := channel.GetUserConcurrencyLimit()
	items := make([]channelUserConcurrencyItem, 0, len(pageUsage))
	for _, item := range pageUsage {
		limits, err := service.ResolveChannelUserEffectiveLimits(c.Request.Context(), channel, item.UserID)
		if err != nil {
			respondChannelUserLimitServiceError(c, "resolve_concurrency_override", err)
			return
		}
		user := userMap[item.UserID]
		items = append(items, channelUserConcurrencyItem{
			UserID:             item.UserID,
			Username:           user.Username,
			DisplayName:        user.DisplayName,
			CurrentConcurrency: item.Concurrency,
			BaseLimit:          limits.BaseConcurrency,
			OverrideLimit:      limits.OverrideConcurrency,
			OverrideExpiresAt:  limits.ExpiresAt,
			Limit:              limits.EffectiveConcurrency,
		})
	}
	common.ApiSuccess(c, channelUserLimitListResponse{
		ChannelID:   channel.Id,
		Limit:       limit,
		StorageMode: storageMode,
		Page:        pageInfo.GetPage(),
		PageSize:    pageInfo.GetPageSize(),
		Total:       len(usage),
		Items:       items,
	})
}

func getChannelForUserLimit(c *gin.Context) (*model.Channel, bool) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		common.ApiErrorI18n(c, i18n.MsgChannelIdFormatError)
		return nil, false
	}
	channel, err := model.GetChannelById(channelID, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorI18n(c, i18n.MsgChannelNotExists)
		} else {
			logger.LogError(c.Request.Context(), fmt.Sprintf(
				"查询渠道用户限制状态失败: channel_id=%d error=%s",
				channelID,
				common.LocalLogPreview(err.Error()),
			))
			common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		}
		return nil, false
	}
	return channel, true
}

func getChannelUserLimitPage(c *gin.Context) (*common.PageInfo, bool) {
	pageInfo := common.GetPageQuery(c)
	if pageInfo.GetPage() <= 0 || pageInfo.GetPageSize() <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return nil, false
	}
	maxInt := int(^uint(0) >> 1)
	if pageInfo.GetPage() > maxInt/pageInfo.GetPageSize() {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return nil, false
	}
	return pageInfo, true
}

func channelUserLimitPageRange(pageInfo *common.PageInfo, total int) (int, int) {
	start := pageInfo.GetStartIdx()
	if start > total {
		start = total
	}
	end := pageInfo.GetEndIdx()
	if end > total {
		end = total
	}
	return start, end
}

func respondChannelUserLimitServiceError(c *gin.Context, operation string, err error) {
	logger.LogError(c.Request.Context(), fmt.Sprintf(
		"渠道用户限制状态服务失败: operation=%s channel_id=%s user_id=%s error=%s",
		operation,
		c.Param("id"),
		c.Param("user_id"),
		common.LocalLogPreview(err.Error()),
	))
	common.ApiErrorI18n(c, i18n.MsgChannelUserLimitStatusUnavailable)
}
