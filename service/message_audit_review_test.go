package service

import (
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitMessageAuditReviewMessagesKeepsEachToolChunkBounded(t *testing.T) {
	messages := []MessageAuditMessage{{Sequence: 7, Role: "user", ContentType: "text", Content: strings.Repeat("需要审核的长消息", 1200)}}

	chunks := splitMessageAuditReviewMessages(messages, "gpt-4o")
	require.Greater(t, len(chunks), 1)
	for index, chunk := range chunks {
		data, err := common.Marshal(chunk)
		require.NoError(t, err)
		assert.LessOrEqual(t, CountTextToken(string(data), "gpt-4o"), messageAuditReviewToolResultLimit)
		assert.Equal(t, 7, chunk.Sequence)
		assert.Equal(t, index, chunk.PartIndex)
		assert.Equal(t, len(chunks), chunk.PartCount)
	}
}

func TestMessageAuditReviewToolRejectsFilesOutsideFixedScope(t *testing.T) {
	files := []messageAuditReviewVirtualFile{{
		FileID: "request:allowed", Available: true,
		Messages: []messageAuditReviewMessage{{Sequence: 0, PartCount: 1, Role: "user", ContentType: "text", Content: "hello"}},
	}}

	_, _, err := executeMessageAuditReviewTool(MessageAuditReviewToolCall{
		Name: "read_file", Arguments: `{"file_id":"request:other","cursor":0,"limit":1}`,
	}, files, "gpt-4o")
	require.Error(t, err)
	var taskErr *messageAuditReviewTaskError
	require.ErrorAs(t, err, &taskErr)
	assert.Equal(t, "tool_scope_denied", taskErr.code)
}

func TestParseMessageAuditReviewOutputRequiresActuallyReadEvidence(t *testing.T) {
	files := []messageAuditReviewVirtualFile{{
		FileID: "request:one", Available: true,
		Messages: []messageAuditReviewMessage{
			{Sequence: 0, PartCount: 1, Role: "user", ContentType: "text", Content: "first"},
			{Sequence: 1, PartCount: 1, Role: "user", ContentType: "text", Content: "second"},
		},
	}}
	coverage := []MessageAuditReviewCoverage{{FileID: "request:one", StartSequence: 0, EndSequence: 0, StartCursor: 0, EndCursor: 0, EstimatedTokens: 10}}

	_, err := parseAndValidateMessageAuditReviewOutput(`{"summary":"发现风险","risk_level":"high","categories":["prompt_injection"],"findings":[{"category":"prompt_injection","severity":"high","file_id":"request:one","start_sequence":1,"end_sequence":1,"reason":"未读取范围"}]}`, files, coverage)
	require.Error(t, err)

	result, err := parseAndValidateMessageAuditReviewOutput(`{"summary":"已检查读取范围","risk_level":"low","categories":["prompt_injection"],"findings":[{"category":"prompt_injection","severity":"low","file_id":"request:one","start_sequence":0,"end_sequence":0,"reason":"读取范围内存在可疑指令"}]}`, files, coverage)
	require.NoError(t, err)
	assert.Equal(t, "low", result.RiskLevel)

	combinedCoverage := []MessageAuditReviewCoverage{
		{FileID: "request:one", StartSequence: 0, EndSequence: 0, StartCursor: 0, EndCursor: 0, EstimatedTokens: 5},
		{FileID: "request:one", StartSequence: 1, EndSequence: 1, StartCursor: 1, EndCursor: 1, EstimatedTokens: 5},
	}
	_, err = parseAndValidateMessageAuditReviewOutput(`{"summary":"已检查连续范围","risk_level":"low","categories":["prompt_injection"],"findings":[{"category":"prompt_injection","severity":"low","file_id":"request:one","start_sequence":0,"end_sequence":1,"reason":"连续读取范围内存在可疑指令"}]}`, files, combinedCoverage)
	require.NoError(t, err)
}

func TestParseMessageAuditReviewOutputRejectsPartiallyReadSplitMessage(t *testing.T) {
	files := []messageAuditReviewVirtualFile{{
		FileID:    "request:split",
		Available: true,
		Messages: []messageAuditReviewMessage{
			{Sequence: 3, PartIndex: 0, PartCount: 2, Role: "user", ContentType: "text", Content: "前半段"},
			{Sequence: 3, PartIndex: 1, PartCount: 2, Role: "user", ContentType: "text", Content: "后半段"},
		},
	}}
	coverage := []MessageAuditReviewCoverage{{
		FileID: "request:split", StartSequence: 3, EndSequence: 3,
		StartCursor: 0, EndCursor: 0, EstimatedTokens: 8,
	}}

	_, err := parseAndValidateMessageAuditReviewOutput(`{"summary":"只读取了半条消息","risk_level":"low","categories":["prompt_injection"],"findings":[{"category":"prompt_injection","severity":"low","file_id":"request:split","start_sequence":3,"end_sequence":3,"reason":"不能引用未完整读取的长消息"}]}`, files, coverage)
	require.Error(t, err)
}

func TestMessageAuditReviewUncoveredUsesVirtualChunkCursors(t *testing.T) {
	files := []messageAuditReviewVirtualFile{{
		FileID: "request:chunked", Available: true,
		Messages: []messageAuditReviewMessage{
			{Sequence: 7, PartIndex: 0, PartCount: 2, Role: "user", ContentType: "text", Content: "first half"},
			{Sequence: 7, PartIndex: 1, PartCount: 2, Role: "user", ContentType: "text", Content: "second half"},
		},
	}}
	coverage := []MessageAuditReviewCoverage{{
		FileID: "request:chunked", StartSequence: 7, EndSequence: 7,
		StartCursor: 0, EndCursor: 0, EstimatedTokens: 4,
	}}

	uncovered := buildMessageAuditReviewUncovered(files, coverage)
	require.Len(t, uncovered, 1)
	assert.Equal(t, "partially_read", uncovered[0].Reason)
}

func TestMessageAuditReviewResultAADIsBoundToUser(t *testing.T) {
	previousManager := messageAuditManagerInst
	messageAuditManagerInst = newMessageAuditTestManager(t)
	t.Cleanup(func() {
		messageAuditManagerInst = previousManager
	})
	payload := MessageAuditReviewPayload{UserID: 12, AuditSessionID: "session-aad", TargetRequestID: "request-aad"}
	result := &MessageAuditReviewResult{Summary: "已审核", RiskLevel: "none"}

	nonce, ciphertext, fingerprint, err := encryptMessageAuditReviewResult(payload, result)
	require.NoError(t, err)
	review := &model.MessageAuditReview{
		UserID: 12, AuditSessionID: payload.AuditSessionID, ReviewedRequestID: payload.TargetRequestID,
		KeyFingerprint: fingerprint, ResultNonce: nonce, ResultCiphertext: ciphertext,
	}
	decrypted, err := decryptMessageAuditReviewResult(review)
	require.NoError(t, err)
	assert.Equal(t, result.Summary, decrypted.Summary)

	review.UserID = 13
	_, err = decryptMessageAuditReviewResult(review)
	require.Error(t, err)
}

func TestGetMessageAuditReviewResponseExposesStableFailureCode(t *testing.T) {
	truncate(t)
	now := time.Now().Unix()
	require.NoError(t, model.DB.Create(&model.MessageAuditRequest{
		RequestID: "request-failed-review", AuditSessionID: "session-failed-review",
		Status: "succeeded", AuditStatus: "captured", CapturedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error)
	task, err := model.CreateSystemTaskWithActiveKey(model.SystemTaskTypeMessageAuditReview, "message_audit_review:session-failed-review", nil, nil)
	require.NoError(t, err)
	claimed, ok, err := model.ClaimSystemTask(task.ID, task.Type, "runner-review", now+60)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, model.FinishSystemTask(claimed.TaskID, "runner-review", model.SystemTaskStatusFailed, nil, "tool_unsupported"))
	require.NoError(t, model.DB.Create(&model.MessageAuditReview{
		AuditSessionID: "session-failed-review", CurrentTaskID: task.TaskID,
		Status: "failed", RiskLevel: "high", CreatedAt: now, UpdatedAt: now,
	}).Error)

	response, err := GetMessageAuditReviewResponse("session-failed-review")
	require.NoError(t, err)
	assert.Equal(t, "tool_unsupported", response.FailureCode)
	assert.Equal(t, "high", response.RiskLevel)
}
