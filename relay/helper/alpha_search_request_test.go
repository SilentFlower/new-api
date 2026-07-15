package helper

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAndValidateAlphaSearchRequest(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantError      string
		wantMaxTokens  uint
		wantMaxPresent bool
	}{
		{
			name:           "保留显式零值并忽略未知字段",
			body:           `{"id":"search_1","model":"gpt-5","max_output_tokens":0,"future":{"enabled":false}}`,
			wantMaxTokens:  0,
			wantMaxPresent: true,
		},
		{
			name:      "缺少模型",
			body:      `{"id":"search_1"}`,
			wantError: "model is required",
		},
		{
			name:      "拒绝重复模型字段",
			body:      `{"model":"allowed-model","model":"other-model"}`,
			wantError: "model must be specified exactly once",
		},
		{
			name:      "最大输出 Token 超限",
			body:      fmt.Sprintf(`{"model":"gpt-5","max_output_tokens":%d}`, uint(maxTokensLimit)+1),
			wantError: "max_output_tokens is invalid",
		},
	}

	gin.SetMode(gin.TestMode)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(test.body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() {
				common.CleanupBodyStorage(ctx)
			})

			request, err := GetAndValidateAlphaSearchRequest(ctx)
			if test.wantError != "" {
				require.EqualError(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, request)
			assert.Equal(t, "gpt-5", request.Model)
			if test.wantMaxPresent {
				require.NotNil(t, request.MaxOutputTokens)
				assert.Equal(t, test.wantMaxTokens, *request.MaxOutputTokens)
			}
		})
	}
}
