package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"

	"gorm.io/gorm"
)

const (
	// TokenLeakScanStatusNotFound 表示本轮没有发现完整令牌命中。
	TokenLeakScanStatusNotFound = "not_found"
	// TokenLeakScanStatusFound 表示本轮发现至少一个完整令牌命中。
	TokenLeakScanStatusFound = "found"
	// TokenLeakScanStatusIncomplete 表示搜索或候选下载不完整。
	TokenLeakScanStatusIncomplete = "incomplete"
	// TokenLeakScanStatusFailed 表示本轮扫描失败。
	TokenLeakScanStatusFailed = "failed"

	// TokenLeakFindingStatusOpen 表示泄露位置仍需处置。
	TokenLeakFindingStatusOpen = "open"
	// TokenLeakFindingStatusMitigated 表示对应令牌已禁用或删除。
	TokenLeakFindingStatusMitigated = "mitigated"

	// TokenLeakNotificationStatusPending 表示通知尚未完成。
	TokenLeakNotificationStatusPending = "pending"
	// TokenLeakNotificationStatusSucceeded 表示通知发送成功。
	TokenLeakNotificationStatusSucceeded = "succeeded"
	// TokenLeakNotificationStatusFailed 表示通知在有限重试后失败。
	TokenLeakNotificationStatusFailed = "failed"
)

// TokenLeakScanState 保存单个用户令牌最近一次公开泄露扫描结果。
type TokenLeakScanState struct {
	ID                  int64  `json:"id" gorm:"primaryKey"`
	TokenID             int    `json:"token_id" gorm:"uniqueIndex"`
	UserID              int    `json:"user_id" gorm:"index"`
	TokenFingerprint    string `json:"-" gorm:"type:varchar(64)"`
	TokenStatus         int    `json:"token_status" gorm:"index"`
	ScanStatus          string `json:"scan_status" gorm:"type:varchar(32);index"`
	ErrorCode           string `json:"error_code" gorm:"type:varchar(64)"`
	CandidateCount      int    `json:"candidate_count"`
	ExactMatchCount     int    `json:"exact_match_count"`
	SearchRequestCount  int    `json:"search_request_count"`
	LastScanStartedAt   int64  `json:"last_scan_started_at" gorm:"bigint;index"`
	LastScanCompletedAt int64  `json:"last_scan_completed_at" gorm:"bigint;index"`
	CreatedAt           int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt           int64  `json:"updated_at" gorm:"bigint;index"`
}

// TokenLeakFinding 保存经本地完整令牌比对确认的公开泄露位置。
type TokenLeakFinding struct {
	ID               int64  `json:"id" gorm:"primaryKey"`
	FindingKey       string `json:"-" gorm:"type:varchar(64);uniqueIndex"`
	TokenID          int    `json:"token_id" gorm:"index"`
	UserID           int    `json:"user_id" gorm:"index"`
	TokenName        string `json:"token_name" gorm:"type:text"`
	TokenFingerprint string `json:"-" gorm:"type:varchar(64)"`
	RepositoryID     int64  `json:"repository_id" gorm:"bigint;index"`
	RepositoryName   string `json:"repository_name" gorm:"type:varchar(255)"`
	FilePath         string `json:"file_path" gorm:"type:text"`
	BlobSHA          string `json:"blob_sha" gorm:"type:varchar(64)"`
	HTMLURL          string `json:"html_url" gorm:"type:text"`
	Status           string `json:"status" gorm:"type:varchar(32);index"`
	ReopenCount      int    `json:"-"`
	FirstFoundAt     int64  `json:"first_found_at" gorm:"bigint;index"`
	LastFoundAt      int64  `json:"last_found_at" gorm:"bigint;index"`
	LastNotifiedAt   int64  `json:"last_notified_at" gorm:"bigint"`
	LastReminderAt   int64  `json:"last_reminder_at" gorm:"bigint"`
	MitigatedAt      int64  `json:"mitigated_at" gorm:"bigint"`
	MitigationReason string `json:"mitigation_reason" gorm:"type:varchar(64)"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt        int64  `json:"updated_at" gorm:"bigint;index"`
}

// TokenLeakNotification 保存泄露告警的发送审计，不保存消息正文或外部凭据。
type TokenLeakNotification struct {
	ID           int64  `json:"id" gorm:"primaryKey"`
	EventKey     string `json:"-" gorm:"type:varchar(64);uniqueIndex"`
	FindingID    int64  `json:"finding_id" gorm:"bigint;index"`
	Channel      string `json:"channel" gorm:"type:varchar(32);index"`
	Trigger      string `json:"trigger" gorm:"type:varchar(32);index"`
	Status       string `json:"status" gorm:"type:varchar(32);index"`
	AttemptCount int    `json:"attempt_count"`
	ErrorCode    string `json:"error_code" gorm:"type:varchar(64)"`
	CompletedAt  int64  `json:"completed_at" gorm:"bigint"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt    int64  `json:"updated_at" gorm:"bigint;index"`
}

// ListTokensForLeakScan 返回全部未软删除令牌及扫描所需字段。
//
// @return 用户令牌列表和数据库错误。
func ListTokensForLeakScan() ([]Token, error) {
	var tokens []Token
	err := DB.Select([]string{"id", "user_id", commonKeyCol, "status", "name", "expired_time", "remain_quota", "unlimited_quota"}).Find(&tokens).Error
	return tokens, err
}

// GetTokenForLeakScanByID 返回指定未软删除令牌及扫描所需字段。
//
// @param tokenID 令牌 ID。
// @return 令牌；不存在时返回 nil、nil。
func GetTokenForLeakScanByID(tokenID int) (*Token, error) {
	var token Token
	err := DB.Select([]string{"id", "user_id", commonKeyCol, "status", "name", "expired_time", "remain_quota", "unlimited_quota"}).First(&token, "id = ?", tokenID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &token, err
}

// ListTokenLeakScanStates 返回全部令牌扫描状态。
//
// @return 扫描状态列表和数据库错误。
func ListTokenLeakScanStates() ([]TokenLeakScanState, error) {
	var states []TokenLeakScanState
	err := DB.Find(&states).Error
	return states, err
}

// GetLatestTokenLeakGitHubCheckState 返回最近一次实际发起 GitHub 搜索请求的扫描状态。
//
// @return 最近 GitHub 检查状态；不存在时返回 nil、nil。
func GetLatestTokenLeakGitHubCheckState() (*TokenLeakScanState, error) {
	var state TokenLeakScanState
	err := DB.Where("search_request_count > ?", 0).Order("last_scan_completed_at desc, id desc").First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &state, err
}

// SaveTokenLeakScanState 按 token ID 新增或覆盖最近扫描状态。
//
// @param state 待保存的扫描状态。
// @return 数据库错误。
func SaveTokenLeakScanState(state *TokenLeakScanState) error {
	now := common.GetTimestamp()
	var existing TokenLeakScanState
	err := DB.Where("token_id = ?", state.TokenID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		state.CreatedAt = now
		state.UpdatedAt = now
		return DB.Create(state).Error
	}
	if err != nil {
		return err
	}
	state.ID = existing.ID
	state.CreatedAt = existing.CreatedAt
	state.UpdatedAt = now
	return DB.Model(&existing).Updates(map[string]any{
		"user_id":                state.UserID,
		"token_fingerprint":      state.TokenFingerprint,
		"token_status":           state.TokenStatus,
		"scan_status":            state.ScanStatus,
		"error_code":             state.ErrorCode,
		"candidate_count":        state.CandidateCount,
		"exact_match_count":      state.ExactMatchCount,
		"search_request_count":   state.SearchRequestCount,
		"last_scan_started_at":   state.LastScanStartedAt,
		"last_scan_completed_at": state.LastScanCompletedAt,
		"updated_at":             now,
	}).Error
}

// GetTokenLeakFindingByKey 按稳定幂等键查询泄露位置。
//
// @param findingKey 泄露位置幂等键。
// @return 泄露位置；不存在时返回 nil、nil。
func GetTokenLeakFindingByKey(findingKey string) (*TokenLeakFinding, error) {
	var finding TokenLeakFinding
	err := DB.Where("finding_key = ?", findingKey).First(&finding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &finding, err
}

// CreateTokenLeakFinding 创建新的泄露位置记录。
//
// @param finding 待创建的泄露位置。
// @return 数据库错误。
func CreateTokenLeakFinding(finding *TokenLeakFinding) error {
	now := common.GetTimestamp()
	finding.CreatedAt = now
	finding.UpdatedAt = now
	return DB.Create(finding).Error
}

// UpdateTokenLeakFinding 更新泄露位置的指定字段。
//
// @param findingID 泄露位置 ID。
// @param updates 待更新字段。
// @return 数据库错误。
func UpdateTokenLeakFinding(findingID int64, updates map[string]any) error {
	updates["updated_at"] = common.GetTimestamp()
	return DB.Model(&TokenLeakFinding{}).Where("id = ?", findingID).Updates(updates).Error
}

// MitigateTokenLeakFindingsByTokenID 将指定令牌的全部开放泄露位置标记为已处置。
//
// @param tokenID 令牌 ID。
// @param reason 处置原因。
// @return 数据库错误。
func MitigateTokenLeakFindingsByTokenID(tokenID int, reason string) error {
	return mitigateTokenLeakFindingsByTokenID(DB, tokenID, reason)
}

// DisableTokenForLeakFinding 在单个事务中禁用泄露记录对应令牌并处置其全部开放记录。
//
// @param findingID 泄露记录 ID。
// @return 泄露记录、令牌；令牌已删除时令牌为 nil；以及数据库错误。
func DisableTokenForLeakFinding(findingID int64) (*TokenLeakFinding, *Token, error) {
	finding := &TokenLeakFinding{}
	var token *Token
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(finding, "id = ?", findingID).Error; err != nil {
			return err
		}

		storedToken := &Token{}
		err := lockForUpdate(tx).First(storedToken, "id = ? AND user_id = ?", finding.TokenID, finding.UserID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return mitigateTokenLeakFindingsByTokenID(tx, finding.TokenID, "token_deleted")
		}
		if err != nil {
			return err
		}
		if storedToken.Status != common.TokenStatusDisabled {
			result := tx.Model(&Token{}).
				Where("id = ? AND user_id = ?", storedToken.Id, storedToken.UserId).
				Update("status", common.TokenStatusDisabled)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("token_disable_conflict")
			}
			storedToken.Status = common.TokenStatusDisabled
		}
		if err := mitigateTokenLeakFindingsByTokenID(tx, storedToken.Id, "token_disabled"); err != nil {
			return err
		}
		token = storedToken
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if token != nil && common.RedisEnabled {
		cachedToken := *token
		gopool.Go(func() {
			if err := cacheSetToken(cachedToken); err != nil {
				common.SysLog("failed to update token cache: " + err.Error())
			}
		})
	}
	return finding, token, nil
}

func mitigateTokenLeakFindingsByTokenID(tx *gorm.DB, tokenID int, reason string) error {
	now := common.GetTimestamp()
	return tx.Model(&TokenLeakFinding{}).
		Where("token_id = ? AND status = ?", tokenID, TokenLeakFindingStatusOpen).
		Updates(map[string]any{
			"status":            TokenLeakFindingStatusMitigated,
			"mitigated_at":      now,
			"mitigation_reason": reason,
			"updated_at":        now,
		}).Error
}

// GetTokenLeakFindingByID 返回指定泄露位置。
//
// @param findingID 泄露位置 ID。
// @return 泄露位置；不存在时返回 nil、nil。
func GetTokenLeakFindingByID(findingID int64) (*TokenLeakFinding, error) {
	var finding TokenLeakFinding
	err := DB.First(&finding, "id = ?", findingID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &finding, err
}

// ListOpenTokenLeakFindings 返回全部未处置泄露位置。
//
// @return 未处置泄露位置和数据库错误。
func ListOpenTokenLeakFindings() ([]TokenLeakFinding, error) {
	var findings []TokenLeakFinding
	err := DB.Where("status = ?", TokenLeakFindingStatusOpen).Find(&findings).Error
	return findings, err
}

// ListTokenLeakFindings 分页返回泄露位置。
//
// @param status 可选状态过滤。
// @param page 页码，从 1 开始。
// @param pageSize 每页数量。
// @return 泄露位置、总数和数据库错误。
func ListTokenLeakFindings(status string, page int, pageSize int) ([]TokenLeakFinding, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	query := DB.Model(&TokenLeakFinding{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var findings []TokenLeakFinding
	err := query.Order("last_found_at desc, id desc").Limit(pageSize).Offset((page - 1) * pageSize).Find(&findings).Error
	return findings, total, err
}

// CountTokenLeakFindingsByStatus 统计指定状态的泄露位置数量。
//
// @param status 泄露位置状态。
// @return 数量和数据库错误。
func CountTokenLeakFindingsByStatus(status string) (int64, error) {
	var count int64
	err := DB.Model(&TokenLeakFinding{}).Where("status = ?", status).Count(&count).Error
	return count, err
}

// GetTokenLeakNotificationByEventKey 查询幂等通知事件。
//
// @param eventKey 通知事件幂等键。
// @return 通知记录；不存在时返回 nil、nil。
func GetTokenLeakNotificationByEventKey(eventKey string) (*TokenLeakNotification, error) {
	var notification TokenLeakNotification
	err := DB.Where("event_key = ?", eventKey).First(&notification).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &notification, err
}

// CreateTokenLeakNotification 创建通知审计记录。
//
// @param notification 待创建的通知记录。
// @return 数据库错误。
func CreateTokenLeakNotification(notification *TokenLeakNotification) error {
	now := common.GetTimestamp()
	notification.CreatedAt = now
	notification.UpdatedAt = now
	return DB.Create(notification).Error
}

// UpdateTokenLeakNotification 更新通知审计状态。
//
// @param notificationID 通知记录 ID。
// @param updates 待更新字段。
// @return 数据库错误。
func UpdateTokenLeakNotification(notificationID int64, updates map[string]any) error {
	updates["updated_at"] = common.GetTimestamp()
	return DB.Model(&TokenLeakNotification{}).Where("id = ?", notificationID).Updates(updates).Error
}

// ListTokenLeakNotificationsByFindingIDs 返回指定泄露位置的通知审计。
//
// @param findingIDs 泄露位置 ID 列表。
// @return 通知审计列表和数据库错误。
func ListTokenLeakNotificationsByFindingIDs(findingIDs []int64) ([]TokenLeakNotification, error) {
	if len(findingIDs) == 0 {
		return []TokenLeakNotification{}, nil
	}
	var notifications []TokenLeakNotification
	err := DB.Where("finding_id IN ?", findingIDs).Order("id desc").Find(&notifications).Error
	return notifications, err
}
