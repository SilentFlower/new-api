package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
)

const clientGoneLocalUsageBillingSkippedReason = "client_gone_local_usage"

func shouldSkipClientGoneLocalUsageBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage) bool {
	if ctx == nil || relayInfo == nil || usage == nil {
		return false
	}
	if !relayInfo.IsStream || relayInfo.StreamStatus == nil {
		return false
	}
	if relayInfo.StreamStatus.EndReason != relaycommon.StreamEndReasonClientGone {
		return false
	}
	// 回复过滤插件也会主动取消上游，这时客户端仍在线，仍按主线计费。
	for _, record := range relayInfo.RequestPluginRecord {
		if record.Stage == "reply" && record.Rejected != nil {
			return false
		}
	}
	return common.GetContextKeyBool(ctx, constant.ContextKeyLocalCountTokens)
}

func handleClientGoneLocalUsageBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, summary textQuotaSummary, originUsage *dto.Usage, adminRejectReason string) bool {
	if !shouldSkipClientGoneLocalUsageBilling(ctx, relayInfo, originUsage) {
		return false
	}
	err, refundFallbackTriggered := settleZeroTextQuota(ctx, relayInfo)
	settlementFailed := err != nil
	if settlementFailed {
		logger.LogError(ctx, "error settling skipped client_gone local usage billing: "+err.Error())
	}
	recordClientGoneLocalUsageErrorLog(ctx, relayInfo, summary, originUsage, adminRejectReason, settlementFailed, refundFallbackTriggered)
	logger.LogWarn(ctx, fmt.Sprintf("handle client_gone local usage billing: userId=%d, channelId=%d, tokenId=%d, model=%s, estimatedQuota=%d, settlementFailed=%t, refundFallbackTriggered=%t",
		relayInfo.UserId, relayInfoChannelID(relayInfo), relayInfo.TokenId, summary.ModelName, summary.Quota, settlementFailed, refundFallbackTriggered))
	return true
}

func recordClientGoneLocalUsageErrorLog(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, summary textQuotaSummary, originUsage *dto.Usage, adminRejectReason string, settlementFailed bool, refundFallbackTriggered bool) {
	if !constant.ErrorLogEnabled {
		return
	}
	other := buildClientGoneLocalUsageErrorOther(ctx, relayInfo, summary, originUsage, adminRejectReason, settlementFailed, refundFallbackTriggered)
	content := "流式请求客户端断开，本地估算 usage 不计费"
	if settlementFailed {
		content = "流式请求客户端断开，本地估算 usage 零额结算失败"
		if refundFallbackTriggered {
			content += "，已触发退款兜底"
		}
	}
	model.RecordErrorLog(ctx, relayInfo.UserId, relayInfoChannelID(relayInfo), summary.ModelName, summary.TokenName, content, relayInfo.TokenId, int(summary.UseTimeSeconds), relayInfo.IsStream, relayInfo.UsingGroup, other)
}

func buildClientGoneLocalUsageErrorOther(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, summary textQuotaSummary, originUsage *dto.Usage, adminRejectReason string, settlementFailed bool, refundFallbackTriggered bool) *model.LogOther {
	var other *model.LogOther
	if summary.IsClaudeUsageSemantic {
		other = GenerateClaudeOtherInfo(ctx, relayInfo,
			summary.ModelRatio, summary.GroupRatio, summary.CompletionRatio,
			summary.CacheTokens, summary.CacheRatio,
			summary.CacheCreationTokens, summary.CacheCreationRatio,
			summary.CacheCreationTokens5m, summary.CacheCreationRatio5m,
			summary.CacheCreationTokens1h, summary.CacheCreationRatio1h,
			summary.ModelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
		other.SetPublic("usage_semantic", "anthropic")
	} else {
		other = GenerateTextOtherInfo(ctx, relayInfo, summary.ModelRatio, summary.GroupRatio, summary.CompletionRatio, summary.CacheTokens, summary.CacheRatio, summary.ModelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
	}
	appendUsageBillingPathForLog(other, true, originUsage)
	if adminRejectReason != "" {
		other.SetAdmin("reject_reason", adminRejectReason)
	}
	if cacheWriteTokens := cacheWriteTokensTotal(summary); cacheWriteTokens > 0 {
		other.SetPublic("cache_write_tokens", cacheWriteTokens)
	}
	MergeContextLogOther(ctx, other)

	other.SetAdmin("billing_skipped_reason", clientGoneLocalUsageBillingSkippedReason)
	other.SetAdmin("estimated_quota", summary.Quota)
	other.SetAdmin("estimated_prompt_tokens", summary.PromptTokens)
	other.SetAdmin("estimated_completion_tokens", summary.CompletionTokens)
	if settlementFailed {
		other.SetAdmin("billing_settlement_failed", true)
		other.SetAdmin("billing_refund_fallback_triggered", refundFallbackTriggered)
	}
	return other
}

func relayInfoChannelID(relayInfo *relaycommon.RelayInfo) int {
	if relayInfo == nil || relayInfo.ChannelMeta == nil {
		return 0
	}
	return relayInfo.ChannelMeta.ChannelId
}
