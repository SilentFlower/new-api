package controller

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const responsesWebSocketFirstFrameTimeoutCode types.ErrorCode = "responses_websocket_first_frame_timeout"

// handleResponsesWebSocketFirstFrameReadError 区分首帧失败并只为超时发送重连恢复信号。
// @param c 当前请求上下文。
// @param conn 已升级的客户端连接。
// @param readErr 首帧读取返回的错误。
// @param timeout 本连接建立时冻结的首帧等待时长。
// @return 无返回值。
func handleResponsesWebSocketFirstFrameReadError(c *gin.Context, conn *websocket.Conn, readErr error, timeout time.Duration) {
	if websocket.IsCloseError(readErr, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
		logger.LogDebug(c, "Responses WebSocket 首帧读取结束：category=closed")
		return
	}
	if errors.Is(readErr, websocket.ErrReadLimit) {
		// Gorilla 已发送报文过大关闭帧，追加业务错误会丢失真实失败类别。
		logger.LogWarn(c, "Responses WebSocket 首帧读取失败：category=message_too_big")
		return
	}
	var networkErr net.Error
	if errors.As(readErr, &networkErr) && networkErr.Timeout() {
		logger.LogWarn(c, fmt.Sprintf("Responses WebSocket 首帧读取失败：category=timeout，timeout_seconds=%g", timeout.Seconds()))
		// 读取超时后原连接不可再次读取，调用方需要新连接与完整请求才能恢复。
		apiErr := types.NewErrorWithStatusCode(
			errors.New("Timed out waiting for the first WebSocket request; reconnect and resend the complete request"),
			responsesWebSocketFirstFrameTimeoutCode,
			http.StatusRequestTimeout,
		)
		writeResponsesWebSocketError(c, conn, apiErr)
		return
	}
	var closeErr *websocket.CloseError
	if errors.As(readErr, &closeErr) {
		logger.LogWarn(c, fmt.Sprintf("Responses WebSocket 首帧读取失败：category=closed，close_code=%d", closeErr.Code))
		return
	}
	// 底层可能已经发送协议关闭帧，或传输已断开；不将其伪装成可恢复超时。
	logger.LogWarn(c, "Responses WebSocket 首帧读取失败：category=read_failure")
}
