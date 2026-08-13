package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageAuditClaudeSessionMergesTransientVariants(t *testing.T) {
	truncate(t)
	manager := newMessageAuditTestManager(t)
	firstRequest := &dto.ClaudeRequest{
		System: []any{
			map[string]any{
				"type":          "text",
				"text":          "x-anthropic-billing-header: cc_version=2.1.76; cch=first; cc_entrypoint=cli;",
				"cache_control": map[string]any{"type": "ephemeral"},
			},
			map[string]any{"type": "text", "text": "稳定的系统指令"},
		},
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []any{map[string]any{
					"type":          "text",
					"text":          "第一条用户消息",
					"cache_control": map[string]any{"type": "ephemeral"},
				}},
			},
			{Role: "assistant", Content: "第一条助手回复"},
		},
	}
	secondRequest := &dto.ClaudeRequest{
		System: []any{
			map[string]any{"type": "text", "text": "x-anthropic-billing-header: cc_version=2.1.76; cch=second; cc_entrypoint=cli;"},
			map[string]any{
				"type":          "text",
				"text":          "稳定的系统指令",
				"cache_control": map[string]any{"type": "ephemeral"},
			},
		},
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "第一条用户消息"},
			{
				Role: "assistant",
				Content: []any{map[string]any{
					"type":          "text",
					"text":          "第一条助手回复",
					"cache_control": map[string]any{"type": "ephemeral"},
				}},
			},
			{Role: "user", Content: []any{map[string]any{"type": "text", "text": "继续处理"}}},
		},
	}
	thirdRequest := &dto.ClaudeRequest{
		System: []any{
			map[string]any{
				"type":          "text",
				"text":          "x-anthropic-billing-header: cch=third; cc_version=2.1.76; cc_entrypoint=cli;",
				"cache_control": map[string]any{"type": "ephemeral"},
			},
			map[string]any{"type": "text", "text": "稳定的系统指令"},
		},
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: []any{map[string]any{"type": "text", "text": "第一条用户消息"}}},
			{Role: "assistant", Content: "第一条助手回复"},
			{Role: "user", Content: "继续处理"},
		},
	}

	firstBefore, err := common.Marshal(firstRequest)
	require.NoError(t, err)
	first := buildClaudeMessageAuditTestRecord(t, manager, "claude-transient-first", 91, 1001, firstRequest)
	firstAfter, err := common.Marshal(firstRequest)
	require.NoError(t, err)
	assert.Equal(t, firstBefore, firstAfter)
	second := buildClaudeMessageAuditTestRecord(t, manager, "claude-transient-second", 91, 1002, secondRequest)
	third := buildClaudeMessageAuditTestRecord(t, manager, "claude-transient-third", 91, 1003, thirdRequest)

	_, err = model.CreateMessageAuditCapture(first)
	require.NoError(t, err)
	_, err = model.CreateMessageAuditCapture(second)
	require.NoError(t, err)
	_, err = model.CreateMessageAuditCapture(third)
	require.NoError(t, err)

	assert.Equal(t, "new", first.Request.SessionMatch)
	assert.Equal(t, first.Request.AuditSessionID, second.Request.AuditSessionID)
	assert.Equal(t, first.Request.RequestID, second.Request.ParentRequestID)
	assert.Equal(t, "prefix", second.Request.SessionMatch)
	assert.Equal(t, second.Request.AuditSessionID, third.Request.AuditSessionID)
	assert.Equal(t, second.Request.RequestID, third.Request.ParentRequestID)
	assert.Equal(t, "exact", third.Request.SessionMatch)

	require.Len(t, first.Blobs, 3)
	assert.Equal(t, []string{first.Blobs[1].ContentHMAC, first.Blobs[2].ContentHMAC}, first.SessionAnchorHMACs)
	systemPlaintext, err := manager.decrypt(91, first.Blobs[0].SchemaVersion, first.Blobs[0].Nonce, first.Blobs[0].Ciphertext)
	require.NoError(t, err)
	assert.Contains(t, string(systemPlaintext), "cch=first")
	assert.Contains(t, string(systemPlaintext), "cache_control")
	userPlaintext, err := manager.decrypt(91, first.Blobs[1].SchemaVersion, first.Blobs[1].Nonce, first.Blobs[1].Ciphertext)
	require.NoError(t, err)
	assert.Contains(t, string(userPlaintext), "cache_control")

	_, fingerprintEntries, _, _, _, _, err := manager.normalizeRequest(firstRequest)
	require.NoError(t, err)
	fingerprintJSON, err := common.Marshal(fingerprintEntries)
	require.NoError(t, err)
	assert.Contains(t, string(fingerprintJSON), claudeMessageAuditBillingHeaderPrefix)
	assert.NotContains(t, string(fingerprintJSON), "cch=")
	assert.NotContains(t, string(fingerprintJSON), "cache_control")
}

func TestMessageAuditClaudeSessionKeepsSemanticChangesDistinct(t *testing.T) {
	manager := newMessageAuditTestManager(t)
	base := newClaudeMessageAuditSemanticRequest("稳定系统指令", "用户问题", "天气", "晴天", "business-a")
	baseFingerprint := claudeMessageAuditTestFingerprint(t, manager, base)
	tests := []struct {
		name    string
		request *dto.ClaudeRequest
	}{
		{name: "稳定系统指令变化", request: newClaudeMessageAuditSemanticRequest("不同系统指令", "用户问题", "天气", "晴天", "business-a")},
		{name: "可见消息变化", request: newClaudeMessageAuditSemanticRequest("稳定系统指令", "不同用户问题", "天气", "晴天", "business-a")},
		{name: "工具输入变化", request: newClaudeMessageAuditSemanticRequest("稳定系统指令", "用户问题", "新闻", "晴天", "business-a")},
		{name: "工具输入同名业务字段变化", request: newClaudeMessageAuditSemanticRequest("稳定系统指令", "用户问题", "天气", "晴天", "business-b")},
		{name: "工具结果变化", request: newClaudeMessageAuditSemanticRequest("稳定系统指令", "用户问题", "天气", "雨天", "business-a")},
	}
	richText := newClaudeMessageAuditSemanticRequest("稳定系统指令", "用户问题", "天气", "晴天", "business-a")
	richText.Messages[0].Content = []any{map[string]any{
		"type":      "text",
		"text":      "用户问题",
		"citations": []any{map[string]any{"source": "document"}},
	}}
	tests = append(tests, struct {
		name    string
		request *dto.ClaudeRequest
	}{name: "单文本块附加语义字段", request: richText})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.NotEqual(t, baseFingerprint, claudeMessageAuditTestFingerprint(t, manager, test.request))
		})
	}
}

func TestMessageAuditSessionFingerprintEntriesLeaveOtherProtocolsUnchanged(t *testing.T) {
	manager := newMessageAuditTestManager(t)
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: "普通 OpenAI 消息"}},
	}
	entries, fingerprintEntries, _, _, _, _, err := manager.normalizeRequest(request)
	require.NoError(t, err)
	assert.Equal(t, entries, fingerprintEntries)

	expectedPrefixes, expectedAnchors, expectedSequence := manager.buildMessageAuditSessionFingerprints(92, string(types.RelayFormatOpenAI), entries)
	actualPrefixes, actualAnchors, actualSequence := manager.buildMessageAuditSessionFingerprints(92, string(types.RelayFormatOpenAI), fingerprintEntries)
	assert.Equal(t, expectedPrefixes, actualPrefixes)
	assert.Equal(t, expectedAnchors, actualAnchors)
	assert.Equal(t, expectedSequence, actualSequence)
}

func buildClaudeMessageAuditTestRecord(t *testing.T, manager *messageAuditManager, requestID string, userID int, capturedAt int64, request *dto.ClaudeRequest) *model.MessageAuditCaptureRecord {
	t.Helper()
	entries, fingerprintEntries, messageCount, toolCount, plaintextBytes, metadataOnly, err := manager.normalizeRequest(request)
	require.NoError(t, err)
	require.False(t, metadataOnly)
	prefixes, anchors, sequence := manager.buildClaudeMessageAuditSessionFingerprints(userID, string(types.RelayFormatClaude), fingerprintEntries, entries)
	capturedPlaintextBytes := messageAuditPlaintextSize(entries)
	record, err := manager.encryptCapture(&messageAuditCaptureEvent{
		request: model.MessageAuditRequest{
			RequestID:              requestID,
			UserID:                 userID,
			Protocol:               string(types.RelayFormatClaude),
			Status:                 "succeeded",
			AuditStatus:            "captured",
			MessageCount:           messageCount,
			ToolCount:              toolCount,
			PlaintextBytes:         plaintextBytes,
			CapturedPlaintextBytes: &capturedPlaintextBytes,
			CapturedAt:             capturedAt,
			CapturedAtNano:         time.Unix(capturedAt, 0).UnixNano(),
			CreatedAt:              capturedAt,
			UpdatedAt:              capturedAt,
		},
		entries:                        entries,
		conversationPrefixFingerprints: prefixes,
		sessionAnchorHMACs:             anchors,
		sequenceFingerprint:            sequence,
		conversationItemCount:          len(prefixes),
		sessionAnchorCount:             len(anchors),
	})
	require.NoError(t, err)
	return record
}

func claudeMessageAuditTestFingerprint(t *testing.T, manager *messageAuditManager, request *dto.ClaudeRequest) string {
	t.Helper()
	entries, fingerprintEntries, _, _, _, _, err := manager.normalizeRequest(request)
	require.NoError(t, err)
	_, _, fingerprint := manager.buildClaudeMessageAuditSessionFingerprints(93, string(types.RelayFormatClaude), fingerprintEntries, entries)
	require.NotEmpty(t, fingerprint)
	return fingerprint
}

func newClaudeMessageAuditSemanticRequest(system string, userText string, toolQuery string, toolResult string, toolCacheControl string) *dto.ClaudeRequest {
	return &dto.ClaudeRequest{
		System: []any{
			map[string]any{"type": "text", "text": "x-anthropic-billing-header: cc_version=2.1.76; cch=dynamic; cc_entrypoint=cli;"},
			map[string]any{"type": "text", "text": system},
		},
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: userText},
			{
				Role: "assistant",
				Content: []any{map[string]any{
					"type":          "tool_use",
					"id":            "tool-1",
					"name":          "lookup",
					"cache_control": map[string]any{"type": "ephemeral"},
					"input": map[string]any{
						"query":         toolQuery,
						"cache_control": toolCacheControl,
					},
				}},
			},
			{
				Role: "user",
				Content: []any{map[string]any{
					"type":        "tool_result",
					"tool_use_id": "tool-1",
					"content":     toolResult,
				}},
			},
		},
	}
}
