package model

import (
	"context"
	"sort"

	"gorm.io/gorm"
)

// LogSummaryByKey 表示按分组、API Key 和用户维度聚合的导出汇总数据。
type LogSummaryByKey struct {
	Group     string `json:"group"`
	TokenName string `json:"token_name"`
	Username  string `json:"username"`
	Count     int    `json:"count"`
	TokenUsed int    `json:"token_used"`
	Quota     int    `json:"quota"`
}

// applyLogTokenNamesFilter 为导出查询追加令牌名称过滤；空列表表示不过滤。
func applyLogTokenNamesFilter(tx *gorm.DB, tokenNames []string) *gorm.DB {
	if len(tokenNames) == 0 {
		return tx
	}
	return tx.Where("token_name IN ?", tokenNames)
}

// applyLogGroupsFilter 为日志查询追加分组过滤；空列表表示不过滤。
func applyLogGroupsFilter(tx *gorm.DB, groups []string) *gorm.DB {
	if len(groups) == 0 {
		return tx
	}
	return tx.Where(logGroupCol+" IN ?", groups)
}

func newLogExportQuery(startTimestamp int64, endTimestamp int64, username string, tokenNames []string, groups []string) *gorm.DB {
	tx := LOG_DB.Model(&Log{}).Where("type = ?", LogTypeConsume)
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	tx = applyLogTokenNamesFilter(tx, tokenNames)
	return applyLogGroupsFilter(tx, groups)
}

// GetLogSummaryByKey 从 logs 表按 group + token_name + username 聚合查询汇总数据（导出 Sheet 1）
// 仅统计消费类型日志（type=2）
// @param startTimestamp 开始时间戳
// @param endTimestamp 结束时间戳
// @param username 用户名过滤（可选）
// @param tokenNames API Key 名称过滤列表（可选）
// @param groups 分组过滤列表（可选）
// @return 按分组、API Key 和用户维度聚合的汇总数据
func GetLogSummaryByKey(startTimestamp int64, endTimestamp int64, username string, tokenNames []string, groups []string) ([]*LogSummaryByKey, error) {
	results, _, err := ProcessLogsForExport(context.Background(), startTimestamp, endTimestamp, username, tokenNames, groups, nil)
	return results, err
}

// LogDetailByKeyModel 表示按分组、API Key、用户和模型维度聚合的导出明细数据。
type LogDetailByKeyModel struct {
	Group     string `json:"group"`
	TokenName string `json:"token_name"`
	Username  string `json:"username"`
	ModelName string `json:"model_name"`
	Count     int    `json:"count"`
	TokenUsed int    `json:"token_used"`
	Quota     int    `json:"quota"`
}

// GetLogDetailByKeyModel 从 logs 表按 group + token_name + username + model_name 聚合查询明细数据（导出 Sheet 2）
// 仅统计消费类型日志（type=2）
// @param startTimestamp 开始时间戳
// @param endTimestamp 结束时间戳
// @param username 用户名过滤（可选）
// @param tokenNames API Key 名称过滤列表（可选）
// @param groups 分组过滤列表（可选）
// @return 按分组、API Key、用户和模型维度聚合的明细数据
func GetLogDetailByKeyModel(startTimestamp int64, endTimestamp int64, username string, tokenNames []string, groups []string) ([]*LogDetailByKeyModel, error) {
	_, results, err := ProcessLogsForExport(context.Background(), startTimestamp, endTimestamp, username, tokenNames, groups, nil)
	return results, err
}

// exportLogMaxRows 导出日志的最大行数限制，防止大数据量导致内存溢出
const exportLogMaxRows = 500000

type logExportAggregateKey struct {
	Group     string
	TokenName string
	Username  string
	ModelName string
}

// ProcessLogsForExport 顺序遍历一次消费日志，同时生成两个聚合 Sheet 的数据并回调请求日志明细。
// @param ctx 请求上下文，用于在客户端断开时取消数据库读取
// @param startTimestamp 开始时间戳
// @param endTimestamp 结束时间戳
// @param username 用户名过滤（可选）
// @param tokenNames API Key 名称过滤列表（可选）
// @param groups 分组过滤列表（可选）
// @param handleDetail 请求日志明细回调，参数依次为日志、缓存读取 Token 和缓存写入 Token；仅对按时间升序排列的前 500000 条记录调用（可选）
// @return 完整匹配范围的 API Key 汇总、模型明细和处理错误
func ProcessLogsForExport(ctx context.Context, startTimestamp int64, endTimestamp int64, username string, tokenNames []string, groups []string, handleDetail func(*Log, int, int) error) ([]*LogSummaryByKey, []*LogDetailByKeyModel, error) {
	summaryMap := make(map[logExportAggregateKey]*LogSummaryByKey)
	detailMap := make(map[logExportAggregateKey]*LogDetailByKeyModel)
	tx := newLogExportQuery(startTimestamp, endTimestamp, username, tokenNames, groups).
		WithContext(ctx).
		Select([]string{"created_at", "username", "token_name", "model_name", "quota", "prompt_tokens", "completion_tokens", "use_time", "is_stream", "channel_id", "group", "request_id", "other"}).
		Order("created_at asc")
	rows, err := tx.Rows()
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	detailCount := 0
	for rows.Next() {
		var log Log
		if err := tx.ScanRows(rows, &log); err != nil {
			return nil, nil, err
		}

		otherMap := parseLogOther(log.Other)
		tokenUsed := statisticTokenUsedFromOther(log.PromptTokens, log.CompletionTokens, otherMap)
		summaryKey := logExportAggregateKey{
			Group:     log.Group,
			TokenName: log.TokenName,
			Username:  log.Username,
		}
		summary, exists := summaryMap[summaryKey]
		if !exists {
			summary = &LogSummaryByKey{
				Group:     log.Group,
				TokenName: log.TokenName,
				Username:  log.Username,
			}
			summaryMap[summaryKey] = summary
		}
		summary.Count++
		summary.TokenUsed += tokenUsed
		summary.Quota += log.Quota

		detailKey := summaryKey
		detailKey.ModelName = log.ModelName
		detail, exists := detailMap[detailKey]
		if !exists {
			detail = &LogDetailByKeyModel{
				Group:     log.Group,
				TokenName: log.TokenName,
				Username:  log.Username,
				ModelName: log.ModelName,
			}
			detailMap[detailKey] = detail
		}
		detail.Count++
		detail.TokenUsed += tokenUsed
		detail.Quota += log.Quota

		detailCount++
		if detailCount <= exportLogMaxRows && handleDetail != nil {
			cacheRead := logExportOtherInt(otherMap, "cache_tokens")
			cacheWrite := logExportOtherInt(otherMap, "cache_creation_tokens_5m") + logExportOtherInt(otherMap, "cache_creation_tokens_1h")
			if cacheWrite == 0 {
				cacheWrite = logExportOtherInt(otherMap, "cache_creation_tokens")
			}
			if err := handleDetail(&log, cacheRead, cacheWrite); err != nil {
				return nil, nil, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	summaryRows := make([]*LogSummaryByKey, 0, len(summaryMap))
	for _, row := range summaryMap {
		summaryRows = append(summaryRows, row)
	}
	sortLogSummaryRows(summaryRows)

	detailRows := make([]*LogDetailByKeyModel, 0, len(detailMap))
	for _, row := range detailMap {
		detailRows = append(detailRows, row)
	}
	sortLogDetailRows(detailRows)
	return summaryRows, detailRows, nil
}

func logExportOtherInt(other map[string]interface{}, key string) int {
	if value, ok := other[key].(float64); ok {
		return int(value)
	}
	return 0
}

func sortLogSummaryRows(rows []*LogSummaryByKey) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Group != rows[j].Group {
			return rows[i].Group < rows[j].Group
		}
		if rows[i].TokenName != rows[j].TokenName {
			return rows[i].TokenName < rows[j].TokenName
		}
		return rows[i].Username < rows[j].Username
	})
}

func sortLogDetailRows(rows []*LogDetailByKeyModel) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Group != rows[j].Group {
			return rows[i].Group < rows[j].Group
		}
		if rows[i].TokenName != rows[j].TokenName {
			return rows[i].TokenName < rows[j].TokenName
		}
		if rows[i].Username != rows[j].Username {
			return rows[i].Username < rows[j].Username
		}
		return rows[i].ModelName < rows[j].ModelName
	})
}

// GetLogsForExport 获取指定条件的消费日志用于导出（不分页）
// 仅查询消费类型日志（type=2），按创建时间升序排列
// 为防止内存溢出，最多返回 exportLogMaxRows 条记录
// @param startTimestamp 开始时间戳
// @param endTimestamp 结束时间戳
// @param username 用户名过滤（可选）
// @param tokenNames API Key 名称过滤列表（可选）
// @param groups 分组过滤列表（可选）
// @return 符合条件的日志列表
func GetLogsForExport(startTimestamp int64, endTimestamp int64, username string, tokenNames []string, groups []string) ([]*Log, error) {
	var logs []*Log
	tx := newLogExportQuery(startTimestamp, endTimestamp, username, tokenNames, groups)
	err := tx.Order("created_at asc").Limit(exportLogMaxRows).Find(&logs).Error
	return logs, err
}
