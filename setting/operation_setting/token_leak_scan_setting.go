package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

const (
	defaultTokenLeakScanIntervalHours = 24
	minTokenLeakScanIntervalHours     = 1
	maxTokenLeakScanIntervalHours     = 168
)

// TokenLeakScanSetting 定义用户令牌公开泄露扫描的持久化配置。
type TokenLeakScanSetting struct {
	Enabled       bool `json:"enabled"`
	IntervalHours int  `json:"interval_hours"`
}

var tokenLeakScanSetting = TokenLeakScanSetting{
	Enabled:       false,
	IntervalHours: defaultTokenLeakScanIntervalHours,
}

func init() {
	config.GlobalConfig.Register("token_leak_scan", &tokenLeakScanSetting)
}

// GetTokenLeakScanSetting 返回经过运行时边界归一化的泄露扫描配置。
//
// @return 当前泄露扫描配置。
func GetTokenLeakScanSetting() *TokenLeakScanSetting {
	if tokenLeakScanSetting.IntervalHours < minTokenLeakScanIntervalHours || tokenLeakScanSetting.IntervalHours > maxTokenLeakScanIntervalHours {
		tokenLeakScanSetting.IntervalHours = defaultTokenLeakScanIntervalHours
	}
	return &tokenLeakScanSetting
}

// ValidateTokenLeakScanInterval 校验泄露扫描周期小时数。
//
// @param intervalHours 扫描周期小时数。
// @return 周期是否位于允许范围内。
func ValidateTokenLeakScanInterval(intervalHours int) bool {
	return intervalHours >= minTokenLeakScanIntervalHours && intervalHours <= maxTokenLeakScanIntervalHours
}
