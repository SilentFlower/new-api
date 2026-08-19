package model

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
