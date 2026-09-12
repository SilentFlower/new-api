package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// channelRuleOccurrence 是某个时段在给定时刻对应的一次 occurrence。
type channelRuleOccurrence struct {
	schedule   dto.ChannelBudgetSchedule
	start, end time.Time
}

// normalizeChannelBudgetSchedules 校验时段、建立稳定身份，并把删除转为停用。
// @param schedules 待保存的时段。
// @param previous 当前权威时段，用于保持身份。
// @param now 当前时间。
// @param idMap 输出：客户端临时 id 到服务端 id 的映射。
// @return 规范化时段及输入错误。
func normalizeChannelBudgetSchedules(schedules, previous []dto.ChannelBudgetSchedule, now time.Time, idMap map[string]string) ([]dto.ChannelBudgetSchedule, error) {
	invalid := func(message string) ([]dto.ChannelBudgetSchedule, error) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidChannelPeriodPolicy, message)
	}
	old := make(map[string]dto.ChannelBudgetSchedule, len(previous))
	for _, item := range previous {
		old[item.ID] = item
	}
	seen := make(map[string]bool)
	result := append([]dto.ChannelBudgetSchedule{}, schedules...)
	for i := range result {
		item := &result[i]
		existing, exists := old[item.ID]
		switch {
		case item.ID == "" || strings.HasPrefix(item.ID, "new-"):
			// 新时段由服务端建立身份；客户端临时 id 只用于同一次保存里的行引用。
			if item.ID != "" {
				if _, dup := idMap[item.ID]; dup || len(item.ID) > 64 {
					return invalid("临时时段 id 重复或过长")
				}
			}
			serverID := common.GetUUID()
			if item.ID != "" {
				idMap[item.ID] = serverID
			}
			item.ID, item.CreatedAt = serverID, now.Unix()
		case !exists || seen[item.ID]:
			return invalid("时段身份无效或重复，请刷新后重试")
		default:
			item.CreatedAt = existing.CreatedAt
		}
		seen[item.ID] = true
		item.Name = strings.TrimSpace(item.Name)
		if item.Name == "" || len([]rune(item.Name)) > 80 {
			return invalid("时段名称须为 1 至 80 字")
		}
		switch item.Kind {
		case "date_range":
			start, err := parseChannelRuleLocalTime(item.StartLocal, now.Location())
			if err != nil {
				return invalid(err.Error())
			}
			end, err := parseChannelRuleLocalTime(item.EndLocal, now.Location())
			if err != nil || !end.After(start) {
				return invalid("结束时间必须晚于开始时间且能唯一解释")
			}
			item.StartAt, item.EndAt = start.Unix(), end.Unix()
			item.StartWeekday, item.EndWeekday, item.StartTime, item.EndTime = 0, 0, "", ""
		case "weekly":
			if _, _, err := channelRuleWeekMinutes(*item); err != nil {
				return invalid(err.Error())
			}
			item.StartLocal, item.EndLocal, item.StartAt, item.EndAt = "", "", 0, 0
		default:
			return invalid("时段类型无效")
		}
		if exists && !sameChannelSchedule(existing, *item) {
			_, active, err := channelPeriodOccurrence(existing, now)
			_, newActive, newErr := channelPeriodOccurrence(*item, now)
			if err != nil || newErr != nil || active || newActive || (existing.Kind == "date_range" && existing.StartAt <= now.Unix()) {
				return invalid("已开始计量的时段不能修改起止边界，请停用并创建新时段")
			}
		}
	}
	// 移除仅转为停用，保留身份才能在恢复时继续使用原累计。
	for _, item := range previous {
		if !seen[item.ID] {
			item.Enabled = false
			result = append(result, item)
		}
	}
	if len(result) > 64 {
		return invalid("时段总数（含停用）不能超过 64")
	}
	return result, nil
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

func channelRuleWeekMinutes(schedule dto.ChannelBudgetSchedule) (int, int, error) {
	start, err := time.Parse("15:04", schedule.StartTime)
	end, endErr := time.Parse("15:04", schedule.EndTime)
	if err != nil || endErr != nil || schedule.StartWeekday < 0 || schedule.StartWeekday > 6 || schedule.EndWeekday < 0 || schedule.EndWeekday > 6 {
		return 0, 0, fmt.Errorf("每周时段须填写合法星期和 HH:mm 时间")
	}
	a := schedule.StartWeekday*1440 + start.Hour()*60 + start.Minute()
	b := schedule.EndWeekday*1440 + end.Hour()*60 + end.Minute()
	if a == b {
		return 0, 0, fmt.Errorf("每周时段起止时间不能相同")
	}
	if b < a {
		b += 7 * 1440
	}
	return a, b, nil
}

func sameChannelSchedule(a, b dto.ChannelBudgetSchedule) bool {
	return a.Kind == b.Kind && a.StartAt == b.StartAt && a.EndAt == b.EndAt && a.StartWeekday == b.StartWeekday && a.EndWeekday == b.EndWeekday && a.StartTime == b.StartTime && a.EndTime == b.EndTime
}

func channelSchedulesOverlap(a, b dto.ChannelBudgetSchedule) bool {
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

func channelPeriodOccurrence(schedule dto.ChannelBudgetSchedule, now time.Time) (channelRuleOccurrence, bool, error) {
	result := channelRuleOccurrence{schedule: schedule}
	if schedule.Kind == "date_range" {
		result.start, result.end = time.Unix(schedule.StartAt, 0), time.Unix(schedule.EndAt, 0)
		return result, !now.Before(result.start) && now.Before(result.end), nil
	}
	a, b, err := channelRuleWeekMinutes(schedule)
	if err != nil {
		return result, false, err
	}
	daysSinceMonday := (int(now.Weekday()) + 6) % 7
	monday := time.Date(now.Year(), now.Month(), now.Day()-daysSinceMonday, 0, 0, 0, 0, now.Location())
	for _, shift := range []int{-7, 0} {
		startDay, endDay := monday.AddDate(0, 0, a/1440+shift), monday.AddDate(0, 0, b/1440+shift)
		result.start, err = parseChannelRuleLocalTime(startDay.Format("2006-01-02")+"T"+schedule.StartTime, now.Location())
		if err != nil {
			return result, false, err
		}
		result.end, err = parseChannelRuleLocalTime(endDay.Format("2006-01-02")+"T"+schedule.EndTime, now.Location())
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
