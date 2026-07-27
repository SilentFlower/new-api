package model

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const messageAuditStateID = 1

// MessageAuditRequest 保存一次已接收 AI 请求的审计元数据。
type MessageAuditRequest struct {
	ID              int64  `json:"id" gorm:"primaryKey"`
	RequestID       string `json:"request_id" gorm:"type:varchar(64);uniqueIndex"`
	UserID          int    `json:"user_id" gorm:"index"`
	Username        string `json:"username" gorm:"type:varchar(128);index"`
	TokenID         int    `json:"token_id" gorm:"index"`
	TokenName       string `json:"token_name" gorm:"type:varchar(128);index"`
	ModelName       string `json:"model_name" gorm:"type:varchar(256);index"`
	RequestPath     string `json:"request_path" gorm:"type:varchar(512);index"`
	Protocol        string `json:"protocol" gorm:"type:varchar(64);index"`
	Status          string `json:"status" gorm:"type:varchar(32);index"`
	AuditStatus     string `json:"audit_status" gorm:"type:varchar(32);index"`
	ErrorCode       string `json:"error_code" gorm:"type:varchar(128)"`
	FinishReason    string `json:"finish_reason" gorm:"type:varchar(128)"`
	HTTPStatus      int    `json:"http_status"`
	IsStream        bool   `json:"is_stream"`
	MessageCount    int    `json:"message_count"`
	ToolCount       int    `json:"tool_count"`
	PlaintextBytes  int64  `json:"plaintext_bytes"`
	DedupSavedBytes int64  `json:"dedup_saved_bytes"`
	DurationMS      int64  `json:"duration_ms"`
	CapturedAt      int64  `json:"captured_at" gorm:"index"`
	CapturedAtNano  int64  `json:"-" gorm:"index"`
	FinalizedAt     int64  `json:"finalized_at"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

// MessageAuditBlob 保存按用户隔离去重后的加密消息块。
type MessageAuditBlob struct {
	ID             int64  `json:"id" gorm:"primaryKey"`
	UserID         int    `json:"user_id" gorm:"uniqueIndex:idx_message_audit_blob_fingerprint,priority:1"`
	SchemaVersion  int    `json:"schema_version" gorm:"uniqueIndex:idx_message_audit_blob_fingerprint,priority:2"`
	ContentHMAC    string `json:"-" gorm:"type:varchar(64);uniqueIndex:idx_message_audit_blob_fingerprint,priority:3"`
	KeyFingerprint string `json:"-" gorm:"type:varchar(32);index"`
	ContentType    string `json:"content_type" gorm:"type:varchar(64)"`
	PlaintextBytes int64  `json:"plaintext_bytes"`
	Nonce          []byte `json:"-"`
	Ciphertext     []byte `json:"-"`
	CreatedAt      int64  `json:"created_at"`
}

// MessageAuditItem 保存审计请求到消息块的有序引用。
type MessageAuditItem struct {
	ID             int64  `json:"id" gorm:"primaryKey"`
	AuditRequestID int64  `json:"audit_request_id" gorm:"uniqueIndex:idx_message_audit_item_sequence,priority:1;index"`
	Sequence       int    `json:"sequence" gorm:"uniqueIndex:idx_message_audit_item_sequence,priority:2"`
	BlobID         int64  `json:"blob_id" gorm:"index"`
	Role           string `json:"role" gorm:"type:varchar(32)"`
	ContentType    string `json:"content_type" gorm:"type:varchar(64)"`
}

// MessageAuditState 保存消息审计清理任务与异步写入共享的单例水位。
type MessageAuditState struct {
	ID              int   `json:"id" gorm:"primaryKey"`
	PurgeBefore     int64 `json:"purge_before" gorm:"index"`
	PurgeBeforeNano int64 `json:"-" gorm:"index"`
	UpdatedAt       int64 `json:"updated_at"`
}

// MessageAuditStoredBlob 描述 service 已完成加密、等待事务持久化的消息块。
type MessageAuditStoredBlob struct {
	SchemaVersion  int
	ContentHMAC    string
	KeyFingerprint string
	ContentType    string
	PlaintextBytes int64
	Nonce          []byte
	Ciphertext     []byte
	Role           string
}

// MessageAuditCaptureRecord 描述一次需要原子写入的请求元数据和有序消息块。
type MessageAuditCaptureRecord struct {
	Request MessageAuditRequest
	Blobs   []MessageAuditStoredBlob
}

// MessageAuditFinalizeRecord 描述请求结束后需要补充的轻量状态。
type MessageAuditFinalizeRecord struct {
	RequestID    string
	Status       string
	ErrorCode    string
	FinishReason string
	HTTPStatus   int
	DurationMS   int64
	FinalizedAt  int64
}

// MessageAuditListFilter 描述 root 管理端列表查询条件。
type MessageAuditListFilter struct {
	StartTimestamp int64
	EndTimestamp   int64
	UserID         int
	Username       string
	TokenID        int
	TokenName      string
	ModelName      string
	RequestID      string
	RequestPath    string
	Status         string
	Offset         int
	Limit          int
}

// MessageAuditEncryptedItem 描述详情查询返回给 service 解密的有序密文块。
type MessageAuditEncryptedItem struct {
	Sequence       int
	Role           string
	ContentType    string
	SchemaVersion  int
	KeyFingerprint string
	Nonce          []byte
	Ciphertext     []byte
}

// CreateMessageAuditCapture 在单个事务内写入请求、去重消息块和有序引用。
//
// 参数 record 包含已加密且不含认证信息的审计快照。
// 返回值表示写入是否被清理水位跳过，以及持久化错误。
func CreateMessageAuditCapture(record *MessageAuditCaptureRecord) (bool, error) {
	if record == nil || record.Request.RequestID == "" {
		return false, errors.New("message audit capture record is invalid")
	}
	skipped := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		state, err := lockedMessageAuditState(tx)
		if err != nil {
			return err
		}
		capturedAtNano := record.Request.CapturedAtNano
		if capturedAtNano <= 0 {
			capturedAtNano = time.Unix(record.Request.CapturedAt, 0).UnixNano()
			record.Request.CapturedAtNano = capturedAtNano
		}
		if capturedAtNano <= messageAuditPurgeBeforeNano(state) {
			skipped = true
			return nil
		}

		var existing MessageAuditRequest
		err = tx.Where("request_id = ?", record.Request.RequestID).First(&existing).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Create(&record.Request).Error; err != nil {
			return err
		}

		var dedupSavedBytes int64
		for sequence, stored := range record.Blobs {
			blob := MessageAuditBlob{
				UserID:         record.Request.UserID,
				SchemaVersion:  stored.SchemaVersion,
				ContentHMAC:    stored.ContentHMAC,
				KeyFingerprint: stored.KeyFingerprint,
				ContentType:    stored.ContentType,
				PlaintextBytes: stored.PlaintextBytes,
				Nonce:          stored.Nonce,
				Ciphertext:     stored.Ciphertext,
				CreatedAt:      record.Request.CreatedAt,
			}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&blob)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				dedupSavedBytes += stored.PlaintextBytes
				if err := tx.Where("user_id = ? AND schema_version = ? AND content_hmac = ?", record.Request.UserID, stored.SchemaVersion, stored.ContentHMAC).First(&blob).Error; err != nil {
					return err
				}
			}
			item := MessageAuditItem{
				AuditRequestID: record.Request.ID,
				Sequence:       sequence,
				BlobID:         blob.ID,
				Role:           stored.Role,
				ContentType:    stored.ContentType,
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		}
		if dedupSavedBytes > 0 {
			return tx.Model(&MessageAuditRequest{}).Where("id = ?", record.Request.ID).Update("dedup_saved_bytes", dedupSavedBytes).Error
		}
		return nil
	})
	return skipped, err
}

// FinalizeMessageAuditRequest 更新已采集请求的最终状态。
//
// 参数 record 是不含正文的结束元数据。
// 返回值为数据库更新错误；记录不存在时视为无操作。
func FinalizeMessageAuditRequest(record MessageAuditFinalizeRecord) error {
	if record.RequestID == "" {
		return nil
	}
	updates := map[string]any{
		"status":        record.Status,
		"error_code":    record.ErrorCode,
		"finish_reason": record.FinishReason,
		"http_status":   record.HTTPStatus,
		"duration_ms":   record.DurationMS,
		"finalized_at":  record.FinalizedAt,
		"updated_at":    record.FinalizedAt,
	}
	return DB.Model(&MessageAuditRequest{}).Where("request_id = ?", record.RequestID).Updates(updates).Error
}

// ListMessageAudits 按 root 管理端筛选条件返回审计元数据和总数。
//
// 参数 filter 包含筛选和分页条件。
// 返回值不包含消息密文或正文。
func ListMessageAudits(filter MessageAuditListFilter) ([]MessageAuditRequest, int64, error) {
	query := DB.Model(&MessageAuditRequest{})
	if filter.StartTimestamp > 0 {
		query = query.Where("captured_at >= ?", filter.StartTimestamp)
	}
	if filter.EndTimestamp > 0 {
		query = query.Where("captured_at <= ?", filter.EndTimestamp)
	}
	if filter.UserID > 0 {
		query = query.Where("user_id = ?", filter.UserID)
	}
	if filter.Username != "" {
		query = query.Where("username = ?", filter.Username)
	}
	if filter.TokenID > 0 {
		query = query.Where("token_id = ?", filter.TokenID)
	}
	if filter.TokenName != "" {
		query = query.Where("token_name = ?", filter.TokenName)
	}
	if filter.ModelName != "" {
		query = query.Where("model_name = ?", filter.ModelName)
	}
	if filter.RequestID != "" {
		query = query.Where("request_id = ?", filter.RequestID)
	}
	if filter.RequestPath != "" {
		query = query.Where("request_path = ?", filter.RequestPath)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var requests []MessageAuditRequest
	selectColumns := "id, request_id, user_id, username, token_id, token_name, model_name, request_path, protocol, status, audit_status, error_code, finish_reason, http_status, is_stream, message_count, tool_count, plaintext_bytes, dedup_saved_bytes, duration_ms, captured_at, finalized_at, created_at, updated_at"
	err := query.Select(selectColumns).Order("id desc").Offset(filter.Offset).Limit(filter.Limit).Find(&requests).Error
	return requests, total, err
}

// GetMessageAuditEncryptedDetail 返回单个请求元数据及其有序密文消息块。
//
// 参数 requestID 是外部请求 ID。
// 返回值由 service 完成密钥校验和解密。
func GetMessageAuditEncryptedDetail(requestID string) (*MessageAuditRequest, []MessageAuditEncryptedItem, error) {
	var request MessageAuditRequest
	if err := DB.Where("request_id = ?", requestID).First(&request).Error; err != nil {
		return nil, nil, err
	}
	var items []MessageAuditEncryptedItem
	err := DB.Table("message_audit_items").
		Select("message_audit_items.sequence, message_audit_items.role, message_audit_items.content_type, message_audit_blobs.schema_version, message_audit_blobs.key_fingerprint, message_audit_blobs.nonce, message_audit_blobs.ciphertext").
		Joins("JOIN message_audit_blobs ON message_audit_blobs.id = message_audit_items.blob_id").
		Where("message_audit_items.audit_request_id = ?", request.ID).
		Order("message_audit_items.sequence asc").
		Scan(&items).Error
	if err != nil {
		return nil, nil, err
	}
	return &request, items, nil
}

// AdvanceMessageAuditPurgeBefore 单调推进清理水位。
//
// 参数 cutoff 是本次清理任务固定的 Unix 纳秒时间。
// 返回值为推进后的有效水位。
func AdvanceMessageAuditPurgeBefore(cutoff int64) (int64, error) {
	var purgeBefore int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		state, err := lockedMessageAuditState(tx)
		if err != nil {
			return err
		}
		currentPurgeBefore := messageAuditPurgeBeforeNano(state)
		if cutoff > currentPurgeBefore {
			state.PurgeBeforeNano = cutoff
			state.PurgeBefore = time.Unix(0, cutoff).Unix()
			state.UpdatedAt = time.Now().Unix()
			if err := tx.Save(state).Error; err != nil {
				return err
			}
			currentPurgeBefore = cutoff
		}
		purgeBefore = currentPurgeBefore
		return nil
	})
	return purgeBefore, err
}

// CountMessageAuditsBefore 统计清理水位之前的请求数量。
func CountMessageAuditsBefore(ctx context.Context, cutoff int64) (int64, error) {
	var count int64
	err := messageAuditsBefore(DB.WithContext(ctx).Model(&MessageAuditRequest{}), cutoff).Count(&count).Error
	return count, err
}

// DeleteMessageAuditsBeforeBatch 分批删除清理水位之前的请求和引用。
//
// 参数 ctx 控制取消，cutoff 是固定水位，batchSize 是单批上限。
// 返回值为本批删除的请求数。
func DeleteMessageAuditsBeforeBatch(ctx context.Context, cutoff int64, batchSize int) (int64, error) {
	var ids []int64
	if err := messageAuditsBefore(DB.WithContext(ctx).Model(&MessageAuditRequest{}), cutoff).Order("id asc").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("audit_request_id IN ?", ids).Delete(&MessageAuditItem{}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", ids).Delete(&MessageAuditRequest{}).Error
	})
	return int64(len(ids)), err
}

// DeleteOrphanMessageAuditBlobsBatch 分批回收没有请求引用的加密消息块。
//
// 返回值为本批删除的消息块数。
func DeleteOrphanMessageAuditBlobsBatch(ctx context.Context, batchSize int) (int64, error) {
	var deleted int64
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []int64
		orphanCheck := tx.Model(&MessageAuditItem{}).
			Select("1").
			Where("message_audit_items.blob_id = message_audit_blobs.id")
		if err := lockForUpdate(tx).Model(&MessageAuditBlob{}).
			Where("NOT EXISTS (?)", orphanCheck).
			Order("message_audit_blobs.id asc").
			Limit(batchSize).
			Pluck("message_audit_blobs.id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}

		var err error
		deleted, err = deleteMessageAuditBlobIDsIfOrphan(tx, ids)
		return err
	})
	return deleted, err
}

func deleteMessageAuditBlobIDsIfOrphan(tx *gorm.DB, ids []int64) (int64, error) {
	// 候选查询后仍可能有新请求复用消息块，删除时再次校验引用以避免悬空引用。
	stillOrphan := tx.Model(&MessageAuditItem{}).
		Select("1").
		Where("message_audit_items.blob_id = message_audit_blobs.id")
	result := tx.Where("id IN ?", ids).
		Where("NOT EXISTS (?)", stillOrphan).
		Delete(&MessageAuditBlob{})
	return result.RowsAffected, result.Error
}

func messageAuditsBefore(query *gorm.DB, cutoff int64) *gorm.DB {
	cutoffSeconds := time.Unix(0, cutoff).Unix()
	return query.Where(
		"(captured_at_nano > 0 AND captured_at_nano <= ?) OR ((captured_at_nano IS NULL OR captured_at_nano = 0) AND captured_at <= ?)",
		cutoff,
		cutoffSeconds,
	)
}

func messageAuditPurgeBeforeNano(state *MessageAuditState) int64 {
	if state == nil {
		return 0
	}
	if state.PurgeBeforeNano > 0 {
		return state.PurgeBeforeNano
	}
	return time.Unix(state.PurgeBefore, 0).UnixNano()
}

func lockedMessageAuditState(tx *gorm.DB) (*MessageAuditState, error) {
	state := &MessageAuditState{ID: messageAuditStateID}
	err := lockForUpdate(tx).First(state, messageAuditStateID).Error
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if createErr := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(state).Error; createErr != nil {
		return nil, createErr
	}
	if err := lockForUpdate(tx).First(state, messageAuditStateID).Error; err != nil {
		return nil, err
	}
	return state, nil
}
