package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMessageAuditTestManager(t *testing.T) *messageAuditManager {
	t.Helper()
	encryptionKey, dedupKey, fingerprint, err := deriveMessageAuditKeys(strings.Repeat("a", messageAuditSecretMinLength))
	require.NoError(t, err)
	reviewKey, reviewFingerprint := deriveMessageAuditReviewKey(strings.Repeat("a", messageAuditSecretMinLength))
	return &messageAuditManager{
		queue:                make(chan messageAuditEvent, messageAuditQueueCapacity),
		stop:                 make(chan struct{}),
		done:                 make(chan struct{}),
		encryptionKey:        encryptionKey,
		dedupKey:             dedupKey,
		reviewKey:            reviewKey,
		reviewKeyFingerprint: reviewFingerprint,
		keyFingerprint:       fingerprint,
		keyConfigured:        true,
	}
}

func TestMessageAuditStorageStatsCacheUsesTTLAndPreservesLastGoodValue(t *testing.T) {
	cache := messageAuditStorageStatsCache{}
	now := time.Unix(1000, 0)
	loadCount := 0
	loader := func() (model.MessageAuditStorageStats, error) {
		loadCount++
		return model.MessageAuditStorageStats{RequestCount: int64(loadCount)}, nil
	}

	first, err := cache.get(now, loader)
	require.NoError(t, err)
	second, err := cache.get(now.Add(messageAuditStorageStatsCacheTTL-time.Second), loader)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, loadCount)

	third, err := cache.get(now.Add(messageAuditStorageStatsCacheTTL), loader)
	require.NoError(t, err)
	assert.Equal(t, int64(2), third.RequestCount)
	assert.Equal(t, 2, loadCount)

	_, err = cache.get(now.Add(messageAuditStorageStatsCacheTTL*2), func() (model.MessageAuditStorageStats, error) {
		return model.MessageAuditStorageStats{RequestCount: 99}, assert.AnError
	})
	require.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, int64(2), cache.stats.RequestCount)
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

	entries, _, messageCount, toolCount, plaintextBytes, metadataOnly, err := manager.normalizeRequest(request)
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

func TestMessageAuditNormalizeImageRequestKeepsOnlySafeFields(t *testing.T) {
	manager := newMessageAuditTestManager(t)
	n := uint(2)
	stream := false
	watermark := true
	request := &dto.ImageRequest{
		Model:             "gpt-image-1",
		Prompt:            "生成一张安全测试图片",
		N:                 &n,
		Size:              "1024x1024",
		Quality:           "high",
		ResponseFormat:    "b64_json",
		Style:             json.RawMessage(`"vivid"`),
		Background:        json.RawMessage(`"transparent"`),
		Moderation:        json.RawMessage(`"auto"`),
		OutputFormat:      json.RawMessage(`"png"`),
		OutputCompression: json.RawMessage(`80`),
		PartialImages:     json.RawMessage(`1`),
		Stream:            &stream,
		InputFidelity:     json.RawMessage(`"https://example.com/input.png"`),
		Watermark:         &watermark,
		WatermarkEnabled:  json.RawMessage(`true`),
		Image:             json.RawMessage(`"data:image/png;base64,aW1hZ2U="`),
		Images:            json.RawMessage(`["data:image/png;base64,aW1hZ2Vz"]`),
		Mask:              json.RawMessage(`"data:image/png;base64,bWFzaw=="`),
		ExtraFields:       json.RawMessage(`{"source":"https://example.com/source.png"}`),
		User:              json.RawMessage(`"external-user"`),
		UserId:            json.RawMessage(`"external-user-id"`),
		Extra: map[string]json.RawMessage{
			"custom_image": json.RawMessage(`"data:image/png;base64,Y3VzdG9t"`),
		},
	}

	entries, fingerprintEntries, messageCount, toolCount, plaintextBytes, metadataOnly, err := manager.normalizeRequest(request)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Len(t, fingerprintEntries, 1)
	assert.Equal(t, 1, messageCount)
	assert.Zero(t, toolCount)
	assert.Positive(t, plaintextBytes)
	assert.False(t, metadataOnly)
	assert.Equal(t, "user", entries[0].Role)
	assert.Equal(t, "image_request", entries[0].ContentType)

	serialized, err := common.Marshal(entries[0].Content)
	require.NoError(t, err)
	content := string(serialized)
	assert.Contains(t, content, "生成一张安全测试图片")
	assert.Contains(t, content, `"model":"gpt-image-1"`)
	assert.Contains(t, content, `"n":2`)
	assert.Contains(t, content, `"size":"1024x1024"`)
	assert.Contains(t, content, `"output_format":"png"`)
	assert.NotContains(t, content, "input_fidelity")
	assert.NotContains(t, content, "aW1hZ2U=")
	assert.NotContains(t, content, "bWFzaw==")
	assert.NotContains(t, content, "example.com")
	assert.NotContains(t, content, "external-user")
	assert.NotContains(t, content, "custom_image")
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
	assert.True(t, isMessageAuditProtocolSupported(types.RelayFormatOpenAIImage))
	assert.False(t, isMessageAuditProtocolSupported(types.RelayFormatOpenAIResponsesCompaction))
}

func TestMessageAuditImageRequestSkipsSessionFingerprints(t *testing.T) {
	manager := newMessageAuditTestManager(t)
	record, err := manager.encryptCapture(&messageAuditCaptureEvent{
		request: model.MessageAuditRequest{UserID: 72, Protocol: string(types.RelayFormatOpenAIImage)},
		entries: []messageAuditPlaintext{
			{Role: "user", ContentType: "image_request", Content: map[string]any{"prompt": "重复提示词"}},
		},
	})
	require.NoError(t, err)
	require.Len(t, record.Blobs, 1)
	assert.Empty(t, record.Request.SequenceFingerprint)
	assert.Zero(t, record.Request.ConversationItemCount)
	assert.Zero(t, record.Request.SessionAnchorCount)
	assert.Empty(t, record.ConversationPrefixFingerprints)
	assert.Empty(t, record.SessionAnchorHMACs)
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

func TestManualMessageAuditCleanupUsesFastClearAndFinishesTask(t *testing.T) {
	truncate(t)

	now := time.Now()
	request := model.MessageAuditRequest{
		RequestID: "manual-clear-request", Status: "succeeded", AuditStatus: "captured",
		CapturedAt: now.Unix(), CapturedAtNano: now.UnixNano(), CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	}
	require.NoError(t, model.DB.Create(&request).Error)
	blob := model.MessageAuditBlob{
		UserID: 1, SchemaVersion: 1, ContentHMAC: "manual-clear-hmac",
		KeyFingerprint: "fingerprint", ContentType: "message", Nonce: []byte("nonce"), Ciphertext: []byte("ciphertext"),
		CreatedAt: now.Unix(),
	}
	require.NoError(t, model.DB.Create(&blob).Error)
	require.NoError(t, model.DB.Create(&model.MessageAuditItem{
		AuditRequestID: request.ID, Sequence: 0, BlobID: blob.ID, Role: "user", ContentType: "message",
	}).Error)

	task, created, err := StartMessageAuditCleanupTask(now.UnixNano())
	require.NoError(t, err)
	require.True(t, created)
	claimed, acquired, err := model.ClaimSystemTask(
		task.ID,
		model.SystemTaskTypeMessageAuditCleanup,
		"manual-clear-runner",
		now.Add(time.Minute).Unix(),
	)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotNil(t, claimed)
	messageAuditStatsCache.mutex.Lock()
	messageAuditStatsCache.valid = true
	messageAuditStatsCache.mutex.Unlock()

	messageAuditCleanupHandler{}.Run(context.Background(), claimed, "manual-clear-runner")

	stored, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, model.SystemTaskStatusSucceeded, stored.Status)
	state := MessageAuditCleanupState{}
	require.NoError(t, stored.DecodeState(&state))
	assert.Equal(t, MessageAuditCleanupState{Total: 1, Processed: 1, Progress: 100}, state)
	result := MessageAuditCleanupResult{}
	require.NoError(t, common.UnmarshalJsonStr(stored.Result, &result))
	assert.Equal(t, MessageAuditCleanupResult{DeletedRequests: 1, DeletedBlobs: 1}, result)
	var requestCount int64
	require.NoError(t, model.DB.Model(&model.MessageAuditRequest{}).Count(&requestCount).Error)
	assert.Zero(t, requestCount)
	messageAuditStatsCache.mutex.Lock()
	cacheValid := messageAuditStatsCache.valid
	messageAuditStatsCache.mutex.Unlock()
	assert.False(t, cacheValid)
}

func TestMessageAuditOversizedSnapshotKeepsSafeMetadata(t *testing.T) {
	manager := newMessageAuditTestManager(t)
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: strings.Repeat("x", int(messageAuditSnapshotMaxSize)+1)}},
	}

	entries, fingerprintEntries, messageCount, toolCount, plaintextBytes, metadataOnly, err := manager.normalizeRequest(request)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Len(t, fingerprintEntries, 1)
	assert.Equal(t, 1, messageCount)
	assert.Zero(t, toolCount)
	assert.Greater(t, plaintextBytes, messageAuditSnapshotMaxSize)
	assert.False(t, metadataOnly)
	assert.Equal(t, "user", entries[0].Role)
	assert.Equal(t, "text", entries[0].ContentType)
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
	_, _, _, toolCount, _, _, err := manager.normalizeRequest(openAIRequest)
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
	_, _, _, toolCount, _, _, err = manager.normalizeRequest(responsesRequest)
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
	_, _, _, toolCount, _, _, err = manager.normalizeRequest(claudeRequest)
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
	_, _, _, toolCount, _, _, err = manager.normalizeRequest(geminiRequest)
	require.NoError(t, err)
	assert.Equal(t, 1, toolCount)
}

func TestGetMessageAuditDetailUsesSemanticToolRolesAcrossProtocols(t *testing.T) {
	truncate(t)
	manager := newMessageAuditTestManager(t)
	previousManager := messageAuditManagerInst
	messageAuditManagerInst = manager
	t.Cleanup(func() {
		messageAuditManagerInst = previousManager
	})
	now := time.Now().Unix()
	tests := []struct {
		name        string
		protocol    types.RelayFormat
		entries     []messageAuditPlaintext
		expectRoles []string
		expectTypes []string
	}{
		{
			name: "Responses 调用和结果", protocol: types.RelayFormatOpenAIResponses,
			entries: []messageAuditPlaintext{
				{Role: "user", ContentType: "input", Content: map[string]any{"type": "function_call", "call_id": "call-1", "name": "lookup"}},
				{Role: "user", ContentType: "input", Content: map[string]any{"type": "function_call_output", "call_id": "call-1", "output": "ok"}},
			},
			expectRoles: []string{"assistant", "tool"}, expectTypes: []string{"tool_call", "tool_result"},
		},
		{
			name: "Claude 纯工具结果", protocol: types.RelayFormatClaude,
			entries: []messageAuditPlaintext{{
				Role: "user", ContentType: "message", Content: map[string]any{
					"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "tool-1", "content": "ok"}},
				},
			}},
			expectRoles: []string{"tool"}, expectTypes: []string{"tool_result"},
		},
		{
			name: "Claude 纯工具调用", protocol: types.RelayFormatClaude,
			entries: []messageAuditPlaintext{{
				Role: "assistant", ContentType: "message", Content: map[string]any{
					"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": "tool-1", "name": "lookup", "input": map[string]any{}}},
				},
			}},
			expectRoles: []string{"assistant"}, expectTypes: []string{"tool_call"},
		},
		{
			name: "Claude 混合用户文本不强制改写", protocol: types.RelayFormatClaude,
			entries: []messageAuditPlaintext{{
				Role: "user", ContentType: "message", Content: map[string]any{
					"role": "user", "content": []any{
						map[string]any{"type": "text", "text": "继续处理"},
						map[string]any{"type": "tool_result", "tool_use_id": "tool-1", "content": "ok"},
					},
				},
			}},
			expectRoles: []string{"user"}, expectTypes: []string{"message"},
		},
		{
			name: "Gemini 纯函数结果", protocol: types.RelayFormatGemini,
			entries: []messageAuditPlaintext{{
				Role: "user", ContentType: "content", Content: map[string]any{
					"role": "user", "parts": []any{map[string]any{"functionResponse": map[string]any{"name": "lookup", "response": map[string]any{"status": "ok"}}}},
				},
			}},
			expectRoles: []string{"tool"}, expectTypes: []string{"tool_result"},
		},
		{
			name: "Gemini 纯函数调用", protocol: types.RelayFormatGemini,
			entries: []messageAuditPlaintext{{
				Role: "model", ContentType: "content", Content: map[string]any{
					"role": "model", "parts": []any{map[string]any{"functionCall": map[string]any{"name": "lookup", "args": map[string]any{}}}},
				},
			}},
			expectRoles: []string{"assistant"}, expectTypes: []string{"tool_call"},
		},
		{
			name: "OpenAI 普通用户对象不改写", protocol: types.RelayFormatOpenAI,
			entries: []messageAuditPlaintext{{
				Role: "user", ContentType: "message", Content: map[string]any{"role": "user", "content": map[string]any{"type": "tool_result"}},
			}},
			expectRoles: []string{"user"}, expectTypes: []string{"message"},
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestID := fmt.Sprintf("semantic-role-%d", index)
			record, err := manager.encryptCapture(&messageAuditCaptureEvent{
				request: model.MessageAuditRequest{
					RequestID: requestID, UserID: 80 + index, Protocol: string(test.protocol), Status: "succeeded", AuditStatus: "captured",
					MessageCount: len(test.entries), CapturedAt: now, CreatedAt: now, UpdatedAt: now,
				},
				entries: test.entries,
			})
			require.NoError(t, err)
			_, err = model.CreateMessageAuditCapture(record)
			require.NoError(t, err)

			detail, err := GetMessageAuditDetail(requestID)
			require.NoError(t, err)
			require.Len(t, detail.Messages, len(test.entries))
			for messageIndex := range detail.Messages {
				assert.Equal(t, test.expectRoles[messageIndex], detail.Messages[messageIndex].Role)
				assert.Equal(t, test.expectTypes[messageIndex], detail.Messages[messageIndex].ContentType)
			}
		})
	}
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
