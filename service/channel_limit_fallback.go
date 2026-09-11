package service

import (
	"errors"
	"net/http"
	"reflect"

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

// ChannelLimitFallbackRequestPortable 判断原始 JSON 是否含绑定上游状态的引用。
// @param body 原始请求 JSON。
// @return 可携带到另一个渠道时返回 true。
func ChannelLimitFallbackRequestPortable(body []byte) bool {
	var value any
	if common.Unmarshal(body, &value) != nil {
		return false
	}
	return channelLimitFallbackValuePortable(value)
}

func channelLimitFallbackValuePortable(value any) bool {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			switch key {
			case "previous_response_id", "conversation", "container", "file_id", "file_ids", "vector_store_ids", "encrypted_content", "prompt":
				if child != nil && child != "" {
					return false
				}
			case "type":
				if child == "item_reference" || child == "compaction" || child == "redacted_thinking" {
					return false
				}
			}
			if !channelLimitFallbackValuePortable(child) {
				return false
			}
		}
	case []any:
		for _, child := range item {
			if !channelLimitFallbackValuePortable(child) {
				return false
			}
		}
	}
	return true
}

// ChannelLimitFallbackPreservesRequest 验证实际转换结果保留所有客户端能力字段。
// @param original 客户端原始 JSON。
// @param converted 目标适配器转换后的 JSON。
// @return 无法证明语义保留时拒绝降级。
func ChannelLimitFallbackPreservesRequest(original, converted []byte) bool {
	var source, target map[string]any
	if common.Unmarshal(original, &source) != nil || common.Unmarshal(converted, &target) != nil || source == nil || target == nil {
		return false
	}
	delete(source, "model")
	// 显式非流式和省略 stream 的语义一致，允许 DTO 的 omitempty 正常工作。
	if source["stream"] == false && target["stream"] == nil {
		delete(source, "stream")
	}
	return channelLimitFallbackFieldsPreserved(source, target)
}

func channelLimitFallbackFieldsPreserved(source, target any) bool {
	switch input := source.(type) {
	case map[string]any:
		output, ok := target.(map[string]any)
		if !ok {
			return false
		}
		for key, child := range input {
			other, exists := output[key]
			if !exists || !channelLimitFallbackFieldsPreserved(child, other) {
				return false
			}
		}
		return true
	case []any:
		output, ok := target.([]any)
		if !ok || len(input) != len(output) {
			return false
		}
		for i, child := range input {
			if !channelLimitFallbackFieldsPreserved(child, output[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(source, target)
	}
}
