package websearch

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

const (
	claudeWebSearchToolName = "web_search"
	tokenEstimateDivisor    = 4
)

// IsPureClaudeWebSearchRequest 判断 Claude 请求是否只包含一个 WebSearch 工具。
func IsPureClaudeWebSearchRequest(request *dto.ClaudeRequest) bool {
	if request == nil {
		return false
	}
	tools := claudeToolsAsSlice(request.Tools)
	return len(tools) == 1 && isClaudeWebSearchTool(tools[0])
}

// ExtractClaudeWebSearchQuery 从 Claude 请求最后一条 user 消息提取搜索查询。
func ExtractClaudeWebSearchQuery(request *dto.ClaudeRequest) string {
	if request == nil || len(request.Messages) == 0 {
		return ""
	}
	lastMessage := request.Messages[len(request.Messages)-1]
	if lastMessage.Role != "user" {
		return ""
	}
	return strings.TrimSpace(extractClaudeMessageText(lastMessage))
}

// BuildClaudeWebSearchResponse 构造非流式 Claude Messages WebSearch 模拟响应。
func BuildClaudeWebSearchResponse(messageID string, toolUseID string, modelName string, query string, results []SearchResult, inputTokens int, outputTokens int) *dto.ClaudeResponse {
	textSummary := BuildTextSummary(query, results)
	return &dto.ClaudeResponse{
		Id:         messageID,
		Type:       "message",
		Role:       "assistant",
		Model:      modelName,
		StopReason: "end_turn",
		Content: []dto.ClaudeMediaMessage{
			{
				Type:  "server_tool_use",
				Id:    toolUseID,
				Name:  claudeWebSearchToolName,
				Input: map[string]string{"query": query},
			},
			{
				Type:      "web_search_tool_result",
				ToolUseId: toolUseID,
				Content:   buildClaudeSearchResultBlocks(results),
			},
			{
				Type: dto.ContentTypeText,
				Text: common.GetPointer(textSummary),
			},
		},
		Usage: BuildClaudeUsage(inputTokens, outputTokens),
	}
}

// BuildClaudeWebSearchStreamEvents 构造 Claude SSE 所需的 WebSearch 模拟事件序列。
func BuildClaudeWebSearchStreamEvents(messageID string, toolUseID string, modelName string, query string, results []SearchResult, inputTokens int, outputTokens int) []*dto.ClaudeResponse {
	textSummary := BuildTextSummary(query, results)
	message := &dto.ClaudeMediaMessage{
		Id:    messageID,
		Type:  "message",
		Role:  "assistant",
		Model: modelName,
		Usage: &dto.ClaudeUsage{InputTokens: inputTokens, OutputTokens: 0},
	}
	message.SetContent([]any{})
	index0 := 0
	index1 := 1
	index2 := 2
	stopReason := "end_turn"
	return []*dto.ClaudeResponse{
		{Type: "message_start", Message: message},
		{
			Type:  "content_block_start",
			Index: &index0,
			ContentBlock: &dto.ClaudeMediaMessage{
				Type:  "server_tool_use",
				Id:    toolUseID,
				Name:  claudeWebSearchToolName,
				Input: map[string]string{"query": query},
			},
		},
		{Type: "content_block_stop", Index: &index0},
		{
			Type:  "content_block_start",
			Index: &index1,
			ContentBlock: &dto.ClaudeMediaMessage{
				Type:      "web_search_tool_result",
				ToolUseId: toolUseID,
				Content:   buildClaudeSearchResultBlocks(results),
			},
		},
		{Type: "content_block_stop", Index: &index1},
		{
			Type:  "content_block_start",
			Index: &index2,
			ContentBlock: &dto.ClaudeMediaMessage{
				Type: dto.ContentTypeText,
				Text: common.GetPointer(""),
			},
		},
		{
			Type:  "content_block_delta",
			Index: &index2,
			Delta: &dto.ClaudeMediaMessage{
				Type: dto.ContentTypeText + "_delta",
				Text: common.GetPointer(textSummary),
			},
		},
		{Type: "content_block_stop", Index: &index2},
		{
			Type:  "message_delta",
			Usage: BuildClaudeUsage(inputTokens, outputTokens),
			Delta: &dto.ClaudeMediaMessage{
				StopReason: &stopReason,
			},
		},
		{Type: "message_stop"},
	}
}

// BuildClaudeUsage 构造包含 server_tool_use.web_search_requests 的 Claude usage。
func BuildClaudeUsage(inputTokens int, outputTokens int) *dto.ClaudeUsage {
	if inputTokens <= 0 {
		inputTokens = 1
	}
	if outputTokens <= 0 {
		outputTokens = 1
	}
	return &dto.ClaudeUsage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		ServerToolUse: &dto.ClaudeServerToolUse{
			WebSearchRequests: 1,
		},
	}
}

// BuildUsage 构造本地 WebSearch 模拟响应参与结算所需的内部 usage。
func BuildUsage(inputTokens int, outputTokens int) *dto.Usage {
	if inputTokens <= 0 {
		inputTokens = 1
	}
	if outputTokens <= 0 {
		outputTokens = 1
	}
	return &dto.Usage{
		PromptTokens:     inputTokens,
		CompletionTokens: outputTokens,
		TotalTokens:      inputTokens + outputTokens,
		UsageSemantic:    "anthropic",
	}
}

// BuildTextSummary 构造给 Claude Code 展示的稳定搜索结果摘要。
func BuildTextSummary(query string, results []SearchResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("No search results found for: %s", query)
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "Here are the search results for %q:\n\n", query)
	for index, result := range results {
		title := strings.TrimSpace(result.Title)
		if title == "" {
			title = strings.TrimSpace(result.URL)
		}
		if title == "" {
			title = fmt.Sprintf("Result %d", index+1)
		}
		fmt.Fprintf(&builder, "%d. %s\n", index+1, title)
		if result.URL != "" {
			fmt.Fprintf(&builder, "   %s\n", result.URL)
		}
		if result.Snippet != "" {
			fmt.Fprintf(&builder, "   %s\n", result.Snippet)
		}
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

// EstimateOutputTokens 根据响应文本长度给本地模拟响应生成非零 token 估算。
func EstimateOutputTokens(query string, results []SearchResult) int {
	estimated := len([]rune(BuildTextSummary(query, results))) / tokenEstimateDivisor
	if estimated <= 0 {
		return 1
	}
	return estimated
}

func claudeToolsAsSlice(tools any) []any {
	switch typed := tools.(type) {
	case nil:
		return nil
	case []any:
		return typed
	case []dto.ClaudeWebSearchTool:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	case []*dto.ClaudeWebSearchTool:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	default:
		rawBytes, err := common.Marshal(typed)
		if err != nil {
			return nil
		}
		var result []any
		if err := common.Unmarshal(rawBytes, &result); err != nil {
			return nil
		}
		return result
	}
}

func isClaudeWebSearchTool(tool any) bool {
	var toolType string
	var toolName string
	switch typed := tool.(type) {
	case dto.ClaudeWebSearchTool:
		toolType = typed.Type
		toolName = typed.Name
	case *dto.ClaudeWebSearchTool:
		if typed == nil {
			return false
		}
		toolType = typed.Type
		toolName = typed.Name
	case map[string]any:
		toolType = firstStringValue(typed, "type")
		toolName = firstStringValue(typed, "name")
	default:
		rawBytes, err := common.Marshal(typed)
		if err != nil {
			return false
		}
		var raw map[string]any
		if err := common.Unmarshal(rawBytes, &raw); err != nil {
			return false
		}
		toolType = firstStringValue(raw, "type")
		toolName = firstStringValue(raw, "name")
	}
	return isClaudeWebSearchToolName(toolType) || isClaudeWebSearchToolName(toolName)
}

func isClaudeWebSearchToolName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "web_search" || name == "google_search" || strings.HasPrefix(name, "web_search_")
}

func extractClaudeMessageText(message dto.ClaudeMessage) string {
	if text := message.GetStringContent(); strings.TrimSpace(text) != "" {
		return text
	}
	var blocks []dto.ClaudeMediaMessage
	rawBytes, err := common.Marshal(message.Content)
	if err != nil {
		return ""
	}
	if err := common.Unmarshal(rawBytes, &blocks); err != nil {
		return ""
	}
	var builder strings.Builder
	for _, block := range blocks {
		if block.Type != dto.ContentTypeText {
			continue
		}
		builder.WriteString(block.GetText())
	}
	return builder.String()
}

func buildClaudeSearchResultBlocks(results []SearchResult) []map[string]string {
	blocks := make([]map[string]string, 0, len(results))
	for _, result := range results {
		block := map[string]string{
			"type":  "web_search_result",
			"url":   result.URL,
			"title": result.Title,
		}
		if result.Snippet != "" {
			block["page_content"] = result.Snippet
		}
		if result.PageAge != "" {
			block["page_age"] = result.PageAge
		}
		blocks = append(blocks, block)
	}
	return blocks
}
