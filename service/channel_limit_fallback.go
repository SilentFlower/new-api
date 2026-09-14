package service

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// ResolveChannelLimitFallbackTarget 按当前有效分组和 Token 权限重新验证唯一目标。
// @param c 已认证的请求上下文。
// @param fallback 管理员保存的降级目标。
// @return 可用目标或明确的本地拒绝。
func ResolveChannelLimitFallbackTarget(c *gin.Context, fallback dto.ChannelLimitFallback) (*model.Channel, *types.NewAPIError) {
	deny := types.NewOpenAIError(errors.New("channel limit fallback target is unavailable or unauthorized"), types.ErrorCodeChannelLimitFallbackUnavailable, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	if _, pinned := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); pinned {
		return nil, deny
	}
	if common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		allowed, ok := common.GetContextKeyType[map[string]bool](c, constant.ContextKeyTokenModelLimit)
		if !ok {
			return nil, deny
		}
		if !allowed[ratio_setting.FormatMatchingModelName(fallback.Model)] {
			return nil, deny
		}
	}
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "auto" {
		group = common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
	}
	if group == "" || group == "auto" || !model.IsChannelEnabledForGroupModel(group, fallback.Model, fallback.ChannelID) {
		return nil, deny
	}
	target, err := model.CacheGetChannel(fallback.ChannelID)
	if err != nil || target == nil || target.Status != common.ChannelStatusEnabled {
		return nil, deny
	}
	if target.Type == constant.ChannelTypeAdvancedCustom {
		config := target.GetOtherSettings().AdvancedCustom
		if config == nil || !config.SupportsPathForModel(c.Request.URL.Path, fallback.Model) {
			return nil, deny
		}
	}
	return target, nil
}
