package constant

import "time"

// ResponsesWebSocketFirstFrameTimeoutOptionKey 是首帧等待时间的站点设置键。
const ResponsesWebSocketFirstFrameTimeoutOptionKey = "ResponsesWebSocketFirstFrameTimeoutSeconds"

// DefaultResponsesWebSocketFirstFrameTimeoutSeconds 是客户端 WebSocket 首帧等待的默认秒数。
const DefaultResponsesWebSocketFirstFrameTimeoutSeconds = 60

// MaxResponsesWebSocketFirstFrameTimeoutSeconds 是能够安全转换为时长的最大完整秒数。
const MaxResponsesWebSocketFirstFrameTimeoutSeconds = int64((1<<63 - 1) / time.Second)

// ResponsesWebSocketFirstFrameTimeoutSeconds 保存启动时规范化的环境缺省，不承担动态更新。
var ResponsesWebSocketFirstFrameTimeoutSeconds = DefaultResponsesWebSocketFirstFrameTimeoutSeconds
