package relay

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const responsesCompactPassthroughDisabledErrorCode types.ErrorCode = "responses_compact_passthrough_disabled"

var responsesCompactPassthroughHopByHopHeaders = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

type responsesCompactPassthroughUsage struct {
	InputTokens       *int                                     `json:"input_tokens"`
	OutputTokens      *int                                     `json:"output_tokens"`
	TotalTokens       *int                                     `json:"total_tokens"`
	InputTokenDetails *responsesCompactPassthroughTokenDetails `json:"input_tokens_details,omitempty"`
}

type responsesCompactPassthroughTokenDetails struct {
	CachedTokens     *int `json:"cached_tokens,omitempty"`
	CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
}

type responsesCompactPassthroughResponse struct {
	Usage json.RawMessage `json:"usage"`
}

type responsesCompactPassthroughStreamEvent struct {
	Type     string                                     `json:"type"`
	Response *responsesCompactPassthroughStreamResponse `json:"response,omitempty"`
	Item     *responsesCompactPassthroughStreamItem     `json:"item,omitempty"`
}

type responsesCompactPassthroughStreamResponse struct {
	Usage json.RawMessage `json:"usage"`
}

type responsesCompactPassthroughStreamItem struct {
	Type string `json:"type"`
}

type responsesCompactPassthroughObserver struct {
	terminalSeen    bool
	terminalSuccess bool
	terminalType    string
	usage           *dto.Usage
	usageValid      bool
}

// ShouldHandleResponsesCompactPassthrough 判断请求是否应进入独立 Compact 透传链路。
// @param info 当前 Relay 请求信息。
// @return 任一 Responses Compact 模式返回 true，普通 Responses 返回 false。
func ShouldHandleResponsesCompactPassthrough(info *relaycommon.RelayInfo) bool {
	return info != nil && info.IsResponsesCompact()
}

// PrepareResponsesCompactPassthrough 在渠道选定后执行能力门禁并固定基础模型计费上下文。
// @param c 当前 Gin 请求上下文。
// @param info 当前 Relay 请求信息。
// @return 渠道未开启透传或请求上下文非法时返回不可重试错误，否则返回 nil。
func PrepareResponsesCompactPassthrough(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if c == nil || info == nil || info.Request == nil || !ShouldHandleResponsesCompactPassthrough(info) {
		return nil
	}

	info.InitChannelMeta(c)
	if !info.ChannelSetting.ResponsesCompactPassthroughEnabled {
		return types.NewErrorWithStatusCode(
			errors.New("selected channel does not enable Responses Compact passthrough"),
			responsesCompactPassthroughDisabledErrorCode,
			http.StatusServiceUnavailable,
			types.ErrOptionWithSkipRetry(),
			types.ErrOptionWithNoRecordErrorLog(),
		)
	}

	baseModel := strings.TrimSpace(info.OriginModelName)
	if baseModel == "" {
		return types.NewErrorWithStatusCode(
			errors.New("Responses Compact model is required"),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	info.OriginModelName = baseModel
	info.UpstreamModelName = baseModel
	info.IsModelMapped = false
	info.ClearBillingModelName()
	info.Request.SetModelName(baseModel)
	return nil
}

// ResponsesCompactPassthroughHelper 原样转发 Compact HTTP 请求与响应，并按基础模型 usage 结算。
// @param c 当前 Gin 请求上下文。
// @param info 已通过能力门禁和预扣的 Relay 请求信息。
// @return 上游请求、响应读取或协议处理失败时返回标准 Relay 错误，否则返回 nil。
func ResponsesCompactPassthroughHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if c == nil || info == nil || info.ChannelMeta == nil {
		return types.NewErrorWithStatusCode(errors.New("Responses Compact passthrough context is incomplete"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	switch info.ApiType {
	case appconstant.APITypeOpenAI, appconstant.APITypeCodex, appconstant.APITypeAdvancedCustom:
	default:
		return types.NewErrorWithStatusCode(
			fmt.Errorf("unsupported Responses Compact passthrough api type: %d", info.ApiType),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
	}

	outboundInfo := responsesCompactPassthroughOutboundInfo(info)
	outboundInfo.UpstreamRequestBodySize = storage.Size()
	adaptor := GetAdaptor(outboundInfo.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", outboundInfo.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(outboundInfo)

	response, err := adaptor.DoRequest(c, outboundInfo, common.ReaderOnly(storage))
	info.UpstreamRequestURLPath = outboundInfo.UpstreamRequestURLPath
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	}
	httpResponse, ok := response.(*http.Response)
	if !ok || httpResponse == nil {
		return types.NewError(errors.New("Responses Compact upstream response is invalid"), types.ErrorCodeBadResponse)
	}
	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		apiErr := service.RelayErrorHandlerWithoutBodyLog(c.Request.Context(), httpResponse)
		service.ResetStatusCode(apiErr, c.GetString("status_code_mapping"))
		return apiErr
	}

	if strings.Contains(strings.ToLower(httpResponse.Header.Get("Content-Type")), "text/event-stream") {
		return relayResponsesCompactPassthroughStream(c, httpResponse, info)
	}
	return relayResponsesCompactPassthroughJSON(c, httpResponse, info)
}

func responsesCompactPassthroughOutboundInfo(info *relaycommon.RelayInfo) *relaycommon.RelayInfo {
	outboundInfo := *info
	outboundInfo.RequestURLPath = responsesCompactPassthroughOutboundRequestURLPath(info.RequestURLPath)
	if info.ResponsesCompactMode == relayconstant.ResponsesCompactModeV1BodyBridge {
		// 仅改变出站路径视图，原始 mode 继续用于审计历史 bridge。
		outboundInfo.ResponsesCompactMode = relayconstant.ResponsesCompactModeV2HTTP
		outboundInfo.RelayMode = relayconstant.RelayModeResponses
	}
	return &outboundInfo
}

func responsesCompactPassthroughOutboundRequestURLPath(requestURLPath string) string {
	parsedURL, err := url.Parse(requestURLPath)
	if err != nil {
		path, _, _ := strings.Cut(requestURLPath, "?")
		return path
	}

	query := parsedURL.Query()
	for key := range query {
		if isResponsesClientQueryCredentialKey(key) {
			// 客户端查询凭证不能覆盖或旁路所选渠道的上游认证。
			query.Del(key)
		}
	}
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String()
}

func relayResponsesCompactPassthroughJSON(c *gin.Context, response *http.Response, info *relaycommon.RelayInfo) *types.NewAPIError {
	defer service.CloseResponseBodyGracefully(response)
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
	}
	info.SetFirstResponseTime()

	var payload responsesCompactPassthroughResponse
	if err := common.Unmarshal(body, &payload); err != nil {
		logger.LogWarn(c, "Responses Compact passthrough JSON missing a readable usage object; refunding request")
		refundResponsesCompactPassthrough(c, info, "invalid_json_response")
		service.IOCopyBytesGracefully(c, safeResponsesCompactPassthroughResponse(c, response), body)
		return nil
	}
	usage, valid := ParseResponsesCompactPassthroughUsage(payload.Usage)
	service.IOCopyBytesGracefully(c, safeResponsesCompactPassthroughResponse(c, response), body)
	if !valid {
		logger.LogWarn(c, "Responses Compact passthrough JSON missing valid usage; refunding request")
		refundResponsesCompactPassthrough(c, info, "completed_without_usage")
		return nil
	}

	service.SetResponsesCompactAudit(c, info, "completed")
	service.PostTextConsumeQuota(c, info, usage, nil)
	return nil
}

func relayResponsesCompactPassthroughStream(c *gin.Context, response *http.Response, info *relaycommon.RelayInfo) *types.NewAPIError {
	defer service.CloseResponseBodyGracefully(response)
	copyResponsesCompactPassthroughHeaders(c, response)
	c.Writer.WriteHeader(response.StatusCode)

	reader := bufio.NewReader(response.Body)
	observer := &responsesCompactPassthroughObserver{}
	var dataLines []string
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			info.SetFirstResponseTime()
			if _, writeErr := c.Writer.Write(line); writeErr != nil {
				logger.LogWarn(c, "Responses Compact passthrough client stream closed; refunding request")
				refundResponsesCompactPassthrough(c, info, "client_disconnected")
				return nil
			}
			observeResponsesCompactPassthroughSSELine(observer, line, &dataLines, info)
			if flushErr := helper.FlushWriter(c); flushErr != nil {
				logger.LogWarn(c, "Responses Compact passthrough stream flush failed; refunding request")
				refundResponsesCompactPassthrough(c, info, "client_disconnected")
				return nil
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				logger.LogWarn(c, "Responses Compact passthrough upstream stream read failed; refunding request")
				refundResponsesCompactPassthrough(c, info, "stream_read_failed")
				return nil
			}
			if len(dataLines) > 0 {
				observer.observe(strings.Join(dataLines, "\n"), info)
			}
			break
		}
	}

	if observer.terminalSeen && observer.terminalSuccess && observer.usageValid {
		service.SetResponsesCompactAudit(c, info, "completed")
		service.PostTextConsumeQuota(c, info, observer.usage, nil)
		return nil
	}
	outcome := observer.terminalType
	if outcome == "" {
		outcome = "stream_incomplete"
	}
	logger.LogWarn(c, fmt.Sprintf("Responses Compact passthrough ended without billable usage: outcome=%s", outcome))
	refundResponsesCompactPassthrough(c, info, outcome)
	return nil
}

func copyResponsesCompactPassthroughHeaders(c *gin.Context, response *http.Response) {
	safeResponse := safeResponsesCompactPassthroughResponse(c, response)
	for name, values := range safeResponse.Header {
		c.Writer.Header().Del(name)
		for _, value := range values {
			c.Writer.Header().Add(name, value)
		}
	}
	c.Writer.Header().Del("Content-Length")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
}

func safeResponsesCompactPassthroughResponse(c *gin.Context, response *http.Response) *http.Response {
	cloned := new(http.Response)
	*cloned = *response
	cloned.Header = make(http.Header)
	connectionHeaders := make(map[string]struct{})
	for _, value := range response.Header.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			name = http.CanonicalHeaderKey(strings.TrimSpace(name))
			if name != "" {
				connectionHeaders[name] = struct{}{}
			}
		}
	}
	for name, values := range response.Header {
		canonicalName := http.CanonicalHeaderKey(name)
		if _, skip := responsesCompactPassthroughHopByHopHeaders[canonicalName]; skip {
			continue
		}
		if _, skip := connectionHeaders[canonicalName]; skip {
			continue
		}
		if !service.ShouldCopyUpstreamHeader(c, name, values) {
			continue
		}
		for _, value := range values {
			cloned.Header.Add(name, value)
		}
	}
	return cloned
}

func observeResponsesCompactPassthroughSSELine(observer *responsesCompactPassthroughObserver, line []byte, dataLines *[]string, info *relaycommon.RelayInfo) {
	trimmed := bytes.TrimSuffix(line, []byte("\n"))
	trimmed = bytes.TrimSuffix(trimmed, []byte("\r"))
	if len(trimmed) == 0 {
		if len(*dataLines) > 0 {
			observer.observe(strings.Join(*dataLines, "\n"), info)
			*dataLines = (*dataLines)[:0]
		}
		return
	}
	if !bytes.HasPrefix(trimmed, []byte("data:")) {
		return
	}
	data := trimmed[len("data:"):]
	if len(data) > 0 && data[0] == ' ' {
		data = data[1:]
	}
	*dataLines = append(*dataLines, string(data))
}

func (observer *responsesCompactPassthroughObserver) observe(data string, info *relaycommon.RelayInfo) {
	if observer == nil || data == "" || data == "[DONE]" {
		return
	}
	var event responsesCompactPassthroughStreamEvent
	if err := common.Unmarshal([]byte(data), &event); err != nil {
		return
	}
	if info != nil && info.ResponsesUsageInfo != nil {
		switch event.Type {
		case dto.ResponsesOutputTypeItemDone:
			info.ResponsesUsageInfo.OutputItemDoneCount++
			if event.Item != nil {
				if event.Item.Type == "compaction" {
					info.ResponsesUsageInfo.CompactionOutputItemCount++
				}
				if event.Item.Type == dto.BuildInCallWebSearchCall {
					if tool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; tool != nil {
						tool.CallCount++
					}
				}
			}
		}
	}

	switch event.Type {
	case "response.completed", "response.done":
		var rawUsage json.RawMessage
		if event.Response != nil {
			rawUsage = event.Response.Usage
		}
		observer.observeTerminal(event.Type, true, rawUsage, info)
	case "response.failed", "response.error", "response.incomplete", "response.cancelled", "response.canceled", "error":
		observer.observeTerminal(event.Type, false, nil, info)
	}
}

func (observer *responsesCompactPassthroughObserver) observeTerminal(eventType string, success bool, rawUsage json.RawMessage, info *relaycommon.RelayInfo) {
	if observer.terminalSeen {
		observer.terminalSuccess = false
		observer.terminalType = "multiple_terminal_events"
		observer.usage = nil
		observer.usageValid = false
		if info != nil && info.ResponsesUsageInfo != nil {
			info.ResponsesUsageInfo.ResponseCompleted = false
			info.ResponsesUsageInfo.TerminalEventType = observer.terminalType
		}
		return
	}

	observer.terminalSeen = true
	observer.terminalSuccess = success
	observer.terminalType = eventType
	observer.usage = nil
	observer.usageValid = false
	if success {
		observer.usage, observer.usageValid = ParseResponsesCompactPassthroughUsage(rawUsage)
	}
	if info != nil && info.ResponsesUsageInfo != nil {
		info.ResponsesUsageInfo.ResponseCompleted = success
		info.ResponsesUsageInfo.TerminalEventType = eventType
	}
}

// ParseResponsesCompactPassthroughUsage 解析并校验 Compact 响应中的完整 usage。
// @param raw 上游返回的 usage 原始 JSON。
// @return 完整且数值合法时返回标准 usage 和 true，否则返回 nil 和 false。
func ParseResponsesCompactPassthroughUsage(raw json.RawMessage) (*dto.Usage, bool) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, false
	}
	var parsed responsesCompactPassthroughUsage
	if err := common.Unmarshal(raw, &parsed); err != nil || parsed.InputTokens == nil || parsed.OutputTokens == nil || parsed.TotalTokens == nil {
		return nil, false
	}
	if *parsed.InputTokens < 0 || *parsed.InputTokens > common.MaxQuota ||
		*parsed.OutputTokens < 0 || *parsed.OutputTokens > common.MaxQuota ||
		*parsed.TotalTokens < 0 || *parsed.TotalTokens > common.MaxQuota {
		return nil, false
	}
	if int64(*parsed.InputTokens)+int64(*parsed.OutputTokens) != int64(*parsed.TotalTokens) {
		return nil, false
	}
	usage := &dto.Usage{
		PromptTokens:     *parsed.InputTokens,
		CompletionTokens: *parsed.OutputTokens,
		TotalTokens:      *parsed.TotalTokens,
	}
	if parsed.InputTokenDetails != nil {
		if parsed.InputTokenDetails.CachedTokens != nil {
			if *parsed.InputTokenDetails.CachedTokens < 0 || *parsed.InputTokenDetails.CachedTokens > common.MaxQuota {
				return nil, false
			}
			usage.PromptTokensDetails.CachedTokens = *parsed.InputTokenDetails.CachedTokens
		}
		if parsed.InputTokenDetails.CacheWriteTokens != nil {
			if *parsed.InputTokenDetails.CacheWriteTokens < 0 || *parsed.InputTokenDetails.CacheWriteTokens > common.MaxQuota {
				return nil, false
			}
			usage.PromptTokensDetails.CacheWriteTokens = *parsed.InputTokenDetails.CacheWriteTokens
		}
	}
	return usage, true
}

func refundResponsesCompactPassthrough(c *gin.Context, info *relaycommon.RelayInfo, outcome string) {
	service.SetResponsesCompactAudit(c, info, outcome)
	if info != nil && info.Billing != nil {
		info.Billing.Refund(c)
	}
}
