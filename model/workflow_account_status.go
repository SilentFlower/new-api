package model

import "gorm.io/gorm"

// WorkflowAccountStatus 表示工作流账号与同名令牌的非敏感状态。
type WorkflowAccountStatus struct {
	User   *WorkflowUserStatus   `json:"user"`
	Tokens []WorkflowTokenStatus `json:"tokens"`
}

// WorkflowUserStatus 是不包含密码或访问凭证的工作流用户状态。
type WorkflowUserStatus struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        int    `json:"role"`
	Status      int    `json:"status"`
	Group       string `json:"group"`
	Quota       int    `json:"quota"`
}

// WorkflowTokenStatus 是不包含明文 Key 的工作流令牌状态。
type WorkflowTokenStatus struct {
	ID                    int    `json:"id"`
	UserID                int    `json:"user_id"`
	Name                  string `json:"name"`
	Status                int    `json:"status"`
	Group                 string `json:"group"`
	UnlimitedQuota        bool   `json:"unlimited_quota"`
	EffectiveStateMatches bool   `json:"effective_state_matches"`
}

// GetWorkflowAccountStatus 按精确用户名和工作流 ID 返回账号归属。
// @param username 工作流中文用户名。
// @param workflowID 工作流原始 ID。
// @return 账号及令牌状态；账号不存在时 User 为 nil。
func GetWorkflowAccountStatus(username, workflowID string) (*WorkflowAccountStatus, error) {
	var user User
	err := DB.Where("username = ?", username).First(&user).Error
	if err == gorm.ErrRecordNotFound {
		return &WorkflowAccountStatus{Tokens: []WorkflowTokenStatus{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var tokens []Token
	if err := DB.Where("user_id = ? AND name = ?", user.Id, workflowID).Find(&tokens).Error; err != nil {
		return nil, err
	}
	result := &WorkflowAccountStatus{User: &WorkflowUserStatus{
		ID: user.Id, Username: user.Username, DisplayName: user.DisplayName,
		Role: user.Role, Status: user.Status, Group: user.Group, Quota: user.Quota,
	}, Tokens: make([]WorkflowTokenStatus, 0, len(tokens))}
	for _, token := range tokens {
		// 数据库迁移成功后缓存可能仍指向旧用户，发布 Secret 前须核对实际鉴权路径。
		effective, effectiveErr := ValidateUserToken(token.Key)
		matches := effectiveErr == nil && effective != nil && effective.Id == token.Id &&
			effective.UserId == token.UserId && effective.Name == token.Name &&
			effective.Status == token.Status && sameWorkflowTokenPolicy(token, *effective)
		result.Tokens = append(result.Tokens, WorkflowTokenStatus{
			ID: token.Id, UserID: token.UserId, Name: token.Name,
			Status: token.Status, Group: token.Group, UnlimitedQuota: token.UnlimitedQuota,
			EffectiveStateMatches: matches,
		})
	}
	return result, nil
}
