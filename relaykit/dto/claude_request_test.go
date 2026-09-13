package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClaudeRequestPreservesMessageOutputConfig 验证消息级输出配置在请求转发时保留。
// @param t 测试上下文。
// @return 无。
func TestClaudeRequestPreservesMessageOutputConfig(t *testing.T) {
	tests := []struct {
		name     string
		messages string
	}{
		{
			name: "压缩后空内容的思考强度消息",
			messages: `[
				{"role":"user","content":"压缩摘要"},
				{"role":"system","content":[],"output_config":{"effort":"high"}}
			]`,
		},
		{
			name: "文本和思考强度共存",
			messages: `[
				{"role":"user","content":"继续"},
				{"role":"system","content":[{"type":"text","text":"继续检查"}],"output_config":{"effort":"max"}}
			]`,
		},
		{
			name:     "普通消息不新增输出配置",
			messages: `[{"role":"user","content":"你好"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 顶层与消息级 effort 独立生效，转发不能丢失或互相覆盖。
			raw := `{"model":"claude-fable-5-1","output_config":{"effort":"low"},"messages":` + tt.messages + `}`
			var request ClaudeRequest
			require.NoError(t, kitutil.Unmarshal([]byte(raw), &request))

			encoded, err := kitutil.Marshal(request)
			require.NoError(t, err)
			assert.JSONEq(t, raw, string(encoded))
		})
	}
}
