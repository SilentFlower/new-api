package service

import (
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/dto"
)

// 预算行是所有渠道金额限制在内核中的统一表示：范围 × 周期 × 模型 × 上限 × 超限动作。
const (
	channelBudgetScopeUser = "user"
	channelBudgetScopePool = "pool"

	channelBudgetWindowDaily      = "daily"
	channelBudgetWindowWeekly     = "weekly"
	channelBudgetWindowOccurrence = "occurrence"

	channelBudgetActionInherit  = "inherit"
	channelBudgetActionReject   = "reject"
	channelBudgetActionFallback = "fallback"

	channelBudgetSourceDefault  = "default"
	channelBudgetSourcePersonal = "personal"

	// 由 v1 迁移派生的固定行 id；这些身份沿用旧 Redis key。
	legacyUserDailyBudgetID  = "legacy-user-daily"
	legacyUserWeeklyBudgetID = "legacy-user-weekly"
	legacyPoolDailyBudgetID  = "legacy-pool-daily"
	legacyPoolWeeklyBudgetID = "legacy-pool-weekly"
)

// channelBudgetSchedule 是带解析上下文的时段。
type channelBudgetSchedule struct {
	ID      string
	Name    string
	Enabled bool
	Kind    string
	rule    dto.ChannelBudgetSchedule
}

// channelBudgetRow 是一条预算行及其内核方法。
type channelBudgetRow struct {
	dto.ChannelBudgetRow
}

// channelBudgetPlan 是一个渠道当前版本的全部时段与预算行。
type channelBudgetPlan struct {
	Tracking        *dto.ChannelModelUsageTracking
	Revision        int
	DefaultOnExceed dto.ChannelBudgetAction
	Schedules       []channelBudgetSchedule
	Rows            []channelBudgetRow
}

// channelBudgetScheduleState 是某个时段在给定时刻的 occurrence 与生效状态。
type channelBudgetScheduleState struct {
	schedule channelBudgetSchedule
	occ      channelRuleOccurrence
	// active 表示当前时刻落在 occurrence 内，与 Enabled 无关；停用时段仍计数但不拦截。
	active bool
}

// channelBudgetResolution 是计划在给定时刻、给定模型下的解析结果。
type channelBudgetResolution struct {
	tracking  *dto.ChannelModelUsageTracking
	now       time.Time
	modelName string
	schedules map[string]channelBudgetScheduleState
	// activeSchedules 按 weekly 类先于 date_range、同类按配置顺序列出当前 occurrence 内的启用时段。
	activeSchedules []channelBudgetScheduleState
	next            int64
	// effective 记录每个分组当前生效行在 plan.Rows 中的下标。
	effective map[string]int
}

// buildChannelBudgetPlan 由 v2 策略构造预算计划。
// @param view 当前权威策略。
// @return 预算计划。
func buildChannelBudgetPlan(view dto.ChannelPeriodPolicyView) channelBudgetPlan {
	plan := channelBudgetPlan{Tracking: view.Config.ModelUsageTracking, Revision: view.Revision, DefaultOnExceed: view.Config.DefaultOnExceed}
	for _, item := range view.Config.Schedules {
		plan.Schedules = append(plan.Schedules, channelBudgetSchedule{ID: item.ID, Name: item.Name, Enabled: item.Enabled, Kind: item.Kind, rule: item})
	}
	for _, item := range view.Config.Budgets {
		plan.Rows = append(plan.Rows, channelBudgetRow{ChannelBudgetRow: item})
	}
	return plan
}

// channelBudgetBaseRows 返回始终计数的渠道级日/周身份：即使没有对应行，用量也要持续累计，便于以后加行时有历史。
func channelBudgetBaseRows() []channelBudgetRow {
	return []channelBudgetRow{
		{ChannelBudgetRow: dto.ChannelBudgetRow{ID: legacyUserDailyBudgetID, Enabled: true, Scope: channelBudgetScopeUser, Window: channelBudgetWindowDaily}},
		{ChannelBudgetRow: dto.ChannelBudgetRow{ID: legacyUserWeeklyBudgetID, Enabled: true, Scope: channelBudgetScopeUser, Window: channelBudgetWindowWeekly}},
	}
}

// channelBudgetModelsKey 返回排序去重后的模型选择器字符串，空表示全部模型。
func channelBudgetModelsKey(models []string) string {
	if len(models) == 0 {
		return ""
	}
	sorted := append([]string(nil), models...)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

// groupKey 标识"同一约束槽位"：同组内只有一行生效，不同组并行约束。
func (row channelBudgetRow) groupKey() string {
	return row.Scope + "|" + row.Window + "|" + channelBudgetModelsKey(row.Models)
}

// identityKey 标识计数身份：同身份的个人行与池子行共用一个计数。
func (row channelBudgetRow) identityKey() string {
	schedule := ""
	if row.Window == channelBudgetWindowOccurrence {
		schedule = row.ScheduleID
	}
	return row.Window + "|" + schedule + "|" + channelBudgetModelsKey(row.Models)
}

// identityHash 为非遗留身份生成短哈希，用作 Redis key 片段。
func (row channelBudgetRow) identityHash() string {
	sum := sha1.Sum([]byte(row.identityKey()))
	return hex.EncodeToString(sum[:])[:12]
}

// matchesModel 判断行是否命中请求模型；空模型名表示展示全部行。
func (row channelBudgetRow) matchesModel(modelName string) bool {
	if len(row.Models) == 0 || modelName == "" {
		return true
	}
	for _, item := range row.Models {
		if item == modelName {
			return true
		}
	}
	return false
}

// precedence 返回同组内的优先级：date_range 时段 > weekly 时段 > 无时段；同级后者覆盖前者。
func (row channelBudgetRow) precedence(schedules map[string]channelBudgetScheduleState) int {
	if row.ScheduleID == "" {
		return 0
	}
	if schedules[row.ScheduleID].schedule.Kind == "date_range" {
		return 2
	}
	return 1
}

// isChannelUserRow 判断是否为渠道级（不限模型）的个人日/周行：这类行沿用旧错误码。
func (row channelBudgetRow) isChannelUserRow() bool {
	return row.Scope == channelBudgetScopeUser && len(row.Models) == 0 && (row.Window == channelBudgetWindowDaily || row.Window == channelBudgetWindowWeekly)
}

// resolveChannelBudgetRows 解析时段状态、下一切换点与每组生效行。
// @param plan 预算计划。
// @param now 解析时刻。
// @param modelName 请求原始模型名，空表示展示全部行。
// @return 解析结果或时段解析错误。
func resolveChannelBudgetRows(plan channelBudgetPlan, now time.Time, modelName string) (channelBudgetResolution, error) {
	res := channelBudgetResolution{tracking: plan.Tracking, now: now, modelName: modelName, schedules: make(map[string]channelBudgetScheduleState, len(plan.Schedules)), effective: make(map[string]int)}
	for _, kind := range []string{"weekly", "date_range"} {
		for _, schedule := range plan.Schedules {
			if schedule.Kind != kind {
				continue
			}
			occ, active, err := channelPeriodOccurrence(schedule.rule, now)
			if err != nil {
				return res, err
			}
			state := channelBudgetScheduleState{schedule: schedule, occ: occ, active: active}
			res.schedules[schedule.ID] = state
			// 停用时段只计数不展示、不拦截，也不参与下一切换点。
			if !schedule.Enabled {
				continue
			}
			if active {
				res.activeSchedules = append(res.activeSchedules, state)
			}
			boundary := occ.start.Unix()
			if active {
				boundary = occ.end.Unix()
			}
			if boundary > now.Unix() && (res.next == 0 || boundary < res.next) {
				res.next = boundary
			}
		}
	}
	for i, row := range plan.Rows {
		if !res.eligible(row) {
			continue
		}
		key := row.groupKey()
		current, exists := res.effective[key]
		if !exists || row.precedence(res.schedules) >= plan.Rows[current].precedence(res.schedules) {
			res.effective[key] = i
		}
	}
	return res, nil
}

// eligible 判断行此刻是否可参与生效判定。
func (res channelBudgetResolution) eligible(row channelBudgetRow) bool {
	if !row.Enabled || !row.matchesModel(res.modelName) {
		return false
	}
	if row.ScheduleID == "" {
		return true
	}
	state, ok := res.schedules[row.ScheduleID]
	return ok && state.active && state.schedule.Enabled
}

// counting 判断行此刻是否需要累计：停用行仍计数，时段外的整段行不计数。
func (res channelBudgetResolution) counting(row channelBudgetRow) bool {
	if !row.matchesModel(res.modelName) {
		return false
	}
	if row.Window != channelBudgetWindowOccurrence {
		return true
	}
	return res.schedules[row.ScheduleID].active
}

// effectiveRow 返回分组当前生效行；不存在时返回 false。
func (res channelBudgetResolution) effectiveRow(plan channelBudgetPlan, groupKey string) (channelBudgetRow, bool) {
	index, ok := res.effective[groupKey]
	if !ok {
		return channelBudgetRow{}, false
	}
	return plan.Rows[index], true
}

// source 返回行的指标来源描述。
func (res channelBudgetResolution) source(row channelBudgetRow) dto.ChannelPeriodSource {
	if row.ScheduleID == "" {
		return dto.ChannelPeriodSource{Kind: channelBudgetSourceDefault}
	}
	state := res.schedules[row.ScheduleID]
	return dto.ChannelPeriodSource{Kind: state.schedule.Kind, ScheduleID: state.schedule.ID, ScheduleName: state.schedule.Name, StartAt: state.occ.start.Unix(), EndAt: state.occ.end.Unix()}
}
