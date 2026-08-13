package dto

import relaydto "github.com/QuantumNous/new-api/relaykit/dto"

const (
	// ChannelWebSearchProviderTavily 表示使用 Tavily Search API 作为渠道 WebSearch 供应商。
	ChannelWebSearchProviderTavily = relaydto.ChannelWebSearchProviderTavily
	// ChannelWebSearchProviderAnySearch 表示使用 AnySearch MCP search 工具作为渠道 WebSearch 供应商。
	ChannelWebSearchProviderAnySearch = relaydto.ChannelWebSearchProviderAnySearch
)

// ChannelSettings 为迁移到 relaykit 前的旧包路径保留兼容别名。
type ChannelSettings = relaydto.ChannelSettings

// ChannelVisionAssistSettings 为迁移到 relaykit 前的旧包路径保留兼容别名。
type ChannelVisionAssistSettings = relaydto.ChannelVisionAssistSettings

// ChannelWebSearchSettings 为迁移到 relaykit 前的旧包路径保留兼容别名。
type ChannelWebSearchSettings = relaydto.ChannelWebSearchSettings

// IsChannelWebSearchProviderSupported 判断 provider 是否为当前支持的渠道 WebSearch 供应商。
func IsChannelWebSearchProviderSupported(provider string) bool {
	return relaydto.IsChannelWebSearchProviderSupported(provider)
}
