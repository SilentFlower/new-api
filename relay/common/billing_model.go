package common

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// ShouldUseUpstreamModelForBilling 判断当前请求是否应按最终上游模型计费。
// 仅当渠道显式开启开关且实际发生模型映射时返回 true，避免影响历史渠道。
// @return 应按最终上游模型计费时返回 true。
func (info *RelayInfo) ShouldUseUpstreamModelForBilling() bool {
	if info == nil || info.ChannelMeta == nil {
		return false
	}
	return info.ChannelSetting.UseUpstreamModelForBilling && info.IsModelMapped && strings.TrimSpace(info.UpstreamModelName) != ""
}

// ResolveBillingModelName 返回当前上下文下应使用的计费模型名，不读取已冻结的计费快照。
// 价格计算阶段使用它来解析当次模型映射链的最终落点。
// @return 当前渠道尝试解析出的计费模型名。
func (info *RelayInfo) ResolveBillingModelName() string {
	if info == nil {
		return ""
	}
	if info.ShouldUseUpstreamModelForBilling() {
		if strings.HasSuffix(info.OriginModelName, ratio_setting.CompactModelSuffix) {
			return ratio_setting.WithCompactModelSuffix(info.UpstreamModelName)
		}
		return info.UpstreamModelName
	}
	return info.OriginModelName
}

// FreezeBillingModelName 冻结本次请求实际用于查价和结算的模型名。
// @param modelName 本次价格计算使用的模型名。
func (info *RelayInfo) FreezeBillingModelName(modelName string) {
	if info == nil {
		return
	}
	info.ResolvedBillingModelName = strings.TrimSpace(modelName)
}

// ClearBillingModelName 清理上一次渠道尝试留下的计费模型快照。
func (info *RelayInfo) ClearBillingModelName() {
	if info == nil {
		return
	}
	info.ResolvedBillingModelName = ""
}

// FrozenBillingModelName 返回当前已经冻结的计费模型名，不执行动态回退解析。
// @return 当前冻结值；尚未冻结时返回空字符串。
func (info *RelayInfo) FrozenBillingModelName() string {
	if info == nil {
		return ""
	}
	return info.ResolvedBillingModelName
}

// BillingModelName 返回本次请求用于计费和消费日志主模型的模型名。
// 价格计算成功后优先返回冻结值，避免上游响应里的实际模型名污染结算。
// @return 冻结的计费模型名；未冻结时返回当前动态解析结果。
func (info *RelayInfo) BillingModelName() string {
	if info == nil {
		return ""
	}
	if strings.TrimSpace(info.ResolvedBillingModelName) != "" {
		return info.ResolvedBillingModelName
	}
	return info.ResolveBillingModelName()
}
