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
