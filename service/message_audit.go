package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
)

const (
	messageAuditSchemaVersion             = 1
	messageAuditQueueCapacity             = 1024
	messageAuditQueueByteLimit            = int64(64 * 1024 * 1024)
	messageAuditSnapshotMaxSize           = int64(1024 * 1024)
	messageAuditReviewTextDefaultMaxSize  = int64(4 * 1024 * 1024)
	messageAuditReviewTextAbsoluteMaxSize = int64(16 * 1024 * 1024)
	messageAuditBatchSize                 = 32
	messageAuditRetryCount                = 3
	messageAuditSecretMinLength           = 32
	messageAuditStorageStatsCacheTTL      = time.Minute
)

var (
	messageAuditManagerOnce sync.Once
	messageAuditManagerInst *messageAuditManager
	messageAuditStatsCache  messageAuditStorageStatsCache
)

type messageAuditStorageStatsCache struct {
	mutex     sync.Mutex
	stats     model.MessageAuditStorageStats
	expiresAt time.Time
	valid     bool
}

func (cache *messageAuditStorageStatsCache) get(now time.Time, loader func() (model.MessageAuditStorageStats, error)) (model.MessageAuditStorageStats, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	if cache.valid && now.Before(cache.expiresAt) {
		return cache.stats, nil
	}
	stats, err := loader()
	if err != nil {
		return model.MessageAuditStorageStats{}, err
	}
	cache.stats = stats
	cache.expiresAt = now.Add(messageAuditStorageStatsCacheTTL)
	cache.valid = true
	return stats, nil
}

func (cache *messageAuditStorageStatsCache) clear() {
	cache.mutex.Lock()
	cache.valid = false
	cache.mutex.Unlock()
}

// MessageAuditCaptureInput 是 controller 传给审计 service 的最小请求上下文。
type MessageAuditCaptureInput struct {
	RequestID   string
	UserID      int
	Username    string
	TokenID     int
	TokenName   string
	ModelName   string
	RequestPath string
	Protocol    types.RelayFormat
	IsStream    bool
	CapturedAt  time.Time
	Request     dto.Request
}

// MessageAuditFinalizeInput 是请求结束时提交的轻量审计状态。
type MessageAuditFinalizeInput struct {
	RequestID    string
	ModelName    string
	Status       string
	ErrorCode    string
	FinishReason string
	HTTPStatus   int
	Duration     time.Duration
}

// MessageAuditStatus 描述当前节点的审计配置和进程内队列指标。
type MessageAuditStatus struct {
	Enabled           bool   `json:"enabled"`
	KeyConfigured     bool   `json:"key_configured"`
	KeyFingerprint    string `json:"key_fingerprint,omitempty"`
	RetentionDays     int    `json:"retention_days"`
	QueueDepth        int    `json:"queue_depth"`
	QueueCapacity     int    `json:"queue_capacity"`
	QueueBytes        int64  `json:"queue_bytes"`
	QueueByteCapacity int64  `json:"queue_byte_capacity"`
	Succeeded         uint64 `json:"succeeded"`
	Retries           uint64 `json:"retries"`
	Failed            uint64 `json:"failed"`
	Dropped           uint64 `json:"dropped"`
	StorageBytes      int64  `json:"storage_bytes"`
	StorageEstimated  bool   `json:"storage_estimated"`
	PayloadBytes      int64  `json:"payload_bytes"`
	RequestCount      int64  `json:"request_count"`
	BlobCount         int64  `json:"blob_count"`
	ItemCount         int64  `json:"item_count"`
}

// MessageAuditMessage 是详情接口返回的单个规范化消息块。
type MessageAuditMessage struct {
	Sequence    int    `json:"sequence"`
	Role        string `json:"role"`
	ContentType string `json:"content_type"`
	Content     any    `json:"content"`
}

// MessageAuditDetail 是详情接口返回的请求元数据和有序消息。
type MessageAuditDetail struct {
	Request  *model.MessageAuditRequest `json:"request"`
	Messages []MessageAuditMessage      `json:"messages"`
}

type messageAuditPlaintext struct {
	Role        string `json:"role"`
	ContentType string `json:"content_type"`
	Content     any    `json:"content"`
}

type messageAuditCaptureEvent struct {
	request                        model.MessageAuditRequest
	entries                        []messageAuditPlaintext
	conversationPrefixFingerprints []string
	sessionAnchorHMACs             []string
	sequenceFingerprint            string
	conversationItemCount          int
	sessionAnchorCount             int
}

type messageAuditEvent struct {
	kind     string
	size     int64
	capture  *messageAuditCaptureEvent
	finalize *model.MessageAuditFinalizeRecord
}

type messageAuditManager struct {
	queue                chan messageAuditEvent
	stop                 chan struct{}
	done                 chan struct{}
	stopOnce             sync.Once
	storageMutex         sync.RWMutex
	stopping             atomic.Bool
	queuedBytes          atomic.Int64
	succeeded            atomic.Uint64
	retries              atomic.Uint64
	failed               atomic.Uint64
	dropped              atomic.Uint64
	encryptionKey        []byte
	dedupKey             []byte
	reviewKey            []byte
	reviewKeyFingerprint string
	keyFingerprint       string
	keyConfigured        bool
}

// StartMessageAuditManager 启动消息审计单写协程。
//
// 该函数可重复调用，只有首次调用会创建后台协程。
func StartMessageAuditManager() {
	messageAuditManagerOnce.Do(func() {
		secret := common.GetEnvOrDefaultString("MESSAGE_AUDIT_SECRET", "")
		encryptionKey, dedupKey, fingerprint, err := deriveMessageAuditKeys(secret)
		reviewKey, reviewFingerprint := deriveMessageAuditReviewKey(secret)
		manager := &messageAuditManager{
			queue:                make(chan messageAuditEvent, messageAuditQueueCapacity),
			stop:                 make(chan struct{}),
			done:                 make(chan struct{}),
			encryptionKey:        encryptionKey,
			dedupKey:             dedupKey,
			reviewKey:            reviewKey,
			reviewKeyFingerprint: reviewFingerprint,
			keyFingerprint:       fingerprint,
			keyConfigured:        err == nil,
		}
		messageAuditManagerInst = manager
		if err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("消息审计密钥未就绪，审计保持不可用: %v", err))
		}
		go manager.run()
	})
}

// StopMessageAuditManager 在数据库关闭前按给定上下文排空审计队列。
//
// 参数 ctx 控制最大等待时间。
// 返回值在超时或取消时返回 ctx 错误。
func StopMessageAuditManager(ctx context.Context) error {
	manager := messageAuditManagerInst
	if manager == nil {
		return nil
	}
	manager.stopping.Store(true)
	manager.stopOnce.Do(func() {
		close(manager.stop)
	})
	select {
	case <-manager.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ValidateMessageAuditConfiguration 校验当前部署是否具备启用审计的密钥条件。
//
// 返回值为 nil 表示可以启用，否则返回不含密钥内容的配置错误。
func ValidateMessageAuditConfiguration() error {
	_, _, _, err := deriveMessageAuditKeys(common.GetEnvOrDefaultString("MESSAGE_AUDIT_SECRET", ""))
	return err
}

// MessageAuditRetentionDays 返回经过边界保护的保留天数。
func MessageAuditRetentionDays() int {
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap["MessageAuditRetentionDays"]
	common.OptionMapRWMutex.RUnlock()
	days, err := strconv.Atoi(raw)
	if err != nil || days < 1 || days > 30 {
		return 7
	}
	return days
}

// GetMessageAuditStatus 返回当前节点的配置、队列和累计指标。
func GetMessageAuditStatus() MessageAuditStatus {
	manager := messageAuditManagerInst
	status := MessageAuditStatus{
		Enabled:           messageAuditEnabled(),
		RetentionDays:     MessageAuditRetentionDays(),
		QueueCapacity:     messageAuditQueueCapacity,
		QueueByteCapacity: messageAuditQueueByteLimit,
	}
	storageStats, err := messageAuditStatsCache.get(time.Now(), model.GetMessageAuditStorageStats)
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("消息审计存储统计失败: %v", err))
	} else {
		status.StorageBytes = storageStats.StorageBytes
		status.StorageEstimated = storageStats.StorageEstimated
		status.PayloadBytes = storageStats.PayloadBytes
		status.RequestCount = storageStats.RequestCount
		status.BlobCount = storageStats.BlobCount
		status.ItemCount = storageStats.ItemCount
	}
	if manager == nil {
		return status
	}
	status.KeyConfigured = manager.keyConfigured
	status.KeyFingerprint = manager.keyFingerprint
	status.QueueDepth = len(manager.queue)
	status.QueueBytes = manager.queuedBytes.Load()
	status.Succeeded = manager.succeeded.Load()
	status.Retries = manager.retries.Load()
	status.Failed = manager.failed.Load()
	status.Dropped = manager.dropped.Load()
	return status
}

// ClearMessageAudits 在暂停本节点审计写入期间快速清空全部消息审计数据。
//
// @param ctx 控制数据库操作取消。
// @return 清空前的请求与消息块数量，以及数据库错误。
func ClearMessageAudits(ctx context.Context) (model.MessageAuditClearResult, error) {
	manager := messageAuditManagerInst
	if manager != nil {
		manager.storageMutex.Lock()
		defer manager.storageMutex.Unlock()
	}
	result, err := model.ClearMessageAudits(ctx)
	if err == nil {
		messageAuditStatsCache.clear()
	}
	return result, err
}

// CaptureMessageAudit 规范化已验证请求并尝试非阻塞投递入站快照。
//
// 参数 input 只能包含 controller 已持有的最小上下文和原始验证后 DTO。
// 返回值表示 capture 是否已进入队列；false 时调用方不应投递 finalize。
func CaptureMessageAudit(input MessageAuditCaptureInput) bool {
	manager := messageAuditManagerInst
	if manager == nil || manager.stopping.Load() || !manager.keyConfigured || !messageAuditEnabled() {
		return false
	}
	if input.RequestID == "" || input.Request == nil || !isMessageAuditProtocolSupported(input.Protocol) {
		return false
	}

	entries, fingerprintEntries, messageCount, toolCount, plaintextBytes, metadataOnly, err := manager.normalizeRequest(input.Request)
	if err != nil {
		manager.dropped.Add(1)
		logger.LogWarn(context.Background(), fmt.Sprintf("消息审计快照生成失败: request_id=%s error=%v", input.RequestID, err))
		return false
	}
	capturedAt := input.CapturedAt
	if capturedAt.IsZero() {
		capturedAt = time.Now()
	}
	now := time.Now().Unix()
	conversationPrefixFingerprints, sessionAnchorHMACs, sequenceFingerprint := manager.buildMessageAuditSessionFingerprints(input.UserID, string(input.Protocol), fingerprintEntries)
	capturedPlaintextBytes := messageAuditPlaintextSize(entries)
	capture := &messageAuditCaptureEvent{
		request: model.MessageAuditRequest{
			RequestID:              input.RequestID,
			UserID:                 input.UserID,
			Username:               input.Username,
			TokenID:                input.TokenID,
			TokenName:              input.TokenName,
			ModelName:              input.ModelName,
			RequestPath:            input.RequestPath,
			Protocol:               string(input.Protocol),
			Status:                 "pending",
			AuditStatus:            "captured",
			IsStream:               input.IsStream,
			MessageCount:           messageCount,
			ToolCount:              toolCount,
			PlaintextBytes:         plaintextBytes,
			CapturedPlaintextBytes: &capturedPlaintextBytes,
			CapturedAt:             capturedAt.Unix(),
			CapturedAtNano:         capturedAt.UnixNano(),
			CreatedAt:              now,
			UpdatedAt:              now,
		},
		entries:                        entries,
		conversationPrefixFingerprints: conversationPrefixFingerprints,
		sessionAnchorHMACs:             sessionAnchorHMACs,
		sequenceFingerprint:            sequenceFingerprint,
		conversationItemCount:          len(conversationPrefixFingerprints),
		sessionAnchorCount:             len(sessionAnchorHMACs),
	}
	if metadataOnly {
		capture.request.AuditStatus = "metadata_only"
		capture.entries = nil
	} else if plaintextBytes > messageAuditSnapshotMaxSize {
		capture.request.AuditStatus = "content_reduced"
	}
	eventSize := plaintextBytes + 512
	if metadataOnly {
		eventSize = 512
	}
	return manager.enqueue(messageAuditEvent{kind: "capture", size: eventSize, capture: capture})
}

// FinalizeMessageAudit 非阻塞投递请求结束元数据。
//
// 参数 input 不包含响应正文，仅记录最终模型、状态和耗时。
func FinalizeMessageAudit(input MessageAuditFinalizeInput) {
	manager := messageAuditManagerInst
	if manager == nil || manager.stopping.Load() || input.RequestID == "" {
		return
	}
	status := input.Status
	if status == "" {
		status = "succeeded"
	}
	record := &model.MessageAuditFinalizeRecord{
		RequestID:    input.RequestID,
		ModelName:    input.ModelName,
		Status:       status,
		ErrorCode:    input.ErrorCode,
		FinishReason: input.FinishReason,
		HTTPStatus:   input.HTTPStatus,
		DurationMS:   input.Duration.Milliseconds(),
		FinalizedAt:  time.Now().Unix(),
	}
	manager.enqueue(messageAuditEvent{kind: "finalize", size: 256, finalize: record})
}

// ListMessageAudits 返回不含正文的分页审计列表。
func ListMessageAudits(filter model.MessageAuditListFilter) ([]model.MessageAuditRequest, int64, error) {
	requests, total, err := model.ListMessageAudits(filter)
	if err != nil {
		return nil, 0, err
	}
	if err := model.AttachMessageAuditReviewMetadata(requests); err != nil {
		return nil, 0, err
	}
	return requests, total, nil
}

// GetMessageAuditDetail 校验密钥指纹并解密单个请求的有序消息。
//
// 参数 requestID 是目标请求 ID。
// 返回值仅在 root 管理接口按需调用时包含明文。
func GetMessageAuditDetail(requestID string) (*MessageAuditDetail, error) {
	manager := messageAuditManagerInst
	if manager == nil || !manager.keyConfigured {
		return nil, errors.New("消息审计密钥未配置")
	}
	request, encryptedItems, err := model.GetMessageAuditEncryptedDetail(requestID)
	if err != nil {
		return nil, err
	}
	messages := make([]MessageAuditMessage, 0, len(encryptedItems))
	for _, item := range encryptedItems {
		if item.KeyFingerprint != manager.keyFingerprint {
			return nil, errors.New("消息审计密钥与存储记录不匹配")
		}
		plaintext, err := manager.decrypt(request.UserID, item.SchemaVersion, item.Nonce, item.Ciphertext)
		if err != nil {
			return nil, errors.New("消息审计正文解密失败")
		}
		var content messageAuditPlaintext
		if err := common.Unmarshal(plaintext, &content); err != nil {
			return nil, errors.New("消息审计正文格式无效")
		}
		messages = append(messages, MessageAuditMessage{
			Sequence:    item.Sequence,
			Role:        content.Role,
			ContentType: content.ContentType,
			Content:     content.Content,
		})
	}
	return &MessageAuditDetail{Request: request, Messages: messages}, nil
}

func messageAuditEnabled() bool {
	common.OptionMapRWMutex.RLock()
	enabled := common.OptionMap["MessageAuditEnabled"] == "true"
	common.OptionMapRWMutex.RUnlock()
	return enabled
}

func isMessageAuditProtocolSupported(protocol types.RelayFormat) bool {
	switch protocol {
	case types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude, types.RelayFormatGemini:
		return true
	default:
		return false
	}
}

func deriveMessageAuditKeys(secret string) ([]byte, []byte, string, error) {
	if len(secret) < messageAuditSecretMinLength {
		return nil, nil, "", fmt.Errorf("MESSAGE_AUDIT_SECRET 长度必须至少为 %d 字节", messageAuditSecretMinLength)
	}
	derive := func(label string) []byte {
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(label))
		return mac.Sum(nil)
	}
	encryptionKey := derive("new-api/message-audit/encryption/v1")
	dedupKey := derive("new-api/message-audit/dedup/v1")
	fingerprintHash := sha256.Sum256(encryptionKey)
	return encryptionKey, dedupKey, hex.EncodeToString(fingerprintHash[:8]), nil
}

func deriveMessageAuditReviewKey(secret string) ([]byte, string) {
	if len(secret) < messageAuditSecretMinLength {
		return nil, ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("new-api/message-audit/review-encryption/v1"))
	key := mac.Sum(nil)
	fingerprintHash := sha256.Sum256(key)
	return key, hex.EncodeToString(fingerprintHash[:8])
}

func (manager *messageAuditManager) enqueue(event messageAuditEvent) bool {
	if manager.stopping.Load() || !manager.reserveBytes(event.size) {
		manager.dropped.Add(1)
		return false
	}
	select {
	case manager.queue <- event:
		return true
	default:
		manager.queuedBytes.Add(-event.size)
		manager.dropped.Add(1)
		return false
	}
}

func (manager *messageAuditManager) reserveBytes(size int64) bool {
	for {
		current := manager.queuedBytes.Load()
		if current+size > messageAuditQueueByteLimit {
			return false
		}
		if manager.queuedBytes.CompareAndSwap(current, current+size) {
			return true
		}
	}
}

func (manager *messageAuditManager) run() {
	defer close(manager.done)
	for {
		select {
		case event := <-manager.queue:
			manager.processBatch(event)
		case <-manager.stop:
			for {
				select {
				case event := <-manager.queue:
					manager.processBatch(event)
				default:
					return
				}
			}
		}
	}
}

func (manager *messageAuditManager) processBatch(first messageAuditEvent) {
	batch := make([]messageAuditEvent, 0, messageAuditBatchSize)
	batch = append(batch, first)
	for len(batch) < messageAuditBatchSize {
		select {
		case event := <-manager.queue:
			batch = append(batch, event)
		default:
			for _, event := range batch {
				manager.processWithRetry(event)
				manager.queuedBytes.Add(-event.size)
			}
			return
		}
	}
	for _, event := range batch {
		manager.processWithRetry(event)
		manager.queuedBytes.Add(-event.size)
	}
}

func (manager *messageAuditManager) processWithRetry(event messageAuditEvent) {
	var err error
	var captureRecord *model.MessageAuditCaptureRecord
	if event.kind == "capture" {
		captureRecord, err = manager.encryptCapture(event.capture)
		if err != nil {
			manager.failed.Add(1)
			logger.LogWarn(context.Background(), fmt.Sprintf("消息审计异步加密失败: request_id=%s error=%v", event.capture.request.RequestID, err))
			return
		}
	}
	for attempt := 0; attempt < messageAuditRetryCount; attempt++ {
		manager.storageMutex.RLock()
		if event.kind == "capture" {
			_, err = model.CreateMessageAuditCapture(captureRecord)
		} else {
			err = model.FinalizeMessageAuditRequest(*event.finalize)
		}
		manager.storageMutex.RUnlock()
		if err == nil {
			manager.succeeded.Add(1)
			return
		}
		if attempt+1 < messageAuditRetryCount {
			manager.retries.Add(1)
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
		}
	}
	manager.failed.Add(1)
	requestID := ""
	if event.capture != nil {
		requestID = event.capture.request.RequestID
	} else if event.finalize != nil {
		requestID = event.finalize.RequestID
	}
	logger.LogWarn(context.Background(), fmt.Sprintf("消息审计异步写入失败: request_id=%s event=%s error=%v", requestID, event.kind, err))
}

func (manager *messageAuditManager) normalizeRequest(request dto.Request) ([]messageAuditPlaintext, []messageAuditPlaintext, int, int, int64, bool, error) {
	plainEntries := make([]messageAuditPlaintext, 0)
	messageCount := 0
	toolCount := 0
	appendValue := func(role string, contentType string, value any, countMessage bool) error {
		data, err := common.Marshal(value)
		if err != nil {
			return err
		}
		var generic any
		if err := common.Unmarshal(data, &generic); err != nil {
			return err
		}
		sanitized := manager.sanitizeValue(generic, "", 0)
		if sanitized == nil {
			return nil
		}
		plainEntries = append(plainEntries, messageAuditPlaintext{Role: role, ContentType: contentType, Content: sanitized})
		if countMessage {
			messageCount++
		}
		if contentType != "tools" && contentType != "functions" {
			toolCount += countMessageAuditToolCalls(sanitized)
		}
		return nil
	}
	appendRaw := func(role string, contentType string, raw json.RawMessage, countMessage bool) error {
		if len(raw) == 0 {
			return nil
		}
		var value any
		if err := common.Unmarshal(raw, &value); err != nil {
			return err
		}
		return appendValue(role, contentType, value, countMessage)
	}

	switch typed := request.(type) {
	case *dto.GeneralOpenAIRequest:
		for _, message := range typed.Messages {
			if err := appendValue(message.Role, "message", message, true); err != nil {
				return nil, nil, 0, 0, 0, false, err
			}
		}
		if len(typed.Tools) > 0 {
			if err := appendValue("system", "tools", typed.Tools, false); err != nil {
				return nil, nil, 0, 0, 0, false, err
			}
		}
		if err := appendRaw("system", "functions", typed.Functions, false); err != nil {
			return nil, nil, 0, 0, 0, false, err
		}
	case *dto.OpenAIResponsesRequest:
		if err := appendRaw("system", "instructions", typed.Instructions, true); err != nil {
			return nil, nil, 0, 0, 0, false, err
		}
		if len(typed.Input) > 0 {
			var input any
			if err := common.Unmarshal(typed.Input, &input); err != nil {
				return nil, nil, 0, 0, 0, false, err
			}
			if list, ok := input.([]any); ok {
				for _, item := range list {
					role := auditRoleFromValue(item, "user")
					if err := appendValue(role, "input", item, true); err != nil {
						return nil, nil, 0, 0, 0, false, err
					}
				}
			} else if err := appendValue("user", "input", input, true); err != nil {
				return nil, nil, 0, 0, 0, false, err
			}
		}
		if err := appendRaw("system", "tools", typed.Tools, false); err != nil {
			return nil, nil, 0, 0, 0, false, err
		}
	case *dto.ClaudeRequest:
		if typed.System != nil {
			if err := appendValue("system", "system", typed.System, true); err != nil {
				return nil, nil, 0, 0, 0, false, err
			}
		}
		for _, message := range typed.Messages {
			if err := appendValue(message.Role, "message", message, true); err != nil {
				return nil, nil, 0, 0, 0, false, err
			}
		}
		if typed.Tools != nil {
			if err := appendValue("system", "tools", typed.Tools, false); err != nil {
				return nil, nil, 0, 0, 0, false, err
			}
		}
	case *dto.GeminiChatRequest:
		if typed.SystemInstructions != nil {
			if err := appendValue("system", "system", typed.SystemInstructions, true); err != nil {
				return nil, nil, 0, 0, 0, false, err
			}
		}
		for _, content := range typed.Contents {
			if err := appendValue(content.Role, "content", content, true); err != nil {
				return nil, nil, 0, 0, 0, false, err
			}
		}
		if err := appendRaw("system", "tools", typed.Tools, false); err != nil {
			return nil, nil, 0, 0, 0, false, err
		}
	default:
		return nil, nil, 0, 0, 0, false, nil
	}

	var totalBytes int64
	for _, entry := range plainEntries {
		plaintext, err := common.Marshal(entry)
		if err != nil {
			return nil, nil, 0, 0, 0, false, err
		}
		totalBytes += int64(len(plaintext))
	}
	if totalBytes <= messageAuditSnapshotMaxSize {
		return plainEntries, plainEntries, messageCount, toolCount, totalBytes, false, nil
	}

	reducedEntries := reduceMessageAuditEntries(plainEntries)
	if len(reducedEntries) == 0 || messageAuditPlaintextSize(reducedEntries) > messageAuditReviewTextMaxSize() {
		return nil, reducedEntries, messageCount, toolCount, totalBytes, true, nil
	}
	return reducedEntries, reducedEntries, messageCount, toolCount, totalBytes, false, nil
}

func (manager *messageAuditManager) encryptCapture(capture *messageAuditCaptureEvent) (*model.MessageAuditCaptureRecord, error) {
	if capture == nil {
		return nil, errors.New("message audit capture event is nil")
	}
	record := &model.MessageAuditCaptureRecord{
		Request: capture.request,
		Blobs:   make([]model.MessageAuditStoredBlob, 0, len(capture.entries)),
	}
	hasPrecomputedFingerprints := capture.sequenceFingerprint != "" || len(capture.conversationPrefixFingerprints) > 0
	if hasPrecomputedFingerprints {
		record.ConversationPrefixFingerprints = capture.conversationPrefixFingerprints
		record.SessionAnchorHMACs = capture.sessionAnchorHMACs
		record.Request.SequenceFingerprint = capture.sequenceFingerprint
		record.Request.ConversationItemCount = capture.conversationItemCount
		record.Request.SessionAnchorCount = capture.sessionAnchorCount
	}
	previousFingerprint := ""
	for _, entry := range capture.entries {
		plaintext, err := common.Marshal(entry)
		if err != nil {
			return nil, err
		}
		nonce, ciphertext, err := manager.encrypt(capture.request.UserID, messageAuditSchemaVersion, plaintext)
		if err != nil {
			return nil, err
		}
		stored := model.MessageAuditStoredBlob{
			SchemaVersion:  messageAuditSchemaVersion,
			ContentHMAC:    manager.contentHMAC(capture.request.UserID, messageAuditSchemaVersion, plaintext),
			KeyFingerprint: manager.keyFingerprint,
			ContentType:    entry.ContentType,
			PlaintextBytes: int64(len(plaintext)),
			Nonce:          nonce,
			Ciphertext:     ciphertext,
			Role:           entry.Role,
		}
		record.Blobs = append(record.Blobs, stored)
		if hasPrecomputedFingerprints || !isMessageAuditConversationBlob(stored) {
			continue
		}
		previousFingerprint = manager.nextMessageAuditSessionFingerprint(capture.request.UserID, capture.request.Protocol, previousFingerprint, stored)
		record.ConversationPrefixFingerprints = append(record.ConversationPrefixFingerprints, previousFingerprint)
		if isMessageAuditSessionAnchor(stored) {
			record.SessionAnchorHMACs = append(record.SessionAnchorHMACs, stored.ContentHMAC)
		}
	}
	if record.Request.SequenceFingerprint == "" {
		record.Request.SequenceFingerprint = previousFingerprint
		record.Request.ConversationItemCount = len(record.ConversationPrefixFingerprints)
		record.Request.SessionAnchorCount = len(record.SessionAnchorHMACs)
	}
	return record, nil
}

func (manager *messageAuditManager) buildMessageAuditSessionFingerprints(userID int, protocol string, entries []messageAuditPlaintext) ([]string, []string, string) {
	prefixes := make([]string, 0, len(entries))
	anchors := make([]string, 0, len(entries))
	previous := ""
	for _, entry := range entries {
		plaintext, err := common.Marshal(entry)
		if err != nil {
			continue
		}
		stored := model.MessageAuditStoredBlob{
			SchemaVersion: messageAuditSchemaVersion,
			ContentHMAC:   manager.contentHMAC(userID, messageAuditSchemaVersion, plaintext),
			ContentType:   entry.ContentType,
			Role:          entry.Role,
		}
		if !isMessageAuditConversationBlob(stored) {
			continue
		}
		previous = manager.nextMessageAuditSessionFingerprint(userID, protocol, previous, stored)
		prefixes = append(prefixes, previous)
		if isMessageAuditSessionAnchor(stored) {
			anchors = append(anchors, stored.ContentHMAC)
		}
	}
	return prefixes, anchors, previous
}

func messageAuditReviewTextMaxSize() int64 {
	raw := common.GetEnvOrDefaultString("MESSAGE_AUDIT_REVIEW_TEXT_MAX_BYTES", "")
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= messageAuditSnapshotMaxSize || value > messageAuditReviewTextAbsoluteMaxSize {
		return messageAuditReviewTextDefaultMaxSize
	}
	return value
}

func messageAuditPlaintextSize(entries []messageAuditPlaintext) int64 {
	var total int64
	for _, entry := range entries {
		data, err := common.Marshal(entry)
		if err == nil {
			total += int64(len(data))
		}
	}
	return total
}

func reduceMessageAuditEntries(entries []messageAuditPlaintext) []messageAuditPlaintext {
	reduced := make([]messageAuditPlaintext, 0, len(entries))
	for _, entry := range entries {
		if strings.ToLower(entry.Role) != "user" {
			continue
		}
		text := extractMessageAuditVisibleText(entry.Content)
		if text == "" {
			continue
		}
		reduced = append(reduced, messageAuditPlaintext{Role: "user", ContentType: "text", Content: text})
	}
	return reduced
}

func extractMessageAuditVisibleText(value any) string {
	parts := make([]string, 0)
	var visit func(any, string)
	visit = func(current any, key string) {
		switch typed := current.(type) {
		case string:
			lowerKey := strings.ToLower(key)
			if key == "" || lowerKey == "text" || lowerKey == "content" || lowerKey == "input_text" {
				trimmed := strings.TrimSpace(typed)
				if trimmed != "" {
					parts = append(parts, trimmed)
				}
			}
		case []any:
			for _, item := range typed {
				visit(item, key)
			}
		case map[string]any:
			itemType := strings.ToLower(common.Interface2String(typed["type"]))
			if isMessageAuditToolCallType(strings.ReplaceAll(itemType, "-", "_")) {
				return
			}
			for childKey, child := range typed {
				lowerKey := strings.ToLower(childKey)
				if lowerKey == "tool_calls" || lowerKey == "function_call" || lowerKey == "arguments" || lowerKey == "args" {
					continue
				}
				visit(child, childKey)
			}
		}
	}
	visit(value, "")
	return strings.Join(parts, "\n")
}

func isMessageAuditConversationBlob(stored model.MessageAuditStoredBlob) bool {
	return stored.ContentType != "tools" && stored.ContentType != "functions"
}

func isMessageAuditSessionAnchor(stored model.MessageAuditStoredBlob) bool {
	if !isMessageAuditConversationBlob(stored) {
		return false
	}
	role := strings.ToLower(stored.Role)
	return role != "developer" && role != "system"
}

func (manager *messageAuditManager) nextMessageAuditSessionFingerprint(userID int, protocol string, previous string, stored model.MessageAuditStoredBlob) string {
	mac := hmac.New(sha256.New, manager.dedupKey)
	_, _ = mac.Write([]byte("message-audit-session-prefix-v1"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(strconv.Itoa(userID)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(protocol))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(previous))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(stored.Role))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(stored.ContentType))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(stored.ContentHMAC))
	return hex.EncodeToString(mac.Sum(nil))
}

func (manager *messageAuditManager) sanitizeValue(value any, parentType string, depth int) any {
	if depth > 16 {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		itemType := strings.ToLower(common.Interface2String(typed["type"]))
		if itemType == "" {
			itemType = strings.ToLower(parentType)
		}
		if isHiddenAuditItemType(itemType) || common.Interface2String(typed["thought"]) == "true" {
			return nil
		}
		result := make(map[string]any)
		for key, child := range typed {
			lowerKey := strings.ToLower(key)
			if isHiddenOrSensitiveAuditKey(lowerKey) {
				continue
			}
			if isMediaAuditKey(lowerKey, itemType) {
				result[key] = manager.summarizeMediaValue(child, lowerKey)
				continue
			}
			childType := itemType
			if lowerKey == "inline_data" || lowerKey == "inlinedata" || lowerKey == "input_audio" || lowerKey == "file_data" {
				childType = lowerKey
			}
			sanitized := manager.sanitizeValue(child, childType, depth+1)
			if sanitized != nil {
				result[key] = sanitized
			}
		}
		if len(result) == 0 {
			return nil
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, child := range typed {
			sanitized := manager.sanitizeValue(child, parentType, depth+1)
			if sanitized != nil {
				result = append(result, sanitized)
			}
		}
		return result
	case string:
		trimmed := strings.TrimSpace(typed)
		if strings.HasPrefix(trimmed, "data:") {
			return manager.summarizeMediaString(trimmed, "data")
		}
		if (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) && len(trimmed) <= int(messageAuditSnapshotMaxSize) {
			var nested any
			if common.UnmarshalJsonStr(trimmed, &nested) == nil {
				return manager.sanitizeValue(nested, parentType, depth+1)
			}
		}
		return typed
	default:
		return value
	}
}

func isHiddenAuditItemType(itemType string) bool {
	return strings.Contains(itemType, "reasoning") || strings.Contains(itemType, "thinking") || strings.Contains(itemType, "encrypted") || itemType == "redacted_thinking"
}

func isHiddenOrSensitiveAuditKey(key string) bool {
	normalized := strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_")
	if strings.Contains(normalized, "reasoning") || strings.Contains(normalized, "thinking") || strings.Contains(normalized, "signature") || strings.Contains(normalized, "encrypted") {
		return true
	}
	switch normalized {
	case "authorization", "headers", "header", "cookie", "password", "passwd", "secret", "api_key", "apikey", "access_token", "refresh_token", "bearer_token", "private_key", "client_secret":
		return true
	default:
		return false
	}
}

func isMediaAuditKey(key string, itemType string) bool {
	if key == "image_url" || key == "audio_url" || key == "video_url" || key == "file_url" || key == "file_uri" || key == "fileuri" || key == "file_id" {
		return true
	}
	mediaType := strings.Contains(itemType, "image") || strings.Contains(itemType, "audio") || strings.Contains(itemType, "video") || strings.Contains(itemType, "file") || strings.Contains(itemType, "inline_data") || strings.Contains(itemType, "inlinedata")
	return mediaType && (key == "url" || strings.HasSuffix(key, "_url") || key == "data" || key == "source")
}

func (manager *messageAuditManager) summarizeMediaValue(value any, key string) map[string]any {
	if object, ok := value.(map[string]any); ok {
		for _, sourceKey := range []string{"url", "file_uri", "fileUri", "file_id", "data"} {
			source := common.Interface2String(object[sourceKey])
			if source == "" {
				continue
			}
			summary := manager.summarizeMediaString(source, sourceKey)
			// 媒体对象的 MIME 通常与 data/url 同级，摘要时显式保留该元数据。
			if mimeType := common.Interface2String(object["media_type"]); mimeType != "" {
				summary["mime"] = mimeType
			} else if mimeType := common.Interface2String(object["mime_type"]); mimeType != "" {
				summary["mime"] = mimeType
			}
			return summary
		}
	}
	data, err := common.Marshal(value)
	if err != nil {
		return map[string]any{"source_kind": "unavailable"}
	}
	text := common.Interface2String(value)
	if text == "" {
		text = string(data)
	}
	return manager.summarizeMediaString(text, key)
}

func (manager *messageAuditManager) summarizeMediaString(value string, key string) map[string]any {
	sourceKind := "inline"
	mimeType := ""
	contentBytes := int64(len(value))
	if strings.HasPrefix(value, "data:") {
		sourceKind = "data_uri"
		if separator := strings.Index(value, ","); separator > 5 {
			header := value[5:separator]
			mimeType = strings.TrimSuffix(header, ";base64")
			payloadLength := len(value) - separator - 1
			contentBytes = int64(payloadLength * 3 / 4)
		}
	} else if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		sourceKind = "remote_url"
	} else if strings.Contains(key, "file") {
		sourceKind = "file_id"
	}
	mac := hmac.New(sha256.New, manager.dedupKey)
	_, _ = mac.Write([]byte(value))
	return map[string]any{
		"source_kind": sourceKind,
		"mime":        mimeType,
		"bytes":       contentBytes,
		"digest":      hex.EncodeToString(mac.Sum(nil)),
	}
}

func auditRoleFromValue(value any, fallback string) string {
	if item, ok := value.(map[string]any); ok {
		if role := common.Interface2String(item["role"]); role != "" {
			return role
		}
	}
	return fallback
}

func countMessageAuditToolCalls(value any) int {
	count := 0
	switch typed := value.(type) {
	case map[string]any:
		itemType := strings.ToLower(strings.ReplaceAll(common.Interface2String(typed["type"]), "-", "_"))
		if isMessageAuditToolCallType(itemType) {
			count++
		}
		for key, child := range typed {
			lowerKey := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
			switch lowerKey {
			case "tool_calls":
				if calls, ok := child.([]any); ok {
					count += len(calls)
				} else if child != nil {
					count++
				}
				continue
			case "functioncall", "function_call":
				if child != nil {
					count++
				}
				continue
			}
			count += countMessageAuditToolCalls(child)
		}
	case []any:
		for _, child := range typed {
			count += countMessageAuditToolCalls(child)
		}
	}
	return count
}

func isMessageAuditToolCallType(itemType string) bool {
	switch itemType {
	case "tool_call", "tool_use", "server_tool_use", "function_call", "computer_call", "web_search_call", "file_search_call", "mcp_call", "code_interpreter_call", "custom_tool_call", "local_shell_call", "shell_call":
		return true
	default:
		return false
	}
}

func (manager *messageAuditManager) contentHMAC(userID int, schemaVersion int, plaintext []byte) string {
	mac := hmac.New(sha256.New, manager.dedupKey)
	_, _ = mac.Write([]byte(strconv.Itoa(userID)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(strconv.Itoa(schemaVersion)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(plaintext)
	return hex.EncodeToString(mac.Sum(nil))
}

func (manager *messageAuditManager) encrypt(userID int, schemaVersion int, plaintext []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(manager.encryptionKey)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	aad := []byte(fmt.Sprintf("%d:%d", userID, schemaVersion))
	return nonce, gcm.Seal(nil, nonce, plaintext, aad), nil
}

func (manager *messageAuditManager) decrypt(userID int, schemaVersion int, nonce []byte, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(manager.encryptionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("invalid nonce size")
	}
	aad := []byte(fmt.Sprintf("%d:%d", userID, schemaVersion))
	return gcm.Open(nil, nonce, ciphertext, aad)
}
