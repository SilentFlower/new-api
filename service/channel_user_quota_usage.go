package service

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// RecordChannelUserQuotaUsage 同时记录指定渠道用户的个人、池子和规则整段正向额度。
//
// @param ctx 请求上下文。
// @param channelID 实际记账的渠道 ID。
// @param userID 实际消费的用户 ID。
// @param quota 本次新增的正向额度。
// @return error 任一周期状态写入失败时返回错误。
func RecordChannelUserQuotaUsage(ctx context.Context, channelID int, userID int, quota int) error {
	return RecordChannelUserModelQuotaUsage(ctx, channelID, userID, quota, "")
}

// RecordChannelUserModelQuotaUsage 记录带原始模型名的正向额度，按模型的预算行据此累计。
//
// @param ctx 请求上下文。
// @param channelID 实际记账的渠道 ID。
// @param userID 实际消费的用户 ID。
// @param quota 本次新增的正向额度。
// @param modelName 客户端原始模型名，空表示不按模型匹配。
// @return error 任一周期状态写入失败时返回错误。
func RecordChannelUserModelQuotaUsage(ctx context.Context, channelID int, userID int, quota int, modelName string) error {
	err := recordChannelBudgetUsage(ctx, channelID, userID, quota, modelName)
	if err != nil && channelID > 0 && userID > 0 && quota > 0 && quota <= common.MaxQuota {
		if ctx == nil {
			ctx = context.Background()
		}
		recordChannelQuotaGap(ctx, channelID, channelPeriodNow().Unix())
	}
	return err
}

// RecordRelayChannelUserQuotaUsage 同时记录 Relay 已完成的个人、池子和规则整段正向额度。
//
// @param ctx 请求上下文。
// @param relayInfo 包含最终渠道和用户的 Relay 信息。
// @param quota 本次新增的正向额度。
// @return 无。
func RecordRelayChannelUserQuotaUsage(ctx context.Context, relayInfo *relaycommon.RelayInfo, quota int) {
	if relayInfo == nil || relayInfo.ChannelMeta == nil || quota <= 0 {
		return
	}
	if err := RecordChannelUserModelQuotaUsage(ctx, relayInfo.ChannelId, relayInfo.UserId, quota, relayInfo.OriginModelName); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf(
			"记录渠道单用户周期额度失败: channel_id=%d user_id=%d quota=%d error=%s",
			relayInfo.ChannelId,
			relayInfo.UserId,
			quota,
			common.LocalLogPreview(err.Error()),
		))
	}
}
