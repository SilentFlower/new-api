package oaichat

import (
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

const userPromptSubmitContextPrefix = "UserPromptSubmit hook additional context: "

// geminiSystemInstructions 收集 Gemini 的全局指令和 Claude 当轮补充上下文。
type geminiSystemInstructions struct {
	fromClaudeMessages bool
	global             []string
	turnContext        []dto.GeminiPart
}

func (s *geminiSystemInstructions) handleMessage(message *dto.Message, contents []dto.GeminiChatContent) bool {
	if message.Role == "assistant" || message.Role == "model" {
		s.appendTurnContext(contents)
		return false
	}
	if message.Role != "system" && message.Role != "developer" {
		return false
	}
	if !s.fromClaudeMessages {
		s.global = append(s.global, message.StringContent())
		return true
	}

	// Claude 中转后的文本可能存为结构化切片，先合并同一条消息的文本块再判断含义。
	var text strings.Builder
	for _, part := range message.ParseContent() {
		if part.Type == dto.ContentTypeText {
			text.WriteString(part.Text)
		}
	}
	if text.Len() == 0 {
		return true
	}
	value := text.String()
	userTurn := len(contents) > 0 && contents[len(contents)-1].Role == "user"
	if message.Role == "system" && userTurn && strings.HasPrefix(value, userPromptSubmitContextPrefix) {
		// 等同一轮分批到达的工具结果归位，保持回填顺序和已完成历史的缓存前缀。
		s.turnContext = append(s.turnContext, dto.GeminiPart{Text: value})
	} else {
		s.global = append(s.global, value)
	}
	return true
}

func (s *geminiSystemInstructions) appendTurnContext(contents []dto.GeminiChatContent) {
	if len(s.turnContext) == 0 {
		return
	}
	last := &contents[len(contents)-1]
	last.Parts = append(last.Parts, s.turnContext...)
	s.turnContext = nil
}

func (s *geminiSystemInstructions) apply(request *dto.GeminiChatRequest) {
	s.appendTurnContext(request.Contents)
	if len(s.global) > 0 {
		request.SystemInstructions = &dto.GeminiChatContent{
			Parts: []dto.GeminiPart{{Text: strings.Join(s.global, "\n")}},
		}
	}
}
