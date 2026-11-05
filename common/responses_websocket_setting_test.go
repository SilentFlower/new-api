package common

import (
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResponsesWebSocketFirstFrameSettingValidation 保护站点设置的整数与时长边界。
// @param t 测试上下文。
// @return 无返回值。
func TestResponsesWebSocketFirstFrameSettingValidation(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int64
	}{
		{value: "1", want: 1},
		{value: "120", want: 120},
		{value: "9223372036", want: 9223372036},
		{value: ""},
		{value: "0"},
		{value: "-1"},
		{value: "1.5"},
		{value: "invalid"},
		{value: "9223372037"},
		{value: "18446744073709551615"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			seconds, err := ParseResponsesWebSocketFirstFrameTimeoutSeconds(tc.value)
			if tc.want == 0 {
				require.Error(t, err)
				assert.Zero(t, seconds)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, seconds)
			assert.Positive(t, time.Duration(seconds)*time.Second)
		})
	}
}

// TestResponsesWebSocketFirstFrameSettingPriority 验证站点值、环境缺省和默认值的优先级。
// @param t 测试上下文。
// @return 无返回值。
func TestResponsesWebSocketFirstFrameSettingPriority(t *testing.T) {
	OptionMapRWMutex.Lock()
	previousOptions := OptionMap
	OptionMapRWMutex.Unlock()
	previousTimeout := constant.ResponsesWebSocketFirstFrameTimeoutSeconds
	t.Cleanup(func() {
		OptionMapRWMutex.Lock()
		OptionMap = previousOptions
		OptionMapRWMutex.Unlock()
		constant.ResponsesWebSocketFirstFrameTimeoutSeconds = previousTimeout
	})
	for _, tc := range []struct {
		name    string
		options map[string]string
		env     int
		want    time.Duration
	}{
		{name: "无站点值使用环境缺省", env: 120, want: 120 * time.Second},
		{name: "站点值覆盖环境缺省", options: map[string]string{constant.ResponsesWebSocketFirstFrameTimeoutOptionKey: "180"}, env: 120, want: 180 * time.Second},
		{name: "非法历史值回退环境缺省", options: map[string]string{constant.ResponsesWebSocketFirstFrameTimeoutOptionKey: "0"}, env: 120, want: 120 * time.Second},
		{name: "缺省异常仍使用六十秒", env: 0, want: 60 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			OptionMapRWMutex.Lock()
			OptionMap = tc.options
			OptionMapRWMutex.Unlock()
			constant.ResponsesWebSocketFirstFrameTimeoutSeconds = tc.env
			assert.Equal(t, tc.want, GetResponsesWebSocketFirstFrameTimeout())
		})
	}
}

// TestResponsesWebSocketFirstFrameSettingConcurrentRead 验证更新与握手读取共享同一把锁。
// @param t 测试上下文。
// @return 无返回值。
func TestResponsesWebSocketFirstFrameSettingConcurrentRead(t *testing.T) {
	OptionMapRWMutex.Lock()
	previousOptions := OptionMap
	OptionMap = map[string]string{constant.ResponsesWebSocketFirstFrameTimeoutOptionKey: "120"}
	OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		OptionMapRWMutex.Lock()
		OptionMap = previousOptions
		OptionMapRWMutex.Unlock()
	})
	start := make(chan struct{})
	observed := make(chan time.Duration, 1)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		OptionMapRWMutex.Lock()
		OptionMap[constant.ResponsesWebSocketFirstFrameTimeoutOptionKey] = "180"
		OptionMapRWMutex.Unlock()
	}()
	go func() {
		defer workers.Done()
		<-start
		observed <- GetResponsesWebSocketFirstFrameTimeout()
	}()
	close(start)
	workers.Wait()
	assert.Contains(t, []time.Duration{120 * time.Second, 180 * time.Second}, <-observed)
	assert.Equal(t, 180*time.Second, GetResponsesWebSocketFirstFrameTimeout())
}
