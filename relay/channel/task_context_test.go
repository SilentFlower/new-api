package channel

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type contextTaskFetcherStub struct {
	TaskAdaptor
	received context.Context
}

func (s *contextTaskFetcherStub) FetchTaskWithContext(ctx context.Context, _ string, _ string, _ map[string]any, _ string) (*http.Response, error) {
	s.received = ctx
	return nil, ctx.Err()
}

type legacyTaskFetcherStub struct {
	TaskAdaptor
	called bool
}

func (s *legacyTaskFetcherStub) FetchTask(_ string, _ string, _ map[string]any, _ string) (*http.Response, error) {
	s.called = true
	return &http.Response{StatusCode: http.StatusOK}, nil
}

func TestFetchTaskWithContextUsesContextAwareAdaptor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	adaptor := &contextTaskFetcherStub{}

	_, err := FetchTaskWithContext(ctx, adaptor, "https://example.com", "key", nil, "")

	require.ErrorIs(t, err, context.Canceled)
	assert.Same(t, ctx, adaptor.received)
}

func TestFetchTaskWithContextSupportsLegacyAdaptor(t *testing.T) {
	adaptor := &legacyTaskFetcherStub{}

	resp, err := FetchTaskWithContext(context.Background(), adaptor, "https://example.com", "key", nil, "")

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, adaptor.called)
}
