package common

// ChannelLimitFallbackInfo 保存请求唯一一次额度降级的审计事实。
type ChannelLimitFallbackInfo struct {
	SourceChannelID int    `json:"source_channel_id"`
	TargetChannelID int    `json:"target_channel_id"`
	OriginalModel   string `json:"original_model"`
	TargetModel     string `json:"target_model"`
	Scope           string `json:"scope"`
	Period          string `json:"period"`
	RuleID          string `json:"rule_id,omitempty"`
}

// RoutingModel 返回当前候选应映射的模型，原始模型仅保留客户端语义。
// @return 降级目标模型或原始请求模型。
func (info *RelayInfo) RoutingModel() string {
	if info == nil {
		return ""
	}
	if info.RoutingModelName != "" {
		return info.RoutingModelName
	}
	return info.OriginModelName
}
