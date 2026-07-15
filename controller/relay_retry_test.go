package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldRetryAfterKeepAliveCommittedResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	_, err := c.Writer.Write([]byte("\n"))
	require.NoError(t, err)
	relayErr := types.NewError(errors.New("channel unavailable"), types.ErrorCodeChannelNoAvailableKey)

	assert.True(t, shouldRetry(c, relayErr, 1))
}
