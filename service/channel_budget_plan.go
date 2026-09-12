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
// 阶段一由 v1 策略与渠道列派生，阶段二直接来自 v2 配置。
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

	legacyUserDailyBudgetID  = "legacy-user-daily"
	legacyUserWeeklyBudgetID = "legacy-user-weekly"
	legacyPoolDailyBudgetID  = "legacy-pool-daily"
	legacyPoolWeeklyBudgetID = "legacy-pool-weekly"
)

// channelBudgetAction 描述某行或策略级的超限动作。
type channelBudgetAction struct {
	Mode      string
	ChannelID int
	Model     string
}

// channelBudgetSchedule 是可复用的时段；阶段一直接包裹 v1 规则以复用 occurrence 解析。
type channelBudgetSchedule struct {
	ID      string
	Name    string
	Enabled bool
	Kind    string
	rule    dto.ChannelPeriodRule
}

// channelBudgetRow 是一条预算行。
type channelBudgetRow struct {
	ID         string
	Name       string
	Enabled    bool
	Scope      string
	Window     string
	ScheduleID string
	Models     []string
	Limit      int64
	OnExceed   channelBudgetAction
	CreatedAt  int64
	// legacyUser 标记由渠道列派生的个人日/周行：指标形状与旧错误码沿用旧契约。
	legacyUser bool
	// implicit 标记 v1 规则未填写的整段额度：只为保持状态输出形状，不参与生效判定。
	implicit bool
}

// channelBudgetPlan 是一个渠道当前版本的全部时段与预算行。
type channelBudgetPlan struct {
	Revision        int
	DefaultOnExceed channelBudgetAction
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
	now       time.Time
	modelName string
	schedules map[string]channelBudgetScheduleState
	// activeSchedules 按 v1 顺序（weekly 类先于 date_range，同类按配置顺序）列出当前 occurrence 内的时段。
	activeSchedules []channelBudgetScheduleState
	next            int64
	// effective 记录每个分组当前生效行在 plan.Rows 中的下标。
	effective map[string]int
}

// buildChannelBudgetPlan 由 v1 策略与渠道列派生预算计划。
// @param view 当前权威策略。
// @param userDaily 渠道列个人日限，0 表示不限。
// @param userWeekly 渠道列个人周限，0 表示不限。
// @return 预算计划。
func buildChannelBudgetPlan(view dto.ChannelPeriodPolicyView, userDaily, userWeekly int64) channelBudgetPlan {
	config := view.Config
	plan := channelBudgetPlan{Revision: view.Revision, DefaultOnExceed: channelBudgetAction{Mode: channelBudgetActionReject}}
	if config.Fallback.Enabled {
		plan.DefaultOnExceed = channelBudgetAction{Mode: channelBudgetActionFallback, ChannelID: config.Fallback.ChannelID, Model: config.Fallback.Model}
	}
	inherit := channelBudgetAction{Mode: channelBudgetActionInherit}
	plan.Rows = append(plan.Rows,
		channelBudgetRow{ID: legacyUserDailyBudgetID, Enabled: true, Scope: channelBudgetScopeUser, Window: channelBudgetWindowDaily, Limit: userDaily, OnExceed: inherit, legacyUser: true},
		channelBudgetRow{ID: legacyUserWeeklyBudgetID, Enabled: true, Scope: channelBudgetScopeUser, Window: channelBudgetWindowWeekly, Limit: userWeekly, OnExceed: inherit, legacyUser: true},
		channelBudgetRow{ID: legacyPoolDailyBudgetID, Enabled: true, Scope: channelBudgetScopePool, Window: channelBudgetWindowDaily, Limit: config.PoolDailyQuotaLimit, OnExceed: inherit},
		channelBudgetRow{ID: legacyPoolWeeklyBudgetID, Enabled: true, Scope: channelBudgetScopePool, Window: channelBudgetWindowWeekly, Limit: config.PoolWeeklyQuotaLimit, OnExceed: inherit},
	)
	for _, rule := range config.Rules {
		plan.Schedules = append(plan.Schedules, channelBudgetSchedule{ID: rule.ID, Name: rule.Name, Enabled: rule.Enabled, Kind: rule.Kind, rule: rule})
		row := func(scope, window, suffix string, value *int64) channelBudgetRow {
			item := channelBudgetRow{ID: "rule-" + rule.ID + "-" + suffix, Name: rule.Name, Enabled: rule.Enabled, Scope: scope, Window: window, ScheduleID: rule.ID, OnExceed: inherit, CreatedAt: rule.CreatedAt}
			if value == nil {
				item.implicit = true
			} else {
				item.Limit = *value
			}
			return item
		}
		// 规则内日限只改上限不换计数，因此仍属于无时段身份；未填写的日限不产生行。
		if rule.UserDailyQuotaLimit != nil {
			plan.Rows = append(plan.Rows, row(channelBudgetScopeUser, channelBudgetWindowDaily, "user-daily", rule.UserDailyQuotaLimit))
		}
		if rule.PoolDailyQuotaLimit != nil {
			plan.Rows = append(plan.Rows, row(channelBudgetScopePool, channelBudgetWindowDaily, "pool-daily", rule.PoolDailyQuotaLimit))
		}
		plan.Rows = append(plan.Rows,
			row(channelBudgetScopeUser, channelBudgetWindowOccurrence, "user-occurrence", rule.UserPeriodQuotaLimit),
			row(channelBudgetScopePool, channelBudgetWindowOccurrence, "pool-occurrence", rule.PoolPeriodQuotaLimit),
		)
	}
	return plan
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

// resolveChannelBudgetRows 解析时段状态、下一切换点与每组生效行。
// @param plan 预算计划。
// @param now 解析时刻。
// @param modelName 请求原始模型名，空表示展示全部行。
// @return 解析结果或时段解析错误。
func resolveChannelBudgetRows(plan channelBudgetPlan, now time.Time, modelName string) (channelBudgetResolution, error) {
	res := channelBudgetResolution{now: now, modelName: modelName, schedules: make(map[string]channelBudgetScheduleState, len(plan.Schedules)), effective: make(map[string]int)}
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
	if !row.Enabled || row.implicit || !row.matchesModel(res.modelName) {
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
	return dto.ChannelPeriodSource{Kind: state.schedule.Kind, RuleID: state.schedule.ID, RuleName: state.schedule.Name, StartAt: state.occ.start.Unix(), EndAt: state.occ.end.Unix()}
}
