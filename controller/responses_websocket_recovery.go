package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/tidwall/gjson"
)

// reconnectResponsesWebSocketTurn 在旧渠道失去当前分组或模型资格后准备新的上游连接
// 仅处理尚未预扣或发送的新轮次，已有 response ID 的增量由客户端完整重放
func reconnectResponsesWebSocketTurn(c *gin.Context, turn *responsesWebSocketTurn, upstream *websocket.Conn, guard *channelUserConcurrencyGuard) (*websocket.Conn, *model.Channel, *types.NewAPIError) {
	_ = upstream.Close()
	// 两个物理渠道可能共享同一父限制，重选前必须释放旧连接的租约
	if apiErr := finishChannelUserConcurrencyGuard(c, guard, nil); apiErr != nil {
		return nil, nil, apiErr
	}
	previousResponse := gjson.GetBytes(turn.rawPayload, "previous_response_id")
	if previousResponse.Type == gjson.String && previousResponse.Str != "" {
		// https://developers.openai.com/api/docs/guides/websocket-mode#reconnect-and-recover
		// 增量依赖原连接的上下文，不能直接交给新渠道或删除 ID 后作为新请求发送
		return nil, nil, types.WithOpenAIError(types.OpenAIError{
			Type:    "invalid_request_error",
			Code:    "previous_response_not_found",
			Param:   "previous_response_id",
			Message: "Previous response is unavailable after the channel changed. Retry with the full input context.",
		}, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	channel, apiErr := middleware.SelectResponsesWebSocketChannel(c, turn.selectionModel)
	if apiErr != nil {
		return nil, nil, apiErr
	}
	addUsedChannel(c, channel.Id)
	return responsesWebSocketTurnConnector(c, turn, channel, 0)
}
