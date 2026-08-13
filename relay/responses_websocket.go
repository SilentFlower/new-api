package relay

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const responsesWebSocketBeta = "responses_websockets=2026-02-06"

// DialResponsesWebSocket 建立到所选渠道的 Responses WebSocket 上游连接。
// @param c 当前 Gin 请求上下文。
// @param info 已完成渠道上下文和模型映射的 RelayInfo。
// @return 上游 WebSocket、握手响应和标准 Relay 错误。
func DialResponsesWebSocket(c *gin.Context, info *relaycommon.RelayInfo) (*websocket.Conn, *http.Response, *types.NewAPIError) {
	if c == nil || c.Request == nil || info == nil || info.ChannelMeta == nil {
		return nil, nil, types.NewErrorWithStatusCode(errors.New("Responses WebSocket relay info is incomplete"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	switch info.ApiType {
	case appconstant.APITypeOpenAI, appconstant.APITypeCodex, appconstant.APITypeAdvancedCustom:
	default:
		return nil, nil, types.NewErrorWithStatusCode(errors.New("channel does not support Responses WebSocket"), types.ErrorCodeChannelModelMappedError, http.StatusBadRequest)
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return nil, nil, types.NewError(errors.New("Responses WebSocket adaptor is unavailable"), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	targetURL, err := adaptor.GetRequestURL(info)
	if err != nil {
		return nil, nil, types.NewError(err, types.ErrorCodeChannelModelMappedError)
	}
	parsedURL, err := url.Parse(targetURL)
	if err != nil || parsedURL.Host == "" {
		return nil, nil, types.NewError(errors.New("invalid Responses WebSocket upstream URL"), types.ErrorCodeChannelModelMappedError)
	}
	switch strings.ToLower(parsedURL.Scheme) {
	case "https":
		parsedURL.Scheme = "wss"
	case "http":
		parsedURL.Scheme = "ws"
	case "wss", "ws":
	default:
		return nil, nil, types.NewError(errors.New("unsupported Responses WebSocket upstream URL scheme"), types.ErrorCodeChannelModelMappedError)
	}

	query := parsedURL.Query()
	for key, values := range c.Request.URL.Query() {
		if isResponsesClientQueryCredentialKey(key) {
			continue
		}
		if _, exists := query[key]; exists {
			continue
		}
		for _, value := range values {
			query.Add(key, value)
		}
	}
	parsedURL.RawQuery = query.Encode()
	info.UpstreamRequestURLPath = parsedURL.EscapedPath()

	targetHeader := http.Header{}
	if err := adaptor.SetupRequestHeader(c, &targetHeader, info); err != nil {
		return nil, nil, types.NewError(err, types.ErrorCodeChannelHeaderOverrideInvalid)
	}
	channel.CopyResponsesMetadataHeaders(c, &targetHeader)
	targetHeader.Set("Content-Type", "application/json")
	targetHeader.Set("OpenAI-Beta", responsesWebSocketBeta)
	if targetHeader.Get("originator") == "" {
		targetHeader.Set("originator", "codex_cli_rs")
	}
	headerOverride, err := channel.ResolveHeaderOverride(info, c)
	if err != nil {
		return nil, nil, types.NewError(err, types.ErrorCodeChannelHeaderOverrideInvalid)
	}
	for key, value := range headerOverride {
		if isGeneratedWebSocketHeader(key) {
			continue
		}
		targetHeader.Set(key, value)
	}

	logger.LogDebug(c, "dial Responses WebSocket upstream: %s", relaycommon.SanitizeURLForLog(parsedURL.String()))
	conn, resp, err := websocket.DefaultDialer.DialContext(c.Request.Context(), parsedURL.String(), targetHeader)
	if resp != nil {
		channel.CaptureResponsesMetadataHeaders(c, resp.Header)
		upstreamRequestID := strings.TrimSpace(resp.Header.Get("X-Request-Id"))
		if upstreamRequestID == "" {
			upstreamRequestID = strings.TrimSpace(resp.Header.Get(common.RequestIdKey))
		}
		if upstreamRequestID != "" {
			c.Set(common.UpstreamRequestIdKey, upstreamRequestID)
		}
	}
	if err != nil {
		statusCode := http.StatusBadGateway
		if resp != nil {
			statusCode = resp.StatusCode
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
		}
		return nil, resp, types.NewErrorWithStatusCode(errors.New("upstream Responses WebSocket handshake failed"), types.ErrorCodeDoRequestFailed, statusCode)
	}
	return conn, resp, nil
}

func isGeneratedWebSocketHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "connection", "upgrade", "host", "content-length",
		"sec-websocket-key", "sec-websocket-version", "sec-websocket-extensions", "sec-websocket-protocol":
		return true
	default:
		return false
	}
}
