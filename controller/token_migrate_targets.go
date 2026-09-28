package controller

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type migrateTokenTarget struct {
	TokenId   int    `json:"token_id"`
	Username  string `json:"username"`
	UserQuota *int   `json:"user_quota"`
}

func validateMigrateTargets(ids []int, targets []migrateTokenTarget) (map[int]migrateTokenTarget, error) {
	if targets == nil {
		return nil, nil
	}
	if len(targets) != len(ids) {
		return nil, fmt.Errorf("targets 必须与 token_ids 一一对应")
	}
	maxQuota := int(1000000000 * common.QuotaPerUnit)
	idSet := make(map[int]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || idSet[id] {
			return nil, fmt.Errorf("token_ids 包含无效或重复的令牌 ID")
		}
		idSet[id] = true
	}
	byId := make(map[int]migrateTokenTarget, len(targets))
	usernames := make(map[string]bool, len(targets))
	for _, target := range targets {
		if !idSet[target.TokenId] || byId[target.TokenId].TokenId != 0 {
			return nil, fmt.Errorf("targets 包含不属于 token_ids 的令牌 ID 或重复项")
		}
		if err := model.ValidateMigrationUsername(target.Username); err != nil {
			return nil, fmt.Errorf("token_id=%d: %w", target.TokenId, err)
		}
		if usernames[target.Username] {
			return nil, fmt.Errorf("targets 包含重复用户名")
		}
		if target.UserQuota == nil || *target.UserQuota < 0 || *target.UserQuota > maxQuota {
			return nil, fmt.Errorf("token_id=%d: user_quota 必须介于 0 和 %d 之间", target.TokenId, maxQuota)
		}
		byId[target.TokenId] = target
		usernames[target.Username] = true
	}
	return byId, nil
}
