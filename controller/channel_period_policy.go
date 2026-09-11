package controller

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetChannelPeriodPolicy 返回指定渠道的权威策略及服务器时区。
// @param c 管理员请求上下文。
// @return 无，写入管理响应。
func GetChannelPeriodPolicy(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	view, err := service.GetChannelPeriodPolicy(c, channel.Id)
	if err != nil {
		respondChannelPeriodPolicyError(c, err, false)
		return
	}
	common.ApiSuccess(c, view)
}

// SetChannelPeriodPolicy 校验版本并保存整份独立策略。
// @param c 管理员请求上下文。
// @return 无，写入管理响应。
func SetChannelPeriodPolicy(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	var input dto.ChannelPeriodPolicyInput
	if !bindChannelPeriodJSON(c, &input) {
		return
	}
	view, err := service.SaveChannelPeriodPolicy(c, channel.Id, input, common.GetContextKeyInt(c, constant.ContextKeyUserId))
	if view.Revision > 0 {
		recordManageAudit(c, "channel.period_policy_set", map[string]interface{}{"channel_id": channel.Id, "revision": view.Revision})
	}
	if err != nil {
		respondChannelPeriodPolicyError(c, err, view.Revision > 0)
		return
	}
	common.ApiSuccess(c, view)
}

// PreviewChannelPeriodPolicy 只读解析输入时间和当前有效规则，不修改配置或用量。
// @param c 管理员请求上下文。
// @return 无，返回服务器解析结果。
func PreviewChannelPeriodPolicy(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	var input dto.ChannelPeriodPolicyInput
	if !bindChannelPeriodJSON(c, &input) {
		return
	}
	current, err := service.GetChannelPeriodPolicy(c, channel.Id)
	if err != nil {
		respondChannelPeriodPolicyError(c, err, false)
		return
	}
	if current.Revision != input.ExpectedRevision {
		respondChannelPeriodPolicyError(c, model.ErrChannelPeriodPolicyConflict, false)
		return
	}
	view, err := service.PreviewChannelPeriodPolicy(c, channel, input.Config, time.Now().In(time.Local))
	if err != nil {
		respondChannelPeriodPolicyError(c, err, false)
		return
	}
	common.ApiSuccess(c, view)
}

// GetChannelPeriodPolicyTargets 返回不含密钥的候选渠道及模型。
// @param c 管理员请求上下文。
// @return 无，写入精简选项。
func GetChannelPeriodPolicyTargets(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	items, err := model.ListEnabledChannelModelOptions()
	if err != nil {
		respondChannelPeriodPolicyError(c, err, false)
		return
	}
	filtered := make([]model.ChannelModelOption, 0, len(items))
	for _, item := range items {
		if item.ID != channel.Id {
			filtered = append(filtered, item)
		}
	}
	common.ApiSuccess(c, filtered)
}

func bindChannelPeriodJSON(c *gin.Context, input any) bool {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 256*1024+1))
	if err != nil || len(body) > 256*1024 || common.Unmarshal(body, input) != nil {
		respondChannelPeriodPolicyError(c, service.ErrInvalidChannelPeriodPolicy, false)
		return false
	}
	// 所有策略字段显式序列化，反向键检查防止拼错字段被忽略后误保存为不限。
	canonical, err := common.Marshal(input)
	var raw, accepted any
	if err != nil || common.Unmarshal(body, &raw) != nil || common.Unmarshal(canonical, &accepted) != nil || !channelPeriodFieldsKnown(raw, accepted) {
		respondChannelPeriodPolicyError(c, service.ErrInvalidChannelPeriodPolicy, false)
		return false
	}
	return true
}

func channelPeriodFieldsKnown(raw, accepted any) bool {
	switch value := raw.(type) {
	case map[string]any:
		known, ok := accepted.(map[string]any)
		if !ok || len(value) != len(known) {
			return false
		}
		for key, child := range value {
			other, ok := known[key]
			if !ok || !channelPeriodFieldsKnown(child, other) {
				return false
			}
		}
	case []any:
		known, ok := accepted.([]any)
		if !ok || len(value) != len(known) {
			return false
		}
		for i, child := range value {
			if !channelPeriodFieldsKnown(child, known[i]) {
				return false
			}
		}
	default:
		return reflect.TypeOf(raw) == reflect.TypeOf(accepted)
	}
	return true
}

func respondChannelPeriodPolicyError(c *gin.Context, err error, committed bool) {
	status, code, message := http.StatusServiceUnavailable, "channel_period_policy_unavailable", "渠道周期策略暂不可用"
	if errors.Is(err, service.ErrInvalidChannelPeriodPolicy) {
		status, code, message = http.StatusBadRequest, "invalid_channel_period_policy", err.Error()
	}
	if errors.Is(err, model.ErrChannelPeriodPolicyConflict) {
		status, code, message = http.StatusConflict, "channel_period_policy_conflict", err.Error()
	}
	if committed {
		message = "策略已保存，但状态刷新失败，请刷新后查看"
	}
	logger.LogWarn(c, "渠道周期策略请求失败: "+common.LocalLogPreview(err.Error()))
	c.JSON(status, gin.H{"success": false, "code": code, "message": message, "committed": committed})
}
