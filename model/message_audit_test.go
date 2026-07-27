package model

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func messageAuditCaptureRecord(requestID string, userID int, capturedAt int64, hmac string) *MessageAuditCaptureRecord {
	return &MessageAuditCaptureRecord{
		Request: MessageAuditRequest{
			RequestID:      requestID,
			UserID:         userID,
			Status:         "pending",
			AuditStatus:    "captured",
			MessageCount:   1,
			PlaintextBytes: 12,
			CapturedAt:     capturedAt,
			CapturedAtNano: time.Unix(capturedAt, 0).UnixNano(),
			CreatedAt:      capturedAt,
			UpdatedAt:      capturedAt,
		},
		Blobs: []MessageAuditStoredBlob{
			{
				SchemaVersion:  1,
				ContentHMAC:    hmac,
				KeyFingerprint: "fingerprint",
				ContentType:    "message",
				PlaintextBytes: 12,
				Nonce:          []byte("nonce-value!"),
				Ciphertext:     []byte("ciphertext"),
				Role:           "user",
			},
		},
	}
}

func messageAuditSessionCaptureRecord(requestID string, userID int, capturedAt int64, fingerprints []string, anchors []string) *MessageAuditCaptureRecord {
	blobs := make([]MessageAuditStoredBlob, 0, len(anchors))
	for _, anchor := range anchors {
		blobs = append(blobs, MessageAuditStoredBlob{
			SchemaVersion:  1,
			ContentHMAC:    anchor,
			KeyFingerprint: "fingerprint",
			ContentType:    "input",
			PlaintextBytes: 12,
			Nonce:          []byte("nonce-value!"),
			Ciphertext:     []byte("ciphertext"),
			Role:           "user",
		})
	}
	sequenceFingerprint := ""
	if len(fingerprints) > 0 {
		sequenceFingerprint = fingerprints[len(fingerprints)-1]
	}
	return &MessageAuditCaptureRecord{
		Request: MessageAuditRequest{
			RequestID:             requestID,
			UserID:                userID,
			Protocol:              "openai_responses",
			Status:                "pending",
			AuditStatus:           "captured",
			MessageCount:          len(anchors),
			PlaintextBytes:        int64(len(anchors) * 12),
			CapturedAt:            capturedAt,
			CapturedAtNano:        time.Unix(capturedAt, 0).UnixNano(),
			CreatedAt:             capturedAt,
			UpdatedAt:             capturedAt,
			SequenceFingerprint:   sequenceFingerprint,
			ConversationItemCount: len(fingerprints),
			SessionAnchorCount:    len(anchors),
		},
		Blobs:                          blobs,
		ConversationPrefixFingerprints: fingerprints,
		SessionAnchorHMACs:             anchors,
	}
}

func TestCreateMessageAuditCaptureDeduplicatesWithinUser(t *testing.T) {
	truncateTables(t)

	first := messageAuditCaptureRecord("request-1", 10, 101, "same-hmac")
	second := messageAuditCaptureRecord("request-2", 10, 102, "same-hmac")
	otherUser := messageAuditCaptureRecord("request-3", 11, 103, "same-hmac")

	skipped, err := CreateMessageAuditCapture(first)
	require.NoError(t, err)
	assert.False(t, skipped)
	skipped, err = CreateMessageAuditCapture(second)
	require.NoError(t, err)
	assert.False(t, skipped)
	skipped, err = CreateMessageAuditCapture(otherUser)
	require.NoError(t, err)
	assert.False(t, skipped)

	var blobCount int64
	require.NoError(t, DB.Model(&MessageAuditBlob{}).Count(&blobCount).Error)
	assert.Equal(t, int64(2), blobCount)

	var secondRequest MessageAuditRequest
	require.NoError(t, DB.Where("request_id = ?", "request-2").First(&secondRequest).Error)
	assert.Equal(t, int64(12), secondRequest.DedupSavedBytes)

	var firstItem MessageAuditItem
	var secondItem MessageAuditItem
	require.NoError(t, DB.Where("audit_request_id = ?", first.Request.ID).First(&firstItem).Error)
	require.NoError(t, DB.Where("audit_request_id = ?", second.Request.ID).First(&secondItem).Error)
	assert.Equal(t, firstItem.BlobID, secondItem.BlobID)
}

func TestFinalizeMessageAuditRequestUpdatesNonEmptyModelName(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	require.NoError(t, DB.Create(&MessageAuditRequest{
		RequestID:   "finalize-model-request",
		ModelName:   "origin-model",
		Status:      "pending",
		AuditStatus: "captured",
		CapturedAt:  now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error)

	require.NoError(t, FinalizeMessageAuditRequest(MessageAuditFinalizeRecord{
		RequestID:   "finalize-model-request",
		ModelName:   "billing-model",
		Status:      "succeeded",
		FinalizedAt: now + 1,
	}))
	require.NoError(t, FinalizeMessageAuditRequest(MessageAuditFinalizeRecord{
		RequestID:   "finalize-model-request",
		Status:      "succeeded",
		FinalizedAt: now + 2,
	}))

	var request MessageAuditRequest
	require.NoError(t, DB.Where("request_id = ?", "finalize-model-request").First(&request).Error)
	assert.Equal(t, "billing-model", request.ModelName)
}

func TestMessageAuditSessionInferenceAndGroupedList(t *testing.T) {
	truncateTables(t)

	first := messageAuditSessionCaptureRecord("session-request-1", 31, 501, []string{"p1", "p2"}, []string{"a1", "a2"})
	second := messageAuditSessionCaptureRecord("session-request-2", 31, 502, []string{"p1", "p2", "p3"}, []string{"a1", "a2", "a3"})
	duplicate := messageAuditSessionCaptureRecord("session-request-3", 31, 503, []string{"p1", "p2", "p3"}, []string{"a1", "a2", "a3"})

	_, err := CreateMessageAuditCapture(first)
	require.NoError(t, err)
	_, err = CreateMessageAuditCapture(second)
	require.NoError(t, err)
	_, err = CreateMessageAuditCapture(duplicate)
	require.NoError(t, err)

	assert.NotEmpty(t, first.Request.AuditSessionID)
	assert.Equal(t, first.Request.AuditSessionID, second.Request.AuditSessionID)
	assert.Equal(t, first.Request.RequestID, second.Request.ParentRequestID)
	assert.Equal(t, "prefix", second.Request.SessionMatch)
	assert.Equal(t, first.Request.AuditSessionID, duplicate.Request.AuditSessionID)
	assert.Equal(t, second.Request.RequestID, duplicate.Request.ParentRequestID)
	assert.Equal(t, "exact", duplicate.Request.SessionMatch)

	requests, total, err := ListMessageAudits(MessageAuditListFilter{Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, requests, 1)
	assert.Equal(t, duplicate.Request.RequestID, requests[0].RequestID)
	assert.Equal(t, int64(3), requests[0].SessionRequestCount)
	assert.Zero(t, requests[0].CompressedCount)

	sessionRequests, sessionTotal, err := ListMessageAudits(MessageAuditListFilter{
		AuditSessionID: first.Request.AuditSessionID,
		Limit:          20,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(3), sessionTotal)
	require.Len(t, sessionRequests, 3)
	assert.Equal(t, duplicate.Request.RequestID, sessionRequests[0].RequestID)
	assert.Equal(t, second.Request.RequestID, sessionRequests[1].RequestID)
	assert.Equal(t, first.Request.RequestID, sessionRequests[2].RequestID)
}

func TestMessageAuditSessionInferenceRejectsAmbiguousPrefix(t *testing.T) {
	truncateTables(t)

	first := messageAuditSessionCaptureRecord("ambiguous-prefix-1", 35, 521, []string{"p1", "shared-prefix"}, []string{"a1", "a2"})
	second := messageAuditSessionCaptureRecord("ambiguous-prefix-2", 35, 522, []string{"other-1", "other-2"}, []string{"b1", "b2"})
	current := messageAuditSessionCaptureRecord("ambiguous-prefix-current", 35, 523, []string{"p1", "shared-prefix", "current"}, []string{"a1", "a2", "a3"})

	_, err := CreateMessageAuditCapture(first)
	require.NoError(t, err)
	_, err = CreateMessageAuditCapture(second)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&MessageAuditRequest{}).
		Where("id = ?", second.Request.ID).
		Updates(map[string]any{
			"audit_session_id":        "audsess_ambiguous_second",
			"sequence_fingerprint":    "shared-prefix",
			"conversation_item_count": 2,
		}).Error)
	_, err = CreateMessageAuditCapture(current)
	require.NoError(t, err)

	assert.NotEqual(t, first.Request.AuditSessionID, current.Request.AuditSessionID)
	assert.NotEqual(t, "audsess_ambiguous_second", current.Request.AuditSessionID)
	assert.Empty(t, current.Request.ParentRequestID)
	assert.Equal(t, "new", current.Request.SessionMatch)
}

func TestMessageAuditGroupedListKeepsHistoricalRowsStandalone(t *testing.T) {
	truncateTables(t)

	for index, requestID := range []string{"legacy-session-1", "legacy-session-2"} {
		require.NoError(t, DB.Create(&MessageAuditRequest{
			RequestID:   requestID,
			UserID:      31,
			Protocol:    "openai_responses",
			Status:      "succeeded",
			AuditStatus: "captured",
			CapturedAt:  int64(510 + index),
			CreatedAt:   int64(510 + index),
			UpdatedAt:   int64(510 + index),
		}).Error)
	}

	requests, total, err := ListMessageAudits(MessageAuditListFilter{Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, requests, 2)
	assert.Equal(t, "legacy-session-2", requests[0].RequestID)
	assert.Equal(t, int64(1), requests[0].SessionRequestCount)
	assert.Equal(t, "legacy-session-1", requests[1].RequestID)
}

func TestMessageAuditSessionInferenceRecognizesCompressedSubsequence(t *testing.T) {
	truncateTables(t)

	previous := messageAuditSessionCaptureRecord(
		"compressed-parent",
		41,
		601,
		[]string{"old-p1", "old-p2", "old-p3", "old-p4", "old-p5", "old-p6", "old-p7", "old-p8"},
		[]string{"h1", "h2", "h3", "h4", "h5", "h6", "h7", "h8"},
	)
	compressed := messageAuditSessionCaptureRecord(
		"compressed-child",
		41,
		602,
		[]string{"new-p1", "new-p2", "new-p3", "new-p4", "new-p5"},
		[]string{"h1", "h3", "h5", "summary-new", "h8"},
	)

	_, err := CreateMessageAuditCapture(previous)
	require.NoError(t, err)
	_, err = CreateMessageAuditCapture(compressed)
	require.NoError(t, err)

	assert.Equal(t, previous.Request.AuditSessionID, compressed.Request.AuditSessionID)
	assert.Equal(t, previous.Request.RequestID, compressed.Request.ParentRequestID)
	assert.Equal(t, "compressed", compressed.Request.SessionMatch)

	requests, total, err := ListMessageAudits(MessageAuditListFilter{Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, requests, 1)
	assert.Equal(t, int64(1), requests[0].CompressedCount)

	detailRequest, _, err := GetMessageAuditEncryptedDetail(compressed.Request.RequestID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), detailRequest.CompressedCount)
}

func TestMessageAuditSessionInferenceRejectsWeakCompressionEvidence(t *testing.T) {
	truncateTables(t)

	previous := messageAuditSessionCaptureRecord(
		"weak-parent",
		51,
		701,
		[]string{"old-p1", "old-p2", "old-p3", "old-p4", "old-p5", "old-p6", "old-p7", "old-p8"},
		[]string{"h1", "h2", "h3", "h4", "h5", "h6", "h7", "h8"},
	)
	weak := messageAuditSessionCaptureRecord(
		"weak-child",
		51,
		702,
		[]string{"new-p1", "new-p2", "new-p3", "new-p4", "new-p5", "new-p6"},
		[]string{"h1", "new-2", "new-3", "new-4", "new-5", "h8"},
	)

	_, err := CreateMessageAuditCapture(previous)
	require.NoError(t, err)
	_, err = CreateMessageAuditCapture(weak)
	require.NoError(t, err)

	assert.NotEqual(t, previous.Request.AuditSessionID, weak.Request.AuditSessionID)
	assert.Empty(t, weak.Request.ParentRequestID)
	assert.Equal(t, "new", weak.Request.SessionMatch)
}

func TestMessageAuditSessionInferenceRejectsAmbiguousCompression(t *testing.T) {
	truncateTables(t)

	first := messageAuditSessionCaptureRecord(
		"ambiguous-compression-1",
		55,
		721,
		[]string{"first-1", "first-2", "first-3", "first-4", "first-5", "first-6", "first-7", "first-8"},
		[]string{"h1", "h2", "h3", "h4", "h5", "h6", "h7", "h8"},
	)
	second := messageAuditSessionCaptureRecord(
		"ambiguous-compression-2",
		55,
		722,
		[]string{"second-1", "second-2", "second-3", "second-4", "second-5", "second-6", "second-7", "second-8"},
		[]string{"h1", "h2", "h3", "h4", "h5", "h6", "h7", "h8"},
	)
	current := messageAuditSessionCaptureRecord(
		"ambiguous-compression-current",
		55,
		723,
		[]string{"current-1", "current-2", "current-3", "current-4", "current-5"},
		[]string{"h1", "h3", "h5", "summary-new", "h8"},
	)

	_, err := CreateMessageAuditCapture(first)
	require.NoError(t, err)
	_, err = CreateMessageAuditCapture(second)
	require.NoError(t, err)
	assert.NotEqual(t, first.Request.AuditSessionID, second.Request.AuditSessionID)
	_, err = CreateMessageAuditCapture(current)
	require.NoError(t, err)

	assert.NotEqual(t, first.Request.AuditSessionID, current.Request.AuditSessionID)
	assert.NotEqual(t, second.Request.AuditSessionID, current.Request.AuditSessionID)
	assert.Empty(t, current.Request.ParentRequestID)
	assert.Equal(t, "new", current.Request.SessionMatch)
}

func TestMessageAuditStorageStatsReturnsAuditTableUsage(t *testing.T) {
	truncateTables(t)

	_, err := CreateMessageAuditCapture(messageAuditCaptureRecord("storage-request", 61, 801, "storage-hmac"))
	require.NoError(t, err)

	stats, err := GetMessageAuditStorageStats()
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.RequestCount)
	assert.Equal(t, int64(1), stats.BlobCount)
	assert.Equal(t, int64(1), stats.ItemCount)
	assert.Equal(t, int64(len("nonce-value!")+len("ciphertext")), stats.PayloadBytes)
	assert.Positive(t, stats.StorageBytes)

	cutoff, err := AdvanceMessageAuditPurgeBefore(time.Unix(802, 0).UnixNano())
	require.NoError(t, err)
	deletedRequests, err := DeleteMessageAuditsBeforeBatch(context.Background(), cutoff, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deletedRequests)
	deletedBlobs, err := DeleteOrphanMessageAuditBlobsBatch(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deletedBlobs)

	stats, err = GetMessageAuditStorageStats()
	require.NoError(t, err)
	assert.Zero(t, stats.RequestCount)
	assert.Zero(t, stats.BlobCount)
	assert.Zero(t, stats.ItemCount)
	assert.Zero(t, stats.PayloadBytes)
	assert.Positive(t, stats.StorageBytes)
}

func TestMessageAuditPurgeWatermarkRejectsLateOldCapture(t *testing.T) {
	truncateTables(t)

	cutoff, err := AdvanceMessageAuditPurgeBefore(time.Unix(200, 0).UnixNano())
	require.NoError(t, err)
	assert.Equal(t, time.Unix(200, 0).UnixNano(), cutoff)

	skipped, err := CreateMessageAuditCapture(messageAuditCaptureRecord("old-request", 1, 200, "old-hmac"))
	require.NoError(t, err)
	assert.True(t, skipped)
	skipped, err = CreateMessageAuditCapture(messageAuditCaptureRecord("new-request", 1, 201, "new-hmac"))
	require.NoError(t, err)
	assert.False(t, skipped)

	var requests []MessageAuditRequest
	require.NoError(t, DB.Order("captured_at asc").Find(&requests).Error)
	require.Len(t, requests, 1)
	assert.Equal(t, "new-request", requests[0].RequestID)

	var state MessageAuditState
	require.NoError(t, DB.First(&state, messageAuditStateID).Error)
	assert.Equal(t, int64(200), state.PurgeBefore)
	assert.Equal(t, time.Unix(200, 0).UnixNano(), state.PurgeBeforeNano)
}

func TestMessageAuditPurgeWatermarkReadsLegacySecondPrecisionState(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&MessageAuditState{
		ID:          messageAuditStateID,
		PurgeBefore: 210,
		UpdatedAt:   210,
	}).Error)

	skipped, err := CreateMessageAuditCapture(messageAuditCaptureRecord("legacy-watermark", 1, 210, "legacy-watermark-hmac"))
	require.NoError(t, err)
	assert.True(t, skipped)
}

func TestMessageAuditPurgeWatermarkKeepsLaterCaptureInSameSecond(t *testing.T) {
	truncateTables(t)

	cutoff := time.Unix(250, 100).UnixNano()
	_, err := AdvanceMessageAuditPurgeBefore(cutoff)
	require.NoError(t, err)

	record := messageAuditCaptureRecord("same-second-new-request", 1, 250, "same-second-hmac")
	record.Request.CapturedAtNano = time.Unix(250, 200).UnixNano()
	skipped, err := CreateMessageAuditCapture(record)
	require.NoError(t, err)
	assert.False(t, skipped)

	count, err := CountMessageAuditsBefore(context.Background(), cutoff)
	require.NoError(t, err)
	assert.Zero(t, count)
	deleted, err := DeleteMessageAuditsBeforeBatch(context.Background(), cutoff, 10)
	require.NoError(t, err)
	assert.Zero(t, deleted)
}

func TestMessageAuditCleanupHandlesLegacySecondPrecisionRows(t *testing.T) {
	truncateTables(t)

	record := messageAuditCaptureRecord("legacy-second-precision", 1, 260, "legacy-second-hmac")
	_, err := CreateMessageAuditCapture(record)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&MessageAuditRequest{}).
		Where("id = ?", record.Request.ID).
		Update("captured_at_nano", 0).Error)

	cutoff := time.Unix(260, 0).UnixNano()
	count, err := CountMessageAuditsBefore(context.Background(), cutoff)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	deleted, err := DeleteMessageAuditsBeforeBatch(context.Background(), cutoff, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)
}

func TestMessageAuditCleanupPreservesSharedBlobUntilOrphaned(t *testing.T) {
	truncateTables(t)

	first := messageAuditCaptureRecord("cleanup-1", 8, 301, "shared-hmac")
	second := messageAuditCaptureRecord("cleanup-2", 8, 302, "shared-hmac")
	_, err := CreateMessageAuditCapture(first)
	require.NoError(t, err)
	_, err = CreateMessageAuditCapture(second)
	require.NoError(t, err)

	deleted, err := DeleteMessageAuditsBeforeBatch(context.Background(), time.Unix(301, 0).UnixNano(), 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)
	deletedBlobs, err := DeleteOrphanMessageAuditBlobsBatch(context.Background(), 10)
	require.NoError(t, err)
	assert.Zero(t, deletedBlobs)

	deleted, err = DeleteMessageAuditsBeforeBatch(context.Background(), time.Unix(302, 0).UnixNano(), 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)
	deletedBlobs, err = DeleteOrphanMessageAuditBlobsBatch(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deletedBlobs)
}

func TestMessageAuditOrphanCleanupRechecksStaleCandidate(t *testing.T) {
	truncateTables(t)

	record := messageAuditCaptureRecord("orphan-race-old", 8, 320, "orphan-race-hmac")
	_, err := CreateMessageAuditCapture(record)
	require.NoError(t, err)
	require.NoError(t, DB.Where("audit_request_id = ?", record.Request.ID).Delete(&MessageAuditItem{}).Error)
	require.NoError(t, DB.Delete(&record.Request).Error)

	var blob MessageAuditBlob
	require.NoError(t, DB.Where("content_hmac = ?", "orphan-race-hmac").First(&blob).Error)
	newRequest := MessageAuditRequest{
		RequestID:      "orphan-race-new",
		UserID:         8,
		Status:         "pending",
		AuditStatus:    "captured",
		CapturedAt:     321,
		CapturedAtNano: time.Unix(321, 0).UnixNano(),
		CreatedAt:      321,
		UpdatedAt:      321,
	}
	require.NoError(t, DB.Create(&newRequest).Error)
	require.NoError(t, DB.Create(&MessageAuditItem{
		AuditRequestID: newRequest.ID,
		Sequence:       0,
		BlobID:         blob.ID,
		Role:           "user",
		ContentType:    "message",
	}).Error)

	deleted, err := deleteMessageAuditBlobIDsIfOrphan(DB, []int64{blob.ID})
	require.NoError(t, err)
	assert.Zero(t, deleted)
	require.NoError(t, DB.First(&blob, blob.ID).Error)
}

func runMessageAuditExternalDatabaseTest(t *testing.T, dialect string, dsn string) {
	t.Helper()
	originalDB := DB
	originalLogDB := LOG_DB
	var (
		db     *gorm.DB
		dbType common.DatabaseType
		err    error
	)
	switch dialect {
	case "mysql":
		dbType = common.DatabaseTypeMySQL
		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	case "postgres":
		dbType = common.DatabaseTypePostgreSQL
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	default:
		t.Fatalf("unsupported dialect %q", dialect)
	}
	require.NoError(t, err)

	DB = db
	LOG_DB = db
	common.SetDatabaseTypes(dbType, dbType)
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(&MessageAuditItem{}, &MessageAuditRequest{}, &MessageAuditBlob{}, &MessageAuditState{})
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
		DB = originalDB
		LOG_DB = originalLogDB
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	})

	require.False(t, db.Migrator().HasTable(&MessageAuditRequest{}), "外部测试数据库必须使用隔离 schema")
	require.NoError(t, db.AutoMigrate(&MessageAuditRequest{}, &MessageAuditBlob{}, &MessageAuditItem{}, &MessageAuditState{}))

	record := messageAuditCaptureRecord(dialect+"-request", 9, 401, dialect+"-hmac")
	skipped, err := CreateMessageAuditCapture(record)
	require.NoError(t, err)
	assert.False(t, skipped)

	request, items, err := GetMessageAuditEncryptedDetail(record.Request.RequestID)
	require.NoError(t, err)
	assert.Equal(t, record.Request.RequestID, request.RequestID)
	require.Len(t, items, 1)
	assert.Equal(t, []byte("ciphertext"), items[0].Ciphertext)

	deleted, err := DeleteMessageAuditsBeforeBatch(context.Background(), time.Unix(401, 0).UnixNano(), 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)
	deletedBlobs, err := DeleteOrphanMessageAuditBlobsBatch(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deletedBlobs)
}

func TestMessageAuditMySQLCompatibility(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN to run mysql compatibility test")
	}
	runMessageAuditExternalDatabaseTest(t, "mysql", dsn)
}

func TestMessageAuditPostgresCompatibility(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set TEST_POSTGRES_DSN to run postgres compatibility test")
	}
	runMessageAuditExternalDatabaseTest(t, "postgres", dsn)
}
