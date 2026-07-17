package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// SumUsedQuotaWithFilters 汇总消费额度和最近 60 秒 RPM/TPM，支持多令牌和多分组过滤。
// @param logType 日志类型
// @param startTimestamp 开始时间戳
// @param endTimestamp 结束时间戳
// @param modelName 模型名称过滤
// @param username 用户名过滤
// @param tokenNames API Key 名称过滤列表
// @param channel 渠道 ID 过滤
// @param groups 分组过滤列表
// @return 统计结果和错误
func SumUsedQuotaWithFilters(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenNames []string, channel int, groups []string) (stat Stat, err error) {
	tx := LOG_DB.Table("logs").Select("COALESCE(sum(quota), 0) quota")

	// 为 RPM 和 TPM 创建单独的查询；TPM 需要按统一统计口径在 Go 侧补算 Anthropic 缓存 Token。
	rpmTpmQuery := LOG_DB.Model(&Log{})

	if tx, err = applyExplicitLogTextFilter(tx, "username", username); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "username", username); err != nil {
		return stat, err
	}
	tx = applyLogTokenNamesFilter(tx, tokenNames)
	rpmTpmQuery = applyLogTokenNamesFilter(rpmTpmQuery, tokenNames)
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if tx, err = applyExplicitLogTextFilter(tx, "model_name", modelName); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "model_name", modelName); err != nil {
		return stat, err
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
		rpmTpmQuery = rpmTpmQuery.Where("channel_id = ?", channel)
	}
	tx = applyLogGroupsFilter(tx, groups)
	rpmTpmQuery = applyLogGroupsFilter(rpmTpmQuery, groups)

	tx = tx.Where("type = ?", LogTypeConsume)
	rpmTpmQuery = rpmTpmQuery.Where("type = ?", LogTypeConsume)

	// 只统计最近60秒的rpm和tpm
	rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())

	// 执行查询
	if err := tx.Scan(&stat).Error; err != nil {
		common.SysError("failed to query log stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	var rpmStat struct {
		Rpm int `json:"rpm"`
	}
	if err := rpmTpmQuery.Select("count(*) rpm").Scan(&rpmStat).Error; err != nil {
		common.SysError("failed to query rpm/tpm stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	stat.Rpm = rpmStat.Rpm
	tpm, err := sumStatisticTokenUsedFromLogQuery(rpmTpmQuery)
	if err != nil {
		common.SysError("failed to query rpm/tpm token stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	stat.Tpm = tpm

	return stat, nil
}
