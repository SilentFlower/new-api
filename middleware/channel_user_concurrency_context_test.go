package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetupContextForSelectedChannelSetsUserConcurrencyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	limit := 4
	channel := &model.Channel{
		Id:                   80,
		Key:                  "test-key",
		UserConcurrencyLimit: &limit,
	}

	apiErr := SetupContextForSelectedChannel(c, channel, "test-model")

	require.Nil(t, apiErr)
	require.Equal(t, 4, common.GetContextKeyInt(c, constant.ContextKeyChannelUserConcurrencyLimit))
}
