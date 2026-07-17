package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// TokenLogStat 按 Token 维度的统计数据结构
// 包含使用次数、消耗额度、Token 用量和实时 RPM/TPM
type TokenLogStat struct {
	Count            int `json:"count"`
	Quota            int `json:"quota"`
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	Rpm              int `json:"rpm"`
	Tpm              int `json:"tpm"`
}

// TokenLogFilterParams 表示公共 API Key 日志查看器的筛选条件。
type TokenLogFilterParams struct {
	TokenID        int
	LogType        int
	StartTimestamp int64
	EndTimestamp   int64
	ModelName      string
	RequestID      string
}

type tokenLogUsageStat struct {
	Quota            int `json:"quota"`
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func tokenLogFilterWithType(params TokenLogFilterParams, logType int) TokenLogFilterParams {
	params.LogType = logType
	return params
}

func tokenLogUsageAvailable(params TokenLogFilterParams) bool {
	return params.LogType == LogTypeUnknown || params.LogType == LogTypeConsume
}

func applyTokenLogFilterParams(tx *gorm.DB, params TokenLogFilterParams, includeTime bool) (*gorm.DB, error) {
	tx = tx.Where("token_id = ?", params.TokenID)
	if params.LogType != LogTypeUnknown {
		tx = tx.Where("type = ?", params.LogType)
	}
	if includeTime {
		if params.StartTimestamp != 0 {
			tx = tx.Where("created_at >= ?", params.StartTimestamp)
		}
		if params.EndTimestamp != 0 {
			tx = tx.Where("created_at <= ?", params.EndTimestamp)
		}
	}
	if params.ModelName != "" {
		var err error
		if tx, err = applyExplicitLogTextFilter(tx, "model_name", params.ModelName); err != nil {
			return nil, err
		}
	}
	if params.RequestID != "" {
		tx = tx.Where("request_id = ?", params.RequestID)
	}
	return tx, nil
}

func queryTokenUsageStat(tx *gorm.DB) (tokenLogUsageStat, error) {
	var stat tokenLogUsageStat
	if err := tx.Session(&gorm.Session{}).
		Select("COALESCE(sum(quota), 0) as quota, COALESCE(sum(prompt_tokens), 0) as prompt_tokens, COALESCE(sum(completion_tokens), 0) as completion_tokens").
		Scan(&stat).Error; err != nil {
		return stat, err
	}
	cacheDelta, err := sumStatisticCacheTokenDeltaFromLogQuery(tx)
	if err != nil {
		return stat, err
	}
	stat.TotalTokens = stat.PromptTokens + stat.CompletionTokens + cacheDelta
	return stat, nil
}

// GetTokenLogStat 按 token_id 聚合查询统计数据（公共 API Key 日志查看器）
// 仅统计消费类型日志（type=2）
// @param tokenId Token ID
// @param startTimestamp 开始时间戳（可选，0 表示不限制）
// @param endTimestamp 结束时间戳（可选，0 表示不限制）
// @return 统计数据
func GetTokenLogStat(tokenId int, startTimestamp int64, endTimestamp int64) (stat TokenLogStat, err error) {
	return GetTokenLogStatWithFilters(TokenLogFilterParams{
		TokenID:        tokenId,
		LogType:        LogTypeConsume,
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	})
}

// GetTokenLogStatWithFilters 按公共日志筛选条件聚合查询统计数据。
// @param params 公共 API Key 日志筛选条件
// @return 统计数据
func GetTokenLogStatWithFilters(params TokenLogFilterParams) (stat TokenLogStat, err error) {
	countQuery, err := applyTokenLogFilterParams(LOG_DB.Model(&Log{}), params, true)
	if err != nil {
		return stat, err
	}
	if err := countQuery.Select("count(*) as count").Scan(&stat).Error; err != nil {
		return stat, errors.New("查询统计数据失败")
	}

	rpmQuery, err := applyTokenLogFilterParams(LOG_DB.Model(&Log{}), params, false)
	if err != nil {
		return stat, err
	}
	rpmQuery = rpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())
	var rpmStat struct {
		Rpm int `json:"rpm"`
	}
	if err := rpmQuery.Select("count(*) as rpm").Scan(&rpmStat).Error; err != nil {
		return stat, errors.New("查询统计数据失败")
	}
	stat.Rpm = rpmStat.Rpm

	if !tokenLogUsageAvailable(params) {
		return stat, nil
	}

	usageParams := tokenLogFilterWithType(params, LogTypeConsume)
	usageQuery, err := applyTokenLogFilterParams(LOG_DB.Model(&Log{}), usageParams, true)
	if err != nil {
		return stat, err
	}
	usageStat, err := queryTokenUsageStat(usageQuery)
	if err != nil {
		return stat, errors.New("查询统计数据失败")
	}
	stat.Quota = usageStat.Quota
	stat.PromptTokens = usageStat.PromptTokens
	stat.CompletionTokens = usageStat.CompletionTokens
	stat.TotalTokens = usageStat.TotalTokens

	tpmQuery, err := applyTokenLogFilterParams(LOG_DB.Model(&Log{}), usageParams, false)
	if err != nil {
		return stat, err
	}
	tpmQuery = tpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())
	tpmStat, err := queryTokenUsageStat(tpmQuery)
	if err != nil {
		return stat, errors.New("查询统计数据失败")
	}
	stat.Tpm = tpmStat.TotalTokens
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
	return GetTokenModelStatsWithFilters(TokenLogFilterParams{
		TokenID:        tokenId,
		LogType:        LogTypeConsume,
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	})
}

// GetTokenModelStatsWithFilters 按公共日志筛选条件聚合查询模型调用统计。
// @param params 公共 API Key 日志筛选条件
// @return 各模型的调用次数
func GetTokenModelStatsWithFilters(params TokenLogFilterParams) ([]*TokenModelStat, error) {
	var results []*TokenModelStat
	tx := LOG_DB.Table("logs").
		Select("model_name, count(*) as count")
	var err error
	if tx, err = applyTokenLogFilterParams(tx, params, true); err != nil {
		return nil, err
	}
	err = tx.Group("model_name").Order("count desc").Find(&results).Error
	return results, err
}

// GetTokenQuotaData 按 token_id 从 logs 表聚合查询配额数据（公共 API Key 日志查看器折线图）
// 按小时粒度聚合，仅统计消费类型日志（type=2）
// @param tokenId Token ID
// @param startTimestamp 开始时间戳
// @param endTimestamp 结束时间戳
// @return 按时间和模型聚合的配额数据
func GetTokenQuotaData(tokenId int, startTimestamp int64, endTimestamp int64) ([]*QuotaData, error) {
	return GetTokenQuotaDataWithFilters(TokenLogFilterParams{
		TokenID:        tokenId,
		LogType:        LogTypeConsume,
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	})
}

// GetTokenQuotaDataWithFilters 按公共日志筛选条件聚合查询消费趋势数据。
// @param params 公共 API Key 日志筛选条件
// @return 按时间和模型聚合的配额数据
func GetTokenQuotaDataWithFilters(params TokenLogFilterParams) ([]*QuotaData, error) {
	if !tokenLogUsageAvailable(params) {
		return []*QuotaData{}, nil
	}
	usageParams := tokenLogFilterWithType(params, LogTypeConsume)
	tx, err := applyTokenLogFilterParams(LOG_DB.Model(&Log{}), usageParams, true)
	if err != nil {
		return nil, err
	}
	return aggregateTokenQuotaDataFromLogQuery(tx)
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
	tx, err := applyTokenLogFilterParams(LOG_DB.Model(&Log{}), TokenLogFilterParams{
		TokenID:        tokenId,
		LogType:        logType,
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		ModelName:      modelName,
		RequestID:      requestId,
	}, true)
	if err != nil {
		return nil, 0, err
	}

	err = tx.Limit(logSearchCountLimit).Count(&total).Error
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
