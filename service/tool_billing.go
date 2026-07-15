package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

// ToolNameWebSearch 是通用 Web Search 工具的计费名称。
const ToolNameWebSearch = "web_search"

// ToolCallUsage 汇总单次请求中的工具调用次数。
type ToolCallUsage struct {
	ModelName              string
	WebSearchCalls         int
	WebSearchToolName      string
	FileSearchCalls        int
	ImageGenerationCall    bool
	ImageGenerationQuality string
	ImageGenerationSize    string
}

// ToolCallItem 描述一项工具调用费用明细。
type ToolCallItem = relaycommon.ToolCallItem

// ToolCallResult 描述单次请求聚合后的工具调用费用。
type ToolCallResult = relaycommon.ToolCallResult

// ComputeToolCallQuota 按模型工具单价和分组倍率计算全部工具调用费用。
// @param usage 单次请求的工具调用统计。
// @param groupRatio 当前使用分组的计费倍率。
// @return 工具费用总额、明细和可能出现的额度饱和标记。
func ComputeToolCallQuota(usage ToolCallUsage, groupRatio float64) ToolCallResult {
	if !(groupRatio > 0) {
		return ToolCallResult{}
	}

	var items []ToolCallItem
	var totalQuota int64
	var quotaClamp *common.QuotaClamp

	addItem := func(toolName string, count int) {
		if count <= 0 {
			return
		}
		pricePer1K := operation_setting.GetToolPriceForModel(toolName, usage.ModelName)
		if pricePer1K <= 0 {
			return
		}
		totalPrice := pricePer1K * float64(count) / 1000
		quota, clamp := common.QuotaRoundChecked(totalPrice * common.QuotaPerUnit * groupRatio)
		if quotaClamp == nil && clamp != nil {
			quotaClamp = clamp
		}
		items = append(items, ToolCallItem{
			Name:       toolName,
			CallCount:  count,
			PricePer1K: pricePer1K,
			TotalPrice: totalPrice,
			Quota:      quota,
		})
		totalQuota += int64(quota)
	}

	if usage.WebSearchCalls > 0 && usage.WebSearchToolName != "" {
		addItem(usage.WebSearchToolName, usage.WebSearchCalls)
	}

	if usage.FileSearchCalls > 0 {
		addItem("file_search", usage.FileSearchCalls)
	}

	if usage.ImageGenerationCall {
		price := operation_setting.GetGPTImage1PriceOnceCall(usage.ImageGenerationQuality, usage.ImageGenerationSize)
		quota, clamp := common.QuotaRoundChecked(price * common.QuotaPerUnit * groupRatio)
		if quotaClamp == nil && clamp != nil {
			quotaClamp = clamp
		}
		items = append(items, ToolCallItem{
			Name:       "image_generation",
			CallCount:  1,
			PricePer1K: price,
			TotalPrice: price,
			Quota:      quota,
		})
		totalQuota += int64(quota)
	}
	checkedTotalQuota, totalClamp := common.QuotaRoundChecked(float64(totalQuota))
	if totalClamp != nil {
		quotaClamp = totalClamp
	}

	return ToolCallResult{
		TotalQuota: checkedTotalQuota,
		Items:      items,
		QuotaClamp: quotaClamp,
	}
}

// PostToolCallConsumeQuota 结算纯工具调用费用并记录零 Token 消费日志。
// @param ctx 当前 Gin 请求上下文。
// @param relayInfo 当前 Relay 请求信息。
func PostToolCallConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo) {
	result, ok := frozenToolCallBillingResult(relayInfo)
	if !ok {
		logger.LogError(ctx, "工具调用计费结果缺失，跳过结算")
		return
	}
	noteQuotaClamp(relayInfo, result.QuotaClamp)

	model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, result.TotalQuota)
	model.UpdateChannelUsedQuota(relayInfo.ChannelId, result.TotalQuota)
	if err := SettleBilling(ctx, relayInfo, result.TotalQuota); err != nil {
		logger.LogError(ctx, "工具调用计费结算失败: "+err.Error())
	}

	webSearchCalls := 0
	webSearchPrice := 0.0
	for _, item := range result.Items {
		if item.Name == ToolNameWebSearch {
			webSearchCalls += item.CallCount
			webSearchPrice = item.PricePer1K
		}
	}
	other := GenerateTextOtherInfo(
		ctx,
		relayInfo,
		0,
		relayInfo.PriceData.GroupRatioInfo.GroupRatio,
		0,
		0,
		0,
		0,
		relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio,
	)
	other["web_search"] = webSearchCalls > 0
	other["web_search_call_count"] = webSearchCalls
	other["web_search_price"] = webSearchPrice
	other["tool_call_items"] = result.Items
	attachQuotaSaturation(ctx, relayInfo, other)

	model.RecordConsumeLog(ctx, relayInfo.UserId, model.RecordConsumeLogParams{
		ChannelId:        relayInfo.ChannelId,
		PromptTokens:     0,
		CompletionTokens: 0,
		ModelName:        relayInfo.BillingModelName(),
		TokenName:        ctx.GetString("token_name"),
		Quota:            result.TotalQuota,
		Content:          fmt.Sprintf("Web Search 调用 %d 次，消耗额度 %s", webSearchCalls, logger.FormatQuota(result.TotalQuota)),
		TokenId:          relayInfo.TokenId,
		UseTimeSeconds:   int(time.Since(relayInfo.StartTime).Seconds()),
		IsStream:         false,
		Group:            relayInfo.UsingGroup,
		Other:            other,
	})
	gopool.Go(func() {
		perfmetrics.RecordRelaySample(relayInfo, true, 0)
	})
}

func frozenToolCallBillingResult(relayInfo *relaycommon.RelayInfo) (ToolCallResult, bool) {
	if relayInfo == nil || relayInfo.ToolCallBilling == nil {
		return ToolCallResult{}, false
	}
	return *relayInfo.ToolCallBilling, true
}
