package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestChannelLimitFallbackProtocolReferences 区分协议引用与工具定义、结构化输出及业务数据。
// @param t 测试上下文。
func TestChannelLimitFallbackProtocolReferences(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		body     string
		portable bool
	}{
		{"Chat工具参数", `{"tools":[{"type":"function","function":{"name":"render","parameters":{"type":"object","properties":{"prompt":{"type":"string"},"file_id":{"type":"string"}}}}}]}`, true},
		{"Responses工具参数", `{"tools":[{"type":"function","name":"render","parameters":{"type":"object","properties":{"conversation":{"type":"string"}}}}]}`, true},
		{"Claude工具参数", `{"tools":[{"name":"render","input_schema":{"type":"object","properties":{"container":{"type":"string"}}}}]}`, true},
		{"结构化输出", `{"response_format":{"type":"json_schema","json_schema":{"schema":{"properties":{"prompt":{"type":"string"}}}}},"text":{"format":{"type":"json_schema","schema":{"properties":{"file_id":{"type":"string"}}}}}}`, true},
		{"业务元数据", `{"metadata":{"prompt":"用户标签","file_id":"业务编号","type":"compaction"},"client_metadata":{"conversation":"业务编号"}}`, true},
		{"函数调用参数", `{"input":[{"type":"function_call","arguments":"{\"prompt\":\"画图\",\"file_id\":\"业务编号\"}"}]}`, true},
		{"Claude工具调用参数", `{"messages":[{"role":"assistant","content":[{"type":"tool_use","name":"render","input":{"prompt":"画图","file_id":"业务编号"}}]}]}`, true},
		{"普通消息文本", `{"messages":[{"role":"user","content":"解释 previous_response_id"}]}`, true},
		{"空引用", `{"previous_response_id":"","conversation":null}`, true},
		{"真实响应引用", `{"previous_response_id":"resp_1"}`, false},
		{"真实会话引用", `{"conversation":{"id":"conv_1"}}`, false},
		{"服务端提示模板", `{"prompt":{"id":"pmpt_1"}}`, false},
		{"内容项引用", `{"input":[{"type":"item_reference","id":"item_1"}]}`, false},
		{"压缩密文", `{"input":[{"type":"compaction","encrypted_content":"opaque"}]}`, false},
		{"Claude加密思考", `{"messages":[{"content":[{"type":"redacted_thinking","data":"opaque"}]}]}`, false},
		{"Chat文件引用", `{"messages":[{"content":[{"type":"file","file":{"file_id":"file_1"}}]}]}`, false},
		{"Responses文件引用", `{"input":[{"role":"user","content":[{"type":"input_file","file_id":"file_1"}]}]}`, false},
		{"Claude文档引用", `{"messages":[{"content":[{"type":"document","source":{"type":"file","file_id":"file_1"}}]}]}`, false},
		{"工具输出图片引用", `{"input":[{"type":"function_call_output","output":[{"type":"input_image","file_id":"file_1"}]}]}`, false},
		{"Claude工具结果引用", `{"messages":[{"content":[{"type":"tool_result","content":[{"type":"document","source":{"file_id":"file_1"}}]}]}]}`, false},
		{"工具容器引用", `{"tools":[{"type":"code_interpreter","container":"cntr_1"}]}`, false},
		{"工具文件资源", `{"tool_resources":{"code_interpreter":{"file_ids":["file_1"]}}}`, false},
		{"文件检索资源", `{"tools":[{"type":"file_search","vector_store_ids":["vs_1"]}]}`, false},
		{"消息附件", `{"messages":[{"attachments":[{"file_id":"file_1"}]}]}`, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			assert.Equal(t, scenario.portable, ChannelLimitFallbackRequestPortable([]byte(scenario.body)))
		})
	}
}
