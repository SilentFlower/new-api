package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetModelRequestMarksResponsesCompactModes(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		body          string
		betaFeatures  string
		expectedMode  relayconstant.ResponsesCompactMode
		expectedModel string
	}{
		{
			name:          "v1 path",
			path:          "/v1/responses/compact",
			body:          `{"model":"gpt-5"}`,
			expectedMode:  relayconstant.ResponsesCompactModeV1Path,
			expectedModel: "gpt-5",
		},
		{
			name:          "v2 http reuses base model channel",
			path:          "/v1/responses",
			body:          `{"model":"gpt-5","stream":true,"input":[{"type":"compaction_trigger"}]}`,
			betaFeatures:  "remote_compaction_v2",
			expectedMode:  relayconstant.ResponsesCompactModeV2HTTP,
			expectedModel: "gpt-5",
		},
		{
			name:          "legacy bridge",
			path:          "/v1/responses",
			body:          `{"model":"gpt-5","stream":true,"input":[{"type":"compaction_trigger"}]}`,
			expectedMode:  relayconstant.ResponsesCompactModeV1BodyBridge,
			expectedModel: "gpt-5",
		},
		{
			name:          "ordinary responses",
			path:          "/v1/responses",
			body:          `{"model":"gpt-5","stream":true,"input":[{"type":"message"}]}`,
			expectedMode:  relayconstant.ResponsesCompactModeNone,
			expectedModel: "gpt-5",
		},
	}

	gin.SetMode(gin.TestMode)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			c.Request.Header.Set("Content-Type", "application/json")
			if test.betaFeatures != "" {
				c.Request.Header.Set("X-Codex-Beta-Features", test.betaFeatures)
			}
			t.Cleanup(func() { common.CleanupBodyStorage(c) })

			request, shouldSelect, err := getModelRequest(c)
			require.NoError(t, err)
			require.True(t, shouldSelect)
			assert.Equal(t, test.expectedModel, request.Model)
			mode, ok := common.GetContextKeyType[relayconstant.ResponsesCompactMode](c, constant.ContextKeyResponsesCompactMode)
			require.True(t, ok)
			assert.Equal(t, test.expectedMode, mode)
		})
	}
}
