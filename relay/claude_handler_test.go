package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
)

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
			name:        "官方 Anthropic base URL 尾部斜杠仍视为官方上游",
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
			name:        "Anthropic 兼容自定义上游未启用本地模拟时继续拦截",
			channelType: constant.ChannelTypeAnthropic,
			baseURL:     "https://anthropic-proxy.example.com",
			enabled:     false,
			want:        true,
		},
		{
			name:        "非原生渠道未启用本地模拟时继续拦截",
			channelType: constant.ChannelTypeDeepSeek,
			baseURL:     "https://api.deepseek.com",
			enabled:     false,
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
	assert.True(t, shouldHandleClaudeWebSearchEmulation(nil))
	assert.True(t, shouldHandleClaudeWebSearchEmulation(&relaycommon.RelayInfo{}))
}
