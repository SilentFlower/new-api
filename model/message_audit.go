package model

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	messageAuditStateID                   = 1
	messageAuditCompressionMinAnchors     = 4
	messageAuditCompressionCoverageBase   = 10
	messageAuditCompressionCoverageTarget = 7
	messageAuditCompressionCandidateLimit = 20
	messageAuditCompressionMaxAnchors     = 512
)

// MessageAuditRequest 保存一次已接收 AI 请求的审计元数据。
type MessageAuditRequest struct {
	ID                    int64  `json:"id" gorm:"primaryKey"`
	RequestID             string `json:"request_id" gorm:"type:varchar(64);uniqueIndex"`
	AuditSessionID        string `json:"audit_session_id" gorm:"type:varchar(64);index"`
	ParentRequestID       string `json:"parent_request_id" gorm:"type:varchar(64);index"`
	SessionMatch          string `json:"session_match" gorm:"type:varchar(16);index"`
	SequenceFingerprint   string `json:"-" gorm:"type:varchar(64);index:idx_message_audit_session_candidate,priority:3"`
	ConversationItemCount int    `json:"-"`
	SessionAnchorCount    int    `json:"-"`
	SessionRequestCount   int64  `json:"session_request_count" gorm:"-:all"`
	CompressedCount       int64  `json:"compressed_request_count" gorm:"-:all"`
	UserID                int    `json:"user_id" gorm:"index;index:idx_message_audit_session_candidate,priority:1"`
	Username              string `json:"username" gorm:"type:varchar(128);index"`
	TokenID               int    `json:"token_id" gorm:"index"`
	TokenName             string `json:"token_name" gorm:"type:varchar(128);index"`
	ModelName             string `json:"model_name" gorm:"type:varchar(256);index"`
	RequestPath           string `json:"request_path" gorm:"type:varchar(512);index"`
	Protocol              string `json:"protocol" gorm:"type:varchar(64);index;index:idx_message_audit_session_candidate,priority:2"`
	Status                string `json:"status" gorm:"type:varchar(32);index"`
	AuditStatus           string `json:"audit_status" gorm:"type:varchar(32);index"`
	ErrorCode             string `json:"error_code" gorm:"type:varchar(128)"`
	FinishReason          string `json:"finish_reason" gorm:"type:varchar(128)"`
	HTTPStatus            int    `json:"http_status"`
	IsStream              bool   `json:"is_stream"`
	MessageCount          int    `json:"message_count"`
	ToolCount             int    `json:"tool_count"`
	PlaintextBytes        int64  `json:"plaintext_bytes"`
	DedupSavedBytes       int64  `json:"dedup_saved_bytes"`
	DurationMS            int64  `json:"duration_ms"`
	CapturedAt            int64  `json:"captured_at" gorm:"index"`
	CapturedAtNano        int64  `json:"-" gorm:"index"`
	FinalizedAt           int64  `json:"finalized_at"`
	CreatedAt             int64  `json:"created_at"`
	UpdatedAt             int64  `json:"updated_at"`
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
	Request                        MessageAuditRequest
	Blobs                          []MessageAuditStoredBlob
	ConversationPrefixFingerprints []string
	SessionAnchorHMACs             []string
}

// MessageAuditFinalizeRecord 描述请求结束后需要补充的轻量状态。
type MessageAuditFinalizeRecord struct {
	RequestID    string
	ModelName    string
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
	AuditSessionID string
	Offset         int
	Limit          int
}

// MessageAuditStorageStats 描述消息审计表的存储占用和行数。
type MessageAuditStorageStats struct {
	StorageBytes     int64
	StorageEstimated bool
	PayloadBytes     int64
	RequestCount     int64
	BlobCount        int64
	ItemCount        int64
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
		if err := assignMessageAuditSession(tx, record); err != nil {
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

type messageAuditSessionCandidate struct {
	ID                    int64
	RequestID             string
	AuditSessionID        string
	SequenceFingerprint   string
	ConversationItemCount int
}

type messageAuditCompressionCandidate struct {
	ID             int64
	RequestID      string
	AuditSessionID string
	MatchedAnchors int
}

type messageAuditCompressionMatch struct {
	RequestID      string
	AuditSessionID string
	MatchedAnchors int
	RequestIDValue int64
}

func assignMessageAuditSession(tx *gorm.DB, record *MessageAuditCaptureRecord) error {
	record.Request.ParentRequestID = ""
	record.Request.SessionMatch = "new"

	candidate, match, err := findMessageAuditPrefixSession(tx, record)
	if err != nil {
		return err
	}
	if candidate == nil {
		candidate, match, err = findMessageAuditCompressedSession(tx, record)
		if err != nil {
			return err
		}
	}
	if candidate != nil {
		record.Request.AuditSessionID = candidate.AuditSessionID
		record.Request.ParentRequestID = candidate.RequestID
		record.Request.SessionMatch = match
		return nil
	}

	randomID, err := common.GenerateRandomCharsKey(24)
	if err != nil {
		return err
	}
	record.Request.AuditSessionID = "audsess_" + randomID
	return nil
}

func findMessageAuditPrefixSession(tx *gorm.DB, record *MessageAuditCaptureRecord) (*messageAuditSessionCandidate, string, error) {
	if len(record.ConversationPrefixFingerprints) == 0 || record.Request.SequenceFingerprint == "" {
		return nil, "", nil
	}

	var candidates []messageAuditSessionCandidate
	err := tx.Model(&MessageAuditRequest{}).
		Select("id, request_id, audit_session_id, sequence_fingerprint, conversation_item_count").
		Where("user_id = ? AND protocol = ?", record.Request.UserID, record.Request.Protocol).
		Where("audit_session_id <> '' AND sequence_fingerprint IN ?", record.ConversationPrefixFingerprints).
		Order("conversation_item_count desc, id desc").
		Find(&candidates).Error
	if err != nil || len(candidates) == 0 {
		return nil, "", err
	}

	longestCount := candidates[0].ConversationItemCount
	longestSessions := make(map[string]messageAuditSessionCandidate)
	for _, candidate := range candidates {
		if candidate.ConversationItemCount != longestCount {
			break
		}
		if existing, ok := longestSessions[candidate.AuditSessionID]; !ok || candidate.ID > existing.ID {
			longestSessions[candidate.AuditSessionID] = candidate
		}
	}
	if len(longestSessions) != 1 {
		return nil, "", nil
	}
	for _, candidate := range longestSessions {
		match := "prefix"
		if candidate.ConversationItemCount == record.Request.ConversationItemCount && candidate.SequenceFingerprint == record.Request.SequenceFingerprint {
			match = "exact"
		}
		return &candidate, match, nil
	}
	return nil, "", nil
}

func findMessageAuditCompressedSession(tx *gorm.DB, record *MessageAuditCaptureRecord) (*messageAuditSessionCandidate, string, error) {
	currentAnchors := record.SessionAnchorHMACs
	if len(currentAnchors) < messageAuditCompressionMinAnchors || len(currentAnchors) > messageAuditCompressionMaxAnchors {
		return nil, "", nil
	}

	var summaries []messageAuditCompressionCandidate
	err := tx.Table("message_audit_items").
		Select("message_audit_requests.id, message_audit_requests.request_id, message_audit_requests.audit_session_id, COUNT(DISTINCT message_audit_items.blob_id) AS matched_anchors").
		Joins("JOIN message_audit_requests ON message_audit_requests.id = message_audit_items.audit_request_id").
		Joins("JOIN message_audit_blobs ON message_audit_blobs.id = message_audit_items.blob_id").
		Where("message_audit_requests.user_id = ? AND message_audit_requests.protocol = ?", record.Request.UserID, record.Request.Protocol).
		Where("message_audit_requests.audit_session_id <> '' AND message_audit_requests.session_anchor_count > ?", len(currentAnchors)).
		Where("message_audit_blobs.user_id = ? AND message_audit_blobs.content_hmac IN ?", record.Request.UserID, currentAnchors).
		Where("message_audit_items.role NOT IN ?", []string{"developer", "system"}).
		Where("message_audit_items.content_type NOT IN ?", []string{"tools", "functions"}).
		Group("message_audit_requests.id, message_audit_requests.request_id, message_audit_requests.audit_session_id").
		Order("matched_anchors desc, message_audit_requests.id desc").
		Limit(messageAuditCompressionCandidateLimit).
		Scan(&summaries).Error
	if err != nil {
		return nil, "", err
	}

	matchesBySession := make(map[string]messageAuditCompressionMatch)
	for _, summary := range summaries {
		if summary.MatchedAnchors < messageAuditCompressionMinAnchors {
			continue
		}
		var previousAnchors []string
		err := tx.Table("message_audit_items").
			Joins("JOIN message_audit_blobs ON message_audit_blobs.id = message_audit_items.blob_id").
			Where("message_audit_items.audit_request_id = ?", summary.ID).
			Where("message_audit_items.role NOT IN ?", []string{"developer", "system"}).
			Where("message_audit_items.content_type NOT IN ?", []string{"tools", "functions"}).
			Order("message_audit_items.sequence asc").
			Pluck("message_audit_blobs.content_hmac", &previousAnchors).Error
		if err != nil {
			return nil, "", err
		}
		if len(previousAnchors) <= len(currentAnchors) || len(previousAnchors) > messageAuditCompressionMaxAnchors*4 {
			continue
		}

		matchedAnchors := messageAuditLCSLength(currentAnchors, previousAnchors)
		if matchedAnchors < messageAuditCompressionMinAnchors || matchedAnchors*messageAuditCompressionCoverageBase < len(currentAnchors)*messageAuditCompressionCoverageTarget {
			continue
		}
		tailStart := len(previousAnchors) * 3 / 4
		if tailStart <= 0 || matchedAnchors == messageAuditLCSLength(currentAnchors, previousAnchors[:tailStart]) {
			continue
		}

		match := messageAuditCompressionMatch{
			RequestID:      summary.RequestID,
			AuditSessionID: summary.AuditSessionID,
			MatchedAnchors: matchedAnchors,
			RequestIDValue: summary.ID,
		}
		existing, ok := matchesBySession[summary.AuditSessionID]
		if !ok || match.MatchedAnchors > existing.MatchedAnchors || (match.MatchedAnchors == existing.MatchedAnchors && match.RequestIDValue > existing.RequestIDValue) {
			matchesBySession[summary.AuditSessionID] = match
		}
	}

	var best *messageAuditCompressionMatch
	secondBestScore := -1
	for _, match := range matchesBySession {
		candidate := match
		if best == nil || candidate.MatchedAnchors > best.MatchedAnchors || (candidate.MatchedAnchors == best.MatchedAnchors && candidate.RequestIDValue > best.RequestIDValue) {
			if best != nil {
				secondBestScore = best.MatchedAnchors
			}
			best = &candidate
			continue
		}
		if candidate.MatchedAnchors > secondBestScore {
			secondBestScore = candidate.MatchedAnchors
		}
	}
	if best == nil || secondBestScore >= best.MatchedAnchors-1 {
		return nil, "", nil
	}
	return &messageAuditSessionCandidate{
		ID:             best.RequestIDValue,
		RequestID:      best.RequestID,
		AuditSessionID: best.AuditSessionID,
	}, "compressed", nil
}

func messageAuditLCSLength(current []string, previous []string) int {
	if len(current) == 0 || len(previous) == 0 {
		return 0
	}
	dp := make([]int, len(previous)+1)
	for _, currentValue := range current {
		previousDiagonal := 0
		for previousIndex, previousValue := range previous {
			oldValue := dp[previousIndex+1]
			if currentValue == previousValue {
				dp[previousIndex+1] = previousDiagonal + 1
			} else if dp[previousIndex] > dp[previousIndex+1] {
				dp[previousIndex+1] = dp[previousIndex]
			}
			previousDiagonal = oldValue
		}
	}
	return dp[len(previous)]
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
	if record.ModelName != "" {
		updates["model_name"] = record.ModelName
	}
	return DB.Model(&MessageAuditRequest{}).Where("request_id = ?", record.RequestID).Updates(updates).Error
}

// ListMessageAudits 按 root 管理端筛选条件返回审计元数据和总数。
//
// 参数 filter 包含筛选和分页条件。
// 返回值不包含消息密文或正文。
func ListMessageAudits(filter MessageAuditListFilter) ([]MessageAuditRequest, int64, error) {
	query := messageAuditFilteredQuery(DB.Model(&MessageAuditRequest{}), filter)
	selectColumns := "id, request_id, audit_session_id, parent_request_id, session_match, user_id, username, token_id, token_name, model_name, request_path, protocol, status, audit_status, error_code, finish_reason, http_status, is_stream, message_count, tool_count, plaintext_bytes, dedup_saved_bytes, duration_ms, captured_at, finalized_at, created_at, updated_at"
	if filter.AuditSessionID != "" {
		var total int64
		if err := query.Where("audit_session_id = ?", filter.AuditSessionID).Count(&total).Error; err != nil {
			return nil, 0, err
		}
		var requests []MessageAuditRequest
		err := query.Where("audit_session_id = ?", filter.AuditSessionID).
			Select(selectColumns).
			Order("id desc").
			Offset(filter.Offset).
			Limit(filter.Limit).
			Find(&requests).Error
		return requests, total, err
	}

	groupedQuery := query.
		Select("MAX(id) AS latest_id, COUNT(*) AS session_request_count, SUM(CASE WHEN session_match = 'compressed' THEN 1 ELSE 0 END) AS compressed_request_count").
		Group("audit_session_id, CASE WHEN audit_session_id IS NULL OR audit_session_id = '' THEN id ELSE 0 END")
	var total int64
	if err := DB.Table("(?) AS message_audit_sessions", groupedQuery).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	type messageAuditSessionRow struct {
		LatestID               int64
		SessionRequestCount    int64
		CompressedRequestCount int64
	}
	var sessionRows []messageAuditSessionRow
	if err := DB.Table("(?) AS message_audit_sessions", groupedQuery).
		Order("latest_id desc").
		Offset(filter.Offset).
		Limit(filter.Limit).
		Scan(&sessionRows).Error; err != nil {
		return nil, 0, err
	}
	if len(sessionRows) == 0 {
		return []MessageAuditRequest{}, total, nil
	}

	latestIDs := make([]int64, 0, len(sessionRows))
	countsByID := make(map[int64]int64, len(sessionRows))
	compressedCountsByID := make(map[int64]int64, len(sessionRows))
	for _, row := range sessionRows {
		latestIDs = append(latestIDs, row.LatestID)
		countsByID[row.LatestID] = row.SessionRequestCount
		compressedCountsByID[row.LatestID] = row.CompressedRequestCount
	}
	var unorderedRequests []MessageAuditRequest
	if err := DB.Model(&MessageAuditRequest{}).
		Select(selectColumns).
		Where("id IN ?", latestIDs).
		Find(&unorderedRequests).Error; err != nil {
		return nil, 0, err
	}
	requestsByID := make(map[int64]MessageAuditRequest, len(unorderedRequests))
	for _, request := range unorderedRequests {
		request.SessionRequestCount = countsByID[request.ID]
		request.CompressedCount = compressedCountsByID[request.ID]
		requestsByID[request.ID] = request
	}
	requests := make([]MessageAuditRequest, 0, len(sessionRows))
	for _, row := range sessionRows {
		if request, ok := requestsByID[row.LatestID]; ok {
			requests = append(requests, request)
		}
	}
	return requests, total, nil
}

func messageAuditFilteredQuery(query *gorm.DB, filter MessageAuditListFilter) *gorm.DB {
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
	return query
}

// GetMessageAuditStorageStats 返回消息审计表占用空间、有效密文载荷和行数。
//
// 返回值优先使用数据库表级物理分配空间，能力不足时回退为有效密文载荷。
func GetMessageAuditStorageStats() (MessageAuditStorageStats, error) {
	stats := MessageAuditStorageStats{}
	if err := DB.Model(&MessageAuditRequest{}).Count(&stats.RequestCount).Error; err != nil {
		return stats, err
	}
	if err := DB.Model(&MessageAuditBlob{}).Count(&stats.BlobCount).Error; err != nil {
		return stats, err
	}
	if err := DB.Model(&MessageAuditItem{}).Count(&stats.ItemCount).Error; err != nil {
		return stats, err
	}
	if err := DB.Model(&MessageAuditBlob{}).
		Select("COALESCE(SUM(LENGTH(ciphertext) + LENGTH(nonce)), 0)").
		Scan(&stats.PayloadBytes).Error; err != nil {
		return stats, err
	}

	var storageBytes int64
	var err error
	switch common.MainDatabaseType() {
	case common.DatabaseTypeMySQL:
		err = DB.Raw("SELECT COALESCE(SUM(data_length + index_length), 0) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name IN ('message_audit_requests', 'message_audit_blobs', 'message_audit_items', 'message_audit_states')").Scan(&storageBytes).Error
	case common.DatabaseTypePostgreSQL:
		err = DB.Raw("SELECT COALESCE(SUM(pg_total_relation_size((quote_ident(schemaname) || '.' || quote_ident(tablename))::regclass)), 0) FROM pg_tables WHERE schemaname = current_schema() AND tablename IN ('message_audit_requests', 'message_audit_blobs', 'message_audit_items', 'message_audit_states')").Scan(&storageBytes).Error
	case common.DatabaseTypeSQLite:
		err = DB.Raw("SELECT COALESCE(SUM(pgsize), 0) FROM dbstat WHERE name IN ('message_audit_requests', 'message_audit_blobs', 'message_audit_items', 'message_audit_states') OR name IN (SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name IN ('message_audit_requests', 'message_audit_blobs', 'message_audit_items', 'message_audit_states'))").Scan(&storageBytes).Error
	default:
		err = errors.New("unsupported message audit storage database")
	}
	if err == nil {
		stats.StorageBytes = storageBytes
		return stats, nil
	}

	stats.StorageEstimated = true
	stats.StorageBytes = stats.PayloadBytes
	return stats, nil
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
	if request.AuditSessionID != "" {
		if err := DB.Model(&MessageAuditRequest{}).
			Where("audit_session_id = ? AND session_match = ?", request.AuditSessionID, "compressed").
			Count(&request.CompressedCount).Error; err != nil {
			return nil, nil, err
		}
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
