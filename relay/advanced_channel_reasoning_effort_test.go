package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// TestAdvancedChannelReasoningEffortFields 验证明确值、缺失及协议隔离。
// @param t 测试上下文。
func TestAdvancedChannelReasoningEffortFields(t *testing.T) {
	tests := []struct {
		name   string
		format types.RelayFormat
		body   string
		want   string
	}{
		{"DeepSeek low", types.RelayFormatOpenAI, `{"reasoning_effort":"low"}`, "low"},
		{"Qwen high", types.RelayFormatOpenAI, `{"reasoning_effort":"high"}`, "high"},
		{"嵌套 Chat effort", types.RelayFormatOpenAI, `{"reasoning":{"effort":"medium"}}`, "medium"},
		{"顶层优先", types.RelayFormatOpenAI, `{"reasoning_effort":"low","reasoning":{"effort":"high"}}`, "low"},
		{"空顶层兼容嵌套", types.RelayFormatOpenAI, `{"reasoning_effort":"","reasoning":{"effort":"high"}}`, "high"},
		{"Responses", types.RelayFormatOpenAIResponses, `{"reasoning":{"effort":"xhigh"}}`, "xhigh"},
		{"Claude", types.RelayFormatClaude, `{"output_config":{"effort":"max"}}`, "max"},
		{"Gemini", types.RelayFormatGemini, `{"generationConfig":{"thinkingConfig":{"thinkingLevel":"HIGH"}}}`, "HIGH"},
		{"Gemini 蛇形", types.RelayFormatGemini, `{"generationConfig":{"thinking_config":{"thinking_level":"low"}}}`, "low"},
		{"Gemini null 别名不覆盖", types.RelayFormatGemini, `{"generationConfig":{"thinkingConfig":{"thinkingLevel":"high"},"thinking_config":null}}`, "high"},
		{"空值", types.RelayFormatOpenAI, `{"reasoning_effort":""}`, ""},
		{"非字符串", types.RelayFormatOpenAI, `{"reasoning_effort":42}`, ""},
		{"null", types.RelayFormatOpenAI, `{"reasoning_effort":null}`, ""},
		{"缺失", types.RelayFormatOpenAI, `{}`, ""},
		{"预算不推断", types.RelayFormatClaude, `{"thinking":{"budget_tokens":4096}}`, ""},
		{"开关不推断", types.RelayFormatOpenAI, `{"enable_thinking":false}`, ""},
		{"最终协议优先", types.RelayFormatClaude, `{"reasoning_effort":"high","output_config":{"effort":"low"}}`, "low"},
		{"未知协议", types.RelayFormatEmbedding, `{"reasoning_effort":"low"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				RelayFormat: types.RelayFormatOpenAI, FinalRequestRelayFormat: tt.format,
				ReasoningEffort: "旧渠道值",
				ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeAdvancedCustom},
			}
			body := []byte(tt.body)
			syncAdvancedChannelReasoningEffortFromBody(info, body)
			assert.Equal(t, tt.want, info.ReasoningEffort)
			assert.Equal(t, tt.body, string(body))
		})
	}
	for _, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeDeepSeek, constant.ChannelTypeOpenRouter, constant.ChannelTypeAnthropic} {
		info := &relaycommon.RelayInfo{
			ReasoningEffort: "已有值", RelayFormat: types.RelayFormatOpenAI,
			ChannelMeta: &relaycommon.ChannelMeta{ChannelType: channelType},
		}
		syncAdvancedChannelReasoningEffortFromBody(info, []byte(`{"reasoning_effort":"low"}`))
		syncAdvancedChannelReasoningEffortFromRequest(info, &dto.GeneralOpenAIRequest{ReasoningEffort: "high"})
		assert.Equal(t, "已有值", info.ReasoningEffort)
	}
	syncAdvancedChannelReasoningEffortFromBody(nil, nil)
	syncAdvancedChannelReasoningEffortFromRequest(nil, nil)
}

// TestAdvancedChannelReasoningEffortHandlers 通过真实 handler 和模拟上游验证最终参数及消费元数据。
// @param t 测试上下文。
func TestAdvancedChannelReasoningEffortHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := model_setting.GetGlobalSettings()
	originalPassThrough := settings.PassThroughRequestEnabled
	settings.PassThroughRequestEnabled = false
	t.Cleanup(func() { settings.PassThroughRequestEnabled = originalPassThrough })

	tests := []struct {
		name      string
		format    types.RelayFormat
		path      string
		mode      int
		body      string
		field     string
		override  string
		converter string
		handler   func(*gin.Context, *relaycommon.RelayInfo) *types.NewAPIError
	}{
		{"Chat", types.RelayFormatOpenAI, "/v1/chat/completions", relayconstant.RelayModeChatCompletions,
			`{"model":"deepseek-flash","messages":[{"role":"user","content":"测试"}],"reasoning_effort":"low"}`, "reasoning_effort", "", relayconvert.ConverterNone, TextHelper},
		{"Responses", types.RelayFormatOpenAIResponses, "/v1/responses", relayconstant.RelayModeResponses,
			`{"model":"deepseek-flash","input":"测试","reasoning":{"effort":"low"}}`, "reasoning.effort", "", relayconvert.ConverterNone, ResponsesHelper},
		{"Claude", types.RelayFormatClaude, "/v1/messages", relayconstant.RelayModeUnknown,
			`{"model":"deepseek-flash","max_tokens":100,"messages":[{"role":"user","content":"测试"}],"output_config":{"effort":"low"}}`, "output_config.effort", "", relayconvert.ConverterNone, ClaudeHelper},
		{"Gemini", types.RelayFormatGemini, "/v1beta/models/deepseek-flash:generateContent", relayconstant.RelayModeGemini,
			`{"contents":[{"role":"user","parts":[{"text":"测试"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"low"}}}`, "generationConfig.thinkingConfig.thinkingLevel", "", relayconvert.ConverterNone, GeminiHelper},
		{"Chat 转 Responses", types.RelayFormatOpenAI, "/v1/chat/completions", relayconstant.RelayModeChatCompletions,
			`{"model":"deepseek-flash","messages":[{"role":"user","content":"测试"}],"reasoning_effort":"low"}`, "reasoning.effort", "", relayconvert.ConverterOpenAIChatToOpenAIResponses, TextHelper},
		{"Chat 转 Claude", types.RelayFormatOpenAI, "/v1/chat/completions", relayconstant.RelayModeChatCompletions,
			`{"model":"deepseek-flash","messages":[{"role":"user","content":"测试"}],"reasoning_effort":"low"}`, "output_config.effort", "", relayconvert.ConverterOpenAIChatToClaudeMessages, TextHelper},
		{"Chat 转 Gemini", types.RelayFormatOpenAI, "/v1/chat/completions", relayconstant.RelayModeChatCompletions,
			`{"model":"deepseek-flash","messages":[{"role":"user","content":"测试"}],"reasoning_effort":"low"}`, "generationConfig.thinkingConfig.thinkingLevel", "", relayconvert.ConverterOpenAIChatToGeminiContent, TextHelper},
		{"Responses 转 Chat", types.RelayFormatOpenAIResponses, "/v1/responses", relayconstant.RelayModeResponses,
			`{"model":"deepseek-flash","input":"测试","reasoning":{"effort":"low"}}`, "reasoning_effort", "", relayconvert.ConverterOpenAIResponsesToOpenAIChat, ResponsesHelper},
		{"Claude 转 Chat", types.RelayFormatClaude, "/v1/messages", relayconstant.RelayModeUnknown,
			`{"model":"deepseek-flash","max_tokens":100,"messages":[{"role":"user","content":"测试"}],"output_config":{"effort":"low"}}`, "reasoning_effort", "", relayconvert.ConverterClaudeMessagesToOpenAIChat, ClaudeHelper},
		{"Gemini 转 Chat", types.RelayFormatGemini, "/v1beta/models/deepseek-flash:generateContent", relayconstant.RelayModeGemini,
			`{"contents":[{"role":"user","parts":[{"text":"测试"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"low"}}}`, "reasoning_effort", "", relayconvert.ConverterGeminiContentToOpenAIChat, GeminiHelper},
		{"Qwen", types.RelayFormatOpenAI, "/v1/chat/completions", relayconstant.RelayModeChatCompletions,
			`{"model":"qwen3.8-max","messages":[{"role":"user","content":"测试"}],"reasoning_effort":"low"}`, "reasoning_effort", "", relayconvert.ConverterNone, TextHelper},
		{"Chat 经 Responses 桥接", types.RelayFormatOpenAI, "/v1/chat/completions", relayconstant.RelayModeChatCompletions,
			`{"model":"deepseek-flash","messages":[{"role":"user","content":"测试"}],"reasoning_effort":"low"}`, "reasoning.effort", "reasoning_effort", relayconvert.ConverterNone,
			func(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
				info.InitChannelMeta(c)
				adaptor := GetAdaptor(info.ApiType)
				adaptor.Init(info)
				_, apiErr := chatCompletionsViaResponses(c, info, adaptor, info.Request.(*dto.GeneralOpenAIRequest))
				return apiErr
			}},
	}
	for _, tt := range tests {
		for _, stream := range []bool{false, true} {
			for _, mode := range []string{"original", "set", "delete", "passthrough"} {
				if mode == "passthrough" && tt.override != "" {
					continue
				}
				if mode == "original" && tt.converter != relayconvert.ConverterNone {
					continue
				}
				if mode == "passthrough" && tt.converter != relayconvert.ConverterNone {
					continue
				}
				t.Run(tt.name+"/"+mode+"/"+map[bool]string{false: "同步", true: "流式"}[stream], func(t *testing.T) {
					inputBody := []byte(tt.body)
					if tt.format != types.RelayFormatGemini {
						var err error
						inputBody, err = sjson.SetBytes(inputBody, "stream", stream)
						require.NoError(t, err)
					}
					captured := make(chan []byte, 1)
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, err := io.ReadAll(r.Body)
						if err != nil {
							w.WriteHeader(http.StatusInternalServerError)
							return
						}
						captured <- body
						// 在响应阶段终止，隔离真实账务写入；消费元数据由现有生成器单独核对。
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusBadRequest)
						_, _ = io.WriteString(w, `{"error":{"message":"模拟上游停止","type":"invalid_request_error"}}`)
					}))
					t.Cleanup(upstream.Close)
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(string(inputBody)))
					c.Request.Header.Set("Content-Type", "application/json")
					t.Cleanup(func() { common.CleanupBodyStorage(c) })
					common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeAdvancedCustom)
					common.SetContextKey(c, constant.ContextKeyChannelId, 91)
					common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
					common.SetContextKey(c, constant.ContextKeyChannelKey, "本机测试凭据")
					common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
						AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
							IncomingPath: tt.path, UpstreamPath: "/mock", Converter: tt.converter,
						}}},
					})
					common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: mode == "passthrough"})
					if mode != "original" {
						overridePath := tt.field
						if tt.override != "" {
							overridePath = tt.override
						}
						common.SetContextKey(c, constant.ContextKeyChannelParamOverride, map[string]interface{}{
							"operations": []interface{}{map[string]interface{}{"path": overridePath, "mode": map[bool]string{true: "delete", false: "set"}[mode == "delete"], "value": "high"}},
						})
					}
					var request dto.Request
					switch tt.format {
					case types.RelayFormatOpenAI:
						request = &dto.GeneralOpenAIRequest{}
					case types.RelayFormatOpenAIResponses:
						request = &dto.OpenAIResponsesRequest{}
					case types.RelayFormatClaude:
						request = &dto.ClaudeRequest{}
					case types.RelayFormatGemini:
						request = &dto.GeminiChatRequest{}
					}
					require.NoError(t, common.Unmarshal(inputBody, request))
					info := &relaycommon.RelayInfo{
						Request: request, RelayFormat: tt.format, RelayMode: tt.mode, RequestURLPath: tt.path,
						OriginModelName: "deepseek-flash", IsStream: stream, ReasoningEffort: "旧渠道值",
					}
					if model := gjson.GetBytes(inputBody, "model").String(); model != "" {
						info.OriginModelName = model
					}
					info.InitRequestConversionChain()
					info.FreezeBillingModelName("冻结计费模型")
					apiErr := tt.handler(c, info)
					require.NotNil(t, apiErr)
					require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
					var body []byte
					select {
					case body = <-captured:
					default:
						t.Fatal("请求未到达模拟上游")
					}
					want := "high"
					if mode == "delete" {
						want = ""
						assert.False(t, gjson.GetBytes(body, tt.field).Exists())
					} else if mode == "passthrough" || mode == "original" {
						want = "low"
						assert.Equal(t, want, gjson.GetBytes(body, tt.field).String())
						if mode == "passthrough" {
							assert.Equal(t, string(inputBody), string(body))
						}
					} else {
						assert.Equal(t, want, gjson.GetBytes(body, tt.field).String())
					}
					assert.Equal(t, want, info.ReasoningEffort)
					assert.Equal(t, "冻结计费模型", info.FrozenBillingModelName())
					other := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 0, 1)
					if want == "" {
						assert.NotContains(t, other, "reasoning_effort")
					} else {
						assert.Equal(t, want, other["reasoning_effort"])
					}
				})
			}
		}
	}
}
