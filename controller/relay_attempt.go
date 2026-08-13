package controller

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func finalizeMainRelayBilling(c *gin.Context, relayInfo *relaycommon.RelayInfo, billingPrepared bool, apiErr *types.NewAPIError) *types.NewAPIError {
	if !billingPrepared || apiErr == nil {
		return apiErr
	}
	// 只有最终失败才退还跨重试复用的主请求预扣费，成功请求由对应 Relay handler 结算。
	apiErr = service.NormalizeViolationFeeError(apiErr)
	if relayInfo.Billing != nil {
		relayInfo.Billing.Refund(c)
	}
	service.ChargeViolationFeeIfNeeded(c, relayInfo, apiErr)
	return apiErr
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{
		TokenType: types.TokenTypeTokenizer,
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = int(maxCompletionTokens)
		} else {
			meta.MaxTokens = int(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = int(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		// 图片请求的价格依赖 ImagePriceRatio，即使关闭 Token 计数也必须保留价格元数据。
		return r.GetTokenCountMeta()
	default:
		// 尽力避免为不需要 Token 计数的请求构造大文本。
	}
	return meta
}

func cloneRelayRequest(request dto.Request) (dto.Request, error) {
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		data, err := common.Marshal(req)
		if err != nil {
			return nil, err
		}
		var cloned dto.GeneralOpenAIRequest
		if err := common.Unmarshal(data, &cloned); err != nil {
			return nil, err
		}
		return &cloned, nil
	case *dto.AlphaSearchRequest:
		cloned := *req
		return &cloned, nil
	case *dto.ClaudeRequest:
		data, err := common.Marshal(req)
		if err != nil {
			return nil, err
		}
		var cloned dto.ClaudeRequest
		if err := common.Unmarshal(data, &cloned); err != nil {
			return nil, err
		}
		return &cloned, nil
	default:
		return request, nil
	}
}

func resetMainRelayAttemptFields(relayInfo *relaycommon.RelayInfo, originModelName string) {
	relayInfo.OriginModelName = originModelName
	relayInfo.FinalPreConsumedQuota = 0
	relayInfo.SubscriptionPostDelta = 0
	relayInfo.ToolCallBilling = nil
	relayInfo.ClearBillingModelName()
}

func prepareMainRelayBilling(c *gin.Context, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	request := relayInfo.Request
	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	needCountToken := constant.CountToken
	// Token 计数和敏感词都关闭时，避免为大请求拼接完整 CombineText。
	var meta *types.TokenCountMeta
	if needSensitiveCheck || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSensitiveCheck && meta != nil {
		contains, words := service.CheckSensitiveText(meta.CombineText)
		if contains {
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
			return types.NewError(fmt.Errorf("sensitive words detected"), types.ErrorCodeSensitiveWordsDetected, types.ErrOptionWithSkipRetry())
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		return types.NewError(err, types.ErrorCodeCountTokenFailed)
	}
	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		return types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
	}
	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", relayInfo.OriginModelName))
		if relayInfo.Billing != nil {
			relayInfo.FinalPreConsumedQuota = relayInfo.Billing.GetPreConsumedQuota()
		}
		return nil
	}
	if relayInfo.Billing != nil {
		if err := relayInfo.Billing.Reserve(priceData.QuotaToPreConsume); err != nil {
			return types.NewError(err, types.ErrorCodePreConsumeTokenQuotaFailed, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		relayInfo.FinalPreConsumedQuota = relayInfo.Billing.GetPreConsumedQuota()
		return nil
	}
	return service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
}
