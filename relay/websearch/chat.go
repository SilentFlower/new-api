package websearch

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// IsPureChatWebSearchRequest 判断 Chat Completions 请求是否只需要本地 WebSearch。
//
// @param request 待判定的 Chat Completions 请求
// @return 请求是否满足纯 WebSearch 本地模拟条件
func IsPureChatWebSearchRequest(request *dto.GeneralOpenAIRequest) bool {
	if request == nil || request.WebSearchOptions == nil || len(request.Tools) > 0 {
		return false
	}

	switch common.GetJsonType(request.Functions) {
	case "unknown", "null":
		return true
	case "array":
		var functions []any
		if err := common.Unmarshal(request.Functions, &functions); err != nil {
			return false
		}
		return len(functions) == 0
	default:
		return false
	}
}

// ExtractChatWebSearchQuery 从 Chat Completions 请求最后一条 user 消息提取搜索查询。
//
// @param request Chat Completions 请求
// @return 归一化后的搜索查询；无法提取时返回空字符串
func ExtractChatWebSearchQuery(request *dto.GeneralOpenAIRequest) string {
	if request == nil || len(request.Messages) == 0 {
		return ""
	}
	lastMessage := request.Messages[len(request.Messages)-1]
	if lastMessage.Role != "user" {
		return ""
	}
	if text := strings.TrimSpace(lastMessage.StringContent()); text != "" {
		return text
	}

	rawBytes, err := common.Marshal(lastMessage.Content)
	if err != nil {
		return ""
	}
	var blocks []dto.MediaContent
	if err := common.Unmarshal(rawBytes, &blocks); err != nil {
		return ""
	}
	var builder strings.Builder
	for _, block := range blocks {
		if block.Type == dto.ContentTypeText {
			builder.WriteString(block.Text)
		}
	}
	return strings.TrimSpace(builder.String())
}

// BuildChatWebSearchResponse 构造非流式 Chat Completions WebSearch 模拟响应。
//
// @param responseID 响应 ID
// @param created 响应创建时间戳
// @param modelName 响应模型名
// @param query 搜索查询
// @param results 归一化搜索结果
// @param inputTokens 输入 Token 数
// @param outputTokens 输出 Token 数
// @return Chat Completions 非流式响应
func BuildChatWebSearchResponse(responseID string, created int64, modelName string, query string, results []SearchResult, inputTokens int, outputTokens int) *dto.OpenAITextResponse {
	message := dto.Message{Role: "assistant"}
	message.SetStringContent(BuildTextSummary(query, results))
	usage := BuildChatUsage(inputTokens, outputTokens)
	return &dto.OpenAITextResponse{
		Id:      responseID,
		Model:   modelName,
		Object:  "chat.completion",
		Created: created,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      message,
				FinishReason: "stop",
			},
		},
		Usage: *usage,
	}
}

// BuildChatUsage 构造 Chat Completions 本地 WebSearch 的标准 token usage。
//
// @param inputTokens 输入 Token 数
// @param outputTokens 输出 Token 数
// @return 至少包含一个输入和输出 Token 的 usage
func BuildChatUsage(inputTokens int, outputTokens int) *dto.Usage {
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
	}
}
