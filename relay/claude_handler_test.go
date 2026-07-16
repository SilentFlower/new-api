package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncAnthropicReasoningEffort(t *testing.T) {
	tests := []struct {
		name          string
		channelType   int
		initialEffort string
		outputConfig  string
		want          string
	}{
		{
			name:         "记录 Anthropic output_config effort",
			channelType:  constant.ChannelTypeAnthropic,
			outputConfig: `{"effort":"high"}`,
			want:         "high",
		},
		{
			name:          "缺失 effort 时清空旧值",
			channelType:   constant.ChannelTypeAnthropic,
			initialEffort: "high",
			outputConfig:  `{}`,
			want:          "",
		},
		{
			name:          "不修改非 Anthropic 渠道",
			channelType:   constant.ChannelTypeOpenAI,
			initialEffort: "medium",
			outputConfig:  `{"effort":"xhigh"}`,
			want:          "medium",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				ReasoningEffort: tt.initialEffort,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType: tt.channelType,
				},
			}

			syncAnthropicReasoningEffort(info, []byte(tt.outputConfig))

			assert.Equal(t, tt.want, info.ReasoningEffort)
		})
	}
}

func TestSyncAnthropicReasoningEffortUsesParamOverrideResult(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeAnthropic,
			ParamOverride: map[string]interface{}{
				"operations": []interface{}{
					map[string]interface{}{
						"path":  "output_config.effort",
						"mode":  "set",
						"value": "xhigh",
						"conditions": []interface{}{
							map[string]interface{}{
								"path":  "output_config.effort",
								"mode":  "full",
								"value": "max",
							},
						},
					},
				},
			},
		},
	}

	requestBody, err := relaycommon.ApplyParamOverrideWithRelayInfo(
		[]byte(`{"output_config":{"effort":"max"}}`),
		info,
	)
	require.NoError(t, err)

	syncAnthropicReasoningEffortFromRequestBody(info, requestBody)

	assert.Equal(t, "xhigh", info.ReasoningEffort)
}

func TestShouldHandleClaudeWebSearchEmulation(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		baseURL     string
		enabled     bool
		want        bool
	}{
		{
			name:        "官方 Anthropic 未启用本地模拟时透传原生 WebSearch",
			channelType: constant.ChannelTypeAnthropic,
			baseURL:     constant.ChannelBaseURLs[constant.ChannelTypeAnthropic],
			enabled:     false,
			want:        false,
		},
		{
			name:        "官方 Anthropic base URL 尾部斜杠未启用本地模拟时透传",
			channelType: constant.ChannelTypeAnthropic,
			baseURL:     constant.ChannelBaseURLs[constant.ChannelTypeAnthropic] + "/",
			enabled:     false,
			want:        false,
		},
		{
			name:        "官方 Anthropic 启用本地模拟时仍短路",
			channelType: constant.ChannelTypeAnthropic,
			baseURL:     constant.ChannelBaseURLs[constant.ChannelTypeAnthropic],
			enabled:     true,
			want:        true,
		},
		{
			name:        "Anthropic 兼容自定义上游未启用本地模拟时透传",
			channelType: constant.ChannelTypeAnthropic,
			baseURL:     "https://anthropic-proxy.example.com",
			enabled:     false,
			want:        false,
		},
		{
			name:        "非原生渠道未启用本地模拟时也透传",
			channelType: constant.ChannelTypeDeepSeek,
			baseURL:     "https://api.deepseek.com",
			enabled:     false,
			want:        false,
		},
		{
			name:        "非原生渠道启用本地模拟时短路",
			channelType: constant.ChannelTypeDeepSeek,
			baseURL:     "https://api.deepseek.com",
			enabled:     true,
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:    tt.channelType,
					ChannelBaseUrl: tt.baseURL,
					ChannelSetting: dto.ChannelSettings{
						WebSearch: dto.ChannelWebSearchSettings{
							Enabled: tt.enabled,
						},
					},
				},
			}

			assert.Equal(t, tt.want, shouldHandleClaudeWebSearchEmulation(info))
		})
	}
}

func TestShouldHandleClaudeWebSearchEmulationDefaultsToLocalGuard(t *testing.T) {
	assert.False(t, shouldHandleClaudeWebSearchEmulation(nil))
	assert.False(t, shouldHandleClaudeWebSearchEmulation(&relaycommon.RelayInfo{}))
}
