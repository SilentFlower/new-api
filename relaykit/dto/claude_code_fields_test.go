package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClaudeRequestPreservesClaudeCodeFields 验证抓包中的分类器和会话字段在请求转发时保留。
// @param t 测试上下文。
// @return 无。
func TestClaudeRequestPreservesClaudeCodeFields(t *testing.T) {
	raw := `{
		"model":"claude-opus-5-5",
		"messages":[{"role":"user","content":"test"}],
		"safeguards":[{"type":"dangerous_tool_use","classifier_context":{"permission_mode":"auto","future_field":{"enabled":true}}}],
		"diagnostics":{"previous_message_id":"msg_previous"},
		"fallbacks":"default",
		"thread":{"type":"request","previous_message_id":"msg_thread_previous"}
	}`
	var request ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(raw), &request))

	encoded, err := kitutil.Marshal(request)
	require.NoError(t, err)
	assert.JSONEq(t, raw, string(encoded))
}
