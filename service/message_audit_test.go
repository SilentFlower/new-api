package service

import (
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMessageAuditTestManager(t *testing.T) *messageAuditManager {
	t.Helper()
	encryptionKey, dedupKey, fingerprint, err := deriveMessageAuditKeys(strings.Repeat("a", messageAuditSecretMinLength))
	require.NoError(t, err)
	return &messageAuditManager{
		queue:          make(chan messageAuditEvent, messageAuditQueueCapacity),
		stop:           make(chan struct{}),
		done:           make(chan struct{}),
		encryptionKey:  encryptionKey,
		dedupKey:       dedupKey,
		keyFingerprint: fingerprint,
		keyConfigured:  true,
	}
}

func TestConsumeLogModelNameMatchesConsumptionLogNormalization(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		expected  string
	}{
		{name: "普通计费模型", modelName: "claude-opus-5", expected: "claude-opus-5"},
		{name: "旧版 gizmo", modelName: "gpt-4-gizmo-custom", expected: "gpt-4-gizmo-*"},
		{name: "新版 gizmo", modelName: "gpt-4o-gizmo-custom", expected: "gpt-4o-gizmo-*"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{OriginModelName: "origin-model"}
			info.FreezeBillingModelName(test.modelName)
			assert.Equal(t, test.expected, ConsumeLogModelName(info))
		})
	}
	assert.Empty(t, ConsumeLogModelName(nil))
}

func TestMessageAuditNormalizeRequestFiltersHiddenContent(t *testing.T) {
	manager := newMessageAuditTestManager(t)
	reasoning := "不得持久化的隐藏推理"
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{
			{
				Role:             "user",
				ReasoningContent: &reasoning,
				Content: []any{
					map[string]any{
						"type":    "text",
						"text":    "可见消息",
						"url":     "https://example.com/report",
						"api_key": "sk-secret",
					},
					map[string]any{"type": "reasoning", "text": "隐藏推理"},
					map[string]any{
						"type": "image_url",
						"image_url": map[string]any{
							"url": "data:image/png;base64,aGVsbG8=",
						},
					},
				},
			},
		},
	}

	entries, messageCount, toolCount, plaintextBytes, metadataOnly, err := manager.normalizeRequest(request)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, 1, messageCount)
	assert.Zero(t, toolCount)
	assert.Positive(t, plaintextBytes)
	assert.False(t, metadataOnly)

	record, err := manager.encryptCapture(&messageAuditCaptureEvent{
		request: model.MessageAuditRequest{UserID: 12},
		entries: entries,
	})
	require.NoError(t, err)
	require.Len(t, record.Blobs, 1)
	plaintext, err := manager.decrypt(12, record.Blobs[0].SchemaVersion, record.Blobs[0].Nonce, record.Blobs[0].Ciphertext)
	require.NoError(t, err)
	var captured messageAuditPlaintext
	require.NoError(t, common.Unmarshal(plaintext, &captured))
	serialized, err := common.Marshal(captured)
	require.NoError(t, err)
	content := string(serialized)
	assert.Contains(t, content, "可见消息")
	assert.Contains(t, content, "https://example.com/report")
	assert.NotContains(t, content, "隐藏推理")
	assert.NotContains(t, content, "不得持久化")
	assert.NotContains(t, content, "sk-secret")
	assert.NotContains(t, content, "aGVsbG8=")
	assert.Contains(t, content, `"source_kind":"data_uri"`)
	assert.Contains(t, content, `"mime":"image/png"`)
	assert.Contains(t, content, `"digest":`)
}

func TestMessageAuditEncryptionAndDedupAreUserScoped(t *testing.T) {
	manager := newMessageAuditTestManager(t)
	plaintext := []byte(`{"role":"user","content_type":"message","content":"same"}`)

	userOneHMAC := manager.contentHMAC(1, messageAuditSchemaVersion, plaintext)
	userTwoHMAC := manager.contentHMAC(2, messageAuditSchemaVersion, plaintext)
	assert.NotEqual(t, userOneHMAC, userTwoHMAC)

	nonce, ciphertext, err := manager.encrypt(1, messageAuditSchemaVersion, plaintext)
	require.NoError(t, err)
	decrypted, err := manager.decrypt(1, messageAuditSchemaVersion, nonce, ciphertext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)

	_, err = manager.decrypt(2, messageAuditSchemaVersion, nonce, ciphertext)
	assert.Error(t, err)
	ciphertext[len(ciphertext)-1] ^= 1
	_, err = manager.decrypt(1, messageAuditSchemaVersion, nonce, ciphertext)
	assert.Error(t, err)
}

func TestMessageAuditSessionFingerprintIgnoresStandaloneToolDefinitions(t *testing.T) {
	manager := newMessageAuditTestManager(t)
	request := model.MessageAuditRequest{UserID: 71, Protocol: string(types.RelayFormatOpenAIResponses)}
	first, err := manager.encryptCapture(&messageAuditCaptureEvent{
		request: request,
		entries: []messageAuditPlaintext{
			{Role: "user", ContentType: "input", Content: "hello"},
			{Role: "assistant", ContentType: "input", Content: "world"},
			{Role: "tool", ContentType: "tools", Content: map[string]any{"name": "first"}},
		},
	})
	require.NoError(t, err)
	second, err := manager.encryptCapture(&messageAuditCaptureEvent{
		request: request,
		entries: []messageAuditPlaintext{
			{Role: "user", ContentType: "input", Content: "hello"},
			{Role: "assistant", ContentType: "input", Content: "world"},
			{Role: "tool", ContentType: "tools", Content: map[string]any{"name": "changed"}},
		},
	})
	require.NoError(t, err)

	assert.Equal(t, first.Request.SequenceFingerprint, second.Request.SequenceFingerprint)
	assert.Equal(t, first.ConversationPrefixFingerprints, second.ConversationPrefixFingerprints)
	assert.Equal(t, []string{first.Blobs[0].ContentHMAC, first.Blobs[1].ContentHMAC}, first.SessionAnchorHMACs)
}

func TestMessageAuditProtocolExcludesResponsesCompaction(t *testing.T) {
	assert.True(t, isMessageAuditProtocolSupported(types.RelayFormatOpenAI))
	assert.True(t, isMessageAuditProtocolSupported(types.RelayFormatOpenAIResponses))
	assert.True(t, isMessageAuditProtocolSupported(types.RelayFormatClaude))
	assert.True(t, isMessageAuditProtocolSupported(types.RelayFormatGemini))
	assert.False(t, isMessageAuditProtocolSupported(types.RelayFormatOpenAIResponsesCompaction))
}

func TestStartMessageAuditCleanupTaskPreservesNanosecondCutoff(t *testing.T) {
	truncate(t)

	cutoff := time.Now().UnixNano()
	task, created, err := StartMessageAuditCleanupTask(cutoff)
	require.NoError(t, err)
	require.True(t, created)

	payload := MessageAuditCleanupPayload{}
	require.NoError(t, task.DecodePayload(&payload))
	assert.Equal(t, cutoff, payload.TargetTimestamp)
}

func TestMessageAuditOversizedSnapshotKeepsSafeMetadata(t *testing.T) {
	manager := newMessageAuditTestManager(t)
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: strings.Repeat("x", int(messageAuditSnapshotMaxSize)+1)}},
	}

	entries, messageCount, toolCount, plaintextBytes, metadataOnly, err := manager.normalizeRequest(request)
	require.NoError(t, err)
	assert.Nil(t, entries)
	assert.Equal(t, 1, messageCount)
	assert.Zero(t, toolCount)
	assert.Greater(t, plaintextBytes, messageAuditSnapshotMaxSize)
	assert.True(t, metadataOnly)
}

func TestMessageAuditToolCallCountAcrossProtocols(t *testing.T) {
	manager := newMessageAuditTestManager(t)

	openAIRequest := &dto.GeneralOpenAIRequest{}
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"gpt-test",
		"messages":[{"role":"assistant","content":"","tool_calls":[
			{"id":"call-1","type":"function","function":{"name":"first","arguments":"{}"}},
			{"id":"call-2","type":"function","function":{"name":"second","arguments":"{}"}}
		]}],
		"tools":[
			{"type":"function","function":{"name":"first","parameters":{"type":"object"}}},
			{"type":"function","function":{"name":"second","parameters":{"type":"object"}}}
		]
	}`), openAIRequest))
	_, _, toolCount, _, _, err := manager.normalizeRequest(openAIRequest)
	require.NoError(t, err)
	assert.Equal(t, 2, toolCount)

	responsesRequest := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"gpt-test",
		"input":[
			{"type":"function_call","call_id":"call-1","name":"lookup","arguments":"{}"},
			{"type":"function_call_output","call_id":"call-1","output":"ok"}
		],
		"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]
	}`), responsesRequest))
	_, _, toolCount, _, _, err = manager.normalizeRequest(responsesRequest)
	require.NoError(t, err)
	assert.Equal(t, 1, toolCount)

	claudeRequest := &dto.ClaudeRequest{}
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"claude-test",
		"max_tokens":32,
		"messages":[{"role":"assistant","content":[
			{"type":"tool_use","id":"tool-1","name":"lookup","input":{}},
			{"type":"tool_result","tool_use_id":"tool-1","content":"ok"}
		]}],
		"tools":[{"name":"lookup","input_schema":{"type":"object"}}]
	}`), claudeRequest))
	_, _, toolCount, _, _, err = manager.normalizeRequest(claudeRequest)
	require.NoError(t, err)
	assert.Equal(t, 1, toolCount)

	geminiRequest := &dto.GeminiChatRequest{}
	require.NoError(t, common.Unmarshal([]byte(`{
		"contents":[{"role":"model","parts":[
			{"functionCall":{"name":"lookup","args":{}}},
			{"functionResponse":{"name":"lookup","response":{"status":"ok"}}}
		]}],
		"tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"object"}}]}]
	}`), geminiRequest))
	_, _, toolCount, _, _, err = manager.normalizeRequest(geminiRequest)
	require.NoError(t, err)
	assert.Equal(t, 1, toolCount)
}

func TestMessageAuditManagerDrainsCaptureBeforeFinalize(t *testing.T) {
	truncate(t)
	manager := newMessageAuditTestManager(t)
	now := time.Now().Unix()
	go manager.run()

	captured := manager.enqueue(messageAuditEvent{
		kind: "capture",
		size: 128,
		capture: &messageAuditCaptureEvent{
			request: model.MessageAuditRequest{
				RequestID:      "async-request",
				UserID:         7,
				ModelName:      "origin-model",
				Status:         "pending",
				AuditStatus:    "captured",
				MessageCount:   1,
				PlaintextBytes: 16,
				CapturedAt:     now,
				CreatedAt:      now,
				UpdatedAt:      now,
			},
			entries: []messageAuditPlaintext{{Role: "user", ContentType: "message", Content: "hello"}},
		},
	})
	require.True(t, captured)
	finalized := manager.enqueue(messageAuditEvent{
		kind: "finalize",
		size: 64,
		finalize: &model.MessageAuditFinalizeRecord{
			RequestID:   "async-request",
			ModelName:   "billing-model",
			Status:      "succeeded",
			HTTPStatus:  200,
			DurationMS:  25,
			FinalizedAt: now + 1,
		},
	})
	require.True(t, finalized)

	manager.stopping.Store(true)
	close(manager.stop)
	<-manager.done

	var request model.MessageAuditRequest
	require.NoError(t, model.DB.Where("request_id = ?", "async-request").First(&request).Error)
	assert.Equal(t, "succeeded", request.Status)
	assert.Equal(t, "billing-model", request.ModelName)
	assert.Equal(t, 200, request.HTTPStatus)
	assert.Equal(t, int64(25), request.DurationMS)
	assert.Equal(t, uint64(2), manager.succeeded.Load())
	assert.Zero(t, manager.queuedBytes.Load())
}
