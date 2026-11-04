package common

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResponsesWebSocketFirstFrameTimeoutEnv 验证部署配置、默认值和时长换算边界。
// @param t 测试上下文。
func TestResponsesWebSocketFirstFrameTimeoutEnv(t *testing.T) {
	original := constant.ResponsesWebSocketFirstFrameTimeoutSeconds
	t.Cleanup(func() { constant.ResponsesWebSocketFirstFrameTimeoutSeconds = original })

	type timeoutCase struct {
		name  string
		value string
		unset bool
		want  int
	}
	cases := []timeoutCase{
		{name: "未配置", unset: true, want: 60},
		{name: "空值", want: 60},
		{name: "有效配置", value: "120", want: 120},
		{name: "最小正数", value: "1", want: 1},
		{name: "非整数", value: "invalid", want: 60},
		{name: "小数", value: "1.5", want: 60},
		{name: "零值", value: "0", want: 60},
		{name: "负数", value: "-1", want: 60},
		{name: "时长溢出", value: "9223372037", want: 60},
		{name: "整数溢出", value: "18446744073709551615", want: 60},
	}
	if strconv.IntSize == 64 {
		maxSeconds := int64((1<<63 - 1) / time.Second)
		cases = append(cases, timeoutCase{name: "最大可表示时长", value: strconv.FormatInt(maxSeconds, 10), want: int(maxSeconds)})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("RESPONSES_WEBSOCKET_FIRST_FRAME_TIMEOUT_SECONDS", tc.value)
			if tc.unset {
				require.NoError(t, os.Unsetenv("RESPONSES_WEBSOCKET_FIRST_FRAME_TIMEOUT_SECONDS"))
			}

			initResponsesWebSocketEnv()

			assert.Equal(t, tc.want, constant.ResponsesWebSocketFirstFrameTimeoutSeconds)
			assert.Positive(t, time.Duration(constant.ResponsesWebSocketFirstFrameTimeoutSeconds)*time.Second)
		})
	}
}
