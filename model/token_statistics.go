package model

import (
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	quotaDataTokenUsedStatsVersionKey = "QuotaDataTokenUsedStatsVersion"
	quotaDataTokenUsedStatsVersion    = "2"
	statisticLogBatchSize             = 1000
)

var quotaDataTokenUsedMigrationLock sync.Mutex

type quotaDataAggregateKey struct {
	UserID    int
	Username  string
	ModelName string
	TokenName string
	CreatedAt int64
}

type logSummaryAggregateKey struct {
	TokenName string
	Username  string
	ModelName string
}

func logStatisticOtherInt(other map[string]interface{}, key string) int {
	if other == nil {
		return 0
	}
	switch value := other[key].(type) {
	case int:
		if value > 0 {
			return value
		}
	case int64:
		if value > 0 {
			return int(value)
		}
	case float64:
		if value > 0 {
			return int(value)
		}
	case string:
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func parseLogOther(other string) map[string]interface{} {
	if other == "" {
		return nil
	}
	otherMap, err := common.StrToMap(other)
	if err != nil {
		return nil
	}
	return otherMap
}

func isAnthropicStatisticUsage(other map[string]interface{}) bool {
	if other == nil {
		return false
	}
	if semantic, ok := other["usage_semantic"].(string); ok && semantic == "anthropic" {
		return true
	}
	if claude, ok := other["claude"].(bool); ok && claude {
		return true
	}
	return false
}

func logStatisticCacheWriteTokens(other map[string]interface{}) int {
	cacheWrite5m := logStatisticOtherInt(other, "cache_creation_tokens_5m")
	cacheWrite1h := logStatisticOtherInt(other, "cache_creation_tokens_1h")
	if cacheWrite5m > 0 || cacheWrite1h > 0 {
		return cacheWrite5m + cacheWrite1h
	}
	return logStatisticOtherInt(other, "cache_creation_tokens")
}

func logStatisticCacheTokenDeltaFromOther(other map[string]interface{}) int {
	if !isAnthropicStatisticUsage(other) {
		return 0
	}
	return logStatisticOtherInt(other, "cache_tokens") + logStatisticCacheWriteTokens(other)
}

func statisticTokenUsedFromOther(promptTokens int, completionTokens int, other map[string]interface{}) int {
	return promptTokens + completionTokens + logStatisticCacheTokenDeltaFromOther(other)
}

func statisticTokenUsedForLog(log *Log) int {
	if log == nil {
		return 0
	}
	return statisticTokenUsedFromOther(log.PromptTokens, log.CompletionTokens, parseLogOther(log.Other))
}

func statisticCacheTokenDeltaForLog(log *Log) int {
	if log == nil {
		return 0
	}
	return logStatisticCacheTokenDeltaFromOther(parseLogOther(log.Other))
}

func hourBucketTimestamp(timestamp int64) int64 {
	return timestamp - timestamp%3600
}

func iterateStatisticLogs(tx *gorm.DB, handle func(*Log)) error {
	batch := make([]Log, 0, statisticLogBatchSize)
	return tx.Select("id", "user_id", "username", "model_name", "token_name", "created_at", "quota", "prompt_tokens", "completion_tokens", "other").
		Order("id asc").
		FindInBatches(&batch, statisticLogBatchSize, func(tx *gorm.DB, batchNumber int) error {
			for i := range batch {
				handle(&batch[i])
			}
			return nil
		}).Error
}

func quotaDataRowsFromAggregate(rowsMap map[quotaDataAggregateKey]*QuotaData) []*QuotaData {
	rows := make([]*QuotaData, 0, len(rowsMap))
	for _, row := range rowsMap {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CreatedAt != rows[j].CreatedAt {
			return rows[i].CreatedAt < rows[j].CreatedAt
		}
		if rows[i].ModelName != rows[j].ModelName {
			return rows[i].ModelName < rows[j].ModelName
		}
		if rows[i].TokenName != rows[j].TokenName {
			return rows[i].TokenName < rows[j].TokenName
		}
		if rows[i].Username != rows[j].Username {
			return rows[i].Username < rows[j].Username
		}
		return rows[i].UserID < rows[j].UserID
	})
	return rows
}

func aggregateQuotaDataFromLogQuery(tx *gorm.DB, groupByUser bool) ([]*QuotaData, error) {
	rowsMap := make(map[quotaDataAggregateKey]*QuotaData)
	err := iterateStatisticLogs(tx, func(log *Log) {
		key := quotaDataAggregateKey{
			ModelName: log.ModelName,
			TokenName: log.TokenName,
			CreatedAt: hourBucketTimestamp(log.CreatedAt),
		}
		if groupByUser {
			key.UserID = log.UserId
			key.Username = log.Username
		}
		row, ok := rowsMap[key]
		if !ok {
			row = &QuotaData{
				UserID:    key.UserID,
				Username:  key.Username,
				ModelName: key.ModelName,
				TokenName: key.TokenName,
				CreatedAt: key.CreatedAt,
			}
			rowsMap[key] = row
		}
		row.Count++
		row.Quota += log.Quota
		row.TokenUsed += statisticTokenUsedForLog(log)
	})
	if err != nil {
		return nil, err
	}
	return quotaDataRowsFromAggregate(rowsMap), nil
}

func aggregateTokenQuotaDataFromLogQuery(tx *gorm.DB) ([]*QuotaData, error) {
	rowsMap := make(map[quotaDataAggregateKey]*QuotaData)
	err := iterateStatisticLogs(tx, func(log *Log) {
		key := quotaDataAggregateKey{
			ModelName: log.ModelName,
			CreatedAt: hourBucketTimestamp(log.CreatedAt),
		}
		row, ok := rowsMap[key]
		if !ok {
			row = &QuotaData{
				ModelName: key.ModelName,
				CreatedAt: key.CreatedAt,
			}
			rowsMap[key] = row
		}
		row.Count++
		row.Quota += log.Quota
		row.TokenUsed += statisticTokenUsedForLog(log)
	})
	if err != nil {
		return nil, err
	}
	return quotaDataRowsFromAggregate(rowsMap), nil
}

func aggregateLogSummariesFromLogQuery(tx *gorm.DB, groupByModel bool) ([]*LogSummaryByKey, []*LogDetailByKeyModel, error) {
	rowsMap := make(map[logSummaryAggregateKey]*LogDetailByKeyModel)
	err := iterateStatisticLogs(tx, func(log *Log) {
		key := logSummaryAggregateKey{
			TokenName: log.TokenName,
			Username:  log.Username,
		}
		if groupByModel {
			key.ModelName = log.ModelName
		}
		row, ok := rowsMap[key]
		if !ok {
			row = &LogDetailByKeyModel{
				TokenName: key.TokenName,
				Username:  key.Username,
				ModelName: key.ModelName,
			}
			rowsMap[key] = row
		}
		row.Count++
		row.Quota += log.Quota
		row.TokenUsed += statisticTokenUsedForLog(log)
	})
	if err != nil {
		return nil, nil, err
	}
	if groupByModel {
		detailRows := make([]*LogDetailByKeyModel, 0, len(rowsMap))
		for _, row := range rowsMap {
			detailRows = append(detailRows, row)
		}
		sort.Slice(detailRows, func(i, j int) bool {
			if detailRows[i].TokenName != detailRows[j].TokenName {
				return detailRows[i].TokenName < detailRows[j].TokenName
			}
			if detailRows[i].Username != detailRows[j].Username {
				return detailRows[i].Username < detailRows[j].Username
			}
			return detailRows[i].ModelName < detailRows[j].ModelName
		})
		return nil, detailRows, nil
	}
	summaryRows := make([]*LogSummaryByKey, 0, len(rowsMap))
	for _, row := range rowsMap {
		summaryRows = append(summaryRows, &LogSummaryByKey{
			TokenName: row.TokenName,
			Username:  row.Username,
			Count:     row.Count,
			TokenUsed: row.TokenUsed,
			Quota:     row.Quota,
		})
	}
	sort.Slice(summaryRows, func(i, j int) bool {
		if summaryRows[i].TokenName != summaryRows[j].TokenName {
			return summaryRows[i].TokenName < summaryRows[j].TokenName
		}
		return summaryRows[i].Username < summaryRows[j].Username
	})
	return summaryRows, nil, nil
}

func sumStatisticTokenUsedFromLogQuery(tx *gorm.DB) (int, error) {
	total := 0
	err := iterateStatisticLogs(tx, func(log *Log) {
		total += statisticTokenUsedForLog(log)
	})
	return total, err
}

func statisticLogQuery() *gorm.DB {
	return LOG_DB.Model(&Log{}).Where("type = ?", LogTypeConsume)
}

func statisticMigrationAlreadyDone() bool {
	common.OptionMapRWMutex.RLock()
	version := common.OptionMap[quotaDataTokenUsedStatsVersionKey]
	common.OptionMapRWMutex.RUnlock()
	return version == quotaDataTokenUsedStatsVersion
}

// StartQuotaDataTokenUsedMigration 在后台将历史 quota_data.token_used 迁移到包含 Anthropic 缓存 Token 的统计口径。
// @return 无返回值；失败时写入系统日志，并在下次启动时重试。
func StartQuotaDataTokenUsedMigration() {
	if !common.IsMasterNode || DB == nil || LOG_DB == nil || statisticMigrationAlreadyDone() {
		return
	}
	go func() {
		if err := migrateQuotaDataTokenUsedStats(); err != nil {
			common.SysError("failed to migrate quota_data token_used stats: " + err.Error())
		}
	}()
}

func migrateQuotaDataTokenUsedStats() error {
	quotaDataTokenUsedMigrationLock.Lock()
	defer quotaDataTokenUsedMigrationLock.Unlock()

	if statisticMigrationAlreadyDone() {
		return nil
	}

	migrationCutoff := common.GetTimestamp()
	deltas := make(map[quotaDataAggregateKey]int)
	err := iterateStatisticLogs(statisticLogQuery().Where("other <> ?", "").Where("created_at < ?", migrationCutoff), func(log *Log) {
		delta := statisticCacheTokenDeltaForLog(log)
		if delta <= 0 {
			return
		}
		key := quotaDataAggregateKey{
			UserID:    log.UserId,
			Username:  log.Username,
			ModelName: log.ModelName,
			TokenName: log.TokenName,
			CreatedAt: hourBucketTimestamp(log.CreatedAt),
		}
		deltas[key] += delta
	})
	if err != nil {
		return err
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		for key, delta := range deltas {
			result := tx.Table("quota_data").
				Where("user_id = ? and username = ? and model_name = ? and token_name = ? and created_at = ?",
					key.UserID, key.Username, key.ModelName, key.TokenName, key.CreatedAt).
				Update("token_used", gorm.Expr("token_used + ?", delta))
			if result.Error != nil {
				return result.Error
			}
		}

		option := Option{Key: quotaDataTokenUsedStatsVersionKey}
		if err := tx.FirstOrCreate(&option, Option{Key: quotaDataTokenUsedStatsVersionKey}).Error; err != nil {
			return err
		}
		option.Value = quotaDataTokenUsedStatsVersion
		if err := tx.Save(&option).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := updateOptionMap(quotaDataTokenUsedStatsVersionKey, quotaDataTokenUsedStatsVersion); err != nil {
		return err
	}
	common.SysLog(fmt.Sprintf("quota_data token_used stats migrated to version %s, buckets=%d", quotaDataTokenUsedStatsVersion, len(deltas)))
	return nil
}
