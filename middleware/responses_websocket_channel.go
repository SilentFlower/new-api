package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const responsesWebSocketRequestPath = "/v1/responses"

// SelectResponsesWebSocketChannel 为 Responses WebSocket 首轮请求选择渠道并初始化上下文。
// @param c 当前 Gin 请求上下文。
// @param modelName 首帧 response.create 中的基础模型名。
// @return 选中的渠道，以及不可继续时的标准 Relay 错误。
func SelectResponsesWebSocketChannel(c *gin.Context, modelName string) (*model.Channel, *types.NewAPIError) {
	if c == nil {
		return nil, types.NewErrorWithStatusCode(errors.New("request context is nil"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	var channel *model.Channel
	channelID, hasSpecificChannel := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId)
	if hasSpecificChannel {
		id, err := strconv.Atoi(fmt.Sprint(channelID))
		if err != nil {
			return nil, types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgDistributorInvalidChannelId)), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		channel, err = model.GetChannelById(id, true)
		if err != nil {
			return nil, types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgDistributorInvalidChannelId)), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if channel.Status != common.ChannelStatusEnabled {
			return nil, types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgDistributorChannelDisabled)), types.ErrorCodeGetChannelFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry())
		}
	} else {
		if apiErr := ValidateResponsesWebSocketModelAccess(c, modelName); apiErr != nil {
			return nil, apiErr
		}
		if modelName == "" {
			return nil, types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgDistributorModelNameRequired)), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}

		var selectGroup string
		usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
		if preferredChannelID, found := service.GetPreferredChannelByAffinity(c, modelName, usingGroup); found {
			affinityUsable := false
			preferred, err := model.CacheGetChannel(preferredChannelID)
			if err == nil && preferred != nil && preferred.Status == common.ChannelStatusEnabled &&
				ResponsesWebSocketChannelSupportsModel(preferred, modelName) {
				if usingGroup == "auto" {
					userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
					for _, group := range service.GetUserAutoGroup(userGroup) {
						if model.IsChannelEnabledForGroupModel(group, modelName, preferred.Id) {
							selectGroup = group
							common.SetContextKey(c, constant.ContextKeyAutoGroup, group)
							channel = preferred
							affinityUsable = true
							service.MarkChannelAffinityUsed(c, group, preferred.Id)
							break
						}
					}
				} else if model.IsChannelEnabledForGroupModel(usingGroup, modelName, preferred.Id) {
					channel = preferred
					selectGroup = usingGroup
					affinityUsable = true
					service.MarkChannelAffinityUsed(c, usingGroup, preferred.Id)
				}
			}
			if !affinityUsable && !service.ShouldKeepChannelAffinityOnChannelDisabled() {
				service.ClearCurrentChannelAffinityCache(c)
			}
		}

		if channel == nil {
			var err error
			channel, selectGroup, err = service.CacheGetRandomSatisfiedChannel(&service.RetryParam{
				Ctx:         c,
				ModelName:   modelName,
				TokenGroup:  usingGroup,
				RequestPath: responsesWebSocketRequestPath,
				Retry:       common.GetPointer(0),
			})
			if err != nil {
				showGroup := usingGroup
				if usingGroup == "auto" {
					showGroup = fmt.Sprintf("auto(%s)", selectGroup)
				}
				message := i18n.T(c, i18n.MsgDistributorGetChannelFailed, map[string]any{"Group": showGroup, "Model": modelName, "Error": err.Error()})
				return nil, types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeModelNotFound, http.StatusServiceUnavailable)
			}
			if channel == nil {
				message := i18n.T(c, i18n.MsgDistributorNoAvailableChannel, map[string]any{"Group": usingGroup, "Model": modelName})
				return nil, types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeModelNotFound, http.StatusServiceUnavailable)
			}
		}
	}

	if apiErr := SetupContextForSelectedChannel(c, channel, modelName); apiErr != nil {
		return nil, apiErr
	}
	return channel, nil
}

// ValidateResponsesWebSocketModelAccess 校验当前 Token 是否允许使用 WebSocket turn 的基础模型。
// @param c 当前 Gin 请求上下文。
// @param modelName WebSocket turn 的基础模型名。
// @return 无权限时返回标准 Relay 错误，否则返回 nil。
func ValidateResponsesWebSocketModelAccess(c *gin.Context, modelName string) *types.NewAPIError {
	if c == nil {
		return types.NewErrorWithStatusCode(errors.New("request context is nil"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if _, hasSpecificChannel := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); hasSpecificChannel {
		return nil
	}
	if !common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		return nil
	}
	value, ok := common.GetContextKey(c, constant.ContextKeyTokenModelLimit)
	if !ok {
		return types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgDistributorTokenNoModelAccess)), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	tokenModelLimit, ok := value.(map[string]bool)
	if !ok {
		tokenModelLimit = map[string]bool{}
	}
	matchName := ratio_setting.FormatMatchingModelName(modelName)
	if _, ok := tokenModelLimit[matchName]; !ok {
		message := i18n.T(c, i18n.MsgDistributorTokenModelForbidden, map[string]any{"Model": modelName})
		return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
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
