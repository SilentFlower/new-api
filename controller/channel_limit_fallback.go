package controller

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

func preflightChannelPeriodLimits(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	request, err := cloneRelayRequest(info.Request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	probe := *info
	probe.Request, probe.ChannelMeta = request, nil
	probe.TieredBillingSnapshot, probe.BillingRequestInput, probe.ToolCallBilling = nil, nil, nil
	probe.ClearBillingModelName()
	if relay.ShouldHandleResponsesCompactPassthrough(&probe) {
		if apiErr := relay.PrepareResponsesCompactPassthrough(c, &probe); apiErr != nil {
			return apiErr
		}
	} else {
		probe.InitChannelMeta(c)
		if err := helper.ModelMappedHelper(c, &probe, request); err != nil {
			return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
		}
	}
	if billing_setting.GetBillingMode(probe.ResolveBillingModelName()) == billing_setting.BillingModeTieredExpr {
		// 表达式依赖真实 token 数或请求字段；纯路由预检不能用虚构的零输入执行它。
		// 阶梯模型的免费豁免仅由分组倍率决定，完整查价仍在目标准备后的预扣阶段执行。
		if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume && helper.HandleGroupRatio(c, &probe).GroupRatio == 0 {
			return nil
		}
	} else {
		price, err := helper.ModelPriceHelper(c, &probe, 0, fastTokenCountMetaForPricing(request))
		if err != nil {
			return types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry(), types.ErrOptionWithStatusCode(http.StatusBadRequest))
		}
		if price.FreeModel {
			return nil
		}
	}
	return service.CheckSelectedChannelPeriodLimits(c)
}

func prepareChannelLimitFallback(c *gin.Context, info *relaycommon.RelayInfo, source *model.Channel, original dto.Request) (*model.Channel, *types.NewAPIError) {
	isResponsesCompact := relay.ShouldHandleResponsesCompactPassthrough(info)
	policy, err := service.GetChannelPeriodPolicy(c, source.Id)
	if err != nil {
		return source, service.ChannelPeriodAPIError(err)
	}
	if len(policy.Config.Budgets) == 0 {
		return source, nil
	}
	// 只为获准的 HTTP 文本入口增加副作用前调度，其余入口仍由原检查点限制。
	switch c.Request.URL.Path {
	case "/v1/chat/completions", "/v1/messages", "/v1/responses", "/v1/responses/compact":
	default:
		return source, nil
	}
	apiErr := preflightChannelPeriodLimits(c, info)
	if apiErr == nil {
		return source, nil
	}
	var block *service.ChannelPeriodBlock
	if value, ok := c.Get("channel_period_block"); ok {
		block, _ = value.(*service.ChannelPeriodBlock)
	}
	// 触发行自带的降级动作优先，未配置时回落策略级默认动作。
	selected, allowed := service.SelectChannelLimitFallback(policy.Config, source.Id, block)
	if apiErr.StatusCode != http.StatusTooManyRequests || !allowed || info.LimitFallback != nil || info.Billing != nil || c.GetBool("channel_limit_upstream_started") || c.GetBool("channel_limit_fallback_used") || info.SendResponseCount > 0 {
		return source, apiErr
	}
	// Compact 透传依赖同一渠道的原生协议能力，只允许在当前渠道内切换模型。
	if isResponsesCompact && selected.ChannelID != source.Id {
		return source, apiErr
	}
	if _, pinned := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); pinned {
		return source, apiErr
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return source, apiErr
	}
	// 管理员配置的唯一降级目标接收原始上游状态，由目标判断跨模型或渠道是否兼容。
	body, err := storage.Bytes()
	if err != nil {
		return source, apiErr
	}
	target, targetErr := service.ResolveChannelLimitFallbackTarget(c, selected)
	if targetErr != nil {
		return source, targetErr
	}
	request, err := cloneRelayRequest(original)
	if err != nil {
		return source, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	fallback := &relaycommon.ChannelLimitFallbackInfo{SourceChannelID: source.Id, TargetChannelID: target.Id, OriginalModel: info.OriginModelName, TargetModel: selected.Model}
	if block != nil {
		fallback.Scope, fallback.Period, fallback.ScheduleID, fallback.BudgetID, fallback.Models = block.Metric.Scope, block.Metric.Period, block.Metric.Source.ScheduleID, block.Metric.BudgetID, block.Metric.Models
	}
	c.Set("channel_limit_fallback_used", true)
	info.LimitFallback, info.RoutingModelName = fallback, fallback.TargetModel
	info.Request, info.ChannelMeta = request, nil
	info.PriceData = hosttypes.PriceData{}
	info.TieredBillingSnapshot, info.BillingRequestInput, info.ToolCallBilling, info.QuotaClamp = nil, nil, nil, nil
	info.ClearBillingModelName()
	// Setup 本身不清理未设置的旧渠道可选字段，切换时显式清除，避免泄露源组织头等参数。
	for _, key := range []string{"channel_organization", "api_version", "region", "plugin", "bot_id", "channel_period_block"} {
		delete(c.Keys, key)
	}
	if targetErr = middleware.SetupContextForSelectedChannel(c, target, fallback.TargetModel); targetErr != nil {
		return target, targetErr
	}
	relay.ResetRequestPreparation(c)
	if targetErr = preflightChannelPeriodLimits(c, info); targetErr != nil {
		return target, targetErr
	}
	if isResponsesCompact {
		// Compact 只能改写顶层模型；局部替换避免重组未知字段、显式零值和加密内容。
		body, err = sjson.SetBytes(append([]byte(nil), body...), "model", info.RoutingModel())
		if err != nil {
			return target, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
		}
	} else {
		if targetErr = validateChannelLimitFallbackConversion(c, info); targetErr != nil {
			return target, targetErr
		}
		// 原始透传也必须使用目标模型，同时保留所有未解析的客户端字段。
		var raw map[string]json.RawMessage
		if err = common.Unmarshal(body, &raw); err != nil {
			return target, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
		}
		raw["model"], err = common.Marshal(c.GetString("channel_limit_fallback_upstream_model"))
		if err != nil {
			return target, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
		}
		body, err = common.Marshal(raw)
		if err != nil {
			return target, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
		}
	}
	replacement, err := common.CreateBodyStorage(body)
	if err != nil {
		return target, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
	}
	c.Set(common.KeyBodyStorage, replacement)
	c.Request.ContentLength = replacement.Size()
	_ = storage.Close()
	return target, nil
}

func validateChannelLimitFallbackConversion(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	probe := *info
	request, err := cloneRelayRequest(info.Request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	probe.Request = request
	probe.InitChannelMeta(c)
	if err = helper.ModelMappedHelper(c, &probe, request); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	adaptor := relay.GetAdaptor(probe.ApiType)
	if adaptor == nil {
		return types.NewError(errors.New("fallback target interface unsupported"), types.ErrorCodeChannelLimitFallbackUnavailable, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(&probe)
	var converted any
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		converted, err = adaptor.ConvertOpenAIRequest(c, &probe, req)
	case *dto.ClaudeRequest:
		converted, err = adaptor.ConvertClaudeRequest(c, &probe, req)
	case *dto.OpenAIResponsesRequest:
		converted, err = adaptor.ConvertOpenAIResponsesRequest(c, &probe, *req)
	default:
		err = errors.New("fallback request interface unsupported")
	}
	// 降级请求走目标渠道的常规出站管道，字段裁剪与直连该渠道完全一致，不再逐字段比对客户端原始 JSON。
	// 只拦截目标适配器根本无法构造上游请求的情况，例如 Claude 渠道尚未实现 Responses 转换。
	if err != nil || converted == nil {
		return types.NewOpenAIError(errors.New("fallback target does not support this request interface"), types.ErrorCodeChannelLimitFallbackUnavailable, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	c.Set("channel_limit_fallback_upstream_model", probe.UpstreamModelName)
	return nil
}
