package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchTaskWithContextCancelsGeminiRequest(t *testing.T) {
	service.InitHttpClient()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	adaptor := &TaskAdaptor{}
	taskID := taskcommon.EncodeLocalTaskID("operations/test-operation")

	_, err := adaptor.FetchTaskWithContext(ctx, server.URL, "test-key", map[string]any{"task_id": taskID}, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), context.Canceled.Error())
	assert.Zero(t, calls.Load())
}
