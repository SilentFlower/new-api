package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type responsesCompactPassthroughWebSocketEvent struct {
	Type     string                                             `json:"type"`
	Response *responsesCompactPassthroughWebSocketEventResponse `json:"response,omitempty"`
	Item     *responsesCompactPassthroughWebSocketEventItem     `json:"item,omitempty"`
}

type responsesCompactPassthroughWebSocketEventResponse struct {
	Usage json.RawMessage `json:"usage"`
}

type responsesCompactPassthroughWebSocketEventItem struct {
	Type string `json:"type"`
}

func prepareResponsesCompactPassthroughWebSocketTurn(c *gin.Context, turn *responsesWebSocketTurn) ([]byte, *types.NewAPIError) {
	if c == nil || turn == nil {
		return nil, types.NewErrorWithStatusCode(errors.New("Responses Compact WebSocket turn is nil"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

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
	} else {
		resetMainRelayAttemptFields(turn.info, turn.selectionModel)
		turn.info.Request = &request
		turn.info.ResponsesCompactMode = turn.compactMode
		turn.info.ResponsesClientStream = false
		turn.info.RelayMode = relayconstant.RelayModeResponses
		turn.info.RelayFormat = types.RelayFormatOpenAIResponses
		turn.info.IsStream = true
		turn.info.ChannelMeta = nil
	}
	turn.info.RequestURLPath = "/v1/responses"

	if apiErr := relay.PrepareResponsesCompactPassthrough(c, turn.info); apiErr != nil {
		return nil, apiErr
	}
	if apiErr := prepareMainRelayBilling(c, turn.info); apiErr != nil {
		return nil, apiErr
	}
	return append([]byte(nil), turn.rawPayload...), nil
}

func handleResponsesCompactPassthroughWebSocketUpstreamEvent(c *gin.Context, turn *responsesWebSocketTurn, channel *model.Channel, payload []byte) bool {
	if turn == nil {
		return false
	}
	if turn.info != nil {
		turn.info.SetFirstResponseTime()
	}
	var event responsesCompactPassthroughWebSocketEvent
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
		var usage *dto.Usage
		validUsage := false
		if event.Response != nil {
			usage, validUsage = relay.ParseResponsesCompactPassthroughUsage(event.Response.Usage)
		}
		if !validUsage {
			logger.LogWarn(c, "Responses Compact WebSocket completed without valid usage; refunding turn")
			apiErr := types.NewErrorWithStatusCode(errors.New("Responses Compact WebSocket completed without valid usage"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			processResponsesWebSocketChannelError(c, turn, channel, apiErr, "completed_without_usage")
			refundResponsesWebSocketTurn(c, turn)
		} else {
			service.SetResponsesCompactAudit(c, turn.info, "completed")
			service.PostTextConsumeQuota(c, turn.info, usage, nil)
			if channel != nil {
				service.RecordChannelAffinity(c, channel.Id)
			}
		}
		if turn.info != nil && turn.info.ResponsesUsageInfo != nil && turn.info.ResponsesUsageInfo.CompactionOutputItemCount != 1 {
			logger.LogWarn(c, fmt.Sprintf("Responses Compact WebSocket compaction item count is %d", turn.info.ResponsesUsageInfo.CompactionOutputItemCount))
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
