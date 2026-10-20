package relay

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupRelayChannelLimitParentDB 为真实选渠边界显式初始化限制关系和兼容预算表。
func setupRelayChannelLimitParentDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "relay-parent.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetDatabaseTypes(previousMain, previousLog)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelLimitParent{}, &model.ChannelUserLimitOverride{}, &model.ChannelPeriodPolicy{}, &model.ChannelQuotaTracking{}, &model.ChannelUserPeriodOverride{}, &model.ChannelUserBudgetOverride{}))
	return db
}

// setupRelayInheritedChannels 构造已绑定的辅助渠道，使用真实关系服务和内存计数。
func setupRelayInheritedChannels(t *testing.T) (*model.Channel, *model.Channel) {
	t.Helper()
	db := setupRelayChannelLimitParentDB(t)
	oldRedis, oldMemory := common.RedisEnabled, common.MemoryCacheEnabled
	common.RedisEnabled, common.MemoryCacheEnabled = false, false
	t.Cleanup(func() { common.RedisEnabled, common.MemoryCacheEnabled = oldRedis, oldMemory })
	parent := &model.Channel{Id: 9931, Name: "限制父渠道", UserConcurrencyLimit: common.GetPointer(4)}
	child := &model.Channel{Id: 9932, Name: "实际上游", Type: constant.ChannelTypeOpenAI, Key: "child-key", Status: common.ChannelStatusEnabled, BaseURL: common.GetPointer("https://upstream.example"), UserConcurrencyLimit: common.GetPointer(1)}
	require.NoError(t, db.Create(parent).Error)
	require.NoError(t, db.Create(child).Error)
	require.NoError(t, service.SaveChannelLimitParent(t.Context(), &model.ChannelLimitParent{ChannelId: child.Id, ParentChannelId: parent.Id, UpdatedBy: 1}))
	return parent, child
}

// TestChannelLimitParentMidjourneyOriginKeepsPhysicalUpstream 验证历史操作切渠后只将限制累计归父。
func TestChannelLimitParentMidjourneyOriginKeepsPhysicalUpstream(t *testing.T) {
	parent, child := setupRelayInheritedChannels(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/change", nil)
	common.SetContextKey(c, constant.ContextKeyUserId, 9933)
	info := &relaycommon.RelayInfo{UserId: 9933, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 9930}}
	require.Nil(t, applyMidjourneyOriginChannel(c, info, child))
	assert.Equal(t, child.Id, info.ChannelId)
	assert.Equal(t, parent.Id, info.ChannelLimitOwnerID)
	assert.Equal(t, "Bearer child-key", c.Request.Header.Get("Authorization"))
	service.RecordRelayChannelUserQuotaUsage(t.Context(), info, 250)
	parentUsed, _, _, err := service.GetChannelUserDailyQuotaUsage(t.Context(), parent.Id, 9933)
	require.NoError(t, err)
	childUsed, _, _, err := service.GetChannelUserDailyQuotaUsage(t.Context(), child.Id, 9933)
	require.NoError(t, err)
	assert.EqualValues(t, 250, parentUsed)
	assert.Zero(t, childUsed)
}

// TestChannelLimitParentVisionAssistRestoresMainOwner 验证辅助调用冻结自己的归属并恢复主请求上下文。
func TestChannelLimitParentVisionAssistRestoresMainOwner(t *testing.T) {
	parent, child := setupRelayInheritedChannels(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyUserId, 9934)
	common.SetContextKey(c, constant.ContextKeyChannelId, 9970)
	common.SetContextKey(c, constant.ContextKeyChannelLimitOwnerID, 9971)
	restore, apiErr := switchContextToVisionAssistChannel(c, child, "vision-model")
	require.Nil(t, apiErr)
	t.Cleanup(restore)
	info := &relaycommon.RelayInfo{UserId: 9934}
	info.InitChannelMeta(c)
	assert.Equal(t, child.Id, info.ChannelId)
	assert.Equal(t, parent.Id, info.ChannelLimitOwnerID)
	assert.Equal(t, 4, common.GetContextKeyInt(c, constant.ContextKeyChannelUserConcurrencyLimit))
	restore()
	assert.Equal(t, 9970, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
	assert.Equal(t, 9971, service.ChannelLimitOwnerID(c))
	service.RecordRelayChannelUserQuotaUsage(t.Context(), info, 30)
	used, _, _, err := service.GetChannelUserDailyQuotaUsage(t.Context(), parent.Id, 9934)
	require.NoError(t, err)
	assert.EqualValues(t, 30, used)
}
