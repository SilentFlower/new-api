package middleware

import (
	"errors"
	"fmt"
	appdto "github.com/QuantumNous/new-api/dto"
	"net/http"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const responsesWebSocketRequestPath = "/v1/responses"

// SelectResponsesWebSocketChannel 为 Responses WebSocket 首轮请求选择渠道并初始化上下文。
// @param c 当前 Gin 请求上下文。
// @param modelName 首帧 response.create 中的基础模型名。
// @return 选中的渠道，以及不可继续时的标准 Relay 错误。
func SelectResponsesWebSocketChannel(c *gin.Context, modelName string, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	if apiErr := ValidateResponsesWebSocketModelAccess(c, modelName); apiErr != nil {
		return nil, apiErr
	}
	constraints := service.GetChannelConstraints(c)
	if !slices.ContainsFunc(constraints.Filters, func(filter appdto.ChannelFilter) bool {
		return filter.Kind == appdto.FilterResponsesWebSocket
	}) {
		constraints.AddFilter(appdto.ChannelFilter{Kind: appdto.FilterResponsesWebSocket})
	}
	channel, _, selectErr := service.SelectChannelForRequest(c, modelName, retryParam)
	if selectErr != nil {
		message := selectErr.Message
		if selectErr.MessageID != "" {
			message = i18n.T(c, selectErr.MessageID, selectErr.Params)
		}
		code := selectErr.Code
		if code == "" {
			code = types.ErrorCodeGetChannelFailed
		}
		return nil, types.NewErrorWithStatusCode(errors.New(message), code, selectErr.StatusCode, types.ErrOptionWithSkipRetry())
	}
	if err := SetupContextForSelectedChannel(c, channel, modelName); err != nil {
		return nil, err
	}
	return channel, nil
}

// ValidateResponsesWebSocketModelAccess 校验当前 Token 是否允许使用 WebSocket turn 的基础模型。
// @param c 当前 Gin 请求上下文。
// @param modelName WebSocket turn 的基础模型名。
// @return 无权限时返回标准 Relay 错误，否则返回 nil。
func ValidateResponsesWebSocketModelAccess(c *gin.Context, modelName string) *types.NewAPIError {
	if !common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		return nil
	}
	raw, ok := common.GetContextKey(c, constant.ContextKeyTokenModelLimit)
	if !ok {
		return types.NewErrorWithStatusCode(errors.New("token has no model access"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	tokenModelLimit, ok := raw.(map[string]bool)
	if !ok {
		tokenModelLimit = map[string]bool{}
	}
	if !TokenModelLimitAllows(tokenModelLimit, modelName) {
		return types.NewErrorWithStatusCode(fmt.Errorf("token is not allowed to use model %s", modelName), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	return nil
}

// ResponsesWebSocketChannelSupportsModel 判断渠道能否承载指定基础模型的 Responses WebSocket。
// @param channel 待检查的渠道。
// @param modelName WebSocket turn 的基础模型名。
// @return 普通渠道返回 true；Advanced Custom 仅在原生 Responses 路由匹配时返回 true。
func ResponsesWebSocketChannelSupportsModel(channel *model.Channel, modelName string) bool {
	if channel == nil {
		return false
	}
	if channel.Type != constant.ChannelTypeAdvancedCustom {
		return true
	}
	config := channel.GetOtherSettings().AdvancedCustom
	return config != nil && config.SupportsPathForModel(responsesWebSocketRequestPath, modelName)
}
