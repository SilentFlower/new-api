package relay

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/tidwall/gjson"
	"io"
	"net"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	appdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	appmodel "github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/pkg/wsmanager"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/requestplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const responsesWSEventTypeResponseCreate = "response.create"

var errResponsesWSChannelMoved = errors.New("the connection channel is no longer allowed for this group and model")

const responsesWSWriteTimeout = 30 * time.Second

// ResponsesWSRequestRunner executes the existing authentication and rate-limit
// middleware around one complete request, without a second HTTP connection.
type ResponsesWSRequestRunner func(*http.Request, string, func(*gin.Context) *types.NewAPIError) *types.NewAPIError

type responsesWSCreateEvent struct {
	Type     string            `json:"type"`
	EventID  string            `json:"event_id,omitempty"`
	StreamID common.RawMessage `json:"stream_id,omitempty"`
	Generate common.RawMessage `json:"generate,omitempty"`
	Request  common.RawMessage `json:"response,omitempty"`
}

type responsesWSCreateRequest struct {
	RawFrame []byte
	Request  dto.OpenAIResponsesRequest
	Body     []byte
	Generate common.RawMessage
	StreamID string
	EventID  string
}

type responsesWSErrorEvent struct {
	Type       string             `json:"type"`
	Status     int                `json:"status"`
	EventID    string             `json:"event_id,omitempty"`
	StreamID   string             `json:"stream_id,omitempty"`
	ResponseID string             `json:"response_id,omitempty"`
	Error      *types.OpenAIError `json:"error"`
}

type responsesWSMessage struct {
	kind int
	body []byte
	err  error
}

// responsesWSControl is a client control event (response.cancel) whose
// envelope the read loop already parsed.
type responsesWSControl struct {
	body              []byte
	eventID, streamID string
}

// Only the request worker reads or changes billing state. Socket readers pass
// bounded messages to it; cancellation never performs an independent refund.
type responsesWSCallState struct {
	inbox    chan responsesWSMessage
	controls chan responsesWSControl
	done     chan struct{}
	terminal *responsesWSMessage
	// A nonzero closeCode ends the connection after this request.
	closeCode   int
	closeReason string
}

type responsesWSSession struct {
	ctx            context.Context
	cancel         context.CancelFunc
	client         *websocket.Conn
	runner         ResponsesWSRequestRunner
	request        *http.Request
	requestID      string
	nextEventIndex int
	workers        sync.WaitGroup
	closeOnce      sync.Once

	clientWriteMu sync.Mutex
	targetWriteMu sync.Mutex
	connectionMu  sync.Mutex
	target        *websocket.Conn
	targetDone    chan struct{}
	unregister    func()
	stateMu       sync.Mutex
	current       *responsesWSCallState

	// clientWriteBudget is guarded by clientWriteMu.
	clientWriteBudget helper.StreamWriteBudget

	// These fields belong to the serial request worker and describe the actual
	// established connection. Per-request token/user data is never stored here.
	lastResponseID  string
	lockedModel     string
	lockedChannelID int
	lockedGroup     string
	lockedKey       string
	lockedKeyIndex  int
	lockedContext   map[appconstant.ContextKey]any
	lockedRoute     dto.AdvancedCustomRoute
}

func ResponsesWebSocketHelper(c *gin.Context, client *websocket.Conn, runner ResponsesWSRequestRunner) *types.NewAPIError {
	ctx, cancel := context.WithCancel(c.Request.Context())
	s := &responsesWSSession{ctx: ctx, cancel: cancel, client: client, runner: runner,
		request: c.Request.Clone(ctx), requestID: c.GetString(common.RequestIdKey)}
	if s.requestID == "" {
		s.requestID = common.NewRequestId()
	}
	maxMB := appconstant.MaxRequestBodyMB
	if maxMB <= 0 {
		maxMB = 128
	}
	client.SetReadLimit(int64(maxMB) << 20)
	defer func() {
		// Unless an earlier shutdown ended the read, the client closed or left.
		// Gorilla already answered a client close frame, so this tells the upstream.
		s.shutdown(websocket.CloseNormalClosure, "")
		s.workers.Wait()
	}()

	first := true
	firstTimeout := common.GetResponsesWebSocketFirstFrameTimeout()
	_ = client.SetReadDeadline(time.Now().Add(firstTimeout))
	for {
		_, message, err := client.ReadMessage()
		wasFirst := first
		if first {
			first = false
			_ = client.SetReadDeadline(time.Time{})
			if err != nil {
				var networkErr net.Error
				if errors.As(err, &networkErr) && networkErr.Timeout() {
					s.sendError("", "", types.NewErrorWithStatusCode(errors.New(common.MessageWithRequestId("Timed out waiting for the first WebSocket request; reconnect and resend the complete request", s.requestID)), types.ErrorCode("responses_websocket_first_frame_timeout"), http.StatusRequestTimeout))
					s.shutdown(websocket.CloseTryAgainLater, "first frame timeout")
				}
			}
		}
		if err != nil {
			return nil
		}
		envelope, streamID, err := parseResponsesWSEnvelope(message)
		eventType := envelope.Type
		if err != nil {
			s.sendError(envelope.EventID, streamID, newResponsesWSInvalidRequestError(err))
			if wasFirst {
				s.shutdown(websocket.ClosePolicyViolation, "invalid first frame")
				return nil
			}
			continue
		}
		if eventType != responsesWSEventTypeResponseCreate {
			// Controls are owned by the active request too. In particular a cancel
			// arriving during authentication must not precede its upstream create.
			state := s.getCurrent()
			if eventType != "response.cancel" || state == nil {
				s.sendError(envelope.EventID, streamID, newResponsesWSInvalidRequestError(fmt.Errorf("unsupported websocket event %q", eventType)))
				continue
			}
			select {
			case state.controls <- responsesWSControl{body: message, eventID: envelope.EventID, streamID: streamID}:
			case <-state.done:
			case <-s.ctx.Done():
				return nil
			default:
				s.sendError(envelope.EventID, streamID, newResponsesWSInvalidRequestError(errors.New("a response control event is already pending")))
			}
			continue
		}
		state := &responsesWSCallState{inbox: make(chan responsesWSMessage), controls: make(chan responsesWSControl, 1), done: make(chan struct{})}
		if !s.tryReserveCurrent(state) {
			s.sendError(envelope.EventID, streamID, types.NewErrorWithStatusCode(errors.New("another response.create is already in progress on this websocket connection"), types.ErrorCodeInvalidRequest, http.StatusConflict, types.ErrOptionWithSkipRetry()))
			continue
		}
		requestID := fmt.Sprintf("%s-ws-%d", s.requestID, s.nextEventIndex)
		s.nextEventIndex++
		s.workers.Go(func() { s.runRequest(state, message, envelope, streamID, requestID) })
	}
}

func (s *responsesWSSession) runRequest(state *responsesWSCallState, message []byte, envelope responsesWSCreateEvent, streamID string, requestID string) {
	create, parseErr := normalizeResponsesWSCreateEvent(message, envelope, streamID)
	var apiErr *types.NewAPIError
	var upstreamURLs []string
	defer func() {
		if recovered := recover(); recovered != nil {
			apiErr = types.NewError(fmt.Errorf("responses websocket request panic: %v", recovered), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
			state.closeCode = websocket.CloseInternalServerErr
		}
		// Finish the middleware count before publishing the terminal event. Hold
		// admission while writing it so an immediate next create cannot race
		// the current request's release. A failing request also shuts down
		// before that release, so a concurrent upstream close cannot report it
		// as a normal closure; shutdown itself takes neither lock.
		outgoing := state.terminal
		if apiErr != nil {
			if body, err := buildResponsesWSErrorPayload(envelope.EventID, streamID, apiErr, upstreamURLs...); err == nil {
				outgoing = &responsesWSMessage{kind: websocket.TextMessage, body: body}
			}
		}
		s.clientWriteMu.Lock()
		s.stateMu.Lock()
		if outgoing != nil {
			if err := s.client.SetWriteDeadline(s.clientWriteBudget.Deadline(time.Now(), len(outgoing.body))); err != nil {
				state.closeCode = websocket.CloseInternalServerErr
			} else if err := s.client.WriteMessage(outgoing.kind, outgoing.body); err != nil {
				state.closeCode = websocket.CloseInternalServerErr
			}
		}
		if state.closeCode != 0 {
			s.shutdown(state.closeCode, state.closeReason)
		}
		s.current = nil
		close(state.done)
		s.stateMu.Unlock()
		s.clientWriteMu.Unlock()
	}()
	request := s.request.Clone(s.ctx)
	request.Body = io.NopCloser(bytes.NewReader(message))
	request.ContentLength = int64(len(message))
	request.Header.Set("Content-Type", "application/json")
	apiErr = s.runner(request, requestID, func(c *gin.Context) *types.NewAPIError {
		if parseErr != nil {
			return newResponsesWSInvalidRequestError(parseErr)
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(create.Body))
		c.Request.ContentLength = int64(len(create.Body))
		callErr := s.runCall(c, state, create)
		upstreamURLs = common.UpstreamAddresses(c)
		return callErr
	})
	if apiErr != nil && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden) {
		state.closeCode = websocket.ClosePolicyViolation
	}
}

func (s *responsesWSSession) runCall(c *gin.Context, state *responsesWSCallState, create responsesWSCreateRequest) (apiErr *types.NewAPIError) {
	policy := service.RequestPolicy(c)
	modelName := create.Request.Model
	started := time.Now()
	var guard *channelUserConcurrencyGuard
	var info *relaycommon.RelayInfo
	billingPrepared := false
	var ingressSteps []relaycommon.RequestPluginStep
	var ingressRecord []relaycommon.RequestPluginRecord
	// stopErr is the error the client gets for a response a request plugin
	// stopped while checking its text.
	var stopErr *types.NewAPIError
	defer func() {
		if recovered := recover(); recovered != nil {
			apiErr = types.NewError(fmt.Errorf("responses websocket call panic: %v", recovered), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
			state.closeCode = websocket.CloseInternalServerErr
		}
		if info == nil && modelName != "" {
			info = &relaycommon.RelayInfo{OriginModelName: modelName, UsingGroup: common.GetContextKeyString(c, appconstant.ContextKeyUsingGroup), StartTime: started}
			info.SetRequestPluginIngress(ingressSteps, ingressRecord)
		}
		apiErr = finishChannelUserConcurrency(c, guard, apiErr)
		perfmetrics.RecordRelayResult(c.Request.Context(), info, apiErr)
		// Settlement already marks the request policy successful, and nothing
		// reads a termination decision after this point on the WebSocket path,
		// so neither policy record belongs here.
		if info != nil && billingPrepared {
			apiErr = RefundFailedRequestBilling(c, info, apiErr)
		}
		if stopErr != nil && info != nil {
			service.RecordRelayErrorLog(c, info.ChannelId, stopErr, info)
		}
		// The request plugins' report hooks get the response once it settled.
		requestplugin.Report(c, types.RelayFormatOpenAIResponses, info, ingressRecord, cmp.Or(apiErr, stopErr))
	}()
	if modelName == "" {
		state.closeCode = websocket.ClosePolicyViolation
		return newResponsesWSInvalidRequestError(errors.New("model is required"))
	}
	common.SetContextKey(c, appconstant.ContextKeyOriginalModel, modelName)
	common.SetContextKey(c, appconstant.ContextKeyRequestStartTime, time.Now())
	// As on HTTP, the request plugins' ingress reads the create before the
	// model access and channel checks; a changed body is decoded again.
	changed, ingressSteps, ingressRecord, ingressErr := requestplugin.SocketIngress(c, types.RelayFormatOpenAIResponses)
	if ingressErr != nil {
		ingress := &relaycommon.RelayInfo{}
		ingress.SetRequestPluginIngress(ingressSteps, ingressRecord)
		service.RecordRelayErrorLog(c, 0, ingressErr, ingress)
		return ingressErr
	}
	if changed {
		if apiErr = reloadResponsesWSCreate(c, &create); apiErr != nil {
			return apiErr
		}
	}
	modelName = create.Request.Model
	compact := helper.DetectResponsesCompactMode(http.MethodGet, c.Request.URL.Path, c.Request.Header, create.RawFrame, helper.ResponsesTransportWebSocket)
	common.SetContextKey(c, appconstant.ContextKeyResponsesCompactMode, compact)
	common.SetContextKey(c, appconstant.ContextKeyOriginalModel, modelName)
	if apiErr = middleware.ValidateResponsesWebSocketModelAccess(c, modelName); apiErr != nil {
		return apiErr
	}
	if s.lockedModel != "" && modelName != s.lockedModel && create.Request.PreviousResponseID != "" {
		return newResponsesWSInvalidRequestError(errors.New("model changed for an incremental response"))
	}
	service.GetChannelConstraints(c).AddFilter(appdto.ChannelFilter{Kind: appdto.FilterRequestPath, RequestPath: c.Request.URL.Path})

	// 每轮重新读取限制与计价，拨号重试时复用原预扣会话。
	var pendingControl, sentControl []byte
	prepareAttempt := func() *types.NewAPIError {
		if err := checkChannelUserQuotaLimits(c); err != nil {
			return err
		}
		var err *types.NewAPIError
		guard, err = acquireChannelUserConcurrency(c)
		if err != nil {
			return err
		}
		info.InitChannelMeta(c)
		info.ClearBillingModelName()
		if compact.IsCompact() {
			if err = PrepareResponsesCompactPassthrough(c, info); err != nil {
				return err
			}
		} else {
			request := create.Request
			if err := helper.ModelMappedHelper(c, info, &request); err != nil {
				return types.NewError(err, types.ErrorCodeChannelModelMappedError)
			}
		}
		return PrepareRequestBilling(c, info)
	}

	buildPayload := func() ([]byte, *types.NewAPIError) {
		if compact.IsCompact() {
			if err := requestplugin.Egress(c, info, create.RawFrame); err != nil {
				return nil, err
			}
			return create.RawFrame, nil
		}
		return buildResponsesWSCreatePayload(c, info, create.Request, create.Generate, create.StreamID, create.Body)
	}
	if s.lockedChannelID != 0 {
		if restoreErr := s.restoreConnectionContext(c, modelName); restoreErr != nil {
			if !errors.Is(restoreErr.Err, errResponsesWSChannelMoved) {
				return restoreErr
			}
			s.retireTarget()
			if create.Request.PreviousResponseID != "" {
				state.closeCode = websocket.ClosePolicyViolation
				return types.WithOpenAIError(types.OpenAIError{Type: "invalid_request_error", Code: "previous_response_not_found", Param: "previous_response_id", Message: "Previous response is unavailable after the channel changed. Retry with the full input context."}, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
		}
	}
	if s.lockedChannelID != 0 {
		info = relaycommon.GenRelayInfoResponses(c, &create.Request)
		info.IsStream = true
		info.SetRequestPluginIngress(ingressSteps, ingressRecord)
		common.SetContextKey(c, appconstant.ContextKeyIsStream, true)
		if apiErr = prepareAttempt(); apiErr != nil {
			return apiErr
		}
		billingPrepared = true
		var payload []byte
		payload, apiErr = buildPayload()
		if apiErr != nil {
			recordResponsesWSPluginError(c, info, apiErr)
			return apiErr
		}
		s.lockedModel = modelName
		if err := s.writeTarget(websocket.TextMessage, payload); err != nil {
			state.closeCode = websocket.CloseInternalServerErr
			return types.NewError(err, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
		}
	} else {
		retry := &service.RetryParam{Ctx: c, TokenGroup: common.GetContextKeyString(c, appconstant.ContextKeyUsingGroup), ModelName: modelName, RequestPath: c.Request.URL.Path, Retry: common.GetPointer(0)}
		for ; retry.GetRetry() <= common.RetryTimes; retry.IncreaseRetry() {
			var channel *appmodel.Channel
			channel, apiErr = middleware.SelectResponsesWebSocketChannel(c, modelName, retry)
			if apiErr != nil {
				state.closeCode = websocket.CloseInternalServerErr
				return apiErr
			}
			service.AppendUsedChannel(c, channel.Id)
			if info == nil {
				info = relaycommon.GenRelayInfoResponses(c, &create.Request)
				info.IsStream = true
				info.SetRequestPluginIngress(ingressSteps, ingressRecord)
				common.SetContextKey(c, appconstant.ContextKeyIsStream, true)
			}
			if apiErr = prepareAttempt(); apiErr != nil {
				return apiErr
			}
			billingPrepared = true
			info.RetryIndex = retry.GetRetry()
			policy.BeginAttempt(channel, info.UsingGroup)
			var payload []byte
			payload, apiErr = buildPayload()
			if apiErr != nil {
				recordResponsesWSPluginError(c, info, apiErr)
				return apiErr
			}
			adaptor := GetAdaptor(info.ApiType)
			adaptor.Init(info)
			target, dialErr := relaychannel.DoWssRequest(adaptor, c, info, nil)
			var first []responsesWSMessage
			if dialErr == nil {
				if !s.setTarget(target) {
					return types.NewError(context.Canceled, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
				}
				if err := s.writeTarget(websocket.TextMessage, payload); err != nil {
					s.retireTarget()
					return types.NewError(err, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
				}
				// 首个业务错误只有在没有续接 ID 且尚未向客户端写入时才可换渠。
				if retry.GetRetry() < common.RetryTimes && create.Request.PreviousResponseID == "" {
					firstFrame := make(chan responsesWSMessage, 1)
					limit := int64(helper.DefaultMaxScannerBufferSize)
					if appconstant.StreamScannerMaxBufferMB > 0 {
						limit = int64(appconstant.StreamScannerMaxBufferMB) << 20
					}
					target.SetReadLimit(limit)
					idle := time.Duration(appconstant.StreamingTimeout) * time.Second
					if idle <= 0 {
						idle = 300 * time.Second
					}
					_ = target.SetReadDeadline(time.Now().Add(idle))
					s.workers.Go(func() {
						kind, body, err := target.ReadMessage()
						firstFrame <- responsesWSMessage{kind: kind, body: body, err: err}
					})
					var lost <-chan struct{}
					if guard != nil {
						lost = guard.lease.LostSignal()
					}
					// 客户端在首帧前也能取消。发出取消后重试会重复执行同一轮。
				waitFirst:
					for {
						select {
						case message := <-firstFrame:
							first = []responsesWSMessage{message}
							break waitFirst
						case control := <-state.controls:
							if pendingControl != nil || sentControl != nil {
								s.sendError(control.eventID, control.streamID, newResponsesWSInvalidRequestError(errors.New("a response control event is already pending")))
							} else if create.StreamID == "" && control.streamID == "" && ojson.GetBytesNoCopy(control.body, "response_id").String() == "" {
								if err := s.writeTarget(websocket.TextMessage, control.body); err != nil {
									s.retireTarget()
									return types.NewError(err, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
								}
								sentControl = control.body
							} else {
								pendingControl = control.body
							}
						case <-lost:
							s.retireTarget()
							state.closeCode = websocket.CloseInternalServerErr
							return newChannelUserConcurrencyError(service.ErrChannelUserConcurrencyUnavailable)
						case <-c.Request.Context().Done():
							s.retireTarget()
							return types.NewError(c.Request.Context().Err(), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
						}
					}
					_ = target.SetReadDeadline(time.Time{})
					body := first[0].body
					if first[0].err == nil && sentControl == nil && pendingControl == nil && (ojson.GetBytesNoCopy(body, "stream_id").String() == "" || ojson.GetBytesNoCopy(body, "stream_id").String() == create.StreamID) {
						if rejected, _ := responsesWebSocketUpstreamBusinessError(body); rejected != nil && service.DecideRelayRetry(c, rejected, common.RetryTimes-retry.GetRetry()).Action == "retry" {
							dialErr = rejected
							s.retireTarget()
						}
					}
				}
			}
			if dialErr != nil {
				apiErr = service.NormalizeViolationFeeError(types.NewError(dialErr, types.ErrorCodeDoRequestFailed))
				service.ResetStatusCode(apiErr, c.GetString("status_code_mapping"))
				info.LastError = apiErr
				decision := service.DecideRelayRetry(c, apiErr, common.RetryTimes-retry.GetRetry())
				service.RecordPolicyFailure(c, channel.Id, apiErr, decision)
				service.ProcessChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, info.ApiKey, channel.GetAutoBan()), apiErr, info)
				if decision.Action == "retry" {
					if lost := finishChannelUserConcurrency(c, guard, nil); lost != nil {
						return lost
					}
					guard = nil
					continue
				}
				return apiErr
			}
			s.lockedModel, s.lockedChannelID, s.lockedGroup = modelName, channel.Id, info.UsingGroup
			s.lockedKey, s.lockedKeyIndex = info.ApiKey, info.ChannelMultiKeyIndex
			s.lockedRoute, _ = connectionOtherSettings(c, channel).AdvancedCustom.MatchPathForModel(c.Request.URL.Path, modelName)
			s.lockedContext = make(map[appconstant.ContextKey]any)
			for _, key := range []appconstant.ContextKey{
				appconstant.ContextKeyChannelId, appconstant.ContextKeyChannelName, appconstant.ContextKeyChannelType,
				appconstant.ContextKeyChannelCreateTime, appconstant.ContextKeyChannelSetting, appconstant.ContextKeyChannelOtherSetting,
				appconstant.ContextKeyChannelParamOverride, appconstant.ContextKeyChannelHeaderOverride, appconstant.ContextKeyChannelOrganization,
				appconstant.ContextKeyChannelAutoBan, appconstant.ContextKeyChannelModelMapping, appconstant.ContextKeyChannelStatusCodeMapping,
				appconstant.ContextKeyChannelIsMultiKey, appconstant.ContextKeyChannelMultiKeyIndex, appconstant.ContextKeyChannelKey, appconstant.ContextKeyChannelBaseUrl,
				appconstant.ContextKeyUpstreamRequestOrigin, appconstant.ContextKeyProviderPlugin,
			} {
				if value, ok := c.Get(string(key)); ok {
					s.lockedContext[key] = value
				}
			}
			s.registerChannelClose(channel.Id)
			s.startTargetReader(target, first...)
			apiErr = nil
			break
		}
		if apiErr != nil {
			return apiErr
		}
	}

	// The request plugins' reply check reads the text of the response before
	// the client gets it; without a reply or report instance the events go
	// out untouched.
	socket := requestplugin.ResponsesSocket(c, info, func(message []byte) {
		if err := s.writeClient(websocket.TextMessage, message); err != nil {
			s.shutdown(websocket.CloseInternalServerErr, "")
		}
	})
	// terminalOf is the event that ends the response for the client: the
	// reply check reads the rest of the text first, and a response it
	// stopped ends with the error instead.
	var responseID string
	terminalOf := func(incoming responsesWSMessage) *responsesWSMessage {
		if socket != nil && stopErr == nil && incoming.kind == websocket.TextMessage {
			if body := socket.Terminal(incoming.body); body != nil {
				incoming.body = body
			}
			stopErr = socket.Stopped()
		}
		if stopErr == nil {
			return &incoming
		}
		openAIErr := stopErr.ToOpenAIError(common.UpstreamAddresses(c)...)
		body, _ := common.Marshal(&responsesWSErrorEvent{Type: "error", Status: stopErr.StatusCode, EventID: create.EventID, StreamID: create.StreamID, ResponseID: responseID, Error: &openAIErr})
		return &responsesWSMessage{kind: websocket.TextMessage, body: body}
	}
	// finishSocket ends the reply check of a response that ended without
	// its terminal event.
	finishSocket := func() {
		if socket != nil && stopErr == nil {
			socket.Finish()
			stopErr = socket.Stopped()
		}
	}

	compactObserver := &responsesCompactPassthroughObserver{}
	accumulator := service.NewResponsesUsageAccumulator(info)
	settle := func(completed bool) {
		// 已确认沿用 build-bak 的轮次账务规则，失败、断连和缺失 usage 都退回本轮预扣。
		if !completed {
			if compact.IsCompact() {
				refundResponsesCompactPassthrough(c, info, cmp.Or(compactObserver.terminalType, "interrupted"))
			} else if info.Billing != nil {
				info.Billing.Refund(c)
			}
			return
		}
		if !compact.IsCompact() {
			ConsumeResponsesQuota(c, info, accumulator.Finish())
			return
		}
		if !compactObserver.terminalSuccess || !compactObserver.usageValid {
			refundResponsesCompactPassthrough(c, info, cmp.Or(compactObserver.terminalType, "interrupted"))
			return
		}
		service.SetResponsesCompactAudit(c, info, "completed")
		ConsumeResponsesQuota(c, info, compactObserver.usage)
	}
	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.RequireTerminal()
	common.SetContextKey(c, appconstant.ContextKeyResponseStreamStatus, info.StreamStatus)
	timeout := time.Duration(appconstant.StreamingTimeout) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	idle := time.NewTimer(timeout)
	defer idle.Stop()
	var leaseLost <-chan struct{}
	if guard != nil {
		leaseLost = guard.lease.LostSignal()
	}
	accepted := false
	// cancelled is set once the response.cancel of a stop went upstream.
	cancelled := false
	for {
		select {
		case <-leaseLost:
			state.closeCode = websocket.CloseInternalServerErr
			return newChannelUserConcurrencyError(service.ErrChannelUserConcurrencyUnavailable)
		case incoming := <-state.inbox:
			idle.Reset(timeout)
			if incoming.err != nil {
				finishSocket()
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, incoming.err)
				state.closeCode, state.closeReason = websocket.CloseInternalServerErr, "upstream connection closed"
				settle(false)
				return nil
			}
			info.SetFirstResponseTime()
			var event struct {
				dto.ResponsesStreamResponse
				StreamID string `json:"stream_id"`
			}
			if err := common.Unmarshal(incoming.body, &event); err != nil {
				info.StreamStatus.RecordError("invalid upstream websocket event")
			} else {
				if event.Type != "error" && event.StreamID != "" && event.StreamID != create.StreamID {
					if err := s.writeClient(incoming.kind, incoming.body); err != nil {
						s.shutdown(websocket.CloseInternalServerErr, "")
					}
					continue
				}
				// A repeated terminal from the previous response must never finish
				// a subsequent request on this persistent connection.
				if event.Response != nil && event.Response.ID != "" && event.Response.ID == s.lastResponseID {
					continue
				}
				if event.Type == "error" {
					var rejection responsesWSErrorEvent
					_ = common.Unmarshal(incoming.body, &rejection)
					if rejection.ResponseID != "" && rejection.ResponseID == s.lastResponseID {
						continue
					}
					terminal, ambiguous, controlError := responsesWSErrorEndsRequest(rejection, create.StreamID, responseID, sentControl)
					if !terminal {
						// A stopped response sends nothing more before its error.
						if stopErr != nil {
							if controlError {
								sentControl = nil
							}
							continue
						}
						if err := s.writeClient(incoming.kind, incoming.body); err != nil {
							s.shutdown(websocket.CloseInternalServerErr, "")
						}
						// Only a control error in this stream resolves its pending control.
						if controlError {
							sentControl = nil
						}
						continue
					}
					if accepted {
						if rejection.Error != nil {
							code := ""
							if rejection.Error.Code != nil {
								code = fmt.Sprint(rejection.Error.Code)
							}
							info.StreamStatus.MarkFailed(code, rejection.Error.Type, rejection.Status)
						}
						if compact.IsCompact() {
							service.ObserveResponsesOutcome(info, &event.ResponsesStreamResponse)
							compactObserver.observe(string(incoming.body), info)
						} else {
							accumulator.Observe(&event.ResponsesStreamResponse, incoming.body)
						}
						info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
						s.lastResponseID = responseID
						state.terminal = terminalOf(incoming)
						if ambiguous {
							state.closeCode = websocket.CloseInternalServerErr
						}
						settle(false)
						return nil
					}
					if rejection.Error == nil {
						// An error frame without an error object still describes the
						// rejected request; keep the client-facing type stable.
						rejection.Error = &types.OpenAIError{Type: "invalid_request_error", Message: event.Message, Code: event.Code}
					}
					rejected := types.WithOpenAIError(*rejection.Error, rejection.Status, types.ErrOptionWithSkipRetry())
					if rejected.StatusCode < 400 || rejected.StatusCode > 599 {
						rejected.StatusCode = http.StatusBadRequest
					}
					return rejected
				}
				if strings.HasPrefix(event.Type, "response.") {
					if !accepted {
						// Like HTTP, bind the session only once upstream accepted the request.
						service.RecordChannelAffinity(c, s.lockedChannelID)
					}
					accepted = true
					if event.Response != nil && event.Response.ID != "" {
						responseID = event.Response.ID
					}
				}
				if compact.IsCompact() {
					service.ObserveResponsesOutcome(info, &event.ResponsesStreamResponse)
					compactObserver.observe(string(incoming.body), info)
				} else {
					accumulator.Observe(&event.ResponsesStreamResponse, incoming.body)
				}
			}
			switch event.Type {
			case "response.completed", "response.done", "response.incomplete", "response.failed", "response.cancelled", "response.canceled", "response.error":
				if event.Response != nil {
					s.lastResponseID = event.Response.ID
				}
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				state.terminal = terminalOf(incoming)
				settle((event.Type == "response.completed" || event.Type == "response.done") && event.Response != nil && event.Response.Usage != nil && !info.StreamStatus.ResponseFailed())
				return nil
			}
			if stopErr == nil {
				if socket != nil && incoming.kind == websocket.TextMessage {
					socket.Event(incoming.body)
					stopErr = socket.Stopped()
				} else if err := s.writeClient(incoming.kind, incoming.body); err != nil {
					s.shutdown(websocket.CloseInternalServerErr, "")
				}
			}
			if accepted && pendingControl != nil {
				if err := s.writeTarget(websocket.TextMessage, pendingControl); err != nil {
					s.shutdown(websocket.CloseInternalServerErr, "")
				}
				sentControl = pendingControl
				pendingControl = nil
			}
			if stopErr != nil && !cancelled && sentControl == nil {
				// A stop ends the response as a client cancel does; the
				// upstream terminal settles it and the client gets the error.
				cancelled = true
				cancel := map[string]string{"type": "response.cancel", "event_id": "evt_" + common.GetUUID()}
				if responseID != "" {
					cancel["response_id"] = responseID
				}
				if create.StreamID != "" {
					cancel["stream_id"] = create.StreamID
				}
				body, _ := common.Marshal(cancel)
				if err := s.writeTarget(websocket.TextMessage, body); err != nil {
					s.shutdown(websocket.CloseInternalServerErr, "")
				}
				sentControl = body
			}
		case control := <-state.controls:
			if stopErr != nil {
				// The response is already ending.
				continue
			}
			if pendingControl != nil || sentControl != nil {
				s.sendError(control.eventID, control.streamID, newResponsesWSInvalidRequestError(errors.New("a response control event is already pending")))
				continue
			}
			if !accepted && (s.lastResponseID != "" || create.StreamID != "" || control.streamID != "" || ojson.GetBytesNoCopy(control.body, "response_id").String() != "") {
				pendingControl = control.body
				continue
			}
			if err := s.writeTarget(websocket.TextMessage, control.body); err != nil {
				s.shutdown(websocket.CloseInternalServerErr, "")
			}
			sentControl = control.body
		case <-idle.C:
			finishSocket()
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, context.DeadlineExceeded)
			state.closeCode, state.closeReason = websocket.CloseInternalServerErr, "upstream response timed out"
			settle(false)
			return nil
		case <-s.ctx.Done():
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, s.ctx.Err())
			settle(false)
			return nil
		}
	}
}

// A single generation is active, but a cancel can fail independently. Correlate
// available IDs first; uncorrelated control/server errors require a bounded
// connection close because continuing could leave the generation stuck forever.
func responsesWSErrorEndsRequest(event responsesWSErrorEvent, streamID, responseID string, control []byte) (terminal, ambiguous, controlError bool) {
	if len(control) > 0 {
		var pending struct {
			EventID    string `json:"event_id"`
			StreamID   string `json:"stream_id"`
			ResponseID string `json:"response_id"`
		}
		_ = common.Unmarshal(control, &pending)
		if event.EventID != "" && event.EventID == pending.EventID {
			return false, false, true
		}
		if event.ResponseID != "" && event.ResponseID == pending.ResponseID && event.ResponseID != responseID {
			return false, false, true
		}
		if (event.StreamID == "" || event.StreamID == pending.StreamID) && event.Error != nil && event.Error.Type == "invalid_request_error" {
			switch event.Error.Code {
			case "response_not_found", "response_not_active", "response_already_completed":
				return false, false, true
			}
		}
	}
	if event.StreamID != "" && event.StreamID != streamID || responseID != "" && event.ResponseID != "" && event.ResponseID != responseID {
		return false, false, false
	}
	return true, len(control) > 0 && event.ResponseID == "", false
}

func (s *responsesWSSession) restoreConnectionContext(c *gin.Context, model string) *types.NewAPIError {
	// The channel type is not rechecked: a locked channel keeps its type, and
	// the FilterResponsesWebSocket rules were applied when it was selected.
	channel, err := appmodel.CacheGetChannel(s.lockedChannelID)
	if err != nil || channel == nil || channel.Status != common.ChannelStatusEnabled || !channel.GetSetting().ResponsesWebSocketEnabled {
		return types.NewErrorWithStatusCode(errors.New("Responses WebSocket is disabled for this channel"), types.ErrorCode(appdto.FilterResponsesWebSocket), http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	keyEnabled := channel.Key == s.lockedKey
	if channel.ChannelInfo.IsMultiKey {
		keys := channel.GetKeys()
		status := channel.ChannelInfo.MultiKeyStatusList[s.lockedKeyIndex]
		keyEnabled = s.lockedKeyIndex >= 0 && s.lockedKeyIndex < len(keys) && keys[s.lockedKeyIndex] == s.lockedKey && (status == 0 || status == common.ChannelStatusEnabled)
	}
	if !keyEnabled {
		return types.NewErrorWithStatusCode(errors.New("the upstream connection credential is no longer enabled"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	// Changes that alter the physical upstream connection require a new
	// handshake. Request-level settings can be refreshed without rotating keys.
	if channel.GetBaseURL() != s.lockedContext[appconstant.ContextKeyChannelBaseUrl] ||
		!reflect.DeepEqual(channel.GetHeaderOverride(), s.lockedContext[appconstant.ContextKeyChannelHeaderOverride]) {
		return types.NewErrorWithStatusCode(errors.New("upstream connection settings changed; reconnect required"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	if previous, ok := s.lockedContext[appconstant.ContextKeyChannelSetting].(dto.ChannelSettings); ok && previous.Proxy != channel.GetSetting().Proxy {
		return types.NewErrorWithStatusCode(errors.New("upstream proxy changed; reconnect required"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	// An advanced custom connection was dialed through the route matched for
	// this model. Only the fields that shape the upstream handshake force a
	// reconnect: target path, protocol conversion and credential placement.
	// Other channel types match the zero route on both sides.
	if pinned, ok := s.lockedContext[appconstant.ContextKeyProviderPlugin]; ok {
		c.Set(string(appconstant.ContextKeyProviderPlugin), pinned)
	}
	otherSettings := connectionOtherSettings(c, channel)
	route, _ := otherSettings.AdvancedCustom.MatchPathForModel(c.Request.URL.Path, model)
	if strings.TrimSpace(route.UpstreamPath) != strings.TrimSpace(s.lockedRoute.UpstreamPath) ||
		route.IsNative() != s.lockedRoute.IsNative() ||
		!reflect.DeepEqual(route.Auth, s.lockedRoute.Auth) {
		return types.NewErrorWithStatusCode(errors.New("upstream route changed; reconnect required"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	if pin, found, _ := service.GetChannelConstraints(c).ResolvedPin(); found {
		if pin.ChannelId != s.lockedChannelID {
			return types.NewErrorWithStatusCode(errors.New("channel pin changed; reconnect required"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
		}
	} else {
		group := common.GetContextKeyString(c, appconstant.ContextKeyUsingGroup)
		if group == "auto" {
			if !slices.Contains(service.GetRequestAutoGroups(c, common.GetContextKeyString(c, appconstant.ContextKeyUserGroup)), s.lockedGroup) {
				return types.NewErrorWithStatusCode(errors.New("the connection group is no longer allowed"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
			}
			group = s.lockedGroup
			common.SetContextKey(c, appconstant.ContextKeyAutoGroup, group)
		}
		if !appmodel.IsChannelEnabledForGroupModel(group, model, s.lockedChannelID) {
			return types.NewErrorWithStatusCode(errResponsesWSChannelMoved, types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
		}
	}
	for key, value := range s.lockedContext {
		c.Set(string(key), value)
	}
	common.SetContextKey(c, appconstant.ContextKeyChannelSetting, channel.GetSetting())
	common.SetContextKey(c, appconstant.ContextKeyChannelOtherSetting, otherSettings)
	common.SetContextKey(c, appconstant.ContextKeyChannelParamOverride, channel.GetParamOverride())
	common.SetContextKey(c, appconstant.ContextKeyChannelModelMapping, channel.GetModelMapping())
	common.SetContextKey(c, appconstant.ContextKeyChannelStatusCodeMapping, channel.GetStatusCodeMapping())
	if err := service.ApplyChannelLimitContext(c, channel); err != nil {
		return err
	}
	policy := service.RequestPolicy(c)
	policy.BeginAttempt(channel, s.lockedGroup)
	policy.AddEvent(service.PolicyEvent{ChannelID: channel.Id, Decision: service.PolicyDecision{Action: "select", Reason: "pinned_channel", Source: "channel_constraint"}})
	return nil
}

// connectionOtherSettings is the channel's other settings as its requests use
// them: a task plugin channel runs on the relay routes of the plugin pinned
// for the connection, with the channel's plugin config.
func connectionOtherSettings(c *gin.Context, channel *appmodel.Channel) dto.ChannelOtherSettings {
	otherSettings := channel.GetOtherSettings()
	pinned, _ := common.GetContextKey(c, appconstant.ContextKeyProviderPlugin)
	if plugin, ok := pinned.(jsplugin.PinnedPlugin); ok && channel.Type == appconstant.ChannelTypeTaskPlugin {
		otherSettings.AdvancedCustom = plugin.Plugin.Meta.ChannelRelayConfig(plugin.Plugin.ChannelConfigFor(otherSettings.PluginConfig))
	}
	return otherSettings
}

// reloadResponsesWSCreate decodes a create body the request plugins' ingress
// changed, with the checks the first decode applied.
func reloadResponsesWSCreate(c *gin.Context, create *responsesWSCreateRequest) *types.NewAPIError {
	storage, err := common.GetBodyStorage(c)
	var body []byte
	if err == nil {
		body, err = storage.Bytes()
	}
	var request dto.OpenAIResponsesRequest
	if err == nil {
		err = common.Unmarshal(body, &request)
	}
	if err == nil && helper.ExceedsMaxTokensLimit(request.MaxOutputTokens) {
		err = errors.New("max_output_tokens is invalid")
	}
	if err != nil {
		return newResponsesWSInvalidRequestError(err)
	}
	// 插件只接收 HTTP 形状的正文，将其改动写回原事件，保留封套身份和显式传输参数。
	original := ojson.NewDoc(create.RawFrame)
	updated := ojson.NewDoc(body)
	source := original
	envelopeKeys := []string{"type", "event_id", "background", "stream", "stream_options", "generate", "stream_id"}
	wrapped := original.Get("response").IsObject()
	if wrapped {
		source = ojson.NewDoc([]byte(original.Get("response").Raw))
		envelopeKeys = []string{"generate", "stream_id"}
	}
	for _, key := range envelopeKeys {
		if err = relaycommon.DeleteJSONMember(updated, key); err != nil {
			return newResponsesWSInvalidRequestError(err)
		}
		if value := source.Get(key); value.Exists() {
			if err = updated.SetRawBytes(key, []byte(value.Raw)); err != nil {
				return newResponsesWSInvalidRequestError(err)
			}
		}
	}
	if wrapped {
		if err = original.SetRawBytes("response", updated.Bytes()); err != nil {
			return newResponsesWSInvalidRequestError(err)
		}
		create.RawFrame = original.Bytes()
	} else {
		create.RawFrame = updated.Bytes()
	}
	request.Stream = nil
	request.StreamOptions = nil
	create.Request = request
	create.Body = body
	return nil
}

// recordResponsesWSPluginError writes the error log of a create a request
// plugin stopped; it shows what the plugins did.
func recordResponsesWSPluginError(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) {
	switch apiErr.GetErrorCode() {
	case types.ErrorCodeRequestRejected, types.ErrorCodeRequestPluginFailed:
		service.RecordRelayErrorLog(c, info.ChannelId, apiErr, info)
	}
}

func buildResponsesWSCreatePayload(c *gin.Context, info *relaycommon.RelayInfo, req dto.OpenAIResponsesRequest, generate common.RawMessage, streamID string, source ...[]byte) ([]byte, *types.NewAPIError) {
	var raw []byte
	if len(source) > 0 {
		raw = source[0]
	}
	_, body, closer, apiErr := prepareResponsesRequest(c, info, &req, raw)
	if apiErr != nil {
		return nil, apiErr
	}
	defer closer.Close()
	jsonData, err := io.ReadAll(body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
	}
	event, err := buildResponsesWSCreateEvent(jsonData, generate, streamID)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	return event, nil
}

func (s *responsesWSSession) startTargetReader(target *websocket.Conn, first ...responsesWSMessage) {
	s.connectionMu.Lock()
	done := s.targetDone
	s.connectionMu.Unlock()
	limit := int64(helper.DefaultMaxScannerBufferSize)
	if appconstant.StreamScannerMaxBufferMB > 0 {
		limit = int64(appconstant.StreamScannerMaxBufferMB) << 20
	}
	target.SetReadLimit(limit)
	s.workers.Go(func() {
		for {
			var incoming responsesWSMessage
			if len(first) > 0 {
				incoming, first = first[0], nil
			} else {
				incoming.kind, incoming.body, incoming.err = target.ReadMessage()
			}
			select {
			case <-done:
				return
			default:
			}
			kind, body, err := incoming.kind, incoming.body, incoming.err
			if state := s.getCurrent(); state != nil {
				select {
				case <-done:
					return
				case state.inbox <- incoming:
					if err != nil {
						return
					}
					continue
				case <-state.done:
				case <-s.ctx.Done():
					return
				}
			}
			// No request owns this message. An idle upstream that closed
			// normally ends the client session with the same code.
			if err != nil {
				code := websocket.CloseInternalServerErr
				var closed *websocket.CloseError
				if errors.As(err, &closed) && (closed.Code == websocket.CloseNormalClosure || closed.Code == websocket.CloseGoingAway || (closed.Code >= 3000 && closed.Code < 5000)) {
					code = closed.Code
				}
				s.shutdown(code, "upstream connection closed")
				return
			}
			if writeErr := s.writeClient(kind, body); writeErr != nil {
				s.shutdown(websocket.CloseInternalServerErr, "")
				return
			}
		}
	})
}

func (s *responsesWSSession) getCurrent() *responsesWSCallState {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.current
}

func (s *responsesWSSession) tryReserveCurrent(state *responsesWSCallState) bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.current != nil {
		return false
	}
	s.current = state
	return true
}

func (s *responsesWSSession) getTarget() *websocket.Conn {
	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()
	return s.target
}

func (s *responsesWSSession) setTarget(target *websocket.Conn) bool {
	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()
	if s.ctx.Err() != nil {
		_ = target.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
		_ = target.Close()
		return false
	}
	s.target = target
	s.targetDone = make(chan struct{})
	return true
}

// retireTarget 仅在当前轮尚未向客户端交付输出时释放失效的物理连接。
func (s *responsesWSSession) retireTarget() {
	s.connectionMu.Lock()
	target, unregister := s.target, s.unregister
	if s.targetDone != nil {
		close(s.targetDone)
	}
	s.target, s.unregister, s.targetDone = nil, nil, nil
	s.connectionMu.Unlock()
	if unregister != nil {
		unregister()
	}
	if target != nil {
		_ = target.Close()
	}
	s.lockedModel, s.lockedGroup, s.lockedKey, s.lastResponseID = "", "", "", ""
	s.lockedChannelID, s.lockedKeyIndex, s.lockedContext = 0, 0, nil
}

func (s *responsesWSSession) writeTarget(kind int, message []byte) error {
	s.targetWriteMu.Lock()
	defer s.targetWriteMu.Unlock()
	target := s.getTarget()
	if target == nil {
		return errors.New("responses websocket upstream is not connected")
	}
	if err := target.SetWriteDeadline(time.Now().Add(responsesWSWriteTimeout)); err != nil {
		return err
	}
	return target.WriteMessage(kind, message)
}

func (s *responsesWSSession) writeClient(kind int, message []byte) error {
	s.clientWriteMu.Lock()
	defer s.clientWriteMu.Unlock()
	if err := s.client.SetWriteDeadline(s.clientWriteBudget.Deadline(time.Now(), len(message))); err != nil {
		return err
	}
	return s.client.WriteMessage(kind, message)
}

func (s *responsesWSSession) sendError(eventID, streamID string, apiErr *types.NewAPIError) {
	if apiErr == nil {
		return
	}
	payload, err := buildResponsesWSErrorPayload(eventID, streamID, apiErr)
	if err == nil {
		_ = s.writeClient(websocket.TextMessage, payload)
	}
}

// shutdown ends the connection once. Each peer gets a close frame that says
// why, bounded to one second, and both sockets close even if a write failed.
// Close frames are control writes, which gorilla allows alongside a blocked
// data writer, so shutdown takes no write lock and never waits for workers.
func (s *responsesWSSession) shutdown(code int, reason string) {
	s.closeOnce.Do(func() {
		s.cancel()
		s.connectionMu.Lock()
		target, unregister := s.target, s.unregister
		s.target, s.unregister = nil, nil
		s.connectionMu.Unlock()
		if unregister != nil {
			unregister()
		}
		closeMessage := websocket.FormatCloseMessage(code, reason)
		_ = s.client.WriteControl(websocket.CloseMessage, closeMessage, time.Now().Add(time.Second))
		if target != nil {
			_ = target.WriteControl(websocket.CloseMessage, closeMessage, time.Now().Add(time.Second))
			_ = target.Close()
		}
		_ = s.client.Close()
	})
}

func (s *responsesWSSession) registerChannelClose(channelID int) {
	unregister := wsmanager.Register(channelID, wsmanager.KindResponses, s.closeForPolicy)
	s.connectionMu.Lock()
	if s.ctx.Err() != nil {
		s.connectionMu.Unlock()
		unregister()
		return
	}
	s.unregister = unregister
	s.connectionMu.Unlock()
}

// closeForPolicy tells both peers why the connection ends, like the realtime
// relay does.
func (s *responsesWSSession) closeForPolicy(reason string) {
	s.shutdown(websocket.ClosePolicyViolation, reason)
}

// Stream identity belongs to the WebSocket envelope. For the legacy wrapped
// input, the top-level field takes precedence over response.stream_id.
func parseResponsesWSEnvelope(message []byte) (responsesWSCreateEvent, string, error) {
	var event responsesWSCreateEvent
	if err := common.Unmarshal(message, &event); err != nil {
		return event, "", fmt.Errorf("invalid websocket event json: %w", err)
	}
	streamRaw := event.StreamID
	if len(streamRaw) == 0 && len(event.Request) > 0 {
		var wrapped struct {
			StreamID common.RawMessage `json:"stream_id"`
		}
		if err := common.Unmarshal(event.Request, &wrapped); err == nil {
			streamRaw = wrapped.StreamID
		}
	}
	var streamID string
	if len(streamRaw) > 0 {
		if err := common.Unmarshal(streamRaw, &streamID); err != nil || len(streamID) < 1 || len(streamID) > 256 {
			return event, "", errors.New("stream_id must contain 1-256 ASCII letters, digits, underscores, hyphens, or periods")
		}
		for _, char := range streamID {
			if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' || char == '.') {
				return event, "", errors.New("stream_id must contain only ASCII letters, digits, underscores, hyphens, or periods")
			}
		}
	}
	if strings.TrimSpace(event.Type) == "" {
		return event, streamID, errors.New("websocket event type is required")
	}
	return event, streamID, nil
}

func newResponsesWSInvalidRequestError(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

// normalizeResponsesWSCreateEvent builds the HTTP-shaped request body from a
// response.create whose envelope the read loop already parsed and validated.
// The body is the message with the WebSocket members removed, and is decoded
// once as the typed request; the envelope is not parsed again.
func normalizeResponsesWSCreateEvent(message []byte, event responsesWSCreateEvent, streamID string) (responsesWSCreateRequest, error) {
	create := responsesWSCreateRequest{RawFrame: message, StreamID: streamID, Generate: event.Generate, EventID: event.EventID}
	// Edit the wrapped request on its own so outer fields cannot enter the HTTP body.
	source := message
	if len(event.Request) > 0 {
		source = event.Request
	}
	if !ojson.GetBytesNoCopy(source, "@this").IsObject() {
		return create, errors.New("response.create request must be a JSON object")
	}
	doc := ojson.NewDoc(source)
	if len(event.Request) > 0 {
		if generate := doc.Get("generate"); len(create.Generate) == 0 && generate.Exists() {
			create.Generate = common.RawMessage(generate.Raw)
		}
	} else {
		for _, key := range []string{"type", "event_id", "background", "stream", "stream_options"} {
			if err := relaycommon.DeleteJSONMember(doc, key); err != nil {
				return create, err
			}
		}
	}
	for _, key := range []string{"generate", "stream_id"} {
		if err := relaycommon.DeleteJSONMember(doc, key); err != nil {
			return create, err
		}
	}
	payload := doc.Bytes()
	if err := common.Unmarshal(payload, &create.Request); err != nil {
		return create, err
	}
	if helper.ExceedsMaxTokensLimit(create.Request.MaxOutputTokens) {
		return create, errors.New("max_output_tokens is invalid")
	}
	create.Request.Stream = nil
	create.Request.StreamOptions = nil
	create.Body = payload
	return create, nil
}

func buildResponsesWSCreateEvent(jsonData []byte, generate common.RawMessage, streamID string) ([]byte, error) {
	if !ojson.ValidBytes(jsonData) || !ojson.GetBytesNoCopy(jsonData, "@this").IsObject() {
		return nil, errors.New("response.create body must be a JSON object")
	}
	event := ojson.NewDoc(jsonData)
	// The normalized body never carries stream_id; a channel parameter
	// override may inject one, and it must not shadow the envelope's value.
	keys := []string{"type", "stream_id", "event_id", "background", "stream", "stream_options"}
	if len(generate) > 0 {
		keys = append(keys, "generate")
	}
	for _, key := range keys {
		if err := relaycommon.DeleteJSONMember(event, key); err != nil {
			return nil, err
		}
	}
	if err := event.Set("type", responsesWSEventTypeResponseCreate); err != nil {
		return nil, err
	}
	if streamID != "" {
		if err := event.Set("stream_id", streamID); err != nil {
			return nil, err
		}
	}
	if len(generate) > 0 {
		if err := event.SetRawBytes("generate", generate); err != nil {
			return nil, err
		}
	}
	return event.Bytes(), nil
}

func buildResponsesWSErrorPayload(eventID, streamID string, apiErr *types.NewAPIError, upstreamURLs ...string) ([]byte, error) {
	if apiErr == nil {
		return nil, errors.New("api error is nil")
	}
	status := apiErr.StatusCode
	if status == 0 {
		status = http.StatusInternalServerError
	}
	openaiErr := apiErr.ToOpenAIError(upstreamURLs...)
	return common.Marshal(&responsesWSErrorEvent{
		Type:     "error",
		Status:   status,
		EventID:  eventID,
		StreamID: streamID,
		Error:    &openaiErr,
	})
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
	for _, path := range []string{"status", "response.error.status_code", "error.status_code", "status_code"} {
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
