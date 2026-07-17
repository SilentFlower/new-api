package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	appI18n "github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestValidateResponsesWebSocketModelAccessUsesSelectionModel(t *testing.T) {
	require.NoError(t, appI18n.Init())
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
	compactModel := ratio_setting.WithCompactModelSuffix("gpt-5")
	common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{
		ratio_setting.FormatMatchingModelName(compactModel): true,
	})

	require.Nil(t, ValidateResponsesWebSocketModelAccess(c, compactModel))
	require.NotNil(t, ValidateResponsesWebSocketModelAccess(c, "gpt-4.1"))
}

func TestResponsesCompactV2UsesBaseModelForTokenAccess(t *testing.T) {
	require.NoError(t, appI18n.Init())
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"compaction_trigger"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-Codex-Beta-Features", "remote_compaction_v2")
	common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
	common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{
		"gpt-5.6-sol": true,
	})
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	request, shouldSelect, err := getModelRequest(c)
	require.NoError(t, err)
	require.True(t, shouldSelect)
	require.Equal(t, "gpt-5.6-sol", request.Model)
	require.Nil(t, ValidateResponsesWebSocketModelAccess(c, request.Model))
}

func TestSelectResponsesWebSocketChannelValidatesModelBeforeSelection(t *testing.T) {
	require.NoError(t, appI18n.Init())
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
	common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{
		"allowed-model": true,
	})

	channel, apiErr := SelectResponsesWebSocketChannel(c, "forbidden-model")

	require.Nil(t, channel)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
}

func TestResponsesWebSocketChannelSupportsModel(t *testing.T) {
	allowedAdvancedChannel := &model.Channel{Type: constant.ChannelTypeAdvancedCustom}
	allowedAdvancedChannel.SetOtherSettings(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
			{IncomingPath: "/v1/responses", Models: []string{"gpt-5"}},
		}},
	})
	wrongPathAdvancedChannel := &model.Channel{Type: constant.ChannelTypeAdvancedCustom}
	wrongPathAdvancedChannel.SetOtherSettings(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
			{IncomingPath: "/v1/chat/completions", Models: []string{"gpt-5"}},
		}},
	})

	tests := []struct {
		name      string
		channel   *model.Channel
		modelName string
		expected  bool
	}{
		{name: "nil channel", modelName: "gpt-5"},
		{name: "ordinary channel", channel: &model.Channel{Type: constant.ChannelTypeOpenAI}, modelName: "gpt-5", expected: true},
		{name: "advanced custom route matches", channel: allowedAdvancedChannel, modelName: "gpt-5", expected: true},
		{name: "advanced custom model mismatches", channel: allowedAdvancedChannel, modelName: "gpt-4.1"},
		{name: "advanced custom path mismatches", channel: wrongPathAdvancedChannel, modelName: "gpt-5"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expected, ResponsesWebSocketChannelSupportsModel(test.channel, test.modelName))
		})
	}
}

func TestSelectResponsesWebSocketChannelUsesSpecifiedChannelAndInitializesContext(t *testing.T) {
	require.NoError(t, appI18n.Init())
	gin.SetMode(gin.TestMode)
	db := setupDistributorTestDatabase(t)
	channel := &model.Channel{
		Id:     7201,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "specified-key",
		Status: common.ChannelStatusEnabled,
		Name:   "specified-channel",
		Models: "gpt-5",
		Group:  "default",
	}
	disabledChannel := &model.Channel{
		Id:     7202,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "disabled-key",
		Status: common.ChannelStatusManuallyDisabled,
		Name:   "disabled-specified-channel",
		Models: "gpt-5",
		Group:  "default",
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(disabledChannel).Error)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, channel.Id)

	selected, apiErr := SelectResponsesWebSocketChannel(c, "gpt-5")

	require.Nil(t, apiErr)
	require.NotNil(t, selected)
	require.Equal(t, channel.Id, selected.Id)
	require.Equal(t, channel.Id, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
	require.Equal(t, "specified-key", common.GetContextKeyString(c, constant.ContextKeyChannelKey))

	disabledContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	disabledContext.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	common.SetContextKey(disabledContext, constant.ContextKeyTokenSpecificChannelId, disabledChannel.Id)

	selected, apiErr = SelectResponsesWebSocketChannel(disabledContext, "gpt-5")

	require.Nil(t, selected)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeGetChannelFailed, apiErr.GetErrorCode())
}

func TestSelectResponsesWebSocketChannelClearsUnsupportedAffinityAndFiltersRandomCandidates(t *testing.T) {
	require.NoError(t, appI18n.Init())
	gin.SetMode(gin.TestMode)
	db := setupDistributorTestDatabase(t)

	invalidAdvancedChannel := &model.Channel{
		Id:       7301,
		Type:     constant.ChannelTypeAdvancedCustom,
		Key:      "advanced-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "advanced-wrong-path",
		Models:   "gpt-5",
		Group:    "default",
		Priority: common.GetPointer[int64](100),
	}
	invalidAdvancedChannel.SetOtherSettings(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
			{IncomingPath: "/v1/chat/completions", Models: []string{"gpt-5"}},
		}},
	})
	validChannel := &model.Channel{
		Id:       7302,
		Type:     constant.ChannelTypeOpenAI,
		Key:      "openai-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "openai-responses",
		Models:   "gpt-5",
		Group:    "default",
		Priority: common.GetPointer[int64](0),
	}
	require.NoError(t, db.Create(invalidAdvancedChannel).Error)
	require.NoError(t, db.Create(validChannel).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gpt-5", ChannelId: invalidAdvancedChannel.Id, Enabled: true, Priority: invalidAdvancedChannel.Priority}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gpt-5", ChannelId: validChannel.Id, Enabled: true, Priority: validChannel.Priority}).Error)
	model.InitChannelCache()

	affinityRule := operation_setting.ChannelAffinityRule{
		Name:       "responses-websocket-stale-affinity",
		ModelRegex: []string{"^gpt-5$"},
		PathRegex:  []string{"^/v1/responses$"},
		KeySources: []operation_setting.ChannelAffinityKeySource{
			{Type: "request_header", Key: "X-Test-Stale-Affinity"},
		},
		IncludeUsingGroup: true,
		IncludeModelName:  true,
		IncludeRuleName:   true,
	}
	affinitySetting := operation_setting.GetChannelAffinitySetting()
	originalAffinitySetting := *affinitySetting
	affinitySetting.Enabled = true
	affinitySetting.SwitchOnSuccess = true
	affinitySetting.KeepOnChannelDisabled = false
	affinitySetting.Rules = []operation_setting.ChannelAffinityRule{affinityRule}
	t.Cleanup(func() { *affinitySetting = originalAffinitySetting })

	affinityValue := fmt.Sprintf("responses-websocket-stale-%d", time.Now().UnixNano())
	primeContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	primeContext.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	primeContext.Request.Header.Set("X-Test-Stale-Affinity", affinityValue)
	_, found := service.GetPreferredChannelByAffinity(primeContext, "gpt-5", "default")
	require.False(t, found)
	service.RecordChannelAffinity(primeContext, invalidAdvancedChannel.Id)
	t.Cleanup(func() { service.ClearCurrentChannelAffinityCache(primeContext) })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Request.Header.Set("X-Test-Stale-Affinity", affinityValue)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")

	selected, apiErr := SelectResponsesWebSocketChannel(c, "gpt-5")

	require.Nil(t, apiErr)
	require.NotNil(t, selected)
	require.Equal(t, validChannel.Id, selected.Id)
	_, found = service.GetPreferredChannelByAffinity(c, "gpt-5", "default")
	require.False(t, found)
}

func TestSelectResponsesWebSocketChannelUsesAffinityWithAutoGroup(t *testing.T) {
	require.NoError(t, appI18n.Init())
	gin.SetMode(gin.TestMode)
	db := setupDistributorTestDatabase(t)

	preferredChannel := &model.Channel{
		Id:       7401,
		Type:     constant.ChannelTypeOpenAI,
		Key:      "preferred-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "preferred-channel",
		Models:   "gpt-5",
		Group:    "default",
		Priority: common.GetPointer[int64](0),
	}
	fallbackChannel := &model.Channel{
		Id:       7402,
		Type:     constant.ChannelTypeOpenAI,
		Key:      "fallback-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "fallback-channel",
		Models:   "gpt-5",
		Group:    "default",
		Priority: common.GetPointer[int64](100),
	}
	require.NoError(t, db.Create(preferredChannel).Error)
	require.NoError(t, db.Create(fallbackChannel).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gpt-5", ChannelId: preferredChannel.Id, Enabled: true, Priority: preferredChannel.Priority}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gpt-5", ChannelId: fallbackChannel.Id, Enabled: true, Priority: fallbackChannel.Priority}).Error)
	model.InitChannelCache()

	affinityRule := operation_setting.ChannelAffinityRule{
		Name:       "responses-websocket-test",
		ModelRegex: []string{"^gpt-5$"},
		PathRegex:  []string{"^/v1/responses$"},
		KeySources: []operation_setting.ChannelAffinityKeySource{
			{Type: "request_header", Key: "X-Test-Affinity"},
		},
		IncludeUsingGroup: true,
		IncludeModelName:  true,
		IncludeRuleName:   true,
	}
	affinitySetting := operation_setting.GetChannelAffinitySetting()
	originalAffinitySetting := *affinitySetting
	affinitySetting.Enabled = true
	affinitySetting.SwitchOnSuccess = true
	affinitySetting.KeepOnChannelDisabled = false
	affinitySetting.Rules = []operation_setting.ChannelAffinityRule{affinityRule}
	t.Cleanup(func() { *affinitySetting = originalAffinitySetting })

	affinityValue := fmt.Sprintf("responses-websocket-%d", time.Now().UnixNano())
	primeContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	primeContext.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	primeContext.Request.Header.Set("X-Test-Affinity", affinityValue)
	_, found := service.GetPreferredChannelByAffinity(primeContext, "gpt-5", "auto")
	require.False(t, found)
	service.RecordChannelAffinity(primeContext, preferredChannel.Id)
	t.Cleanup(func() { service.ClearCurrentChannelAffinityCache(primeContext) })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Request.Header.Set("X-Test-Affinity", affinityValue)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "auto")
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")

	selected, apiErr := SelectResponsesWebSocketChannel(c, "gpt-5")

	require.Nil(t, apiErr)
	require.NotNil(t, selected)
	require.Equal(t, preferredChannel.Id, selected.Id)
	require.Equal(t, "default", common.GetContextKeyString(c, constant.ContextKeyAutoGroup))
}
