package model

import (
	"strconv"
	"strings"
)

// UserLimitSummary 描述渠道用户限制视图所需的最小用户信息。
type UserLimitSummary struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// GetUserLimitSummaries 批量查询渠道用户限制视图所需的用户摘要。
//
// @param userIDs 用户 ID 列表。
// @return []UserLimitSummary 已存在且未删除的用户摘要。
// @return error 数据库查询失败时返回错误。
func GetUserLimitSummaries(userIDs []int) ([]UserLimitSummary, error) {
	if len(userIDs) == 0 {
		return []UserLimitSummary{}, nil
	}
	var users []UserLimitSummary
	err := DB.Model(&User{}).
		Select("id, username, display_name").
		Where("id IN ?", userIDs).
		Find(&users).Error
	return users, err
}

// SearchUserLimitSummaries 搜索可配置个人限制的最小用户摘要。
//
// @param keyword 用户 ID、用户名或显示名关键字。
// @param offset 分页偏移量。
// @param limit 分页大小。
// @return []UserLimitSummary 匹配且未删除的最小用户摘要。
// @return int64 匹配用户总数。
// @return error 数据库查询失败时返回错误。
func SearchUserLimitSummaries(keyword string, offset int, limit int) ([]UserLimitSummary, int64, error) {
	query := DB.Model(&User{}).Select("id, username, display_name")
	keyword = strings.TrimSpace(keyword)
	if keyword != "" {
		pattern := "%" + keyword + "%"
		condition := "username LIKE ? OR display_name LIKE ?"
		args := []interface{}{pattern, pattern}
		if userID, err := strconv.Atoi(keyword); err == nil && userID > 0 {
			condition = "id = ? OR " + condition
			args = append([]interface{}{userID}, args...)
		}
		query = query.Where("("+condition+")", args...)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var users []UserLimitSummary
	err := query.Order("id ASC").Offset(offset).Limit(limit).Find(&users).Error
	return users, total, err
}
