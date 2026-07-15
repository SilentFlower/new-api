package helper

import (
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

var jsonKeepAlivePayload = []byte("\n")

type nonStreamKeepAliveWriter struct {
	gin.ResponseWriter
	context                 *gin.Context
	mutex                   sync.Mutex
	stopOnce                sync.Once
	stop                    chan struct{}
	done                    chan struct{}
	finalResponseStarted    bool
	keepAlivePayloadWritten bool
}

// StartNonStreamKeepAlive 为符合条件的非流式 JSON 请求启动空白心跳。
// @param c 当前 Gin 请求上下文。
// @param info 当前 relay 请求信息。
// @return 幂等的停止函数；调用后会等待心跳协程退出。
func StartNonStreamKeepAlive(c *gin.Context, info *relaycommon.RelayInfo) func() {
	setting := operation_setting.GetGeneralSetting()
	if setting == nil || !setting.NonStreamKeepAliveEnabled || !supportsNonStreamKeepAlive(info) {
		return func() {}
	}

	interval := time.Duration(setting.PingIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = DefaultPingInterval
	}
	ticker := time.NewTicker(interval)
	return startNonStreamKeepAlive(c, ticker.C, ticker.Stop)
}

func supportsNonStreamKeepAlive(info *relaycommon.RelayInfo) bool {
	if info == nil || info.IsStream {
		return false
	}

	switch info.RelayFormat {
	case types.RelayFormatClaude:
		return true
	case types.RelayFormatGemini:
		return info.RelayMode == relayconstant.RelayModeGemini
	case types.RelayFormatOpenAIResponses:
		return info.RelayMode == relayconstant.RelayModeResponses
	case types.RelayFormatOpenAIResponsesCompaction:
		return info.RelayMode == relayconstant.RelayModeResponsesCompact
	case types.RelayFormatOpenAIAlphaSearch:
		return info.RelayMode == relayconstant.RelayModeAlphaSearch
	case types.RelayFormatOpenAIImage:
		return info.RelayMode == relayconstant.RelayModeImagesGenerations ||
			info.RelayMode == relayconstant.RelayModeImagesEdits ||
			info.RelayMode == relayconstant.RelayModeEdits
	case types.RelayFormatEmbedding:
		return info.RelayMode == relayconstant.RelayModeEmbeddings
	case types.RelayFormatRerank:
		return info.RelayMode == relayconstant.RelayModeRerank
	case types.RelayFormatOpenAI:
		switch info.RelayMode {
		case relayconstant.RelayModeChatCompletions,
			relayconstant.RelayModeCompletions,
			relayconstant.RelayModeEmbeddings,
			relayconstant.RelayModeModerations:
			return true
		}
	}

	return false
}

func startNonStreamKeepAlive(c *gin.Context, ticks <-chan time.Time, stopTicker func()) func() {
	if c == nil || c.Writer == nil || c.Request == nil || ticks == nil {
		if stopTicker != nil {
			stopTicker()
		}
		return func() {}
	}

	writer := &nonStreamKeepAliveWriter{
		ResponseWriter: c.Writer,
		context:        c,
		stop:           make(chan struct{}),
		done:           make(chan struct{}),
	}
	c.Writer = writer
	logger.LogDebug(c, "non-stream keep-alive started")

	gopool.Go(func() {
		defer close(writer.done)
		if stopTicker != nil {
			defer stopTicker()
		}
		writer.run(ticks)
	})

	return writer.stopAndWait
}

func (w *nonStreamKeepAliveWriter) run(ticks <-chan time.Time) {
	for {
		select {
		case <-ticks:
			if !w.writeKeepAlivePayload() {
				return
			}
		case <-w.stop:
			return
		case <-w.context.Request.Context().Done():
			return
		}
	}
}

func (w *nonStreamKeepAliveWriter) writeKeepAlivePayload() bool {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.finalResponseStarted || w.context.Request.Context().Err() != nil {
		return false
	}

	header := w.ResponseWriter.Header()
	header.Set("Content-Type", "application/json")
	header.Set("X-Accel-Buffering", "no")
	header.Del("Content-Length")
	ExtendWriteDeadline(w.context)
	if _, err := w.ResponseWriter.Write(jsonKeepAlivePayload); err != nil {
		return false
	}
	w.ResponseWriter.Flush()
	w.keepAlivePayloadWritten = true
	return true
}

func (w *nonStreamKeepAliveWriter) stopAndWait() {
	w.stopOnce.Do(func() {
		close(w.stop)
	})
	<-w.done
	logger.LogDebug(w.context, "non-stream keep-alive stopped, payload_written=%t", w.NonStreamKeepAliveWritten())
}

func (w *nonStreamKeepAliveWriter) beginFinalResponseLocked() {
	w.finalResponseStarted = true
	w.stopOnce.Do(func() {
		close(w.stop)
	})
}

// Header 返回底层响应头，并在业务代码访问响应头前停止后续心跳。
// @return 可供最终响应读取或修改的 HTTP 响应头。
func (w *nonStreamKeepAliveWriter) Header() http.Header {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.beginFinalResponseLocked()
	return w.ResponseWriter.Header()
}

// BeginFinalResponse 在设置最终响应头之前停止后续空白心跳。
// @return 无返回值。
func (w *nonStreamKeepAliveWriter) BeginFinalResponse() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.beginFinalResponseLocked()
}

// NonStreamKeepAliveWritten 返回当前请求是否已经写出过空白心跳。
// @return 已写出至少一个空白心跳时返回 true。
func (w *nonStreamKeepAliveWriter) NonStreamKeepAliveWritten() bool {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.keepAlivePayloadWritten
}

// WriteHeader 串行写入最终 HTTP 状态码并终止后续心跳。
// @param code 最终 HTTP 状态码。
// @return 无返回值。
func (w *nonStreamKeepAliveWriter) WriteHeader(code int) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.beginFinalResponseLocked()
	if w.keepAlivePayloadWritten {
		return
	}
	w.ResponseWriter.WriteHeader(code)
}

// WriteHeaderNow 立即提交最终响应头并终止后续心跳。
// @return 无返回值。
func (w *nonStreamKeepAliveWriter) WriteHeaderNow() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.beginFinalResponseLocked()
	if w.keepAlivePayloadWritten {
		return
	}
	w.ResponseWriter.WriteHeaderNow()
}

// Write 串行写入最终响应字节并终止后续心跳。
// @param data 最终响应字节。
// @return 实际写入字节数和写入错误。
func (w *nonStreamKeepAliveWriter) Write(data []byte) (int, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.beginFinalResponseLocked()
	return w.ResponseWriter.Write(data)
}

// WriteString 串行写入最终响应字符串并终止后续心跳。
// @param data 最终响应字符串。
// @return 实际写入字节数和写入错误。
func (w *nonStreamKeepAliveWriter) WriteString(data string) (int, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.beginFinalResponseLocked()
	return w.ResponseWriter.WriteString(data)
}

// Flush 立即刷新最终响应并终止后续心跳。
// @return 无返回值。
func (w *nonStreamKeepAliveWriter) Flush() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.beginFinalResponseLocked()
	w.ResponseWriter.Flush()
}

// Unwrap 返回底层 HTTP writer，供 http.ResponseController 使用。
// @return 底层 HTTP ResponseWriter。
func (w *nonStreamKeepAliveWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
