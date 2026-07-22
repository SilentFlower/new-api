package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

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
	return common.GetContextKeyBool(ctx, constant.ContextKeyLocalCountTokens)
}

func handleClientGoneLocalUsageBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, summary textQuotaSummary, originUsage *dto.Usage, adminRejectReason string) bool {
	if !shouldSkipClientGoneLocalUsageBilling(ctx, relayInfo, originUsage) {
		return false
	}
	if err := SettleBilling(ctx, relayInfo, 0); err != nil {
		logger.LogError(ctx, "error settling skipped client_gone local usage billing: "+err.Error())
	}
	recordClientGoneLocalUsageErrorLog(ctx, relayInfo, summary, originUsage, adminRejectReason)
	logger.LogWarn(ctx, fmt.Sprintf("skip billing for client_gone local usage: userId=%d, channelId=%d, tokenId=%d, model=%s, estimatedQuota=%d",
		relayInfo.UserId, relayInfoChannelID(relayInfo), relayInfo.TokenId, summary.ModelName, summary.Quota))
	return true
}

func recordClientGoneLocalUsageErrorLog(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, summary textQuotaSummary, originUsage *dto.Usage, adminRejectReason string) {
	if !constant.ErrorLogEnabled {
		return
	}
	other := buildClientGoneLocalUsageErrorOther(ctx, relayInfo, summary, originUsage, adminRejectReason)
	content := "流式请求客户端断开，本地估算 usage 不计费"
	model.RecordErrorLog(ctx, relayInfo.UserId, relayInfoChannelID(relayInfo), summary.ModelName, summary.TokenName, content, relayInfo.TokenId, int(summary.UseTimeSeconds), relayInfo.IsStream, relayInfo.UsingGroup, other)
}

func buildClientGoneLocalUsageErrorOther(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, summary textQuotaSummary, originUsage *dto.Usage, adminRejectReason string) map[string]interface{} {
	var other map[string]interface{}
	if summary.IsClaudeUsageSemantic {
		other = GenerateClaudeOtherInfo(ctx, relayInfo,
			summary.ModelRatio, summary.GroupRatio, summary.CompletionRatio,
			summary.CacheTokens, summary.CacheRatio,
			summary.CacheCreationTokens, summary.CacheCreationRatio,
			summary.CacheCreationTokens5m, summary.CacheCreationRatio5m,
			summary.CacheCreationTokens1h, summary.CacheCreationRatio1h,
			summary.ModelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
		other["usage_semantic"] = "anthropic"
	} else {
		other = GenerateTextOtherInfo(ctx, relayInfo, summary.ModelRatio, summary.GroupRatio, summary.CompletionRatio, summary.CacheTokens, summary.CacheRatio, summary.ModelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
	}
	appendUsageBillingPathForLog(other, true, originUsage)
	if adminRejectReason != "" {
		other["reject_reason"] = adminRejectReason
	}
	if cacheWriteTokens := cacheWriteTokensTotal(summary); cacheWriteTokens > 0 {
		other["cache_write_tokens"] = cacheWriteTokens
	}
	MergeContextLogOther(ctx, other)
	adminInfo := ensureLogAdminInfo(other)
	adminInfo["billing_skipped_reason"] = clientGoneLocalUsageBillingSkippedReason
	adminInfo["estimated_quota"] = summary.Quota
	adminInfo["estimated_prompt_tokens"] = summary.PromptTokens
	adminInfo["estimated_completion_tokens"] = summary.CompletionTokens
	return other
}

func ensureLogAdminInfo(other map[string]interface{}) map[string]interface{} {
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	if !ok || adminInfo == nil {
		adminInfo = map[string]interface{}{}
		other["admin_info"] = adminInfo
	}
	return adminInfo
}

func relayInfoChannelID(relayInfo *relaycommon.RelayInfo) int {
	if relayInfo == nil || relayInfo.ChannelMeta == nil {
		return 0
	}
	return relayInfo.ChannelMeta.ChannelId
}
