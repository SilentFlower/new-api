package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// RecordChannelUserQuotaUsage 同时记录指定渠道用户的日、周正向额度。
//
// @param ctx 请求上下文。
// @param channelID 实际记账的渠道 ID。
// @param userID 实际消费的用户 ID。
// @param quota 本次新增的正向额度。
// @return error 日或周状态任一写入失败时返回错误。
func RecordChannelUserQuotaUsage(ctx context.Context, channelID int, userID int, quota int) error {
	dailyErr := RecordChannelUserDailyQuota(ctx, channelID, userID, quota)
	weeklyErr := RecordChannelUserWeeklyQuota(ctx, channelID, userID, quota)
	return errors.Join(dailyErr, weeklyErr)
}

// RecordRelayChannelUserQuotaUsage 同时记录 Relay 已完成的日、周正向额度。
//
// @param ctx 请求上下文。
// @param relayInfo 包含最终渠道和用户的 Relay 信息。
// @param quota 本次新增的正向额度。
// @return 无。
func RecordRelayChannelUserQuotaUsage(ctx context.Context, relayInfo *relaycommon.RelayInfo, quota int) {
	if relayInfo == nil || relayInfo.ChannelMeta == nil || quota <= 0 {
		return
	}
	if err := RecordChannelUserQuotaUsage(ctx, relayInfo.ChannelId, relayInfo.UserId, quota); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf(
			"记录渠道单用户周期额度失败: channel_id=%d user_id=%d quota=%d error=%s",
			relayInfo.ChannelId,
			relayInfo.UserId,
			quota,
			common.LocalLogPreview(err.Error()),
		))
	}
}
