package jsplugin

import (
	"context"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"net/http"
	"net/http/httptest"
	"os"
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
	source, err := os.ReadFile("../../../../plugins/tasks/google/plugin.js")
	require.NoError(t, err)
	plugin, err := pluginruntime.NewRegistry().Register(string(source), pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	taskID := taskcommon.EncodeLocalTaskID("operations/test-operation")

	_, err = adaptor.FetchTaskWithContext(ctx, server.URL, "test-key", &model.Task{TaskID: taskID}, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), context.Canceled.Error())
	assert.Zero(t, calls.Load())
}
