package helper

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type keepAliveSignalWriter struct {
	gin.ResponseWriter
	written chan struct{}
	once    sync.Once
}

func (w *keepAliveSignalWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.once.Do(func() {
		close(w.written)
	})
	return n, err
}

func TestSupportsNonStreamKeepAlive(t *testing.T) {
	tests := []struct {
		name   string
		info   *relaycommon.RelayInfo
		allows bool
	}{
		{name: "nil info", info: nil, allows: false},
		{name: "stream image", info: &relaycommon.RelayInfo{IsStream: true, RelayFormat: types.RelayFormatOpenAIImage, RelayMode: relayconstant.RelayModeImagesGenerations}, allows: false},
		{name: "chat completions", info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions}, allows: true},
		{name: "completions", info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeCompletions}, allows: true},
		{name: "moderations", info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeModerations}, allows: true},
		{name: "responses", info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIResponses, RelayMode: relayconstant.RelayModeResponses}, allows: true},
		{name: "responses compaction", info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIResponsesCompaction, RelayMode: relayconstant.RelayModeResponsesCompact}, allows: true},
		{name: "claude messages", info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, RelayMode: relayconstant.RelayModeUnknown}, allows: true},
		{name: "gemini", info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatGemini, RelayMode: relayconstant.RelayModeGemini}, allows: true},
		{name: "embedding", info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatEmbedding, RelayMode: relayconstant.RelayModeEmbeddings}, allows: true},
		{name: "rerank", info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatRerank, RelayMode: relayconstant.RelayModeRerank}, allows: true},
		{name: "image generation", info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIImage, RelayMode: relayconstant.RelayModeImagesGenerations}, allows: true},
		{name: "image edit", info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIImage, RelayMode: relayconstant.RelayModeImagesEdits}, allows: true},
		{name: "legacy image edit", info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIImage, RelayMode: relayconstant.RelayModeEdits}, allows: true},
		{name: "audio", info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIAudio, RelayMode: relayconstant.RelayModeAudioSpeech}, allows: false},
		{name: "realtime", info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIRealtime, RelayMode: relayconstant.RelayModeRealtime}, allows: false},
		{name: "unknown", info: &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeUnknown}, allows: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.allows, supportsNonStreamKeepAlive(test.info))
		})
	}
}

func TestNonStreamKeepAliveWritesWhitespaceBeforeFinalJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	signalWriter := &keepAliveSignalWriter{ResponseWriter: c.Writer, written: make(chan struct{})}
	c.Writer = signalWriter
	ticks := make(chan time.Time, 1)
	stop := startNonStreamKeepAlive(c, ticks, nil)
	defer stop()

	ticks <- time.Now()
	select {
	case <-signalWriter.written:
	case <-time.After(time.Second):
		t.Fatal("空白心跳未写入")
	}

	c.JSON(http.StatusOK, gin.H{"data": []string{"image"}})
	body := recorder.Body.String()
	assert.True(t, strings.HasPrefix(body, "\n"))
	assert.JSONEq(t, `{"data":["image"]}`, strings.TrimSpace(body))
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "no", recorder.Header().Get("X-Accel-Buffering"))
}

func TestNonStreamKeepAliveDoesNotWriteAfterFinalResponseStarts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ticks := make(chan time.Time, 1)
	stop := startNonStreamKeepAlive(c, ticks, nil)

	c.JSON(http.StatusCreated, gin.H{"ok": true})
	ticks <- time.Now()
	stop()

	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.JSONEq(t, `{"ok":true}`, recorder.Body.String())
	assert.False(t, strings.HasPrefix(recorder.Body.String(), "\n"))
}

func TestNonStreamKeepAliveStopsBeforeBusinessHeaderAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ticks := make(chan time.Time, 1)
	stop := startNonStreamKeepAlive(c, ticks, nil)

	c.Writer.Header().Set("X-Business-Response", "ready")
	ticks <- time.Now()
	stop()

	assert.Equal(t, "ready", recorder.Header().Get("X-Business-Response"))
	assert.Empty(t, recorder.Body.String())
}

func TestNonStreamKeepAliveStopsAfterRequestCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	requestContext, cancel := context.WithCancel(context.Background())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(requestContext)
	ticks := make(chan time.Time, 1)
	stop := startNonStreamKeepAlive(c, ticks, nil)
	writer, ok := c.Writer.(*nonStreamKeepAliveWriter)
	require.True(t, ok)

	cancel()
	select {
	case <-writer.done:
	case <-time.After(time.Second):
		t.Fatal("请求取消后心跳协程未退出")
	}
	stop()
	ticks <- time.Now()
	assert.Empty(t, recorder.Body.String())
}

func TestNonStreamKeepAliveReturnsErrorJSONWithCommittedStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		format   types.RelayFormat
		expected string
	}{
		{name: "openai", format: types.RelayFormatOpenAI, expected: `{"error":{"message":"upstream failed","type":"new_api_error","param":"","code":"bad_response"}}`},
		{name: "gemini", format: types.RelayFormatGemini, expected: `{"error":{"message":"upstream failed","type":"new_api_error","param":"","code":"bad_response"}}`},
		{name: "claude", format: types.RelayFormatClaude, expected: `{"type":"error","error":{"type":"new_api_error","message":"upstream failed"}}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Writer.Header().Set(common.RequestIdKey, "request-keepalive")
			signalWriter := &keepAliveSignalWriter{ResponseWriter: c.Writer, written: make(chan struct{})}
			c.Writer = signalWriter
			ticks := make(chan time.Time, 1)
			stop := startNonStreamKeepAlive(c, ticks, nil)
			defer stop()

			ticks <- time.Now()
			select {
			case <-signalWriter.written:
			case <-time.After(time.Second):
				t.Fatal("空白心跳未写入")
			}
			relayError := types.NewErrorWithStatusCode(errors.New("upstream failed"), types.ErrorCodeBadResponse, http.StatusBadGateway)
			if test.format == types.RelayFormatClaude {
				c.JSON(relayError.StatusCode, gin.H{
					"type":  "error",
					"error": relayError.ToClaudeError(),
				})
			} else {
				c.JSON(relayError.StatusCode, gin.H{"error": relayError.ToOpenAIError()})
			}

			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, "request-keepalive", recorder.Header().Get(common.RequestIdKey))
			assert.JSONEq(t, test.expected, recorder.Body.String())
		})
	}
}

func TestNonStreamKeepAliveFlushDoesNotFinishResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ready := make(chan struct{})
	ticks := make(chan time.Time, 1)
	releaseFinal := make(chan struct{})
	router := gin.New()
	router.POST("/v1/images/generations", func(c *gin.Context) {
		stop := startNonStreamKeepAlive(c, ticks, nil)
		defer stop()
		close(ready)
		<-releaseFinal
		c.JSON(http.StatusOK, gin.H{"data": []string{"done"}})
	})
	server := httptest.NewServer(router)
	defer server.Close()

	responseDone := make(chan *http.Response, 1)
	requestErr := make(chan error, 1)
	go func() {
		resp, err := http.Post(server.URL+"/v1/images/generations", "application/json", nil)
		if err != nil {
			requestErr <- err
			return
		}
		responseDone <- resp
	}()

	<-ready
	ticks <- time.Now()
	var response *http.Response
	select {
	case err := <-requestErr:
		require.NoError(t, err)
	case response = <-responseDone:
	case <-time.After(time.Second):
		t.Fatal("客户端未收到已刷新的心跳")
	}
	defer response.Body.Close()

	firstByte := make([]byte, 1)
	_, err := io.ReadFull(response.Body, firstByte)
	require.NoError(t, err)
	assert.Equal(t, "\n", string(firstByte))
	assert.Equal(t, int64(-1), response.ContentLength)

	readDone := make(chan []byte, 1)
	readErr := make(chan error, 1)
	go func() {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			readErr <- err
			return
		}
		readDone <- body
	}()
	select {
	case <-readDone:
		t.Fatal("心跳 Flush 后响应不应提前结束")
	case err := <-readErr:
		require.NoError(t, err)
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseFinal)
	select {
	case err := <-readErr:
		require.NoError(t, err)
	case body := <-readDone:
		assert.JSONEq(t, `{"data":["done"]}`, string(body))
	case <-time.After(time.Second):
		t.Fatal("最终 JSON 写入后响应未结束")
	}
}
