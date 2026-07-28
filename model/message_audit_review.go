package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// MessageAuditReview 保存推断会话最后一次成功审核结果和当前重审状态。
type MessageAuditReview struct {
	ID                int64  `json:"id" gorm:"primaryKey"`
	AuditSessionID    string `json:"audit_session_id" gorm:"type:varchar(64);uniqueIndex"`
	UserID            int    `json:"user_id" gorm:"index"`
	ReviewedRequestID string `json:"reviewed_request_id" gorm:"type:varchar(64);index"`
	CurrentTaskID     string `json:"current_task_id" gorm:"type:varchar(64);index"`
	Status            string `json:"status" gorm:"type:varchar(32);index"`
	RiskLevel         string `json:"risk_level" gorm:"type:varchar(16);index"`
	ReviewChannelID   int    `json:"review_channel_id"`
	ReviewModel       string `json:"review_model" gorm:"type:varchar(256)"`
	KeyFingerprint    string `json:"-" gorm:"type:varchar(32);index"`
	ResultNonce       []byte `json:"-"`
	ResultCiphertext  []byte `json:"-"`
	ReviewedAt        int64  `json:"reviewed_at"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

// MessageAuditReviewSource 保存审核结果所引用的固定消息审计请求。
type MessageAuditReviewSource struct {
	ID        int64  `json:"id" gorm:"primaryKey"`
	ReviewID  int64  `json:"review_id" gorm:"index;uniqueIndex:idx_message_audit_review_source,priority:1"`
	RequestID string `json:"request_id" gorm:"type:varchar(64);index;uniqueIndex:idx_message_audit_review_source,priority:2"`
	CreatedAt int64  `json:"created_at"`
}

// GetLatestMessageAuditSessionRequest 返回推断会话最新请求。
//
// @param auditSessionID 推断会话 ID。
// @return 最新请求；会话不存在时返回 gorm.ErrRecordNotFound。
func GetLatestMessageAuditSessionRequest(auditSessionID string) (*MessageAuditRequest, error) {
	var request MessageAuditRequest
	err := DB.Where("audit_session_id = ?", auditSessionID).Order("id desc").First(&request).Error
	return &request, err
}

// BuildMessageAuditReviewSourceIDs 固定多次压缩会话的审核资料来源。
//
// @param auditSessionID 推断会话 ID。
// @param targetRequestID 触发时固定的最新请求 ID。
// @return 每个压缩断点的压缩前请求和目标最新请求，按时间升序去重。
func BuildMessageAuditReviewSourceIDs(auditSessionID string, targetRequestID string) ([]string, error) {
	var requests []MessageAuditRequest
	if err := DB.Select("id, request_id, parent_request_id, session_match").
		Where("audit_session_id = ?", auditSessionID).
		Where("id <= (SELECT id FROM message_audit_requests WHERE request_id = ?)", targetRequestID).
		Order("id asc").Find(&requests).Error; err != nil {
		return nil, err
	}
	if len(requests) == 0 || requests[len(requests)-1].RequestID != targetRequestID {
		return nil, gorm.ErrRecordNotFound
	}
	requestIDs := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		requestIDs[request.RequestID] = struct{}{}
	}
	seen := make(map[string]struct{})
	sources := make([]string, 0)
	for _, request := range requests {
		if request.SessionMatch != "compressed" || request.ParentRequestID == "" {
			continue
		}
		if _, ok := requestIDs[request.ParentRequestID]; !ok {
			continue
		}
		if _, ok := seen[request.ParentRequestID]; ok {
			continue
		}
		seen[request.ParentRequestID] = struct{}{}
		sources = append(sources, request.ParentRequestID)
	}
	if _, ok := seen[targetRequestID]; !ok {
		sources = append(sources, targetRequestID)
	}
	return sources, nil
}

// GetMessageAuditReview 返回推断会话当前审核行。
//
// @param auditSessionID 推断会话 ID。
// @return 审核行；未审核时返回 nil、nil。
func GetMessageAuditReview(auditSessionID string) (*MessageAuditReview, error) {
	var review MessageAuditReview
	err := DB.Where("audit_session_id = ?", auditSessionID).First(&review).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &review, err
}

// CreateMessageAuditReviewTask 在同一事务中创建系统任务和对应审核状态。
//
// @param activeKey 同会话活动任务幂等键。
// @param payload 不包含正文的固定任务输入。
// @param review 会话、用户和本次审核配置元数据。
// @return 已提交的系统任务或序列化、数据库错误。
func CreateMessageAuditReviewTask(activeKey string, payload any, review MessageAuditReview) (*SystemTask, error) {
	taskID, err := GenerateSystemTaskID()
	if err != nil {
		return nil, err
	}
	payloadText, err := marshalSystemTaskJSON(payload)
	if err != nil {
		return nil, err
	}
	task := &SystemTask{
		TaskID:    taskID,
		Type:      SystemTaskTypeMessageAuditReview,
		Status:    SystemTaskStatusPending,
		ActiveKey: &activeKey,
		Payload:   payloadText,
	}
	review.CurrentTaskID = task.TaskID
	review.Status = string(task.Status)
	if err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		return upsertMessageAuditReviewTask(tx, review)
	}); err != nil {
		return nil, err
	}
	return task, nil
}

func upsertMessageAuditReviewTask(tx *gorm.DB, review MessageAuditReview) error {
	now := common.GetTimestamp()
	if review.CreatedAt == 0 {
		review.CreatedAt = now
	}
	review.UpdatedAt = now
	var existing MessageAuditReview
	err := lockForUpdate(tx).Where("audit_session_id = ?", review.AuditSessionID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tx.Create(&review).Error
	}
	if err != nil {
		return err
	}
	updates := map[string]any{
		"current_task_id": review.CurrentTaskID,
		"status":          review.Status,
		"updated_at":      now,
	}
	// 已成功的结果必须继续展示其实际审核模型，只有新结果成功后才能替换归属信息。
	if len(existing.ResultCiphertext) == 0 {
		updates["review_channel_id"] = review.ReviewChannelID
		updates["review_model"] = review.ReviewModel
	}
	if err := tx.Model(&MessageAuditReview{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
		return err
	}
	if existing.CurrentTaskID == "" || existing.CurrentTaskID == review.CurrentTaskID {
		return nil
	}
	if err := tx.Where("task_id = ?", existing.CurrentTaskID).Delete(&SystemTaskLock{}).Error; err != nil {
		return err
	}
	return tx.Where("task_id = ? AND type = ?", existing.CurrentTaskID, SystemTaskTypeMessageAuditReview).Delete(&SystemTask{}).Error
}

// UpdateMessageAuditReviewStatus 按任务 ID 更新审核运行状态。
//
// @param taskID 系统任务 ID。
// @param status 待写入状态。
// @return 数据库写入错误。
func UpdateMessageAuditReviewStatus(taskID string, status string) error {
	result := DB.Model(&MessageAuditReview{}).Where("current_task_id = ?", taskID).Updates(map[string]any{
		"status":     status,
		"updated_at": common.GetTimestamp(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// CompleteMessageAuditReview 原子替换审核结果、来源引用并完成系统任务。
//
// @param taskID 系统任务 ID。
// @param runnerID 当前任务执行器 ID。
// @param review 已加密的新审核结果元数据。
// @param sourceRequestIDs 本次固定资料来源请求 ID。
// @return 来源过期、锁丢失或数据库错误。
func CompleteMessageAuditReview(taskID string, runnerID string, review MessageAuditReview, sourceRequestIDs []string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		now := common.GetTimestamp()
		var task SystemTask
		if err := lockForUpdate(tx).Where("task_id = ? AND status = ? AND locked_by = ?", taskID, SystemTaskStatusRunning, runnerID).First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSystemTaskLockLost
			}
			return err
		}
		var lockCount int64
		if err := tx.Model(&SystemTaskLock{}).Where("task_id = ? AND locked_by = ? AND locked_until >= ?", taskID, runnerID, now).Count(&lockCount).Error; err != nil {
			return err
		}
		if lockCount != 1 {
			return ErrSystemTaskLockLost
		}
		var sourceCount int64
		if err := tx.Model(&MessageAuditRequest{}).Where("request_id IN ?", sourceRequestIDs).Count(&sourceCount).Error; err != nil {
			return err
		}
		if sourceCount != int64(len(sourceRequestIDs)) {
			return errors.New("source_expired")
		}

		var existing MessageAuditReview
		if err := lockForUpdate(tx).Where("audit_session_id = ? AND current_task_id = ?", review.AuditSessionID, taskID).First(&existing).Error; err != nil {
			return err
		}
		updates := map[string]any{
			"reviewed_request_id": review.ReviewedRequestID,
			"status":              "succeeded",
			"risk_level":          review.RiskLevel,
			"review_channel_id":   review.ReviewChannelID,
			"review_model":        review.ReviewModel,
			"key_fingerprint":     review.KeyFingerprint,
			"result_nonce":        review.ResultNonce,
			"result_ciphertext":   review.ResultCiphertext,
			"reviewed_at":         review.ReviewedAt,
			"updated_at":          now,
		}
		if err := tx.Model(&MessageAuditReview{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Where("review_id = ?", existing.ID).Delete(&MessageAuditReviewSource{}).Error; err != nil {
			return err
		}
		sources := make([]MessageAuditReviewSource, 0, len(sourceRequestIDs))
		for _, requestID := range sourceRequestIDs {
			sources = append(sources, MessageAuditReviewSource{ReviewID: existing.ID, RequestID: requestID, CreatedAt: now})
		}
		if len(sources) > 0 {
			if err := tx.Create(&sources).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&SystemTask{}).Where("id = ?", task.ID).Updates(map[string]any{
			"status":     SystemTaskStatusSucceeded,
			"active_key": nil,
			"result":     "",
			"error":      "",
			"updated_at": now,
		}).Error; err != nil {
			return err
		}
		return tx.Where("task_id = ? AND locked_by = ?", taskID, runnerID).Delete(&SystemTaskLock{}).Error
	})
}

// AttachMessageAuditReviewMetadata 为列表请求批量附加无正文审核元数据。
//
// @param requests 消息审计列表行。
// @return 数据库查询错误。
func AttachMessageAuditReviewMetadata(requests []MessageAuditRequest) error {
	sessionIDs := make([]string, 0, len(requests))
	for _, request := range requests {
		if request.AuditSessionID != "" {
			sessionIDs = append(sessionIDs, request.AuditSessionID)
		}
	}
	if len(sessionIDs) == 0 {
		return nil
	}
	var reviews []MessageAuditReview
	if err := DB.Select("audit_session_id, reviewed_request_id, status, risk_level, reviewed_at").Where("audit_session_id IN ?", sessionIDs).Find(&reviews).Error; err != nil {
		return err
	}
	bySession := make(map[string]MessageAuditReview, len(reviews))
	for _, review := range reviews {
		bySession[review.AuditSessionID] = review
	}
	for index := range requests {
		review, ok := bySession[requests[index].AuditSessionID]
		if !ok {
			requests[index].ReviewStatus = "unreviewed"
			continue
		}
		requests[index].ReviewStatus = review.Status
		requests[index].ReviewRiskLevel = review.RiskLevel
		requests[index].ReviewedAt = review.ReviewedAt
		requests[index].ReviewStale = review.ReviewedRequestID != "" && review.ReviewedRequestID != requests[index].RequestID
	}
	return nil
}

// DeleteMessageAuditReviewsForRequestIDs 删除引用指定请求的审核结果。
//
// @param tx 当前清理事务。
// @param requestIDs 即将删除的消息审计请求 ID。
// @return 数据库删除错误。
func DeleteMessageAuditReviewsForRequestIDs(tx *gorm.DB, requestIDs []string) error {
	if len(requestIDs) == 0 {
		return nil
	}
	if !tx.Migrator().HasTable(&MessageAuditReviewSource{}) || !tx.Migrator().HasTable(&MessageAuditReview{}) {
		return nil
	}
	var reviewIDs []int64
	if err := tx.Model(&MessageAuditReviewSource{}).Where("request_id IN ?", requestIDs).Distinct().Pluck("review_id", &reviewIDs).Error; err != nil {
		return err
	}
	var directReviewIDs []int64
	if err := tx.Model(&MessageAuditReview{}).
		Where("reviewed_request_id IN ?", requestIDs).
		Or("NOT EXISTS (?)", tx.Model(&MessageAuditRequest{}).Select("1").Where("message_audit_requests.audit_session_id = message_audit_reviews.audit_session_id")).
		Pluck("id", &directReviewIDs).Error; err != nil {
		return err
	}
	reviewIDs = append(reviewIDs, directReviewIDs...)
	requestIDSet := make(map[string]struct{}, len(requestIDs))
	for _, requestID := range requestIDs {
		requestIDSet[requestID] = struct{}{}
	}
	var activeReviews []MessageAuditReview
	if err := tx.Select("id, current_task_id").Where("status IN ? AND current_task_id <> ''", []string{"pending", "running"}).Find(&activeReviews).Error; err != nil {
		return err
	}
	activeTaskIDs := make([]string, 0, len(activeReviews))
	for _, review := range activeReviews {
		activeTaskIDs = append(activeTaskIDs, review.CurrentTaskID)
	}
	var activeTasks []SystemTask
	if len(activeTaskIDs) > 0 {
		if err := tx.Select("task_id, payload").Where("task_id IN ?", activeTaskIDs).Find(&activeTasks).Error; err != nil {
			return err
		}
	}
	taskPayloads := make(map[string]struct {
		SourceRequestIDs []string `json:"source_request_ids"`
	}, len(activeTasks))
	for _, task := range activeTasks {
		var payload struct {
			SourceRequestIDs []string `json:"source_request_ids"`
		}
		if err := task.DecodePayload(&payload); err == nil {
			taskPayloads[task.TaskID] = payload
		}
	}
	for _, review := range activeReviews {
		payload, ok := taskPayloads[review.CurrentTaskID]
		if !ok {
			continue
		}
		for _, sourceRequestID := range payload.SourceRequestIDs {
			if _, deleting := requestIDSet[sourceRequestID]; deleting {
				reviewIDs = append(reviewIDs, review.ID)
				break
			}
		}
	}
	if len(reviewIDs) == 0 {
		return nil
	}
	reviewIDSet := make(map[int64]struct{}, len(reviewIDs))
	deduplicatedReviewIDs := make([]int64, 0, len(reviewIDs))
	for _, reviewID := range reviewIDs {
		if _, ok := reviewIDSet[reviewID]; ok {
			continue
		}
		reviewIDSet[reviewID] = struct{}{}
		deduplicatedReviewIDs = append(deduplicatedReviewIDs, reviewID)
	}
	reviewIDs = deduplicatedReviewIDs
	var taskIDs []string
	if err := tx.Model(&MessageAuditReview{}).Where("id IN ? AND current_task_id <> ''", reviewIDs).Distinct().Pluck("current_task_id", &taskIDs).Error; err != nil {
		return err
	}
	if err := tx.Where("review_id IN ?", reviewIDs).Delete(&MessageAuditReviewSource{}).Error; err != nil {
		return err
	}
	if err := tx.Where("id IN ?", reviewIDs).Delete(&MessageAuditReview{}).Error; err != nil {
		return err
	}
	if len(taskIDs) == 0 {
		return nil
	}
	if err := tx.Where("task_id IN ?", taskIDs).Delete(&SystemTaskLock{}).Error; err != nil {
		return err
	}
	return tx.Where("task_id IN ? AND type = ?", taskIDs, SystemTaskTypeMessageAuditReview).Delete(&SystemTask{}).Error
}

// GetMessageAuditReviewSourceIDs 返回当前成功结果的固定来源引用。
//
// @param reviewID 审核结果主键。
// @return 来源请求 ID，按引用创建顺序排列。
func GetMessageAuditReviewSourceIDs(reviewID int64) ([]string, error) {
	var requestIDs []string
	err := DB.Model(&MessageAuditReviewSource{}).Where("review_id = ?", reviewID).Order("id asc").Pluck("request_id", &requestIDs).Error
	return requestIDs, err
}
