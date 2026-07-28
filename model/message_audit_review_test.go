package model

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMessageAuditReviewSourceIDsUsesEveryCompressionBoundary(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	requests := []MessageAuditRequest{
		{RequestID: "request-1", AuditSessionID: "session-1", SessionMatch: "new", CapturedAt: now, CreatedAt: now, UpdatedAt: now},
		{RequestID: "request-2", AuditSessionID: "session-1", ParentRequestID: "request-1", SessionMatch: "compressed", CapturedAt: now + 1, CreatedAt: now + 1, UpdatedAt: now + 1},
		{RequestID: "request-3", AuditSessionID: "session-1", ParentRequestID: "request-2", SessionMatch: "prefix", CapturedAt: now + 2, CreatedAt: now + 2, UpdatedAt: now + 2},
		{RequestID: "request-4", AuditSessionID: "session-1", ParentRequestID: "request-3", SessionMatch: "compressed", CapturedAt: now + 3, CreatedAt: now + 3, UpdatedAt: now + 3},
		{RequestID: "request-5", AuditSessionID: "session-1", ParentRequestID: "request-4", SessionMatch: "prefix", CapturedAt: now + 4, CreatedAt: now + 4, UpdatedAt: now + 4},
	}
	require.NoError(t, DB.Create(&requests).Error)

	sources, err := BuildMessageAuditReviewSourceIDs("session-1", "request-5")
	require.NoError(t, err)
	assert.Equal(t, []string{"request-1", "request-3", "request-5"}, sources)
}

func TestAttachMessageAuditReviewMetadataPreservesOldRiskWhenStale(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	require.NoError(t, DB.Create(&MessageAuditReview{
		AuditSessionID: "session-risk", ReviewedRequestID: "request-old", Status: "running",
		RiskLevel: "high", ReviewedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error)
	requests := []MessageAuditRequest{{RequestID: "request-new", AuditSessionID: "session-risk"}}

	require.NoError(t, AttachMessageAuditReviewMetadata(requests))
	assert.Equal(t, "running", requests[0].ReviewStatus)
	assert.Equal(t, "high", requests[0].ReviewRiskLevel)
	assert.True(t, requests[0].ReviewStale)
}

func TestCreateMessageAuditReviewTaskPreservesSuccessfulResultAttribution(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()
	oldTask := SystemTask{
		TaskID: "review-task-old", Type: SystemTaskTypeMessageAuditReview,
		Status: SystemTaskStatusSucceeded, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, DB.Create(&oldTask).Error)
	require.NoError(t, DB.Create(&MessageAuditReview{
		AuditSessionID: "session-review", UserID: 9, CurrentTaskID: oldTask.TaskID,
		Status: "succeeded", RiskLevel: "high", ReviewChannelID: 1, ReviewModel: "old-model",
		ResultCiphertext: []byte("ciphertext"), ReviewedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error)

	task, err := CreateMessageAuditReviewTask(
		SystemTaskTypeMessageAuditReview+":session-review",
		map[string]any{"source_request_ids": []string{"request-1"}},
		MessageAuditReview{AuditSessionID: "session-review", UserID: 9, ReviewChannelID: 2, ReviewModel: "new-model"},
	)
	require.NoError(t, err)

	review, err := GetMessageAuditReview("session-review")
	require.NoError(t, err)
	require.NotNil(t, review)
	assert.Equal(t, task.TaskID, review.CurrentTaskID)
	assert.Equal(t, "pending", review.Status)
	assert.Equal(t, 1, review.ReviewChannelID)
	assert.Equal(t, "old-model", review.ReviewModel)
	var oldTaskCount int64
	require.NoError(t, DB.Model(&SystemTask{}).Where("task_id = ?", oldTask.TaskID).Count(&oldTaskCount).Error)
	assert.Zero(t, oldTaskCount)
}

func TestDeleteMessageAuditsRemovesReferencedReview(t *testing.T) {
	truncateTables(t)
	now := time.Now()
	request := MessageAuditRequest{
		RequestID: "request-cleanup", AuditSessionID: "session-cleanup", Status: "succeeded",
		CapturedAt: now.Unix(), CapturedAtNano: now.UnixNano(), CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	}
	require.NoError(t, DB.Create(&request).Error)
	review := MessageAuditReview{
		AuditSessionID: "session-cleanup", ReviewedRequestID: request.RequestID, Status: "succeeded",
		CurrentTaskID: "review-task-cleanup", RiskLevel: "medium", CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	}
	require.NoError(t, DB.Create(&review).Error)
	require.NoError(t, DB.Create(&MessageAuditReviewSource{ReviewID: review.ID, RequestID: request.RequestID, CreatedAt: now.Unix()}).Error)
	require.NoError(t, DB.Create(&SystemTask{
		TaskID: review.CurrentTaskID, Type: SystemTaskTypeMessageAuditReview,
		Status: SystemTaskStatusSucceeded, CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	}).Error)

	deleted, err := DeleteMessageAuditsBeforeBatch(context.Background(), now.Add(time.Second).UnixNano(), 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted.DeletedRequests)
	var reviewCount int64
	require.NoError(t, DB.Model(&MessageAuditReview{}).Count(&reviewCount).Error)
	assert.Zero(t, reviewCount)
	var taskCount int64
	require.NoError(t, DB.Model(&SystemTask{}).Where("task_id = ?", review.CurrentTaskID).Count(&taskCount).Error)
	assert.Zero(t, taskCount)
}

func TestDeleteMessageAuditsRemovesRunningReviewByFixedTaskSources(t *testing.T) {
	truncateTables(t)
	now := time.Now()
	requests := []MessageAuditRequest{
		{RequestID: "request-source", AuditSessionID: "session-running", CapturedAt: now.Unix(), CapturedAtNano: now.UnixNano(), CreatedAt: now.Unix(), UpdatedAt: now.Unix()},
		{RequestID: "request-latest", AuditSessionID: "session-running", CapturedAt: now.Add(time.Second).Unix(), CapturedAtNano: now.Add(time.Second).UnixNano(), CreatedAt: now.Unix(), UpdatedAt: now.Unix()},
	}
	require.NoError(t, DB.Create(&requests).Error)
	task, err := CreateMessageAuditReviewTask(
		SystemTaskTypeMessageAuditReview+":session-running",
		map[string]any{"source_request_ids": []string{"request-source", "request-latest"}},
		MessageAuditReview{AuditSessionID: "session-running", UserID: 4, ReviewChannelID: 1, ReviewModel: "review-model"},
	)
	require.NoError(t, err)

	deleted, err := DeleteMessageAuditsBeforeBatch(context.Background(), now.Add(500*time.Millisecond).UnixNano(), 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted.DeletedRequests)
	review, err := GetMessageAuditReview("session-running")
	require.NoError(t, err)
	assert.Nil(t, review)
	storedTask, err := GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	assert.Nil(t, storedTask)
}
