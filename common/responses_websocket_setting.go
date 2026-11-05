package common

import (
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/constant"
)

// ParseResponsesWebSocketFirstFrameTimeoutSeconds 校验首帧等待时间及秒转时长的安全边界。
// @param value 待解析的整数秒数字符串。
// @return 有效秒数；非法或无法安全转换为时长时返回错误。
func ParseResponsesWebSocketFirstFrameTimeoutSeconds(value string) (int64, error) {
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds <= 0 || seconds > constant.MaxResponsesWebSocketFirstFrameTimeoutSeconds {
		return 0, fmt.Errorf("Responses WebSocket 首帧等待时间必须是 1 到 %d 之间的整数秒", constant.MaxResponsesWebSocketFirstFrameTimeoutSeconds)
	}
	return seconds, nil
}

// GetResponsesWebSocketFirstFrameTimeout 返回新连接使用的有效首帧等待时长。
// @return 依次由有效站点值、启动环境缺省或 60 秒默认值确定的时长。
func GetResponsesWebSocketFirstFrameTimeout() time.Duration {
	OptionMapRWMutex.RLock()
	value, exists := OptionMap[constant.ResponsesWebSocketFirstFrameTimeoutOptionKey]
	OptionMapRWMutex.RUnlock()
	if exists {
		if seconds, err := ParseResponsesWebSocketFirstFrameTimeoutSeconds(value); err == nil {
			return time.Duration(seconds) * time.Second
		}
	}
	seconds, err := ParseResponsesWebSocketFirstFrameTimeoutSeconds(strconv.Itoa(constant.ResponsesWebSocketFirstFrameTimeoutSeconds))
	if err != nil {
		seconds = constant.DefaultResponsesWebSocketFirstFrameTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}
