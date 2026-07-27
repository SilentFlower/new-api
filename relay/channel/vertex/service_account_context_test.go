package vertex

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExchangeJwtForAccessTokenUsesRequestContext(t *testing.T) {
	service.InitHttpClient()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := exchangeJwtForAccessTokenWithProxyContext(ctx, "signed-jwt", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), context.Canceled.Error())
}
