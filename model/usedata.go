package model

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// QuotaData 柱状图数据
type QuotaData struct {
	Id        int    `json:"id"`
	UserID    int    `json:"user_id" gorm:"index"`
	Username  string `json:"username" gorm:"index:idx_qdt_model_user_name,priority:2;size:64;default:''"`
	ModelName string `json:"model_name" gorm:"index:idx_qdt_model_user_name,priority:1;size:64;default:''"`
	TokenName string `json:"token_name" gorm:"index;size:64;default:''"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index:idx_qdt_created_at,priority:2"`
	UseGroup  string `json:"use_group" gorm:"index;size:64;default:''"`
	TokenID   int    `json:"token_id" gorm:"index;default:0"`
	ChannelID int    `json:"channel_id" gorm:"index;default:0"`
	NodeName  string `json:"node_name" gorm:"index;size:64;default:''"`
	TokenUsed int    `json:"token_used" gorm:"default:0"`
	Count     int    `json:"count" gorm:"default:0"`
	Quota     int    `json:"quota" gorm:"default:0"`
}

type QuotaDataLogParams struct {
	UserID    int
	Username  string
	ModelName string
	TokenName string
	Quota     int
	CreatedAt int64
	TokenUsed int
	UseGroup  string
	TokenID   int
	ChannelID int
	NodeName  string
}

func UpdateQuotaData() {
	for {
		if common.DataExportEnabled {
			common.SysLog("正在更新数据看板数据...")
			SaveQuotaDataCache()
		}
		time.Sleep(time.Duration(common.DataExportInterval) * time.Minute)
	}
}

var CacheQuotaData = make(map[string]*QuotaData)
var CacheQuotaDataLock = sync.Mutex{}

func logQuotaDataCache(quotaData *QuotaData) {
	key := fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%d\x00%s\x00%d\x00%d\x00%s",
		quotaData.UserID,
		quotaData.Username,
		quotaData.ModelName,
		quotaData.TokenName,
		quotaData.CreatedAt,
		quotaData.UseGroup,
		quotaData.TokenID,
		quotaData.ChannelID,
		quotaData.NodeName,
	)
	count := quotaData.Count
	quota := quotaData.Quota
	tokenUsed := quotaData.TokenUsed
	cachedQuotaData, ok := CacheQuotaData[key]
	if ok {
		cachedQuotaData.Count += count
		cachedQuotaData.Quota += quota
		cachedQuotaData.TokenUsed += tokenUsed
		quotaData = cachedQuotaData
	}
	CacheQuotaData[key] = quotaData
}

// LogQuotaData 记录配额数据到缓存，按小时粒度聚合。
func LogQuotaData(params QuotaDataLogParams) {
	// 只精确到小时
	createdAt := params.CreatedAt - (params.CreatedAt % 3600)
	quotaData := &QuotaData{
		UserID:    params.UserID,
		Username:  params.Username,
		ModelName: params.ModelName,
		TokenName: params.TokenName,
		CreatedAt: createdAt,
		UseGroup:  params.UseGroup,
		TokenID:   params.TokenID,
		ChannelID: params.ChannelID,
		NodeName:  params.NodeName,
		Count:     1,
		Quota:     params.Quota,
		TokenUsed: params.TokenUsed,
	}

	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	logQuotaDataCache(quotaData)
}

func SaveQuotaDataCache() {
	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	size := len(CacheQuotaData)
	// 如果缓存中有数据，就保存到数据库中
	// 1. 先查询数据库中是否有数据
	// 2. 如果有数据，就更新数据
	// 3. 如果没有数据，就插入数据
	for _, quotaData := range CacheQuotaData {
		quotaDataDB := &QuotaData{}
		DB.Table("quota_data").
			Where("user_id = ? and username = ? and model_name = ? and token_name = ? and created_at = ? and use_group = ? and token_id = ? and channel_id = ? and node_name = ?",
				quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.TokenName, quotaData.CreatedAt, quotaData.UseGroup, quotaData.TokenID, quotaData.ChannelID, quotaData.NodeName).
			First(quotaDataDB)
		if quotaDataDB.Id > 0 {
			increaseQuotaData(quotaData)
		} else {
			DB.Table("quota_data").Create(quotaData)
		}
	}
	CacheQuotaData = make(map[string]*QuotaData)
	common.SysLog(fmt.Sprintf("保存数据看板数据成功，共保存%d条数据", size))
}

func increaseQuotaData(quotaData *QuotaData) {
	err := DB.Table("quota_data").
		Where("user_id = ? and username = ? and model_name = ? and token_name = ? and created_at = ? and use_group = ? and token_id = ? and channel_id = ? and node_name = ?",
			quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.TokenName, quotaData.CreatedAt, quotaData.UseGroup, quotaData.TokenID, quotaData.ChannelID, quotaData.NodeName).
		Updates(map[string]interface{}{
			"count":      gorm.Expr("count + ?", quotaData.Count),
			"quota":      gorm.Expr("quota + ?", quotaData.Quota),
			"token_used": gorm.Expr("token_used + ?", quotaData.TokenUsed),
		}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("increaseQuotaData error: %s", err))
	}
}

// getQuotaDataFromLogs 从 logs 表按筛选条件聚合查询配额数据。
func getQuotaDataFromLogs(startTime int64, endTime int64, tokenNames []string, groups []string, userFilter string, userFilterValue interface{}, groupByUser bool) ([]*QuotaData, error) {
	tx := LOG_DB.Model(&Log{}).
		Where("type = ? and created_at >= ? and created_at <= ?", LogTypeConsume, startTime, endTime)
	if userFilter != "" {
		tx = tx.Where(userFilter, userFilterValue)
	}
	if len(tokenNames) > 0 {
		tx = tx.Where("token_name IN ?", tokenNames)
	}
	if len(groups) > 0 {
		tx = tx.Where(logGroupCol+" IN ?", groups)
	}
	return aggregateQuotaDataFromLogQuery(tx, groupByUser)
}

// GetQuotaDataByUsername 按用户名查询配额数据，支持按 tokenName 过滤
// 当指定 tokenName 时从 logs 表查询以支持历史数据
func GetQuotaDataByUsername(username string, startTime int64, endTime int64, tokenName string) (quotaData []*QuotaData, err error) {
	return GetQuotaDataByUsernameWithFilters(username, startTime, endTime, singleValueSlice(tokenName), nil)
}

// GetQuotaDataByUsernameWithFilters 按用户名查询配额数据，支持多令牌和多分组过滤。
// @param username 用户名
// @param startTime 开始时间戳
// @param endTime 结束时间戳
// @param tokenNames API Key 名称过滤列表
// @param groups 分组过滤列表
// @return 配额统计数据和错误
func GetQuotaDataByUsernameWithFilters(username string, startTime int64, endTime int64, tokenNames []string, groups []string) (quotaData []*QuotaData, err error) {
	if len(tokenNames) > 0 || len(groups) > 0 {
		return getQuotaDataFromLogs(startTime, endTime, tokenNames, groups, "username = ?", username, true)
	}
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

// GetQuotaDataByUserId 按用户ID查询配额数据，支持按 tokenName 过滤
// 当指定 tokenName 时从 logs 表查询以支持历史数据
func GetQuotaDataByUserId(userId int, startTime int64, endTime int64, tokenName string) (quotaData []*QuotaData, err error) {
	return GetQuotaDataByUserIdWithFilters(userId, startTime, endTime, singleValueSlice(tokenName))
}

// GetQuotaDataByUserIdWithFilters 按用户 ID 查询配额数据，支持多令牌过滤。
// @param userId 用户 ID
// @param startTime 开始时间戳
// @param endTime 结束时间戳
// @param tokenNames API Key 名称过滤列表
// @return 配额统计数据和错误
func GetQuotaDataByUserIdWithFilters(userId int, startTime int64, endTime int64, tokenNames []string) (quotaData []*QuotaData, err error) {
	if len(tokenNames) > 0 {
		return getQuotaDataFromLogs(startTime, endTime, tokenNames, nil, "user_id = ?", userId, true)
	}
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("user_id = ? and created_at >= ? and created_at <= ?", userId, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataGroupByUser(startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	return GetQuotaDataGroupByUserWithFilters(startTime, endTime, "", nil, nil)
}

// GetQuotaDataGroupByUserWithFilters 按用户维度聚合查询配额数据，支持用户名、多令牌和多分组过滤。
// @param startTime 开始时间戳
// @param endTime 结束时间戳
// @param username 用户名过滤
// @param tokenNames API Key 名称过滤列表
// @param groups 分组过滤列表
// @return 用户维度配额统计数据和错误
func GetQuotaDataGroupByUserWithFilters(startTime int64, endTime int64, username string, tokenNames []string, groups []string) (quotaData []*QuotaData, err error) {
	if len(tokenNames) > 0 || len(groups) > 0 {
		return getQuotaDataFromLogs(startTime, endTime, tokenNames, groups, usernameFilterCondition(username), username, true)
	}
	var quotaDatas []*QuotaData
	tx := DB.Table("quota_data").
		Select("username, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("created_at >= ? and created_at <= ?", startTime, endTime)
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	err = tx.Group("username, created_at").Find(&quotaDatas).Error
	return quotaDatas, err
}

// GetAllQuotaDates 查询所有配额数据，支持按用户名和 tokenName 过滤
// 当指定 tokenName 时从 logs 表查询以支持历史数据
// 当未指定 username 时，按 model_name + created_at 聚合统计
func GetAllQuotaDates(startTime int64, endTime int64, username string, tokenName string) (quotaData []*QuotaData, err error) {
	return GetAllQuotaDatesWithFilters(startTime, endTime, username, singleValueSlice(tokenName), nil)
}

// GetAllQuotaDatesWithFilters 查询所有配额数据，支持用户名、多令牌和多分组过滤。
// @param startTime 开始时间戳
// @param endTime 结束时间戳
// @param username 用户名过滤
// @param tokenNames API Key 名称过滤列表
// @param groups 分组过滤列表
// @return 配额统计数据和错误
func GetAllQuotaDatesWithFilters(startTime int64, endTime int64, username string, tokenNames []string, groups []string) (quotaData []*QuotaData, err error) {
	if username != "" {
		return GetQuotaDataByUsernameWithFilters(username, startTime, endTime, tokenNames, groups)
	}
	if len(tokenNames) > 0 || len(groups) > 0 {
		return getQuotaDataFromLogs(startTime, endTime, tokenNames, groups, "", nil, false)
	}
	var quotaDatas []*QuotaData
	err = DB.Table("quota_data").Select("model_name, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, created_at").Where("created_at >= ? and created_at <= ?", startTime, endTime).Group("model_name, created_at").Find(&quotaDatas).Error
	return quotaDatas, err
}

func singleValueSlice(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

func usernameFilterCondition(username string) string {
	if username == "" {
		return ""
	}
	return "username = ?"
}
