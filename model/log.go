package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

type Log struct {
	Id               int    `json:"id" gorm:"index:idx_created_at_id,priority:1;index:idx_user_id_id,priority:2"`
	UserId           int    `json:"user_id" gorm:"index;index:idx_user_id_id,priority:1"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint;index:idx_created_at_id,priority:2;index:idx_created_at_type"`
	Type             int    `json:"type" gorm:"index:idx_created_at_type"`
	Content          string `json:"content"`
	Username         string `json:"username" gorm:"index;index:index_username_model_name,priority:2;default:''"`
	TokenName        string `json:"token_name" gorm:"index;default:''"`
	ModelName        string `json:"model_name" gorm:"index;index:index_username_model_name,priority:1;default:''"`
	Quota            int    `json:"quota" gorm:"default:0"`
	PromptTokens     int    `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens int    `json:"completion_tokens" gorm:"default:0"`
	UseTime          int    `json:"use_time" gorm:"default:0"`
	IsStream         bool   `json:"is_stream"`
	ChannelId        int    `json:"channel" gorm:"index"`
	ChannelName      string `json:"channel_name" gorm:"->"`
	TokenId          int    `json:"token_id" gorm:"default:0;index"`
	Group            string `json:"group" gorm:"index"`
	Ip               string `json:"ip" gorm:"index;default:''"`
	RequestId        string `json:"request_id,omitempty" gorm:"type:varchar(64);index:idx_logs_request_id;default:''"`
	Other            string `json:"other"`
}

// don't use iota, avoid change log type value
const (
	LogTypeUnknown = 0
	LogTypeTopup   = 1
	LogTypeConsume = 2
	LogTypeManage  = 3
	LogTypeSystem  = 4
	LogTypeError   = 5
	LogTypeRefund  = 6
)

func formatUserLogs(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].ChannelName = ""
		var otherMap map[string]interface{}
		otherMap, _ = common.StrToMap(logs[i].Other)
		if otherMap != nil {
			// Remove admin-only debug fields.
			delete(otherMap, "admin_info")
			// delete(otherMap, "reject_reason")
			delete(otherMap, "stream_status")
		}
		logs[i].Other = common.MapToJsonStr(otherMap)
		logs[i].Id = startIdx + i + 1
	}
}

func GetLogByTokenId(tokenId int) (logs []*Log, err error) {
	err = LOG_DB.Model(&Log{}).Where("token_id = ?", tokenId).Order("id desc").Limit(common.MaxRecentItems).Find(&logs).Error
	formatUserLogs(logs, 0)
	return logs, err
}

func RecordLog(userId int, logType int, content string) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

func RecordErrorLog(c *gin.Context, userId int, channelId int, modelName string, tokenName string, content string, tokenId int, useTimeSeconds int,
	isStream bool, group string, other map[string]interface{}) {
	logger.LogInfo(c, fmt.Sprintf("record error log: userId=%d, channelId=%d, modelName=%s, tokenName=%s, content=%s", userId, channelId, modelName, tokenName, content))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	otherStr := common.MapToJsonStr(other)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeError,
		Content:          content,
		PromptTokens:     0,
		CompletionTokens: 0,
		TokenName:        tokenName,
		ModelName:        modelName,
		Quota:            0,
		ChannelId:        channelId,
		TokenId:          tokenId,
		UseTime:          useTimeSeconds,
		IsStream:         isStream,
		Group:            group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId: requestId,
		Other:     otherStr,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
}

type RecordConsumeLogParams struct {
	ChannelId        int                    `json:"channel_id"`
	PromptTokens     int                    `json:"prompt_tokens"`
	CompletionTokens int                    `json:"completion_tokens"`
	ModelName        string                 `json:"model_name"`
	TokenName        string                 `json:"token_name"`
	Quota            int                    `json:"quota"`
	Content          string                 `json:"content"`
	TokenId          int                    `json:"token_id"`
	UseTimeSeconds   int                    `json:"use_time_seconds"`
	IsStream         bool                   `json:"is_stream"`
	Group            string                 `json:"group"`
	Other            map[string]interface{} `json:"other"`
}

func RecordConsumeLog(c *gin.Context, userId int, params RecordConsumeLogParams) {
	if !common.LogConsumeEnabled {
		return
	}
	logger.LogInfo(c, fmt.Sprintf("record consume log: userId=%d, params=%s", userId, common.GetJsonString(params)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	otherStr := common.MapToJsonStr(params.Other)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeConsume,
		Content:          params.Content,
		PromptTokens:     params.PromptTokens,
		CompletionTokens: params.CompletionTokens,
		TokenName:        params.TokenName,
		ModelName:        params.ModelName,
		Quota:            params.Quota,
		ChannelId:        params.ChannelId,
		TokenId:          params.TokenId,
		UseTime:          params.UseTimeSeconds,
		IsStream:         params.IsStream,
		Group:            params.Group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId: requestId,
		Other:     otherStr,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
	if common.DataExportEnabled {
		gopool.Go(func() {
			LogQuotaData(userId, username, params.ModelName, params.TokenName, params.Quota, common.GetTimestamp(), params.PromptTokens+params.CompletionTokens)
		})
	}
}

type RecordTaskBillingLogParams struct {
	UserId    int
	LogType   int
	Content   string
	ChannelId int
	ModelName string
	Quota     int
	TokenId   int
	Group     string
	Other     map[string]interface{}
}

func RecordTaskBillingLog(params RecordTaskBillingLogParams) {
	if params.LogType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(params.UserId, false)
	tokenName := ""
	if params.TokenId > 0 {
		if token, err := GetTokenById(params.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	log := &Log{
		UserId:    params.UserId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      params.LogType,
		Content:   params.Content,
		TokenName: tokenName,
		ModelName: params.ModelName,
		Quota:     params.Quota,
		ChannelId: params.ChannelId,
		TokenId:   params.TokenId,
		Group:     params.Group,
		Other:     common.MapToJsonStr(params.Other),
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record task billing log: " + err.Error())
	}
}

func GetAllLogs(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, channel int, group string, requestId string) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB
	} else {
		tx = LOG_DB.Where("logs.type = ?", logType)
	}

	if modelName != "" {
		tx = tx.Where("logs.model_name like ?", modelName)
	}
	if username != "" {
		tx = tx.Where("logs.username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if channel != 0 {
		tx = tx.Where("logs.channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	err = tx.Order("logs.id desc").Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}

	channelIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
	}

	if channelIds.Len() > 0 {
		var channels []struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if common.MemoryCacheEnabled {
			// Cache get channel
			for _, channelId := range channelIds.Items() {
				if cacheChannel, err := CacheGetChannel(channelId); err == nil {
					channels = append(channels, struct {
						Id   int    `gorm:"column:id"`
						Name string `gorm:"column:name"`
					}{
						Id:   channelId,
						Name: cacheChannel.Name,
					})
				}
			}
		} else {
			// Bulk query channels from DB
			if err = DB.Table("channels").Select("id, name").Where("id IN ?", channelIds.Items()).Find(&channels).Error; err != nil {
				return logs, total, err
			}
		}
		channelMap := make(map[int]string, len(channels))
		for _, channel := range channels {
			channelMap[channel.Id] = channel.Name
		}
		for i := range logs {
			logs[i].ChannelName = channelMap[logs[i].ChannelId]
		}
	}

	return logs, total, err
}

const logSearchCountLimit = 10000

func GetUserLogs(userId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, startIdx int, num int, group string, requestId string) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB.Where("logs.user_id = ?", userId)
	} else {
		tx = LOG_DB.Where("logs.user_id = ? and logs.type = ?", userId, logType)
	}

	if modelName != "" {
		modelNamePattern, err := sanitizeLikePattern(modelName)
		if err != nil {
			return nil, 0, err
		}
		tx = tx.Where("logs.model_name LIKE ? ESCAPE '!'", modelNamePattern)
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Limit(logSearchCountLimit).Count(&total).Error
	if err != nil {
		common.SysError("failed to count user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}
	err = tx.Order("logs.id desc").Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		common.SysError("failed to search user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}

	formatUserLogs(logs, startIdx)
	return logs, total, err
}

type Stat struct {
	Quota int `json:"quota"`
	Rpm   int `json:"rpm"`
	Tpm   int `json:"tpm"`
}

func SumUsedQuota(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string) (stat Stat, err error) {
	tx := LOG_DB.Table("logs").Select("sum(quota) quota")

	// 为rpm和tpm创建单独的查询
	rpmTpmQuery := LOG_DB.Table("logs").Select("count(*) rpm, sum(prompt_tokens) + sum(completion_tokens) tpm")

	if username != "" {
		tx = tx.Where("username = ?", username)
		rpmTpmQuery = rpmTpmQuery.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
		rpmTpmQuery = rpmTpmQuery.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		modelNamePattern, err := sanitizeLikePattern(modelName)
		if err != nil {
			return stat, err
		}
		tx = tx.Where("model_name LIKE ? ESCAPE '!'", modelNamePattern)
		rpmTpmQuery = rpmTpmQuery.Where("model_name LIKE ? ESCAPE '!'", modelNamePattern)
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
		rpmTpmQuery = rpmTpmQuery.Where("channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where(logGroupCol+" = ?", group)
		rpmTpmQuery = rpmTpmQuery.Where(logGroupCol+" = ?", group)
	}

	tx = tx.Where("type = ?", LogTypeConsume)
	rpmTpmQuery = rpmTpmQuery.Where("type = ?", LogTypeConsume)

	// 只统计最近60秒的rpm和tpm
	rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())

	// 执行查询
	if err := tx.Scan(&stat).Error; err != nil {
		common.SysError("failed to query log stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	if err := rpmTpmQuery.Scan(&stat).Error; err != nil {
		common.SysError("failed to query rpm/tpm stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}

	return stat, nil
}

func SumUsedToken(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) (token int) {
	tx := LOG_DB.Table("logs").Select("ifnull(sum(prompt_tokens),0) + ifnull(sum(completion_tokens),0)")
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		tx = tx.Where("model_name = ?", modelName)
	}
	tx.Where("type = ?", LogTypeConsume).Scan(&token)
	return token
}

// LogSummaryByKey 按 API Key 维度的汇总统计结构（从 logs 表聚合）
type LogSummaryByKey struct {
	TokenName string `json:"token_name"`
	Username  string `json:"username"`
	Count     int    `json:"count"`
	TokenUsed int    `json:"token_used"`
	Quota     int    `json:"quota"`
}

// GetLogSummaryByKey 从 logs 表按 token_name 分组聚合查询汇总数据（导出 Sheet 1）
// 仅统计消费类型日志（type=2）
// @param startTimestamp 开始时间戳
// @param endTimestamp 结束时间戳
// @param username 用户名过滤（可选）
// @param tokenName API Key 名称过滤（可选）
// @return 按 API Key 维度聚合的汇总数据
func GetLogSummaryByKey(startTimestamp int64, endTimestamp int64, username string, tokenName string) ([]*LogSummaryByKey, error) {
	var results []*LogSummaryByKey
	tx := LOG_DB.Table("logs").
		Select("token_name, username, count(*) as count, sum(prompt_tokens + completion_tokens) as token_used, sum(quota) as quota").
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ? AND created_at <= ?", startTimestamp, endTimestamp)
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	err := tx.Group("token_name, username").Find(&results).Error
	return results, err
}

// LogDetailByKeyModel 按 API Key + 模型维度的明细统计结构（从 logs 表聚合）
type LogDetailByKeyModel struct {
	TokenName string `json:"token_name"`
	Username  string `json:"username"`
	ModelName string `json:"model_name"`
	Count     int    `json:"count"`
	TokenUsed int    `json:"token_used"`
	Quota     int    `json:"quota"`
}

// GetLogDetailByKeyModel 从 logs 表按 token_name + model_name 分组聚合查询明细数据（导出 Sheet 2）
// 仅统计消费类型日志（type=2）
// @param startTimestamp 开始时间戳
// @param endTimestamp 结束时间戳
// @param username 用户名过滤（可选）
// @param tokenName API Key 名称过滤（可选）
// @return 按 API Key + 模型维度聚合的明细数据
func GetLogDetailByKeyModel(startTimestamp int64, endTimestamp int64, username string, tokenName string) ([]*LogDetailByKeyModel, error) {
	var results []*LogDetailByKeyModel
	tx := LOG_DB.Table("logs").
		Select("token_name, username, model_name, count(*) as count, sum(prompt_tokens + completion_tokens) as token_used, sum(quota) as quota").
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ? AND created_at <= ?", startTimestamp, endTimestamp)
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	err := tx.Group("token_name, username, model_name").Find(&results).Error
	return results, err
}

// exportLogMaxRows 导出日志的最大行数限制，防止大数据量导致内存溢出
const exportLogMaxRows = 100000

// GetLogsForExport 获取指定条件的消费日志用于导出（不分页）
// 仅查询消费类型日志（type=2），按创建时间升序排列
// 为防止内存溢出，最多返回 exportLogMaxRows 条记录
// @param startTimestamp 开始时间戳
// @param endTimestamp 结束时间戳
// @param username 用户名过滤（可选）
// @param tokenName API Key 名称过滤（可选）
// @return 符合条件的日志列表
func GetLogsForExport(startTimestamp int64, endTimestamp int64, username string, tokenName string) ([]*Log, error) {
	var logs []*Log
	tx := LOG_DB.Where("type = ?", LogTypeConsume)
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	err := tx.Order("created_at asc").Limit(exportLogMaxRows).Find(&logs).Error
	return logs, err
}

// TokenLogStat 按 Token 维度的统计数据结构
// 包含使用次数、消耗额度、Token 用量和实时 RPM/TPM
type TokenLogStat struct {
	Count            int `json:"count"`
	Quota            int `json:"quota"`
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	Rpm              int `json:"rpm"`
	Tpm              int `json:"tpm"`
}

// GetTokenLogStat 按 token_id 聚合查询统计数据（公共 API Key 日志查看器）
// 仅统计消费类型日志（type=2）
// @param tokenId Token ID
// @param startTimestamp 开始时间戳（可选，0 表示不限制）
// @param endTimestamp 结束时间戳（可选，0 表示不限制）
// @return 统计数据
func GetTokenLogStat(tokenId int, startTimestamp int64, endTimestamp int64) (stat TokenLogStat, err error) {
	tx := LOG_DB.Table("logs").
		Select("count(*) as count, COALESCE(sum(quota), 0) as quota, COALESCE(sum(prompt_tokens), 0) as prompt_tokens, COALESCE(sum(completion_tokens), 0) as completion_tokens").
		Where("token_id = ?", tokenId).
		Where("type = ?", LogTypeConsume)
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if err := tx.Scan(&stat).Error; err != nil {
		return stat, errors.New("查询统计数据失败")
	}

	// 查询实时 RPM/TPM（最近 60 秒），使用独立变量避免覆盖已有统计字段
	var rpmTpm struct {
		Rpm int `json:"rpm"`
		Tpm int `json:"tpm"`
	}
	rpmTpmQuery := LOG_DB.Table("logs").
		Select("count(*) as rpm, COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0) as tpm").
		Where("token_id = ?", tokenId).
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())
	if err := rpmTpmQuery.Scan(&rpmTpm).Error; err != nil {
		return stat, errors.New("查询统计数据失败")
	}
	stat.Rpm = rpmTpm.Rpm
	stat.Tpm = rpmTpm.Tpm

	return stat, nil
}

// TokenModelStat 按模型维度的调用统计结构（用于饼图）
type TokenModelStat struct {
	ModelName string `json:"model_name"`
	Count     int    `json:"count"`
}

// GetTokenModelStats 按 token_id + model_name 聚合查询模型调用统计（公共 API Key 日志查看器饼图）
// 仅统计消费类型日志（type=2）
// @param tokenId Token ID
// @param startTimestamp 开始时间戳（可选）
// @param endTimestamp 结束时间戳（可选）
// @return 各模型的调用次数
func GetTokenModelStats(tokenId int, startTimestamp int64, endTimestamp int64) ([]*TokenModelStat, error) {
	var results []*TokenModelStat
	tx := LOG_DB.Table("logs").
		Select("model_name, count(*) as count").
		Where("token_id = ?", tokenId).
		Where("type = ?", LogTypeConsume)
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	err := tx.Group("model_name").Order("count desc").Find(&results).Error
	return results, err
}

// GetTokenQuotaData 按 token_id 从 logs 表聚合查询配额数据（公共 API Key 日志查看器折线图）
// 按小时粒度聚合，仅统计消费类型日志（type=2）
// @param tokenId Token ID
// @param startTimestamp 开始时间戳
// @param endTimestamp 结束时间戳
// @return 按时间和模型聚合的配额数据
func GetTokenQuotaData(tokenId int, startTimestamp int64, endTimestamp int64) ([]*QuotaData, error) {
	var quotaDatas []*QuotaData
	tx := LOG_DB.Table("logs").
		Select("model_name, sum(quota) as quota, count(*) as count, sum(prompt_tokens + completion_tokens) as token_used, (created_at - created_at % 3600) as created_at").
		Where("token_id = ?", tokenId).
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ? AND created_at <= ?", startTimestamp, endTimestamp)
	err := tx.Group("model_name, (created_at - created_at % 3600)").Find(&quotaDatas).Error
	return quotaDatas, err
}

// GetLogsByTokenId 按 token_id 分页查询日志（公共 API Key 日志查看器）
// 支持完整的过滤参数，返回脱敏后的日志
// @param tokenId Token ID
// @param logType 日志类型（0 表示全部）
// @param startTimestamp 开始时间戳
// @param endTimestamp 结束时间戳
// @param modelName 模型名称过滤（支持 LIKE）
// @param requestId 请求 ID 过滤
// @param startIdx 分页起始索引
// @param num 每页条数
// @return 脱敏后的日志列表、总数、错误
func GetLogsByTokenId(tokenId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, requestId string, startIdx int, num int) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB.Where("token_id = ?", tokenId)
	} else {
		tx = LOG_DB.Where("token_id = ? AND type = ?", tokenId, logType)
	}

	if modelName != "" {
		modelNamePattern, err := sanitizeLikePattern(modelName)
		if err != nil {
			return nil, 0, err
		}
		tx = tx.Where("model_name LIKE ? ESCAPE '!'", modelNamePattern)
	}
	if requestId != "" {
		tx = tx.Where("request_id = ?", requestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}

	err = tx.Model(&Log{}).Limit(logSearchCountLimit).Count(&total).Error
	if err != nil {
		return nil, 0, errors.New("查询日志失败")
	}
	err = tx.Order("id desc").Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, errors.New("查询日志失败")
	}

	// 脱敏处理：隐藏敏感字段
	formatTokenPublicLogs(logs, startIdx)
	return logs, total, nil
}

// formatTokenPublicLogs 对公共 API Key 查看器的日志进行脱敏处理
// 隐藏渠道信息、用户名、IP 等管理员字段，以及 other 中的 admin_info 和 reject_reason
func formatTokenPublicLogs(logs []*Log, startIdx int) {
	for i := range logs {
		// 隐藏敏感字段
		logs[i].ChannelId = 0
		logs[i].ChannelName = ""
		logs[i].Username = ""
		logs[i].Ip = ""

		// 清理 other 字段中的敏感信息
		if logs[i].Other != "" {
			var otherMap map[string]interface{}
			otherMap, _ = common.StrToMap(logs[i].Other)
			if otherMap != nil {
				delete(otherMap, "admin_info")
				delete(otherMap, "reject_reason")
				logs[i].Other = common.MapToJsonStr(otherMap)
			}
		}
		logs[i].Id = startIdx + i + 1
	}
}

func DeleteOldLog(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	var total int64 = 0

	for {
		if nil != ctx.Err() {
			return total, ctx.Err()
		}

		result := LOG_DB.Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&Log{})
		if nil != result.Error {
			return total, result.Error
		}

		total += result.RowsAffected

		if result.RowsAffected < int64(limit) {
			break
		}
	}

	return total, nil
}
