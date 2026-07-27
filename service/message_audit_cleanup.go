package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/model"
)

const messageAuditCleanupBatchSize = 100

// MessageAuditCleanupPayload 描述消息审计清理任务的固定截止时间和来源。
type MessageAuditCleanupPayload struct {
	TargetTimestamp int64  `json:"target_timestamp"`
	BatchSize       int    `json:"batch_size"`
	Source          string `json:"source"`
}

// MessageAuditCleanupState 描述消息审计清理任务的实时进度。
type MessageAuditCleanupState struct {
	Total     int64 `json:"total"`
	Processed int64 `json:"processed"`
	Progress  int   `json:"progress"`
	Remaining int64 `json:"remaining"`
}

// MessageAuditCleanupResult 描述消息审计清理任务的最终删除数量。
type MessageAuditCleanupResult struct {
	DeletedRequests int64 `json:"deleted_requests"`
	DeletedBlobs    int64 `json:"deleted_blobs"`
}

type messageAuditCleanupHandler struct{}

func (messageAuditCleanupHandler) Type() string {
	return model.SystemTaskTypeMessageAuditCleanup
}

func (messageAuditCleanupHandler) Enabled() bool {
	return true
}

func (messageAuditCleanupHandler) Interval() time.Duration {
	return 24 * time.Hour
}

func (messageAuditCleanupHandler) NewPayload() any {
	return MessageAuditCleanupPayload{
		TargetTimestamp: time.Now().AddDate(0, 0, -MessageAuditRetentionDays()).UnixNano(),
		BatchSize:       messageAuditCleanupBatchSize,
		Source:          "retention",
	}
}

func (messageAuditCleanupHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := MessageAuditCleanupPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		failSystemTask(task, runnerID, err)
		return
	}
	if payload.TargetTimestamp <= 0 {
		failSystemTask(task, runnerID, errors.New("target timestamp is required"))
		return
	}
	if payload.BatchSize <= 0 {
		payload.BatchSize = messageAuditCleanupBatchSize
	}
	cutoff, err := model.AdvanceMessageAuditPurgeBefore(payload.TargetTimestamp)
	if err != nil {
		failSystemTask(task, runnerID, err)
		return
	}
	total, err := model.CountMessageAuditsBefore(ctx, cutoff)
	if err != nil {
		failSystemTask(task, runnerID, err)
		return
	}
	state := MessageAuditCleanupState{Total: total, Remaining: total}
	if total == 0 {
		state.Progress = 100
	}
	if err := model.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
		logSystemTaskLockError(ctx, task, err)
		return
	}

	for state.Remaining > 0 {
		if err := ctx.Err(); err != nil {
			failSystemTask(task, runnerID, err)
			return
		}
		deleted, err := model.DeleteMessageAuditsBeforeBatch(ctx, cutoff, payload.BatchSize)
		if err != nil {
			failSystemTask(task, runnerID, err)
			return
		}
		if deleted == 0 {
			failSystemTask(task, runnerID, errors.New("no message audit rows were deleted"))
			return
		}
		state.Processed += deleted
		if state.Processed >= state.Total {
			state.Processed = state.Total
			state.Remaining = 0
			state.Progress = 100
		} else {
			state.Remaining = state.Total - state.Processed
			state.Progress = int(state.Processed * 100 / state.Total)
		}
		if err := model.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
			logSystemTaskLockError(ctx, task, err)
			return
		}
	}

	var deletedBlobs int64
	for {
		if err := ctx.Err(); err != nil {
			failSystemTask(task, runnerID, err)
			return
		}
		deleted, err := model.DeleteOrphanMessageAuditBlobsBatch(ctx, payload.BatchSize)
		if err != nil {
			failSystemTask(task, runnerID, err)
			return
		}
		deletedBlobs += deleted
		if deleted == 0 {
			break
		}
	}

	result := MessageAuditCleanupResult{DeletedRequests: state.Processed, DeletedBlobs: deletedBlobs}
	if err := model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, result, ""); err != nil {
		logSystemTaskLockError(ctx, task, err)
	}
}

func init() {
	RegisterSystemTaskHandler(messageAuditCleanupHandler{})
}

// StartMessageAuditCleanupTask 创建或复用当前活动的一键清空任务。
//
// 参数 targetTimestamp 是点击清空时固定的 Unix 纳秒截止时间。
// 返回值包含任务以及是否新建，便于前端避免重复提交。
func StartMessageAuditCleanupTask(targetTimestamp int64) (*model.SystemTask, bool, error) {
	if targetTimestamp <= 0 {
		return nil, false, errors.New("target timestamp is required")
	}
	payload := MessageAuditCleanupPayload{
		TargetTimestamp: targetTimestamp,
		BatchSize:       messageAuditCleanupBatchSize,
		Source:          "manual",
	}
	return EnqueueSystemTask(model.SystemTaskTypeMessageAuditCleanup, payload)
}
