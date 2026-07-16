package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	responsesWebSocketFirstFrameTimeout = 30 * time.Second
	responsesWebSocketWriteTimeout      = 30 * time.Second
	responsesWebSocketDefaultReadLimit  = 128 << 20
)

var responsesWebSocketUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var responsesWebSocketTurnConnector = connectResponsesWebSocketTurn

type responsesWebSocketTurn struct {
	messageType            int
	rawPayload             []byte
	baseModel              string
	selectionModel         string
	compactMode            relayconstant.ResponsesCompactMode
	info                   *relaycommon.RelayInfo
	retryIndex             int
	downstreamEventWritten bool
}

type responsesWebSocketReadResult struct {
	generation  int
	messageType int
	payload     []byte
	err         error
}

// RelayResponsesWebSocket 处理 GET /v1/responses 的首帧分发和双向 WebSocket 转发。
// @param c 已通过性能检查、Token 鉴权和请求限流的 Gin 上下文。
func RelayResponsesWebSocket(c *gin.Context) {
	if !websocket.IsWebSocketUpgrade(c.Request) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": types.NewErrorWithStatusCode(errors.New("WebSocket upgrade is required"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry()).ToOpenAIError(),
		})
		return
	}

	clientConn, err := responsesWebSocketUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.LogError(c, "upgrade Responses WebSocket failed: "+err.Error())
		return
	}
	defer clientConn.Close()
	clientConn.SetReadLimit(responsesWebSocketReadLimit())
	_ = clientConn.SetReadDeadline(time.Now().Add(responsesWebSocketFirstFrameTimeout))
	messageType, firstPayload, err := clientConn.ReadMessage()
	_ = clientConn.SetReadDeadline(time.Time{})
	if err != nil {
		writeResponsesWebSocketError(c, clientConn, types.NewErrorWithStatusCode(errors.New("read first response.create failed"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry()))
		return
	}

	turn, apiErr := parseResponsesWebSocketTurn(c, messageType, firstPayload)
	if apiErr != nil {
		writeResponsesWebSocketError(c, clientConn, apiErr)
		return
	}
	channel, apiErr := middleware.SelectAndSetupChannel(c, &middleware.ModelRequest{Model: turn.selectionModel}, true)
	if apiErr != nil {
		writeResponsesWebSocketError(c, clientConn, apiErr)
		return
	}
	common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
	addUsedChannel(c, channel.Id)

	upstreamConn, selectedChannel, apiErr := connectResponsesWebSocketTurn(c, turn, channel, 0)
	if apiErr != nil {
		refundResponsesWebSocketTurn(c, turn)
		writeResponsesWebSocketError(c, clientConn, apiErr)
		return
	}
	defer upstreamConn.Close()

	if apiErr := proxyResponsesWebSocket(c, clientConn, upstreamConn, selectedChannel, turn); apiErr != nil {
		logger.LogError(c, "Responses WebSocket relay ended with error: "+common.LocalLogPreview(apiErr.Error()))
	}
}

func responsesWebSocketReadLimit() int64 {
	if constant.MaxRequestBodyMB > 0 {
		return int64(constant.MaxRequestBodyMB) << 20
	}
	return responsesWebSocketDefaultReadLimit
}

func parseResponsesWebSocketTurn(c *gin.Context, messageType int, payload []byte) (*responsesWebSocketTurn, *types.NewAPIError) {
	if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
		return nil, types.NewErrorWithStatusCode(errors.New("response.create must be a text or binary JSON frame"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	var envelope struct {
		Type  string `json:"type"`
		Model string `json:"model"`
	}
	if err := common.Unmarshal(payload, &envelope); err != nil {
		return nil, types.NewErrorWithStatusCode(errors.New("invalid response.create JSON"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if envelope.Type != "response.create" {
		return nil, types.NewErrorWithStatusCode(errors.New("first WebSocket frame must be response.create"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	baseModel := strings.TrimSpace(envelope.Model)
	if baseModel == "" {
		return nil, types.NewErrorWithStatusCode(errors.New("response.create model is required"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	compactMode := relayhelper.DetectResponsesCompactMode(
		http.MethodGet,
		c.Request.URL.Path,
		c.Request.Header,
		payload,
		relayhelper.ResponsesTransportWebSocket,
	)
	selectionModel := baseModel
	if compactMode.IsCompact() {
		selectionModel = ratio_setting.WithCompactModelSuffix(baseModel)
	}
	return &responsesWebSocketTurn{
		messageType:    messageType,
		rawPayload:     append([]byte(nil), payload...),
		baseModel:      baseModel,
		selectionModel: selectionModel,
		compactMode:    compactMode,
	}, nil
}

func connectResponsesWebSocketTurn(c *gin.Context, turn *responsesWebSocketTurn, initialChannel *model.Channel, startRetry int) (*websocket.Conn, *model.Channel, *types.NewAPIError) {
	if turn == nil {
		return nil, nil, types.NewErrorWithStatusCode(errors.New("Responses WebSocket turn is nil"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	retryParam := &service.RetryParam{
		Ctx:         c,
		TokenGroup:  common.GetContextKeyString(c, constant.ContextKeyTokenGroup),
		ModelName:   turn.selectionModel,
		RequestPath: "/v1/responses",
		Retry:       common.GetPointer(startRetry),
	}
	var lastErr *types.NewAPIError
	for retryIndex := startRetry; retryIndex <= common.RetryTimes; retryIndex++ {
		turn.retryIndex = retryIndex
		var channel *model.Channel
		if retryIndex == 0 {
			channel = initialChannel
		} else {
			retryParam.Retry = common.GetPointer(retryIndex)
			var apiErr *types.NewAPIError
			channel, apiErr = getChannel(c, turn.info, retryParam)
			if apiErr != nil {
				lastErr = apiErr
				break
			}
			addUsedChannel(c, channel.Id)
		}
		if channel == nil {
			lastErr = types.NewError(errors.New("Responses WebSocket channel is nil"), types.ErrorCodeGetChannelFailed)
			break
		}

		upstreamPayload, apiErr := prepareResponsesWebSocketTurnAttempt(c, turn)
		if apiErr != nil {
			lastErr = apiErr
			if types.IsChannelError(apiErr) {
				processResponsesWebSocketChannelError(c, turn, channel, apiErr, "prepare_failed")
			}
			if !shouldRetry(c, apiErr, common.RetryTimes-retryIndex) {
				break
			}
			continue
		}
		upstreamConn, _, apiErr := relay.DialResponsesWebSocket(c, turn.info)
		if apiErr == nil {
			_ = upstreamConn.SetWriteDeadline(time.Now().Add(responsesWebSocketWriteTimeout))
			if err := upstreamConn.WriteMessage(turn.messageType, upstreamPayload); err != nil {
				_ = upstreamConn.Close()
				apiErr = types.NewErrorWithStatusCode(errors.New("write first response.create upstream failed"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
			}
			_ = upstreamConn.SetWriteDeadline(time.Time{})
		}
		if apiErr == nil {
			service.SetResponsesCompactAudit(c, turn.info, "connected")
			return upstreamConn, channel, nil
		}

		lastErr = apiErr
		processResponsesWebSocketChannelError(c, turn, channel, apiErr, "connect_failed")
		if !shouldRetry(c, apiErr, common.RetryTimes-retryIndex) {
			break
		}
	}
	if lastErr == nil {
		lastErr = types.NewError(errors.New("Responses WebSocket upstream unavailable"), types.ErrorCodeGetChannelFailed)
	}
	return nil, nil, lastErr
}

func processResponsesWebSocketChannelError(c *gin.Context, turn *responsesWebSocketTurn, channel *model.Channel, apiErr *types.NewAPIError, outcome string) {
	if apiErr == nil {
		return
	}
	if turn != nil && turn.info != nil {
		turn.info.LastError = apiErr
		service.SetResponsesCompactAudit(c, turn.info, outcome)
	}
	if channel == nil {
		recordRelayErrorLog(c, apiErr)
		return
	}
	processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), apiErr)
}

func prepareResponsesWebSocketTurnAttempt(c *gin.Context, turn *responsesWebSocketTurn) ([]byte, *types.NewAPIError) {
	service.ClearResponsesCompactAudit(c)
	var request dto.OpenAIResponsesRequest
	if err := common.Unmarshal(turn.rawPayload, &request); err != nil {
		return nil, types.NewErrorWithStatusCode(errors.New("invalid response.create request"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if err := relayhelper.ValidateResponsesRequest(&request); err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	request.Model = turn.baseModel
	stream := true
	request.Stream = &stream
	common.SetContextKey(c, constant.ContextKeyResponsesCompactMode, turn.compactMode)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, turn.selectionModel)

	if turn.info == nil {
		common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
		info, err := relaycommon.GenRelayInfo(c, types.RelayFormatOpenAIResponses, &request, nil)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		}
		turn.info = info
		turn.info.RequestURLPath = "/v1/responses"
	} else {
		resetMainRelayAttemptFields(turn.info, turn.selectionModel)
		turn.info.Request = &request
		turn.info.ResponsesCompactMode = turn.compactMode
		turn.info.ResponsesClientStream = false
		turn.info.RelayMode = relayconstant.RelayModeResponses
		turn.info.RelayFormat = types.RelayFormatOpenAIResponses
		turn.info.RequestURLPath = "/v1/responses"
		turn.info.IsStream = true
		turn.info.ChannelMeta = nil
	}
	turn.info.InitChannelMeta(c)
	if err := relayhelper.ModelMappedHelper(c, turn.info, &request); err != nil {
		return nil, types.NewError(err, types.ErrorCodeChannelModelMappedError)
	}

	adaptor := relay.GetAdaptor(turn.info.ApiType)
	if adaptor == nil {
		return nil, types.NewError(errors.New("Responses WebSocket adaptor is unavailable"), types.ErrorCodeChannelModelMappedError)
	}
	adaptor.Init(turn.info)
	converted, err := adaptor.ConvertOpenAIResponsesRequest(c, turn.info, request)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeChannelModelMappedError)
	}
	var convertedRequest dto.OpenAIResponsesRequest
	switch value := converted.(type) {
	case dto.OpenAIResponsesRequest:
		convertedRequest = value
	case *dto.OpenAIResponsesRequest:
		if value == nil {
			return nil, types.NewError(errors.New("Responses WebSocket converted request is nil"), types.ErrorCodeChannelModelMappedError)
		}
		convertedRequest = *value
	default:
		return nil, types.NewError(fmt.Errorf("Responses WebSocket route converter produced %T", converted), types.ErrorCodeChannelModelMappedError)
	}
	turn.info.Request = &convertedRequest
	payload, err := applyConvertedResponsesWebSocketRequest(turn.rawPayload, &convertedRequest)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	payload, err = relaycommon.RemoveDisabledFields(payload, turn.info.ChannelOtherSettings, turn.info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if len(turn.info.ParamOverride) > 0 {
		payload, err = relaycommon.ApplyParamOverrideWithRelayInfo(payload, turn.info)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeChannelParamOverrideInvalid)
		}
	}

	if apiErr := prepareMainRelayBilling(c, turn.info); apiErr != nil {
		return nil, apiErr
	}
	logger.LogDebug(c, "Responses WebSocket turn prepared: compact_mode=%s, model=%s, channel=%d, bytes=%d", turn.compactMode, turn.info.UpstreamModelName, turn.info.ChannelId, len(payload))
	return payload, nil
}

func applyConvertedResponsesWebSocketRequest(payload []byte, request *dto.OpenAIResponsesRequest) ([]byte, error) {
	if request == nil {
		return nil, errors.New("converted Responses WebSocket request is nil")
	}
	result, err := sjson.SetBytes(append([]byte(nil), payload...), "model", request.Model)
	if err != nil {
		return nil, err
	}
	result, err = sjson.SetBytes(result, "stream", true)
	if err != nil {
		return nil, err
	}
	setRaw := func(field string, value []byte) error {
		var setErr error
		if len(value) == 0 {
			result, setErr = sjson.DeleteBytes(result, field)
		} else {
			result, setErr = sjson.SetRawBytes(result, field, value)
		}
		return setErr
	}
	if err := setRaw("instructions", request.Instructions); err != nil {
		return nil, err
	}
	if err := setRaw("store", request.Store); err != nil {
		return nil, err
	}
	if request.MaxOutputTokens == nil {
		result, err = sjson.DeleteBytes(result, "max_output_tokens")
	} else {
		result, err = sjson.SetBytes(result, "max_output_tokens", *request.MaxOutputTokens)
	}
	if err != nil {
		return nil, err
	}
	if request.Temperature == nil {
		result, err = sjson.DeleteBytes(result, "temperature")
	} else {
		result, err = sjson.SetBytes(result, "temperature", *request.Temperature)
	}
	if err != nil {
		return nil, err
	}
	if request.Reasoning == nil {
		result, err = sjson.DeleteBytes(result, "reasoning")
	} else {
		reasoningJSON, marshalErr := common.Marshal(request.Reasoning)
		if marshalErr != nil {
			return nil, marshalErr
		}
		result, err = sjson.SetRawBytes(result, "reasoning", reasoningJSON)
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

func proxyResponsesWebSocket(c *gin.Context, clientConn *websocket.Conn, initialUpstream *websocket.Conn, initialChannel *model.Channel, initialTurn *responsesWebSocketTurn) *types.NewAPIError {
	ctx, cancel := context.WithCancel(c.Request.Context())
	clientResults := make(chan responsesWebSocketReadResult, 16)
	upstreamResults := make(chan responsesWebSocketReadResult, 16)
	var wg sync.WaitGroup
	startResponsesWebSocketReader := func(conn *websocket.Conn, generation int, target chan<- responsesWebSocketReadResult) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				messageType, payload, err := conn.ReadMessage()
				result := responsesWebSocketReadResult{generation: generation, messageType: messageType, payload: payload, err: err}
				select {
				case target <- result:
				case <-ctx.Done():
					return
				}
				if err != nil {
					return
				}
			}
		}()
	}

	upstreamConn := initialUpstream
	selectedChannel := initialChannel
	currentTurn := initialTurn
	upstreamGeneration := 1
	startResponsesWebSocketReader(clientConn, 0, clientResults)
	startResponsesWebSocketReader(upstreamConn, upstreamGeneration, upstreamResults)
	defer func() {
		cancel()
		_ = clientConn.Close()
		_ = upstreamConn.Close()
		wg.Wait()
	}()

	for {
		select {
		case <-ctx.Done():
			refundResponsesWebSocketTurn(c, currentTurn)
			return nil
		case result := <-clientResults:
			if result.err != nil {
				refundResponsesWebSocketTurn(c, currentTurn)
				forwardResponsesWebSocketClose(upstreamConn, result.err)
				if websocket.IsCloseError(result.err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					return nil
				}
				return types.NewError(errors.New("client Responses WebSocket closed"), types.ErrorCodeBadResponse)
			}
			if gjson.GetBytes(result.payload, "type").String() == "response.create" {
				if currentTurn != nil {
					apiErr := types.NewErrorWithStatusCode(errors.New("only one active response.create turn is allowed"), types.ErrorCodeInvalidRequest, http.StatusConflict, types.ErrOptionWithSkipRetry())
					writeResponsesWebSocketError(c, clientConn, apiErr)
					refundResponsesWebSocketTurn(c, currentTurn)
					return apiErr
				}
				turn, apiErr := parseResponsesWebSocketTurn(c, result.messageType, result.payload)
				if apiErr != nil {
					writeResponsesWebSocketError(c, clientConn, apiErr)
					return apiErr
				}
				if apiErr := middleware.ValidateTokenModelAccess(c, turn.selectionModel); apiErr != nil {
					writeResponsesWebSocketError(c, clientConn, apiErr)
					return apiErr
				}
				if !responsesWebSocketChannelSupportsTurn(c, selectedChannel, turn.selectionModel) {
					apiErr := types.NewErrorWithStatusCode(errors.New("selected channel does not support the next Responses WebSocket model"), types.ErrorCodeModelNotFound, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
					writeResponsesWebSocketError(c, clientConn, apiErr)
					return apiErr
				}
				common.SetContextKey(c, constant.ContextKeyOriginalModel, turn.selectionModel)
				payload, apiErr := prepareResponsesWebSocketTurnAttempt(c, turn)
				if apiErr != nil {
					writeResponsesWebSocketError(c, clientConn, apiErr)
					refundResponsesWebSocketTurn(c, turn)
					return apiErr
				}
				_ = upstreamConn.SetWriteDeadline(time.Now().Add(responsesWebSocketWriteTimeout))
				err := upstreamConn.WriteMessage(result.messageType, payload)
				_ = upstreamConn.SetWriteDeadline(time.Time{})
				if err != nil {
					_ = upstreamConn.Close()
					newUpstream, newChannel, connectErr := responsesWebSocketTurnConnector(c, turn, nil, 1)
					if connectErr != nil {
						writeResponsesWebSocketError(c, clientConn, connectErr)
						refundResponsesWebSocketTurn(c, turn)
						return connectErr
					}
					upstreamConn = newUpstream
					selectedChannel = newChannel
					upstreamGeneration++
					startResponsesWebSocketReader(upstreamConn, upstreamGeneration, upstreamResults)
				}
				currentTurn = turn
				continue
			}
			_ = upstreamConn.SetWriteDeadline(time.Now().Add(responsesWebSocketWriteTimeout))
			err := upstreamConn.WriteMessage(result.messageType, result.payload)
			_ = upstreamConn.SetWriteDeadline(time.Time{})
			if err != nil {
				refundResponsesWebSocketTurn(c, currentTurn)
				return types.NewErrorWithStatusCode(errors.New("write client frame upstream failed"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
			}
		case result := <-upstreamResults:
			if result.generation != upstreamGeneration {
				continue
			}
			if result.err != nil {
				apiErr := types.NewErrorWithStatusCode(errors.New("upstream Responses WebSocket closed"), types.ErrorCodeBadResponse, http.StatusBadGateway)
				if currentTurn != nil {
					processResponsesWebSocketChannelError(c, currentTurn, selectedChannel, apiErr, "upstream_closed")
				}
				if currentTurn != nil && !currentTurn.downstreamEventWritten && shouldRetry(c, apiErr, common.RetryTimes-currentTurn.retryIndex) {
					newUpstream, newChannel, connectErr := responsesWebSocketTurnConnector(c, currentTurn, nil, currentTurn.retryIndex+1)
					if connectErr == nil {
						_ = upstreamConn.Close()
						upstreamConn = newUpstream
						selectedChannel = newChannel
						upstreamGeneration++
						startResponsesWebSocketReader(upstreamConn, upstreamGeneration, upstreamResults)
						continue
					}
					writeResponsesWebSocketError(c, clientConn, connectErr)
					refundResponsesWebSocketTurn(c, currentTurn)
					return connectErr
				}
				refundResponsesWebSocketTurn(c, currentTurn)
				if _, ok := result.err.(*websocket.CloseError); ok {
					forwardResponsesWebSocketClose(clientConn, result.err)
					if currentTurn == nil || websocket.IsCloseError(result.err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						return nil
					}
					return types.NewError(errors.New("upstream Responses WebSocket closed before turn terminal"), types.ErrorCodeBadResponse)
				}
				writeResponsesWebSocketError(c, clientConn, apiErr)
				return apiErr
			}

			businessErr, businessErrorEvent := responsesWebSocketUpstreamBusinessError(result.payload)
			if businessErr != nil && currentTurn != nil {
				processResponsesWebSocketChannelError(c, currentTurn, selectedChannel, businessErr, businessErrorEvent)
				if !currentTurn.downstreamEventWritten && shouldRetry(c, businessErr, common.RetryTimes-currentTurn.retryIndex) {
					newUpstream, newChannel, connectErr := responsesWebSocketTurnConnector(c, currentTurn, nil, currentTurn.retryIndex+1)
					if connectErr == nil {
						_ = upstreamConn.Close()
						upstreamConn = newUpstream
						selectedChannel = newChannel
						upstreamGeneration++
						startResponsesWebSocketReader(upstreamConn, upstreamGeneration, upstreamResults)
						continue
					}
				}
			}

			_ = clientConn.SetWriteDeadline(time.Now().Add(responsesWebSocketWriteTimeout))
			err := clientConn.WriteMessage(result.messageType, result.payload)
			_ = clientConn.SetWriteDeadline(time.Time{})
			if err != nil {
				refundResponsesWebSocketTurn(c, currentTurn)
				return types.NewError(errors.New("write upstream frame to client failed"), types.ErrorCodeBadResponse)
			}
			if currentTurn != nil {
				currentTurn.downstreamEventWritten = true
				if handleResponsesWebSocketUpstreamEvent(c, currentTurn, selectedChannel, result.payload) {
					currentTurn = nil
				}
			}
		}
	}
}

func handleResponsesWebSocketUpstreamEvent(c *gin.Context, turn *responsesWebSocketTurn, channel *model.Channel, payload []byte) bool {
	var event dto.ResponsesStreamResponse
	if err := common.Unmarshal(payload, &event); err != nil {
		return false
	}
	if turn.info != nil && turn.info.ResponsesUsageInfo != nil {
		switch event.Type {
		case dto.ResponsesOutputTypeItemDone:
			turn.info.ResponsesUsageInfo.OutputItemDoneCount++
			if event.Item != nil {
				if event.Item.Type == "compaction" {
					turn.info.ResponsesUsageInfo.CompactionOutputItemCount++
				}
				if event.Item.Type == dto.BuildInCallWebSearchCall {
					if tool := turn.info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; tool != nil {
						tool.CallCount++
					}
				}
			}
		case "response.completed", "response.done":
			turn.info.ResponsesUsageInfo.ResponseCompleted = true
			turn.info.ResponsesUsageInfo.TerminalEventType = event.Type
		case "response.failed", "response.incomplete", "response.cancelled", "response.canceled", "response.error", "error":
			turn.info.ResponsesUsageInfo.TerminalEventType = event.Type
		}
	}

	switch event.Type {
	case "response.completed", "response.done":
		if event.Response == nil || event.Response.Usage == nil {
			logger.LogWarn(c, "Responses WebSocket completed without usage; refunding turn")
			apiErr := types.NewErrorWithStatusCode(errors.New("Responses WebSocket completed without usage"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			processResponsesWebSocketChannelError(c, turn, channel, apiErr, "completed_without_usage")
			refundResponsesWebSocketTurn(c, turn)
		} else {
			service.SetResponsesCompactAudit(c, turn.info, "completed")
			usage := &dto.Usage{
				PromptTokens:     event.Response.Usage.InputTokens,
				CompletionTokens: event.Response.Usage.OutputTokens,
				TotalTokens:      event.Response.Usage.TotalTokens,
			}
			if event.Response.Usage.InputTokensDetails != nil {
				usage.PromptTokensDetails = *event.Response.Usage.InputTokensDetails
			}
			service.PostTextConsumeQuota(c, turn.info, usage, nil)
			if channel != nil {
				service.RecordChannelAffinity(c, channel.Id)
			}
		}
		if turn.info != nil && turn.info.IsResponsesCompactV2() && turn.info.ResponsesUsageInfo != nil && turn.info.ResponsesUsageInfo.CompactionOutputItemCount != 1 {
			logger.LogWarn(c, fmt.Sprintf("Responses WebSocket Compact V2 compaction item count is %d", turn.info.ResponsesUsageInfo.CompactionOutputItemCount))
		}
		return true
	case "response.failed", "response.incomplete", "response.cancelled", "response.canceled", "response.error", "error":
		service.SetResponsesCompactAudit(c, turn.info, event.Type)
		refundResponsesWebSocketTurn(c, turn)
		return true
	default:
		return false
	}
}

func responsesWebSocketUpstreamBusinessError(payload []byte) (*types.NewAPIError, string) {
	eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
	switch eventType {
	case "response.failed", "response.error", "error":
	default:
		return nil, ""
	}

	errorResult := gjson.GetBytes(payload, "response.error")
	if !errorResult.Exists() {
		errorResult = gjson.GetBytes(payload, "error")
	}
	message := strings.TrimSpace(gjson.Get(errorResult.Raw, "message").String())
	if message == "" {
		message = "upstream Responses WebSocket returned " + eventType
	}
	errorType := strings.TrimSpace(gjson.Get(errorResult.Raw, "type").String())
	if errorType == "" {
		errorType = "upstream_error"
	}
	var errorCode any
	if codeResult := gjson.Get(errorResult.Raw, "code"); codeResult.Exists() && codeResult.Type != gjson.Null {
		errorCode = codeResult.Value()
	}
	statusCode := responsesWebSocketBusinessErrorStatus(payload, errorType, fmt.Sprint(errorCode), message)
	return types.WithOpenAIError(types.OpenAIError{
		Message: message,
		Type:    errorType,
		Code:    errorCode,
	}, statusCode), eventType
}

func responsesWebSocketBusinessErrorStatus(payload []byte, errorType string, errorCode string, message string) int {
	for _, path := range []string{"response.error.status_code", "error.status_code", "status_code"} {
		statusCode := int(gjson.GetBytes(payload, path).Int())
		if statusCode >= http.StatusBadRequest && statusCode <= 599 {
			return statusCode
		}
	}
	combined := strings.ToLower(strings.TrimSpace(errorType + " " + errorCode + " " + message))
	switch {
	case strings.Contains(combined, "context_length_exceeded"),
		strings.Contains(combined, "context_too_large"),
		strings.Contains(combined, "invalid_request"),
		strings.Contains(combined, "content_policy"),
		strings.Contains(combined, "safety"):
		return http.StatusBadRequest
	case strings.Contains(combined, "rate_limit"):
		return http.StatusTooManyRequests
	case strings.Contains(combined, "authentication"),
		strings.Contains(combined, "unauthorized"),
		strings.Contains(combined, "invalid_api_key"):
		return http.StatusUnauthorized
	case strings.Contains(combined, "permission"),
		strings.Contains(combined, "forbidden"),
		strings.Contains(combined, "access denied"):
		return http.StatusForbidden
	case strings.Contains(combined, "server_is_overloaded"), strings.Contains(combined, "slow_down"):
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadGateway
	}
}

func responsesWebSocketChannelSupportsTurn(c *gin.Context, channel *model.Channel, modelName string) bool {
	if channel == nil || !middleware.ChannelSupportsRequest(channel, "/v1/responses", modelName) {
		return false
	}
	if _, specific := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); specific {
		return true
	}
	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if usingGroup == "auto" {
		usingGroup = common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
	}
	return model.IsChannelEnabledForGroupModel(usingGroup, modelName, channel.Id)
}

func refundResponsesWebSocketTurn(c *gin.Context, turn *responsesWebSocketTurn) {
	if turn == nil || turn.info == nil || turn.info.Billing == nil {
		return
	}
	turn.info.Billing.Refund(c)
}

func writeResponsesWebSocketError(c *gin.Context, conn *websocket.Conn, apiErr *types.NewAPIError) {
	if conn == nil || apiErr == nil {
		return
	}
	apiErr.SetMessage(common.MessageWithRequestId(apiErr.Error(), c.GetString(common.RequestIdKey)))
	payload, err := common.Marshal(map[string]any{
		"type":  "error",
		"error": apiErr.ToOpenAIError(),
	})
	if err == nil {
		closeCode := websocket.ClosePolicyViolation
		if apiErr.StatusCode >= http.StatusInternalServerError {
			closeCode = websocket.CloseInternalServerErr
		}
		_ = conn.SetWriteDeadline(time.Now().Add(responsesWebSocketWriteTimeout))
		_ = conn.WriteMessage(websocket.TextMessage, payload)
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(closeCode, "Responses WebSocket request failed"), time.Now().Add(responsesWebSocketWriteTimeout))
		_ = conn.SetWriteDeadline(time.Time{})
	}
}

func forwardResponsesWebSocketClose(conn *websocket.Conn, readErr error) {
	if conn == nil {
		return
	}
	closeErr, ok := readErr.(*websocket.CloseError)
	if !ok {
		return
	}
	message := closeErr.Text
	if len(message) > 100 {
		message = message[:100]
	}
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(closeErr.Code, message), time.Now().Add(responsesWebSocketWriteTimeout))
}
