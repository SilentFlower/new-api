package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

// budgetRow 构造一条待保存的预算行；scheduleID 可用 new-* 临时 id 引用同一次保存的新时段。
func budgetRow(name, scope, window, scheduleID string, limit int64) dto.ChannelBudgetRow {
	return dto.ChannelBudgetRow{Name: name, Enabled: true, Scope: scope, Window: window, ScheduleID: scheduleID, Models: []string{}, Limit: limit, OnExceed: dto.ChannelBudgetAction{Mode: channelBudgetActionInherit}}
}

// budgetConfig 构造 v2 策略：默认动作为拒绝。
func budgetConfig(schedules []dto.ChannelBudgetSchedule, rows ...dto.ChannelBudgetRow) dto.ChannelPeriodPolicyConfig {
	if schedules == nil {
		schedules = []dto.ChannelBudgetSchedule{}
	}
	if rows == nil {
		rows = []dto.ChannelBudgetRow{}
	}
	return dto.ChannelPeriodPolicyConfig{SchemaVersion: channelBudgetSchemaVersion, DefaultOnExceed: dto.ChannelBudgetAction{Mode: channelBudgetActionReject}, Schedules: schedules, Budgets: rows}
}

// poolDailyConfig 只含一条池子每日行。
func poolDailyConfig(limit int64) dto.ChannelPeriodPolicyConfig {
	return budgetConfig(nil, budgetRow("池子每日", channelBudgetScopePool, channelBudgetWindowDaily, "", limit))
}

// dateRangeSchedule 构造待保存的日期区间时段。
func dateRangeSchedule(id, name, startLocal, endLocal string) dto.ChannelBudgetSchedule {
	return dto.ChannelBudgetSchedule{ID: id, Name: name, Enabled: true, Kind: "date_range", StartLocal: startLocal, EndLocal: endLocal}
}

// weeklySchedule 构造待保存的每周时段。
func weeklySchedule(id, name string, startWeekday int, startTime string, endWeekday int, endTime string) dto.ChannelBudgetSchedule {
	return dto.ChannelBudgetSchedule{ID: id, Name: name, Enabled: true, Kind: "weekly", StartWeekday: startWeekday, StartTime: startTime, EndWeekday: endWeekday, EndTime: endTime}
}

// mustNormalizeBudgetConfig 返回可直接写入数据库的规范策略。
func mustNormalizeBudgetConfig(t *testing.T, config dto.ChannelPeriodPolicyConfig, now time.Time) dto.ChannelPeriodPolicyConfig {
	t.Helper()
	normalized, err := NormalizeChannelBudgetConfig(config, defaultChannelBudgetConfig(), now)
	require.NoError(t, err)
	return normalized
}

// findMetric 返回第一条匹配范围与周期的指标。
func findMetric(status dto.ChannelPeriodStatus, scope, period string, enforced bool) (dto.ChannelPeriodMetric, bool) {
	for _, metric := range status.Metrics {
		if metric.Scope == scope && metric.Period == period && (!enforced || metric.Enforced) {
			return metric, true
		}
	}
	return dto.ChannelPeriodMetric{}, false
}
