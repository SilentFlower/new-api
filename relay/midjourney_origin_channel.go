package relay

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func applyMidjourneyOriginChannel(c *gin.Context, relayInfo *relaycommon.RelayInfo, channel *model.Channel) {
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, channel.GetBaseURL())
	common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
	common.SetContextKey(c, constant.ContextKeyChannelType, channel.Type)
	common.SetContextKey(c, constant.ContextKeyChannelKey, channel.Key)
	limits := service.ApplyChannelUserEffectiveLimits(c, channel)

	relayInfo.ChannelBaseUrl = channel.GetBaseURL()
	relayInfo.ChannelId = channel.Id
	relayInfo.ChannelType = channel.Type
	relayInfo.ApiKey = channel.Key
	relayInfo.ChannelUserDailyQuotaLimit = limits.EffectiveDailyQuota
	relayInfo.ChannelUserWeeklyQuotaLimit = limits.EffectiveWeeklyQuota
	c.Request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", channel.Key))
}
