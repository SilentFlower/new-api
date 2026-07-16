package helper

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

const responsesCompactSSEBridgeContextKey = "responses_compact_sse_bridge"

type responsesCompactSSEBridge struct {
	context              *gin.Context
	mutex                sync.Mutex
	stopOnce             sync.Once
	stop                 chan struct{}
	done                 chan struct{}
	finalResponseStarted bool
	payloadWritten       bool
}

// StartResponsesCompactSSEBridge 为历史 Responses Compact body signal 启动 SSE 心跳桥接。
// @param c 当前 Gin 请求上下文。
// @param info 当前 relay 请求信息。
// @return 幂等的停止函数；调用后会等待心跳协程退出。
func StartResponsesCompactSSEBridge(c *gin.Context, info *relaycommon.RelayInfo) func() {
	if c == nil || c.Writer == nil || c.Request == nil || info == nil ||
		info.ResponsesCompactMode != relayconstant.ResponsesCompactModeV1BodyBridge || !info.ResponsesClientStream {
		return func() {}
	}

	info.DisablePing = true
	setting := operation_setting.GetGeneralSetting()
	interval := DefaultPingInterval
	if setting != nil && setting.PingIntervalSeconds > 0 {
		interval = time.Duration(setting.PingIntervalSeconds) * time.Second
	}
	ticker := time.NewTicker(interval)
	return startResponsesCompactSSEBridge(c, ticker.C, ticker.Stop)
}

func startResponsesCompactSSEBridge(c *gin.Context, ticks <-chan time.Time, stopTicker func()) func() {
	if c == nil || c.Writer == nil || c.Request == nil || ticks == nil {
		if stopTicker != nil {
			stopTicker()
		}
		return func() {}
	}

	bridge := &responsesCompactSSEBridge{
		context: c,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	c.Set(responsesCompactSSEBridgeContextKey, bridge)
	logger.LogDebug(c, "Responses Compact SSE bridge started")

	gopool.Go(func() {
		defer close(bridge.done)
		if stopTicker != nil {
			defer stopTicker()
		}
		bridge.run(ticks)
	})

	return bridge.stopAndWait
}

func (b *responsesCompactSSEBridge) run(ticks <-chan time.Time) {
	for {
		select {
		case <-ticks:
			if !b.writePing() {
				return
			}
		case <-b.stop:
			return
		case <-b.context.Request.Context().Done():
			return
		}
	}
}

func (b *responsesCompactSSEBridge) setEventStreamHeadersLocked() {
	header := b.context.Writer.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("Transfer-Encoding", "chunked")
	header.Set("X-Accel-Buffering", "no")
}

func (b *responsesCompactSSEBridge) writePing() bool {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if b.finalResponseStarted || b.context.Request.Context().Err() != nil {
		return false
	}
	b.setEventStreamHeadersLocked()
	ExtendWriteDeadline(b.context)
	written, err := b.context.Writer.Write([]byte(": PING\n\n"))
	if written > 0 {
		b.payloadWritten = true
	}
	if err != nil {
		return false
	}
	b.context.Writer.Flush()
	return true
}

func (b *responsesCompactSSEBridge) beginFinalResponseLocked() {
	b.finalResponseStarted = true
	b.stopOnce.Do(func() {
		close(b.stop)
	})
}

func (b *responsesCompactSSEBridge) writeEvent(eventType string, payload any) error {
	jsonData, err := common.Marshal(payload)
	if err != nil {
		return err
	}
	eventData := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, jsonData))

	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.beginFinalResponseLocked()
	if b.context.Request.Context().Err() != nil {
		return b.context.Request.Context().Err()
	}
	b.setEventStreamHeadersLocked()
	ExtendWriteDeadline(b.context)
	written, err := b.context.Writer.Write(eventData)
	if written > 0 {
		b.payloadWritten = true
	}
	if err != nil {
		return err
	}
	b.context.Writer.Flush()
	return nil
}

func (b *responsesCompactSSEBridge) prepareFinalResponse() bool {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.beginFinalResponseLocked()
	return b.payloadWritten
}

func (b *responsesCompactSSEBridge) stopAndWait() {
	b.stopOnce.Do(func() {
		close(b.stop)
	})
	<-b.done
	logger.LogDebug(b.context, "Responses Compact SSE bridge stopped, payload_written=%t", b.payloadWritten)
}

func getResponsesCompactSSEBridge(c *gin.Context) (*responsesCompactSSEBridge, bool) {
	if c == nil {
		return nil, false
	}
	value, exists := c.Get(responsesCompactSSEBridgeContextKey)
	if !exists {
		return nil, false
	}
	bridge, ok := value.(*responsesCompactSSEBridge)
	return bridge, ok && bridge != nil
}

// PrepareResponsesCompactSSEFinal 停止 Compact SSE 心跳并判断响应是否已经提交。
// @param c 当前 Gin 请求上下文。
// @return bridge 是否存在，以及是否已经写出心跳或业务事件。
func PrepareResponsesCompactSSEFinal(c *gin.Context) (bool, bool) {
	bridge, ok := getResponsesCompactSSEBridge(c)
	if !ok {
		return false, false
	}
	return true, bridge.prepareFinalResponse()
}

// WriteResponsesCompactSSECompleted 将 unary Compact JSON 合成为 Responses SSE 成功终态。
// @param c 当前 Gin 请求上下文。
// @param responseBody 上游返回的完整 Compact JSON。
// @param output 上游返回的 output 数组原始 JSON。
// @return 写入或 JSON 转换错误。
func WriteResponsesCompactSSECompleted(c *gin.Context, responseBody []byte, output json.RawMessage) error {
	bridge, ok := getResponsesCompactSSEBridge(c)
	if !ok {
		return errors.New("Responses Compact SSE bridge is not active")
	}

	var outputItems []json.RawMessage
	if len(output) > 0 {
		if err := common.Unmarshal(output, &outputItems); err != nil {
			return fmt.Errorf("decode Compact output failed: %w", err)
		}
	}
	outputIndex := 0
	for _, item := range outputItems {
		if common.GetJsonType(item) != "object" {
			continue
		}
		payload := map[string]any{
			"type":         "response.output_item.done",
			"output_index": outputIndex,
			"item":         item,
		}
		if err := bridge.writeEvent("response.output_item.done", payload); err != nil {
			return err
		}
		outputIndex++
	}

	var responseFields map[string]json.RawMessage
	if err := common.Unmarshal(responseBody, &responseFields); err != nil {
		return fmt.Errorf("decode Compact response failed: %w", err)
	}
	if len(responseFields["id"]) == 0 || string(responseFields["id"]) == "null" || string(responseFields["id"]) == `""` {
		requestID := c.GetString(common.RequestIdKey)
		if requestID == "" {
			requestID = common.NewRequestId()
		}
		responseFields["id"], _ = common.Marshal("resp_" + requestID)
	}
	usage, usageExists := responseFields["usage"]
	if !usageExists || common.GetJsonType(usage) == "null" {
		responseFields["usage"] = json.RawMessage(`{"input_tokens":0,"output_tokens":0,"total_tokens":0}`)
	} else if !responsesCompactUsageParsableByCodex(usage) {
		delete(responseFields, "usage")
	}
	normalizedResponse, err := common.Marshal(responseFields)
	if err != nil {
		return fmt.Errorf("encode Compact response failed: %w", err)
	}
	return bridge.writeEvent("response.completed", map[string]any{
		"type":     "response.completed",
		"response": json.RawMessage(normalizedResponse),
	})
}

func responsesCompactUsageParsableByCodex(usage json.RawMessage) bool {
	if common.GetJsonType(usage) != "object" {
		return false
	}
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(usage, &fields); err != nil {
		return false
	}
	for _, name := range []string{"input_tokens", "output_tokens", "total_tokens"} {
		value, exists := fields[name]
		if !exists || common.GetJsonType(value) != "number" {
			return false
		}
	}
	return true
}

// WriteResponsesCompactSSEFailed 写入心跳提交后的 Responses SSE 失败终态。
// @param c 当前 Gin 请求上下文。
// @param openAIError 对客户端安全的 OpenAI 错误对象。
// @return 写入或 JSON 转换错误。
func WriteResponsesCompactSSEFailed(c *gin.Context, openAIError types.OpenAIError) error {
	bridge, ok := getResponsesCompactSSEBridge(c)
	if !ok {
		return errors.New("Responses Compact SSE bridge is not active")
	}
	return bridge.writeEvent("response.failed", map[string]any{
		"type": "response.failed",
		"response": map[string]any{
			"status": "failed",
			"error":  openAIError,
		},
	})
}
