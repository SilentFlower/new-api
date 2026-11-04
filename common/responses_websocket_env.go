package common

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/constant"
)

// initResponsesWebSocketEnv 读取并规范化客户端 WebSocket 首帧等待配置。
func initResponsesWebSocketEnv() {
	const envName = "RESPONSES_WEBSOCKET_FIRST_FRAME_TIMEOUT_SECONDS"
	timeoutSeconds := GetEnvOrDefault(envName, constant.DefaultResponsesWebSocketFirstFrameTimeoutSeconds)
	// time.Duration 以纳秒存储，合法整数秒数在换算时仍可能溢出。
	if timeoutSeconds <= 0 || int64(timeoutSeconds) > int64((1<<63-1)/time.Second) {
		SysError(fmt.Sprintf("%s 必须是可安全转换为时长的正整数，使用默认值 %d", envName, constant.DefaultResponsesWebSocketFirstFrameTimeoutSeconds))
		timeoutSeconds = constant.DefaultResponsesWebSocketFirstFrameTimeoutSeconds
	}
	constant.ResponsesWebSocketFirstFrameTimeoutSeconds = timeoutSeconds
}
