package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturedResponsesCompactChannelTestRequest struct {
	path          string
	authorization string
	body          []byte
	err           error
}

func setupResponsesCompactChannelTest(t *testing.T, modelRatios map[string]float64) int {
	t.Helper()
	service.InitHttpClient()
	db := setupModelListControllerTestDB(t)
	user := &model.User{
		Id:       100,
		Username: "compact-test-user",
		Password: "compact-test-password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Quota:    100000,
		Group:    "default",
	}
	require.NoError(t, db.Create(user).Error)

	savedModelRatios := ratio_setting.ModelRatio2JSONString()
	ratioJSON, err := common.Marshal(modelRatios)
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(ratioJSON)))
	savedLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = savedLogConsumeEnabled
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModelRatios))
	})
	return user.Id
}

func newResponsesCompactChannelTestServer(t *testing.T, expectedPath string) (*httptest.Server, <-chan capturedResponsesCompactChannelTestRequest) {
	t.Helper()
	requests := make(chan capturedResponsesCompactChannelTestRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		requests <- capturedResponsesCompactChannelTestRequest{
			path:          r.URL.Path,
			authorization: r.Header.Get("Authorization"),
			body:          body,
			err:           err,
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != expectedPath {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"unexpected path"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"compact_1","usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}`))
	}))
	t.Cleanup(server.Close)
	return server, requests
}

func TestNormalizeResponsesCompactChannelTestModelUsesBaseModel(t *testing.T) {
	tests := []struct {
		name         string
		model        string
		endpointType string
		expected     string
	}{
		{
			name:         "基础模型保持不变",
			model:        "gpt-5.6-sol",
			endpointType: string(constant.EndpointTypeOpenAIResponseCompact),
			expected:     "gpt-5.6-sol",
		},
		{
			name:         "旧后缀配置归一为基础模型",
			model:        ratio_setting.WithCompactModelSuffix("gpt-5.6-sol"),
			endpointType: string(constant.EndpointTypeOpenAIResponseCompact),
			expected:     "gpt-5.6-sol",
		},
		{
			name:         "普通端点不改写模型",
			model:        ratio_setting.WithCompactModelSuffix("gpt-5.6-sol"),
			endpointType: string(constant.EndpointTypeOpenAIResponse),
			expected:     ratio_setting.WithCompactModelSuffix("gpt-5.6-sol"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeResponsesCompactChannelTestModel(tt.model, tt.endpointType))
		})
	}
}

func TestResponsesCompactChannelTestRejectsDisabledCapabilityBeforePricingAndRequest(t *testing.T) {
	userID := setupResponsesCompactChannelTest(t, map[string]float64{})
	baseURL := "http://127.0.0.1:1"
	channel := &model.Channel{
		Id:      201,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "channel-secret",
		BaseURL: &baseURL,
		Models:  "unpriced-compact-model",
		Group:   "default",
	}

	result := testChannel(
		context.Background(),
		channel,
		userID,
		"unpriced-compact-model",
		string(constant.EndpointTypeOpenAIResponseCompact),
		false,
	)

	require.Error(t, result.localErr)
	require.NotNil(t, result.newAPIError)
	assert.Equal(t, "responses_compact_passthrough_disabled", string(result.newAPIError.GetErrorCode()))
	assert.Equal(t, http.StatusServiceUnavailable, result.newAPIError.StatusCode)
}

func TestResponsesCompactChannelTestUsesBaseModelWithoutMappingOrOverride(t *testing.T) {
	const baseModel = "compact-test-base-model"
	userID := setupResponsesCompactChannelTest(t, map[string]float64{baseModel: 0})
	server, requests := newResponsesCompactChannelTestServer(t, "/v1/responses/compact")
	modelMapping := `{"compact-test-base-model":"mapped-model"}`
	paramOverride := `{"model":"overridden-model","temperature":1}`
	channel := &model.Channel{
		Id:            202,
		Type:          constant.ChannelTypeOpenAI,
		Key:           "channel-secret",
		BaseURL:       &server.URL,
		Models:        baseModel,
		Group:         "default",
		ModelMapping:  &modelMapping,
		ParamOverride: &paramOverride,
	}
	channel.SetSetting(dto.ChannelSettings{ResponsesCompactPassthroughEnabled: true})

	result := testChannel(
		context.Background(),
		channel,
		userID,
		baseModel,
		string(constant.EndpointTypeOpenAIResponseCompact),
		false,
	)

	require.NoError(t, result.localErr)
	require.Nil(t, result.newAPIError)
	captured := <-requests
	require.NoError(t, captured.err)
	assert.Equal(t, "/v1/responses/compact", captured.path)
	assert.Equal(t, "Bearer channel-secret", captured.authorization)
	assert.JSONEq(t, `{"model":"compact-test-base-model","input":[{"role":"user","content":"hi"}]}`, string(captured.body))
}

func TestResponsesCompactChannelTestSupportsCodexPath(t *testing.T) {
	const baseModel = "codex-compact-model"
	userID := setupResponsesCompactChannelTest(t, map[string]float64{baseModel: 0})
	server, requests := newResponsesCompactChannelTestServer(t, "/backend-api/codex/responses/compact")
	channel := &model.Channel{
		Id:      203,
		Type:    constant.ChannelTypeCodex,
		Key:     `{"access_token":"codex-access-token","account_id":"account-1"}`,
		BaseURL: &server.URL,
		Models:  baseModel,
		Group:   "default",
	}
	channel.SetSetting(dto.ChannelSettings{ResponsesCompactPassthroughEnabled: true})

	result := testChannel(
		context.Background(),
		channel,
		userID,
		baseModel,
		string(constant.EndpointTypeOpenAIResponseCompact),
		false,
	)

	require.NoError(t, result.localErr)
	require.Nil(t, result.newAPIError)
	captured := <-requests
	require.NoError(t, captured.err)
	assert.Equal(t, "/backend-api/codex/responses/compact", captured.path)
	assert.Equal(t, "Bearer codex-access-token", captured.authorization)
	assert.JSONEq(t, `{"model":"codex-compact-model","input":[{"role":"user","content":"hi"}]}`, string(captured.body))
}

func TestResponsesCompactChannelTestSupportsAdvancedCustomNativeRoute(t *testing.T) {
	const baseModel = "advanced-compact-model"
	userID := setupResponsesCompactChannelTest(t, map[string]float64{baseModel: 0})
	server, requests := newResponsesCompactChannelTestServer(t, "/native/compact")
	channel := &model.Channel{
		Id:      204,
		Type:    constant.ChannelTypeAdvancedCustom,
		Key:     "advanced-secret",
		BaseURL: &server.URL,
		Models:  baseModel,
		Group:   "default",
	}
	channel.SetSetting(dto.ChannelSettings{ResponsesCompactPassthroughEnabled: true})
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{
			Routes: []dto.AdvancedCustomRoute{
				{
					IncomingPath: "/v1/responses/compact",
					UpstreamPath: "/native/compact",
					Converter:    "none",
					Models:       []string{baseModel},
				},
			},
		},
	})

	result := testChannel(
		context.Background(),
		channel,
		userID,
		baseModel,
		string(constant.EndpointTypeOpenAIResponseCompact),
		false,
	)

	require.NoError(t, result.localErr)
	require.Nil(t, result.newAPIError)
	captured := <-requests
	require.NoError(t, captured.err)
	assert.Equal(t, "/native/compact", captured.path)
	assert.Equal(t, "Bearer advanced-secret", captured.authorization)
	assert.JSONEq(t, `{"model":"advanced-compact-model","input":[{"role":"user","content":"hi"}]}`, string(captured.body))
}
