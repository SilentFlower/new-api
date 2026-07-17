package model

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

// GetQuotaDataGroupByUser 按用户维度聚合查询配额数据。
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
