package relayconvert

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const claudePromptHookText = "UserPromptSubmit hook additional context: 当前轮的工作流状态。"

func convertClaudeMessagesForGeminiCache(t *testing.T, body string) *dto.GeminiChatRequest {
	t.Helper()
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(body), &request))
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatGemini, &request)
	require.NoError(t, err)
	converted, ok := result.Value.(*dto.GeminiChatRequest)
	require.True(t, ok)
	return converted
}

// TestClaudeGeminiKeepsSystemTextBlocks 核对真正的系统文本与空块在同一转换中的处理。
// @param t 测试上下文。
func TestClaudeGeminiKeepsSystemTextBlocks(t *testing.T) {
	converted := convertClaudeMessagesForGeminiCache(t, `{
		"model":"gemini-test", "system":"全局规则", "messages":[
			{"role":"user","content":"查看项目"},
			{"role":"system","content":[{"type":"text","text":"项目"},{"type":"text","text":"环境"}]},
			{"role":"system","content":[{"type":"text","text":""}]}
		]
	}`)
	require.NotNil(t, converted.SystemInstructions)
	require.Len(t, converted.SystemInstructions.Parts, 1)
	assert.Equal(t, "全局规则\n项目环境", converted.SystemInstructions.Parts[0].Text)
}

// TestClaudeGeminiPromptHookPreservesCachePrefixAfterToolResults 比较搜索和普通工具分批回填前后的缓存前缀。
// @param t 测试上下文。
func TestClaudeGeminiPromptHookPreservesCachePrefixAfterToolResults(t *testing.T) {
	const firstTurn = `{"role":"user","content":"查询两份资料"}, {"role":"system","content":"项目环境"}`
	const request = `{
		"model":"gemini-test", "system":"全局规则",
		"tools":[{"name":"WebSearch","input_schema":{"type":"object"}},{"name":"Read","input_schema":{"type":"object"}}],
		"messages":[%s]
	}`
	previous := convertClaudeMessagesForGeminiCache(t, fmt.Sprintf(request, firstTurn))
	require.Len(t, previous.Contents, 1)
	require.NotNil(t, previous.SystemInstructions)
	for _, shape := range []struct {
		name string
		body string
	}{
		{name: "字符串", body: fmt.Sprintf(`%q`, claudePromptHookText)},
		{name: "文本块数组", body: fmt.Sprintf(`[{"type":"text","text":%q}]`, claudePromptHookText)},
		{name: "分块前缀", body: `[{"type":"text","text":"UserPromptSubmit hook "},{"type":"text","text":"additional context: 当前轮的工作流状态。"}]`},
	} {
		t.Run(shape.name, func(t *testing.T) {
			turns := firstTurn + fmt.Sprintf(`,
				{"role":"assistant","content":[
					{"type":"tool_use","id":"search","name":"WebSearch","input":{}},
					{"type":"tool_use","id":"read","name":"Read","input":{}}
				]},
				{"role":"user","content":[{"type":"tool_result","tool_use_id":"search","content":"合成搜索结果"}]},
				{"role":"system","content":%s},
				{"role":"user","content":[{"type":"tool_result","tool_use_id":"read","content":"合成读取结果"}]}`, shape.body)
			converted := convertClaudeMessagesForGeminiCache(t, fmt.Sprintf(request, turns))
			assert.Equal(t, previous.SystemInstructions, converted.SystemInstructions)
			assert.Equal(t, previous.Tools, converted.Tools)
			require.Len(t, converted.Contents, 3)
			assert.Equal(t, previous.Contents, converted.Contents[:len(previous.Contents)])
			last := converted.Contents[2]
			assert.Equal(t, "user", last.Role)
			require.Len(t, last.Parts, 3)
			require.NotNil(t, last.Parts[0].FunctionResponse)
			require.NotNil(t, last.Parts[1].FunctionResponse)
			assert.Equal(t, "WebSearch", last.Parts[0].FunctionResponse.Name)
			assert.Equal(t, "Read", last.Parts[1].FunctionResponse.Name)
			assert.Equal(t, claudePromptHookText, last.Parts[2].Text)

			nextTurn := convertClaudeMessagesForGeminiCache(t, fmt.Sprintf(request, turns+`,
				{"role":"assistant","content":"已读取资料。"},
				{"role":"user","content":"继续。"}`))
			require.Len(t, nextTurn.Contents, 5)
			assert.Equal(t, converted.Contents, nextTurn.Contents[:len(converted.Contents)])
			assert.Equal(t, previous.SystemInstructions, nextTurn.SystemInstructions)
		})
	}
}

// TestGeminiPromptHookConversionScope 核对钩子处理的来源和角色边界。
// @param t 测试上下文。
func TestGeminiPromptHookConversionScope(t *testing.T) {
	t.Run("Claude 顶层系统指令", func(t *testing.T) {
		converted := convertClaudeMessagesForGeminiCache(t, fmt.Sprintf(`{
			"model":"gemini-test","system":%q,"messages":[{"role":"user","content":"测试"}]
		}`, claudePromptHookText))
		require.NotNil(t, converted.SystemInstructions)
		require.Len(t, converted.SystemInstructions.Parts, 1)
		assert.Equal(t, claudePromptHookText, converted.SystemInstructions.Parts[0].Text)
	})
	t.Run("OpenAI Chat 转 Gemini", func(t *testing.T) {
		var request dto.GeneralOpenAIRequest
		require.NoError(t, kitutil.Unmarshal([]byte(fmt.Sprintf(`{
			"model":"gemini-test","messages":[{"role":"user","content":"测试"},{"role":"system","content":%q}]
		}`, claudePromptHookText)), &request))
		result, err := ConvertRequest(context.Background(), nil, types.RelayFormatGemini, &request)
		require.NoError(t, err)
		converted, ok := result.Value.(*dto.GeminiChatRequest)
		require.True(t, ok)
		require.NotNil(t, converted.SystemInstructions)
		assert.Equal(t, claudePromptHookText, converted.SystemInstructions.Parts[0].Text)
		require.Len(t, converted.Contents, 1)
		require.Len(t, converted.Contents[0].Parts, 1)
	})
}

// TestClaudeGeminiReadTruncationNoticeKeepsCachePrefix 核对截断提示随原工具轮传递及其后续表示。
// @param t 测试上下文。
func TestClaudeGeminiReadTruncationNoticeKeepsCachePrefix(t *testing.T) {
	const notice = "[Truncated: /fixture/large.txt. Showing lines 1–200 of 420 total (24000 tokens). Read with offset=201 limit=200 for the next portion.]"
	const first = `{"role":"user","content":"读取资料"},{"role":"system","content":"项目环境"}`
	const request = `{
		"model":"gemini-test","system":"全局规则",
		"tools":[{"name":"Read","input_schema":{"type":"object"}},{"name":"Bash","input_schema":{"type":"object"}}],
		"messages":[%s]
	}`
	const toolTurn = `,
		{"role":"assistant","content":[
			{"type":"tool_use","id":"read","name":%q,"input":{"file_path":"/fixture/large.txt"}},
			{"type":"tool_use","id":"check","name":"Bash","input":{}}
		]},
		{"role":"user","content":[
			{"type":"tool_result","tool_use_id":"read","name":"Read","content":"文件片段"},
			{"type":"tool_result","tool_use_id":"check","content":"检查结果"}
		]},
		{"role":"system","content":%s}`
	arrayNotice := fmt.Sprintf(`[{"type":"text","text":%q,"cache_control":{"type":"ephemeral"}}]`, notice)
	previous := convertClaudeMessagesForGeminiCache(t, fmt.Sprintf(request, first))
	converted := convertClaudeMessagesForGeminiCache(t, fmt.Sprintf(request, first+fmt.Sprintf(toolTurn, "Read", arrayNotice)))
	assert.Equal(t, previous.SystemInstructions, converted.SystemInstructions)
	assert.Equal(t, previous.Tools, converted.Tools)
	require.Len(t, converted.Contents, 3)
	assert.Equal(t, previous.Contents, converted.Contents[:1])
	parts := converted.Contents[2].Parts
	require.Len(t, parts, 3)
	require.NotNil(t, parts[0].FunctionResponse)
	require.NotNil(t, parts[1].FunctionResponse)
	assert.Equal(t, "Read", parts[0].FunctionResponse.Name)
	assert.Equal(t, map[string]interface{}{"content": "文件片段"}, parts[0].FunctionResponse.Response)
	assert.Equal(t, "Bash", parts[1].FunctionResponse.Name)
	assert.Equal(t, notice, parts[2].Text)

	history := first + fmt.Sprintf(toolTurn, "Read", fmt.Sprintf(`%q`, notice)) + `,
		{"role":"assistant","content":"已读取。"},{"role":"user","content":"继续读取。"}`
	next := convertClaudeMessagesForGeminiCache(t, fmt.Sprintf(request, history))
	require.Len(t, next.Contents, 5)
	assert.Equal(t, converted.Contents, next.Contents[:len(converted.Contents)])
	assert.Equal(t, previous.SystemInstructions, next.SystemInstructions)

	unrelated := convertClaudeMessagesForGeminiCache(t, fmt.Sprintf(request, first+fmt.Sprintf(toolTurn, "Bash", arrayNotice)))
	require.NotNil(t, unrelated.SystemInstructions)
	assert.Equal(t, "全局规则\n项目环境\n"+notice, unrelated.SystemInstructions.Parts[0].Text)
	assert.Len(t, unrelated.Contents[2].Parts, 2)
}
