package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	appI18n "github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestValidateTokenModelAccessUsesCompactSelectionModel(t *testing.T) {
	require.NoError(t, appI18n.Init())
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
	compactModel := ratio_setting.WithCompactModelSuffix("gpt-5")
	common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{
		ratio_setting.FormatMatchingModelName(compactModel): true,
	})

	require.Nil(t, ValidateTokenModelAccess(c, compactModel))
	require.NotNil(t, ValidateTokenModelAccess(c, "gpt-4.1"))
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
	require.Nil(t, ValidateTokenModelAccess(c, request.Model))
}

func TestAbortWithDistributorErrorPreservesHTTPErrorCodeContract(t *testing.T) {
	tests := []struct {
		name         string
		errorCode    types.ErrorCode
		expectedCode string
	}{
		{name: "token model access", errorCode: types.ErrorCodeAccessDenied},
		{name: "invalid request", errorCode: types.ErrorCodeInvalidRequest},
		{name: "specified channel disabled", errorCode: types.ErrorCodeGetChannelFailed},
		{name: "no available channel", errorCode: types.ErrorCodeModelNotFound, expectedCode: string(types.ErrorCodeModelNotFound)},
	}

	gin.SetMode(gin.TestMode)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			apiErr := types.NewErrorWithStatusCode(errors.New("distributor failed"), test.errorCode, http.StatusForbidden)

			abortWithDistributorError(c, apiErr)

			require.Equal(t, http.StatusForbidden, recorder.Code)
			require.Equal(t, test.expectedCode, gjson.Get(recorder.Body.String(), "error.code").String())
		})
	}
}

func TestSelectAndSetupChannelStillValidatesModelWhenSelectionIsSkipped(t *testing.T) {
	require.NoError(t, appI18n.Init())
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-1", nil)
	common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
	common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{
		"allowed-model": true,
	})

	channel, apiErr := SelectAndSetupChannel(c, &ModelRequest{Model: "forbidden-model"}, false)

	require.Nil(t, channel)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
}
