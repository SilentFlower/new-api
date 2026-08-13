package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

const claudeMessageAuditBillingHeaderPrefix = "x-anthropic-billing-header:"

func messageAuditSessionFingerprintEntries(request dto.Request, entries []messageAuditPlaintext) []messageAuditPlaintext {
	if _, ok := request.(*dto.ClaudeRequest); !ok || len(entries) == 0 {
		return entries
	}

	fingerprintEntries := make([]messageAuditPlaintext, 0, len(entries))
	for _, entry := range entries {
		normalizeBillingHeader := entry.Role == "system" && entry.ContentType == "system"
		content := normalizeClaudeMessageAuditFingerprintValue(entry.Content, normalizeBillingHeader, false)
		switch entry.ContentType {
		case "system":
			content = normalizeClaudeMessageAuditTextContent(content)
		case "message":
			if message, ok := content.(map[string]any); ok {
				if messageContent, exists := message["content"]; exists {
					message["content"] = normalizeClaudeMessageAuditTextContent(messageContent)
				}
			}
		}
		fingerprintEntries = append(fingerprintEntries, messageAuditPlaintext{
			Role:        entry.Role,
			ContentType: entry.ContentType,
			Content:     content,
		})
	}
	return fingerprintEntries
}

func normalizeClaudeMessageAuditFingerprintValue(value any, normalizeBillingHeader bool, preserveCacheControl bool) any {
	switch typed := value.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		itemType := strings.ToLower(common.Interface2String(typed["type"]))
		for key, child := range typed {
			if key == "cache_control" && !preserveCacheControl {
				continue
			}
			// Tool input 是用户可见语义载荷，其中同名字段不能按 Claude 协议元数据删除。
			childPreservesCacheControl := preserveCacheControl || (itemType == "tool_use" && key == "input")
			normalized[key] = normalizeClaudeMessageAuditFingerprintValue(child, normalizeBillingHeader, childPreservesCacheControl)
		}
		return normalized
	case []any:
		normalized := make([]any, 0, len(typed))
		for _, child := range typed {
			normalized = append(normalized, normalizeClaudeMessageAuditFingerprintValue(child, normalizeBillingHeader, preserveCacheControl))
		}
		return normalized
	case string:
		if normalizeBillingHeader {
			return normalizeClaudeMessageAuditBillingHeader(typed)
		}
		return typed
	default:
		return typed
	}
}

func normalizeClaudeMessageAuditTextContent(value any) any {
	blocks, ok := value.([]any)
	if !ok || len(blocks) != 1 {
		return value
	}
	block, ok := blocks[0].(map[string]any)
	if !ok || len(block) != 2 || common.Interface2String(block["type"]) != dto.ContentTypeText {
		return value
	}
	text, ok := block["text"].(string)
	if !ok {
		return value
	}
	return text
}

func normalizeClaudeMessageAuditBillingHeader(value string) string {
	if !strings.HasPrefix(value, claudeMessageAuditBillingHeaderPrefix) {
		return value
	}
	headerEnd := strings.IndexByte(value, '\n')
	header := value
	suffix := ""
	if headerEnd >= 0 {
		header = value[:headerEnd]
		suffix = value[headerEnd:]
	}
	headerParameters := header[len(claudeMessageAuditBillingHeaderPrefix):]
	parts := strings.Split(headerParameters, ";")
	normalized := make([]string, 0, len(parts))
	removed := false
	for _, part := range parts {
		if strings.HasPrefix(strings.TrimSpace(part), "cch=") {
			removed = true
			continue
		}
		normalized = append(normalized, part)
	}
	if !removed {
		return value
	}
	return claudeMessageAuditBillingHeaderPrefix + strings.Join(normalized, ";") + suffix
}

func (manager *messageAuditManager) buildClaudeMessageAuditSessionFingerprints(userID int, protocol string, fingerprintEntries []messageAuditPlaintext, storedEntries []messageAuditPlaintext) ([]string, []string, string) {
	prefixes := make([]string, 0, len(fingerprintEntries))
	anchors := make([]string, 0, len(storedEntries))
	previous := ""
	for index, entry := range fingerprintEntries {
		plaintext, err := common.Marshal(entry)
		if err != nil {
			continue
		}
		fingerprintBlob := model.MessageAuditStoredBlob{
			SchemaVersion: messageAuditSchemaVersion,
			ContentHMAC:   manager.contentHMAC(userID, messageAuditSchemaVersion, plaintext),
			ContentType:   entry.ContentType,
			Role:          entry.Role,
		}
		if !isMessageAuditConversationBlob(fingerprintBlob) {
			continue
		}
		previous = manager.nextMessageAuditSessionFingerprint(userID, protocol, previous, fingerprintBlob)
		prefixes = append(prefixes, previous)
		if !isMessageAuditSessionAnchor(fingerprintBlob) || index >= len(storedEntries) {
			continue
		}

		// exact/prefix 使用语义 HMAC，compressed 必须使用真实 Blob HMAC 反查候选。
		storedEntry := storedEntries[index]
		storedPlaintext, err := common.Marshal(storedEntry)
		if err != nil {
			continue
		}
		storedBlob := model.MessageAuditStoredBlob{
			SchemaVersion: messageAuditSchemaVersion,
			ContentHMAC:   manager.contentHMAC(userID, messageAuditSchemaVersion, storedPlaintext),
			ContentType:   storedEntry.ContentType,
			Role:          storedEntry.Role,
		}
		if isMessageAuditSessionAnchor(storedBlob) {
			anchors = append(anchors, storedBlob.ContentHMAC)
		}
	}
	return prefixes, anchors, previous
}
