package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

const (
	channelBudgetSchemaVersion = 2
	maxChannelBudgetRows       = 128
	maxChannelBudgetModels     = 64
)

var channelBudgetIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// defaultChannelBudgetConfig 返回未配置渠道的空策略。
func defaultChannelBudgetConfig() dto.ChannelPeriodPolicyConfig {
	return dto.ChannelPeriodPolicyConfig{SchemaVersion: channelBudgetSchemaVersion, DefaultOnExceed: dto.ChannelBudgetAction{Mode: channelBudgetActionReject}, Schedules: []dto.ChannelBudgetSchedule{}, Budgets: []dto.ChannelBudgetRow{}}
}

// NormalizeChannelBudgetConfig 校验预算策略、建立稳定身份并输出可直接持久化的规范形状。
// @param config 待保存或预览的策略副本。
// @param previous 当前权威策略，用于保持时段与行的身份。
// @param now 当前时间。
// @return 规范化配置及输入错误。
func NormalizeChannelBudgetConfig(config, previous dto.ChannelPeriodPolicyConfig, now time.Time) (dto.ChannelPeriodPolicyConfig, error) {
	normalized, _, _, err := normalizeChannelBudgetConfig(config, previous, now)
	return normalized, err
}

// normalizeChannelBudgetConfig 是归一化主体，额外返回新时段与新行的客户端临时 id 映射，供预览回填。
func normalizeChannelBudgetConfig(config, previous dto.ChannelPeriodPolicyConfig, now time.Time) (dto.ChannelPeriodPolicyConfig, map[string]string, map[string]string, error) {
	invalid := func(message string) (dto.ChannelPeriodPolicyConfig, map[string]string, map[string]string, error) {
		return config, nil, nil, fmt.Errorf("%w: %s", ErrInvalidChannelPeriodPolicy, message)
	}
	if config.SchemaVersion != channelBudgetSchemaVersion {
		return invalid("策略版本无效")
	}
	action, err := normalizeChannelBudgetAction(config.DefaultOnExceed, nil, false)
	if err != nil {
		return invalid("策略级默认动作无效：" + err.Error())
	}
	if action.Mode == channelBudgetActionFallback && action.ChannelID <= 0 {
		return invalid("策略级降级必须指定目标渠道")
	}
	config.DefaultOnExceed = action
	scheduleIDs := make(map[string]string)
	schedules, err := normalizeChannelBudgetSchedules(config.Schedules, previous.Schedules, now, scheduleIDs)
	if err != nil {
		return config, nil, nil, err
	}
	config.Schedules = schedules
	scheduleByID := make(map[string]dto.ChannelBudgetSchedule, len(schedules))
	for _, item := range schedules {
		scheduleByID[item.ID] = item
	}
	oldRows := make(map[string]dto.ChannelBudgetRow, len(previous.Budgets))
	for _, row := range previous.Budgets {
		oldRows[row.ID] = row
	}
	rowIDs := make(map[string]string)
	seen := make(map[string]bool)
	rows := append([]dto.ChannelBudgetRow{}, config.Budgets...)
	for i := range rows {
		row := &rows[i]
		existing, exists := oldRows[row.ID]
		switch {
		case row.ID == "" || strings.HasPrefix(row.ID, "new-"):
			if row.ID != "" {
				if _, dup := rowIDs[row.ID]; dup || len(row.ID) > 64 {
					return invalid("临时预算 id 重复或过长")
				}
			}
			serverID := common.GetUUID()
			if row.ID != "" {
				rowIDs[row.ID] = serverID
			}
			row.ID, row.CreatedAt = serverID, now.Unix()
		case !exists || seen[row.ID]:
			return invalid("预算行身份无效或重复，请刷新后重试")
		default:
			row.CreatedAt = existing.CreatedAt
		}
		seen[row.ID] = true
		row.Name = strings.TrimSpace(row.Name)
		if row.Name == "" || len([]rune(row.Name)) > 80 {
			return invalid("预算名称须为 1 至 80 字")
		}
		if row.Scope != channelBudgetScopeUser && row.Scope != channelBudgetScopePool {
			return invalid("预算范围无效：" + row.Name)
		}
		if row.Window != channelBudgetWindowDaily && row.Window != channelBudgetWindowWeekly && row.Window != channelBudgetWindowOccurrence {
			return invalid("预算周期无效：" + row.Name)
		}
		if mapped, ok := scheduleIDs[row.ScheduleID]; ok {
			row.ScheduleID = mapped
		}
		if _, ok := scheduleByID[row.ScheduleID]; row.ScheduleID != "" && !ok {
			return invalid("预算引用的时段不存在：" + row.Name)
		}
		if row.Window == channelBudgetWindowOccurrence && row.ScheduleID == "" {
			return invalid("整段预算必须引用时段：" + row.Name)
		}
		models, err := normalizeChannelBudgetModels(row.Models)
		if err != nil {
			return invalid(err.Error() + "：" + row.Name)
		}
		row.Models = models
		if exists && (channelBudgetRow{ChannelBudgetRow: *row}).identityKey() != (channelBudgetRow{ChannelBudgetRow: existing}).identityKey() {
			// 新模型或窗口尚无历史计量，不能把旧行创建时间当作新身份的覆盖起点。
			row.CreatedAt = now.Unix()
		}
		if row.Limit < 0 || row.Limit > common.MaxPeriodQuota {
			return invalid("预算额度须为合法非负整数：" + row.Name)
		}
		action, err := normalizeChannelBudgetAction(row.OnExceed, models, true)
		if err != nil {
			return invalid(row.Name + " 的超限动作无效：" + err.Error())
		}
		row.OnExceed = action
	}
	for _, old := range previous.Budgets {
		if !seen[old.ID] {
			old.Enabled = false
			rows = append(rows, old)
		}
	}
	if len(rows) > maxChannelBudgetRows {
		return invalid(fmt.Sprintf("预算行总数（含停用）不能超过 %d", maxChannelBudgetRows))
	}
	// 同一约束槽位内：无时段行至多一条，每个时段至多一条，同类型时段不得相交。
	slots := make(map[string][]dto.ChannelBudgetRow)
	for _, row := range rows {
		if !row.Enabled || (row.ScheduleID != "" && !scheduleByID[row.ScheduleID].Enabled) {
			continue
		}
		key := row.Scope + "|" + row.Window + "|" + channelBudgetModelsKey(row.Models)
		for _, other := range slots[key] {
			if other.ScheduleID == row.ScheduleID {
				return invalid("同一范围、周期与模型的预算重复：" + row.Name + " / " + other.Name)
			}
			if other.ScheduleID == "" || row.ScheduleID == "" {
				continue
			}
			a, b := scheduleByID[row.ScheduleID], scheduleByID[other.ScheduleID]
			if a.Kind == b.Kind && channelSchedulesOverlap(a, b) {
				return invalid("同级时段对同一预算存在时间重叠：" + row.Name + " / " + other.Name)
			}
		}
		slots[key] = append(slots[key], row)
	}
	config.Budgets = rows
	return config, scheduleIDs, rowIDs, nil
}

// normalizeChannelBudgetAction 校验超限动作；models 为 nil 表示策略级，不允许 inherit 与同渠道。
func normalizeChannelBudgetAction(action dto.ChannelBudgetAction, models []string, allowInherit bool) (dto.ChannelBudgetAction, error) {
	action.Model = strings.TrimSpace(action.Model)
	switch action.Mode {
	case channelBudgetActionInherit:
		if !allowInherit {
			return action, errors.New("策略级默认动作不能为 inherit")
		}
		return dto.ChannelBudgetAction{Mode: channelBudgetActionInherit}, nil
	case channelBudgetActionReject:
		return dto.ChannelBudgetAction{Mode: channelBudgetActionReject}, nil
	case channelBudgetActionFallback:
		if action.ChannelID < 0 || action.Model == "" || len(action.Model) > 255 {
			return action, errors.New("降级目标渠道及模型无效")
		}
		if action.ChannelID == 0 {
			if len(models) == 0 {
				return action, errors.New("只有按模型的预算才能同渠道换模型")
			}
			for _, item := range models {
				if item == action.Model {
					return action, errors.New("同渠道降级目标不能是本行模型")
				}
			}
		}
		return action, nil
	default:
		return action, errors.New("动作类型无效")
	}
}

func normalizeChannelBudgetModels(models []string) ([]string, error) {
	result := make([]string, 0, len(models))
	seen := make(map[string]bool, len(models))
	for _, item := range models {
		item = strings.TrimSpace(item)
		if item == "" || len(item) > 255 {
			return nil, errors.New("模型名不能为空或过长")
		}
		if seen[item] {
			continue
		}
		seen[item] = true
		result = append(result, item)
	}
	if len(result) > maxChannelBudgetModels {
		return nil, fmt.Errorf("单行模型数不能超过 %d", maxChannelBudgetModels)
	}
	sort.Strings(result)
	return result, nil
}

// validateChannelBudgetStoredPolicy 校验已存储策略仍是规范形状，防止损坏数据被当作宽松配置。
func validateChannelBudgetStoredPolicy(view dto.ChannelPeriodPolicyView) error {
	// 版本 0 表示尚未持久化；迁移窗口内仍可携带由旧渠道列派生的有效预算行。
	if view.Revision < 0 {
		return errors.New("周期策略版本无效")
	}
	for _, item := range view.Config.Schedules {
		if !channelBudgetIDPattern.MatchString(item.ID) || strings.HasPrefix(item.ID, "new-") || item.CreatedAt <= 0 {
			return errors.New("已存储的时段身份或统计起点无效")
		}
	}
	for _, row := range view.Config.Budgets {
		if !channelBudgetIDPattern.MatchString(row.ID) || strings.HasPrefix(row.ID, "new-") || row.CreatedAt <= 0 {
			return errors.New("已存储的预算身份或统计起点无效")
		}
	}
	normalized, err := NormalizeChannelBudgetConfig(view.Config, view.Config, channelPeriodNow().In(time.Local))
	if err != nil || !reflect.DeepEqual(normalized, view.Config) {
		return errors.New("已存储的周期策略无效")
	}
	return nil
}

// PreviewChannelBudgetPolicy 解析服务端时间与每行生效状态，不写配置；新时段与新行保留客户端 id 以便随后保存。
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param config 待预览的配置。
// @param now 预览时刻。
// @return 预览结果，或校验错误。
func PreviewChannelBudgetPolicy(ctx context.Context, channelID int, config dto.ChannelPeriodPolicyConfig, now time.Time) (dto.ChannelBudgetPreview, error) {
	previous, err := GetChannelPeriodPolicy(ctx, channelID)
	if err != nil {
		return dto.ChannelBudgetPreview{}, err
	}
	normalized, scheduleIDs, rowIDs, err := normalizeChannelBudgetConfig(channelBudgetSameChannelToZero(config, channelID), previous.Config, now)
	if err != nil {
		return dto.ChannelBudgetPreview{}, err
	}
	plan := buildChannelBudgetPlan(dto.ChannelPeriodPolicyView{Revision: previous.Revision, Config: normalized})
	res, err := resolveChannelBudgetRows(plan, now, "")
	if err != nil {
		return dto.ChannelBudgetPreview{}, err
	}
	preview := dto.ChannelBudgetPreview{Config: normalized, Revision: previous.Revision, Timezone: time.Local.String(), Now: now.Unix(), NextChangeAt: res.next, Rows: make([]dto.ChannelBudgetPreviewRow, 0, len(plan.Rows))}
	for _, row := range plan.Rows {
		effective, ok := res.effectiveRow(plan, row.groupKey())
		preview.Rows = append(preview.Rows, dto.ChannelBudgetPreviewRow{BudgetID: row.ID, Active: res.eligible(row), Enforced: ok && effective.ID == row.ID, Source: res.source(row)})
	}
	// 预览不会建立身份：把服务端分配的 id 还原为客户端临时 id，预览结果才能原样保存。
	reverse := make(map[string]string, len(scheduleIDs)+len(rowIDs))
	for client, server := range scheduleIDs {
		reverse[server] = client
	}
	for client, server := range rowIDs {
		reverse[server] = client
	}
	existing := make(map[string]bool, len(previous.Config.Schedules)+len(previous.Config.Budgets))
	for _, item := range previous.Config.Schedules {
		existing[item.ID] = true
	}
	for _, item := range previous.Config.Budgets {
		existing[item.ID] = true
	}
	// 不在旧策略中的 id 都是本次分配的：有临时 id 的还原临时 id，没有的还原为空。
	restore := func(id string) string {
		if existing[id] {
			return id
		}
		return reverse[id]
	}
	for i := range preview.Config.Schedules {
		preview.Config.Schedules[i].ID = restore(preview.Config.Schedules[i].ID)
	}
	for i := range preview.Config.Budgets {
		row := &preview.Config.Budgets[i]
		row.ID = restore(row.ID)
		if row.ScheduleID != "" {
			row.ScheduleID = restore(row.ScheduleID)
		}
	}
	for i := range preview.Rows {
		preview.Rows[i].BudgetID = restore(preview.Rows[i].BudgetID)
		if preview.Rows[i].Source.ScheduleID != "" {
			preview.Rows[i].Source.ScheduleID = restore(preview.Rows[i].Source.ScheduleID)
		}
	}
	return preview, nil
}

// channelBudgetSameChannelToZero 把指向本渠道的行级降级归一为 0（同渠道换模型），策略级不归一以便校验拒绝自指。
func channelBudgetSameChannelToZero(config dto.ChannelPeriodPolicyConfig, channelID int) dto.ChannelPeriodPolicyConfig {
	config.Budgets = append([]dto.ChannelBudgetRow{}, config.Budgets...)
	for i := range config.Budgets {
		if config.Budgets[i].OnExceed.Mode == channelBudgetActionFallback && config.Budgets[i].OnExceed.ChannelID == channelID {
			config.Budgets[i].OnExceed.ChannelID = 0
		}
	}
	return config
}
