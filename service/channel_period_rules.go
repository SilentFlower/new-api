package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

type channelRuleOccurrence struct {
	rule       dto.ChannelPeriodRule
	start, end time.Time
}

// NormalizeChannelPeriodConfig 校验规则、服务端时间和稳定计量身份。
// @param config 待保存或预览的策略副本。
// @param previous 当前权威策略，用于保持规则身份。
// @param now 当前时间。
// @return 规范化配置及输入错误。
func NormalizeChannelPeriodConfig(config, previous dto.ChannelPeriodPolicyConfig, now time.Time) (dto.ChannelPeriodPolicyConfig, error) {
	invalid := func(message string) (dto.ChannelPeriodPolicyConfig, error) {
		return config, fmt.Errorf("%w: %s", ErrInvalidChannelPeriodPolicy, message)
	}
	if config.SchemaVersion != 1 || config.PoolDailyQuotaLimit < 0 || config.PoolDailyQuotaLimit > common.MaxPeriodQuota || config.PoolWeeklyQuotaLimit < 0 || config.PoolWeeklyQuotaLimit > common.MaxPeriodQuota {
		return invalid("策略版本或池子额度无效")
	}
	if len(config.Rules) > 64 {
		return invalid("每个渠道最多保存 64 条时间规则")
	}
	config.Fallback.Model = strings.TrimSpace(config.Fallback.Model)
	if config.Fallback.ChannelID < 0 || len(config.Fallback.Model) > 255 || (config.Fallback.Enabled && (config.Fallback.ChannelID <= 0 || config.Fallback.Model == "")) {
		return invalid("降级目标渠道及模型无效")
	}
	oldRules := make(map[string]dto.ChannelPeriodRule, len(previous.Rules))
	for _, rule := range previous.Rules {
		oldRules[rule.ID] = rule
	}
	seen := make(map[string]bool)
	config.Rules = append([]dto.ChannelPeriodRule{}, config.Rules...)
	for i := range config.Rules {
		rule := &config.Rules[i]
		old, exists := oldRules[rule.ID]
		if rule.ID == "" {
			rule.ID = common.GetUUID()
			rule.CreatedAt = now.Unix()
		} else if !exists || seen[rule.ID] {
			return invalid("规则身份无效或重复，请刷新后重试")
		} else {
			rule.CreatedAt = old.CreatedAt
		}
		seen[rule.ID] = true
		rule.Name = strings.TrimSpace(rule.Name)
		if rule.Name == "" || len([]rune(rule.Name)) > 80 {
			return invalid("规则名称须为 1 至 80 字")
		}
		values := []*int64{rule.UserDailyQuotaLimit, rule.PoolDailyQuotaLimit, rule.UserPeriodQuotaLimit, rule.PoolPeriodQuotaLimit}
		hasLimit := false
		for _, value := range values {
			if value == nil {
				continue
			}
			hasLimit = true
			if *value < 0 || *value > common.MaxPeriodQuota {
				return invalid("规则额度须为合法非负整数")
			}
		}
		if !hasLimit {
			return invalid("规则至少填写一项额度，留空表示继承")
		}
		switch rule.Kind {
		case "date_range":
			start, err := parseChannelRuleLocalTime(rule.StartLocal, now.Location())
			if err != nil {
				return invalid(err.Error())
			}
			end, err := parseChannelRuleLocalTime(rule.EndLocal, now.Location())
			if err != nil || !end.After(start) {
				return invalid("结束时间必须晚于开始时间且能唯一解释")
			}
			rule.StartAt, rule.EndAt = start.Unix(), end.Unix()
			rule.StartWeekday, rule.EndWeekday, rule.StartTime, rule.EndTime = 0, 0, "", ""
		case "weekly":
			if _, _, err := channelRuleWeekMinutes(*rule); err != nil {
				return invalid(err.Error())
			}
			rule.StartLocal, rule.EndLocal, rule.StartAt, rule.EndAt = "", "", 0, 0
		default:
			return invalid("时间规则类型无效")
		}
		if exists && !sameChannelRuleSchedule(old, *rule) {
			_, active, err := channelPeriodOccurrence(old, now)
			_, newActive, newErr := channelPeriodOccurrence(*rule, now)
			if err != nil || newErr != nil || active || newActive || (old.Kind == "date_range" && old.StartAt <= now.Unix()) {
				return invalid("已开始计量的规则不能修改起止边界，请停用并创建新规则")
			}
		}
	}
	// 移除仅转为停用，保留身份才能在恢复时继续使用原累计。
	for _, old := range previous.Rules {
		if !seen[old.ID] {
			old.Enabled = false
			config.Rules = append(config.Rules, old)
		}
	}
	if len(config.Rules) > 64 {
		return invalid("规则总数（含停用）不能超过 64")
	}
	for i, a := range config.Rules {
		if !a.Enabled {
			continue
		}
		for _, b := range config.Rules[i+1:] {
			if !b.Enabled || a.Kind != b.Kind || !channelRulesShareMetric(a, b) {
				continue
			}
			if channelRulesOverlap(a, b) {
				return invalid("同级规则对同一指标存在时间重叠：" + a.Name + " / " + b.Name)
			}
		}
	}
	return config, nil
}

func parseChannelRuleLocalTime(value string, loc *time.Location) (time.Time, error) {
	const layout = "2006-01-02T15:04"
	parsed, err := time.ParseInLocation(layout, value, loc)
	if err != nil || parsed.Format(layout) != value {
		return time.Time{}, fmt.Errorf("日期时间无效，请使用 YYYY-MM-DDTHH:mm")
	}
	// 夏令时回拨会产生两个相同墙上时间，不能默选其中一个预算边界。
	_, offset := parsed.Zone()
	for hours := -48; hours <= 48; hours++ {
		_, otherOffset := parsed.Add(time.Duration(hours) * time.Hour).Zone()
		candidate := parsed.Add(time.Duration(offset-otherOffset) * time.Second)
		if !candidate.Equal(parsed) && candidate.Format(layout) == value {
			return time.Time{}, fmt.Errorf("时间因时区回拨存在歧义，请选择其他边界")
		}
	}
	return parsed, nil
}

func channelRuleWeekMinutes(rule dto.ChannelPeriodRule) (int, int, error) {
	start, err := time.Parse("15:04", rule.StartTime)
	end, endErr := time.Parse("15:04", rule.EndTime)
	if err != nil || endErr != nil || rule.StartWeekday < 0 || rule.StartWeekday > 6 || rule.EndWeekday < 0 || rule.EndWeekday > 6 {
		return 0, 0, fmt.Errorf("每周规则须填写合法星期和 HH:mm 时间")
	}
	a := rule.StartWeekday*1440 + start.Hour()*60 + start.Minute()
	b := rule.EndWeekday*1440 + end.Hour()*60 + end.Minute()
	if a == b {
		return 0, 0, fmt.Errorf("每周规则起止时间不能相同")
	}
	if b < a {
		b += 7 * 1440
	}
	return a, b, nil
}

func sameChannelRuleSchedule(a, b dto.ChannelPeriodRule) bool {
	return a.Kind == b.Kind && a.StartAt == b.StartAt && a.EndAt == b.EndAt && a.StartWeekday == b.StartWeekday && a.EndWeekday == b.EndWeekday && a.StartTime == b.StartTime && a.EndTime == b.EndTime
}

func channelRulesShareMetric(a, b dto.ChannelPeriodRule) bool {
	return a.UserDailyQuotaLimit != nil && b.UserDailyQuotaLimit != nil || a.PoolDailyQuotaLimit != nil && b.PoolDailyQuotaLimit != nil || a.UserPeriodQuotaLimit != nil && b.UserPeriodQuotaLimit != nil || a.PoolPeriodQuotaLimit != nil && b.PoolPeriodQuotaLimit != nil
}

func channelRulesOverlap(a, b dto.ChannelPeriodRule) bool {
	if a.Kind == "date_range" {
		return a.StartAt < b.EndAt && b.StartAt < a.EndAt
	}
	as, ae, _ := channelRuleWeekMinutes(a)
	bs, be, _ := channelRuleWeekMinutes(b)
	for _, shift := range []int{-10080, 0, 10080} {
		if as < be+shift && bs+shift < ae {
			return true
		}
	}
	return false
}

func channelPeriodOccurrence(rule dto.ChannelPeriodRule, now time.Time) (channelRuleOccurrence, bool, error) {
	result := channelRuleOccurrence{rule: rule}
	if rule.Kind == "date_range" {
		result.start, result.end = time.Unix(rule.StartAt, 0), time.Unix(rule.EndAt, 0)
		return result, !now.Before(result.start) && now.Before(result.end), nil
	}
	a, b, err := channelRuleWeekMinutes(rule)
	if err != nil {
		return result, false, err
	}
	daysSinceMonday := (int(now.Weekday()) + 6) % 7
	monday := time.Date(now.Year(), now.Month(), now.Day()-daysSinceMonday, 0, 0, 0, 0, now.Location())
	for _, shift := range []int{-7, 0} {
		startDay, endDay := monday.AddDate(0, 0, a/1440+shift), monday.AddDate(0, 0, b/1440+shift)
		result.start, err = parseChannelRuleLocalTime(startDay.Format("2006-01-02")+"T"+rule.StartTime, now.Location())
		if err != nil {
			return result, false, err
		}
		result.end, err = parseChannelRuleLocalTime(endDay.Format("2006-01-02")+"T"+rule.EndTime, now.Location())
		if err != nil {
			return result, false, err
		}
		if !now.Before(result.start) && now.Before(result.end) {
			return result, true, nil
		}
	}
	if !result.start.After(now) {
		result.start = result.start.AddDate(0, 0, 7)
		result.end = result.end.AddDate(0, 0, 7)
	}
	return result, false, nil
}

// resolveChannelPeriodSources 是预算计划的 v1 投影：五个固定指标槽位的生效额度与来源。
// 仅供 v1 预览与旧个人覆盖校验使用；判定与计数直接基于预算行。
func resolveChannelPeriodSources(config dto.ChannelPeriodPolicyConfig, userDaily int64, now time.Time) (map[string]int64, map[string]dto.ChannelPeriodSource, []channelRuleOccurrence, int64, error) {
	plan := buildChannelBudgetPlan(dto.ChannelPeriodPolicyView{Config: config}, userDaily, 0)
	res, err := resolveChannelBudgetRows(plan, now, "")
	if err != nil {
		return nil, nil, nil, 0, err
	}
	limits := map[string]int64{"user_daily": userDaily, "pool_daily": config.PoolDailyQuotaLimit, "pool_weekly": config.PoolWeeklyQuotaLimit, "user_custom": 0, "pool_custom": 0}
	groups := map[string]string{
		"user_daily":  channelBudgetScopeUser + "|" + channelBudgetWindowDaily + "|",
		"pool_daily":  channelBudgetScopePool + "|" + channelBudgetWindowDaily + "|",
		"pool_weekly": channelBudgetScopePool + "|" + channelBudgetWindowWeekly + "|",
		"user_custom": channelBudgetScopeUser + "|" + channelBudgetWindowOccurrence + "|",
		"pool_custom": channelBudgetScopePool + "|" + channelBudgetWindowOccurrence + "|",
	}
	sources := make(map[string]dto.ChannelPeriodSource, len(groups))
	for key, group := range groups {
		sources[key] = dto.ChannelPeriodSource{Kind: channelBudgetSourceDefault}
		row, ok := res.effectiveRow(plan, group)
		if !ok || row.ScheduleID == "" {
			continue
		}
		limits[key], sources[key] = row.Limit, res.source(row)
	}
	var active []channelRuleOccurrence
	for _, state := range res.activeSchedules {
		active = append(active, state.occ)
	}
	return limits, sources, active, res.next, nil
}
