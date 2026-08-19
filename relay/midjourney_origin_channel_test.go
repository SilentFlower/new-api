package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyMidjourneyOriginChannelAttributesDailyQuotaToOriginChannel(t *testing.T) {
	previousRedisEnabled, previousRedis := common.RedisEnabled, common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRedis
	})

	const initialChannelID = 9821
	const originChannelID = 9822
	const userID = 9823
	dailyLimit := 1000
	baseURL := "https://origin.example.com"
	channel := &model.Channel{
		Id:                  originChannelID,
		Type:                constant.ChannelTypeMidjourney,
		Key:                 "origin-key",
		BaseURL:             &baseURL,
		UserDailyQuotaLimit: &dailyLimit,
	}
	relayInfo := &relaycommon.RelayInfo{
		UserId: userID,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:                  initialChannelID,
			ChannelType:                constant.ChannelTypeMidjourneyPlus,
			ChannelBaseUrl:             "https://initial.example.com",
			ApiKey:                     "initial-key",
			ChannelUserDailyQuotaLimit: dailyLimit,
		},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/change", nil)

	require.NoError(t, service.SetChannelUserDailyQuota(t.Context(), initialChannelID, userID, 0))
	require.NoError(t, service.SetChannelUserDailyQuota(t.Context(), originChannelID, userID, 0))

	applyMidjourneyOriginChannel(c, relayInfo, channel)
	service.RecordRelayChannelUserDailyQuota(c, relayInfo, 250)

	assert.Equal(t, originChannelID, relayInfo.ChannelId)
	assert.Equal(t, originChannelID, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
	assert.Equal(t, baseURL, relayInfo.ChannelBaseUrl)
	assert.Equal(t, "Bearer origin-key", c.Request.Header.Get("Authorization"))

	initialUsed, err := service.CheckChannelUserDailyQuota(t.Context(), initialChannelID, userID, dailyLimit)
	require.NoError(t, err)
	assert.Zero(t, initialUsed)
	originUsed, err := service.CheckChannelUserDailyQuota(t.Context(), originChannelID, userID, dailyLimit)
	require.NoError(t, err)
	assert.Equal(t, int64(250), originUsed)
}
