package common

import (
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/constant"
)

// initResponsesWebSocketEnv 读取并规范化客户端 WebSocket 首帧等待配置。
func initResponsesWebSocketEnv() {
	const envName = "RESPONSES_WEBSOCKET_FIRST_FRAME_TIMEOUT_SECONDS"
	timeoutSeconds := GetEnvOrDefault(envName, constant.DefaultResponsesWebSocketFirstFrameTimeoutSeconds)
	if _, err := ParseResponsesWebSocketFirstFrameTimeoutSeconds(strconv.Itoa(timeoutSeconds)); err != nil {
		SysError(fmt.Sprintf("%s 必须是可安全转换为时长的正整数，使用默认值 %d", envName, constant.DefaultResponsesWebSocketFirstFrameTimeoutSeconds))
		timeoutSeconds = constant.DefaultResponsesWebSocketFirstFrameTimeoutSeconds
	}
	constant.ResponsesWebSocketFirstFrameTimeoutSeconds = timeoutSeconds
}
