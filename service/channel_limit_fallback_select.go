package service

import "github.com/QuantumNous/new-api/dto"

// SelectChannelLimitFallback 按"行级优先、策略级兜底"选择超限后的降级目标。
// @param config 当前策略配置，提供策略级默认动作。
// @param sourceChannelID 触发超限的来源渠道；行级同渠道换模型时作为目标渠道。
// @param block 触发拒绝的指标与预算行，可为 nil。
// @return 目标与是否允许降级；拒绝或未配置时返回 false。
func SelectChannelLimitFallback(config dto.ChannelPeriodPolicyConfig, sourceChannelID int, block *ChannelPeriodBlock) (dto.ChannelLimitFallback, bool) {
	action := dto.ChannelBudgetAction{Mode: channelBudgetActionInherit}
	if block != nil && block.row.OnExceed.Mode != "" {
		action = block.row.OnExceed
	}
	if action.Mode == channelBudgetActionInherit {
		action = config.DefaultOnExceed
	}
	if action.Mode != channelBudgetActionFallback {
		return dto.ChannelLimitFallback{}, false
	}
	target := dto.ChannelLimitFallback{Enabled: true, ChannelID: action.ChannelID, Model: action.Model}
	if target.ChannelID == 0 {
		target.ChannelID = sourceChannelID
	}
	return target, true
}
