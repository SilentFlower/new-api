package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// SetChannelUserPeriodOverride 保存指定规则的个人整段提额，并回读完整有效状态。
// @param c 管理员请求上下文。
// @return 无，写入保存结果。
func SetChannelUserPeriodOverride(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	userID, ok := getChannelUserLimitUserID(c)
	if !ok || !channelUserLimitUserExists(c, userID) {
		return
	}
	var input dto.ChannelUserPeriodOverrideInput
	if !bindChannelPeriodJSON(c, &input) {
		return
	}
	ruleID := c.Param("rule_id")
	if err := service.ReplaceChannelUserPeriodOverride(c, channel.Id, userID, ruleID, input, common.GetContextKeyInt(c, constant.ContextKeyUserId)); err != nil {
		respondChannelPeriodPolicyError(c, err, false)
		return
	}
	recordManageAudit(c, "channel.user_period_override_set", map[string]interface{}{"channel_id": channel.Id, "user_id": userID, "rule_id": ruleID, "limit": input.UserPeriodQuotaLimit, "expires_at": input.ExpiresAt})
	status, err := buildChannelUserLimitStatus(c, channel, userID)
	if err != nil {
		respondChannelPeriodPolicyError(c, err, true)
		return
	}
	common.ApiSuccess(c, status)
}

// DeleteChannelUserPeriodOverride 撤销规则整段特批，其他指标不受影响。
// @param c 管理员请求上下文。
// @return 无，写入撤销结果。
func DeleteChannelUserPeriodOverride(c *gin.Context) {
	channel, ok := getChannelForUserLimit(c)
	if !ok {
		return
	}
	userID, ok := getChannelUserLimitUserID(c)
	if !ok || !channelUserLimitUserExists(c, userID) {
		return
	}
	ruleID := c.Param("rule_id")
	view, err := service.GetChannelPeriodPolicy(c, channel.Id)
	if err != nil {
		respondChannelPeriodPolicyError(c, err, false)
		return
	}
	found := false
	for _, rule := range view.Config.Rules {
		if rule.ID == ruleID {
			found = true
			break
		}
	}
	if !found {
		respondChannelPeriodPolicyError(c, service.ErrInvalidChannelPeriodPolicy, false)
		return
	}
	if err := model.DeleteChannelUserPeriodOverride(c, channel.Id, userID, ruleID); err != nil {
		respondChannelPeriodPolicyError(c, err, false)
		return
	}
	recordManageAudit(c, "channel.user_period_override_delete", map[string]interface{}{"channel_id": channel.Id, "user_id": userID, "rule_id": ruleID})
	status, err := buildChannelUserLimitStatus(c, channel, userID)
	if err != nil {
		respondChannelPeriodPolicyError(c, err, true)
		return
	}
	common.ApiSuccess(c, status)
}
