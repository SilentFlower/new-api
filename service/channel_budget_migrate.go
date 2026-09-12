package service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// convertChannelBudgetPolicyV1 把 v1 策略与渠道列派生为 v2 预算列表；派生身份与阶段一内核一致，计数 key 不变。
// @param v1 已存储的 v1 配置。
// @param userDaily 渠道列个人日限，0 表示不限。
// @param userWeekly 渠道列个人周限，0 表示不限。
// @param now 转换时刻，作为新行的 created_at。
// @return 规范形状的 v2 配置。
func convertChannelBudgetPolicyV1(v1 dto.ChannelPeriodPolicyConfigV1, userDaily, userWeekly int64, now int64) dto.ChannelPeriodPolicyConfig {
	config := defaultChannelBudgetConfig()
	if v1.Fallback.Enabled {
		config.DefaultOnExceed = dto.ChannelBudgetAction{Mode: channelBudgetActionFallback, ChannelID: v1.Fallback.ChannelID, Model: v1.Fallback.Model}
	}
	inherit := dto.ChannelBudgetAction{Mode: channelBudgetActionInherit}
	row := func(id, name, scope, window, scheduleID string, limit, createdAt int64) dto.ChannelBudgetRow {
		return dto.ChannelBudgetRow{ID: id, Name: name, Enabled: true, Scope: scope, Window: window, ScheduleID: scheduleID, Models: []string{}, Limit: limit, OnExceed: inherit, CreatedAt: createdAt}
	}
	// 渠道级额度为 0 表示不限，v2 不需要占位行；渠道级日/周身份无论有无行都会累计。
	if userDaily > 0 {
		config.Budgets = append(config.Budgets, row(legacyUserDailyBudgetID, "个人 · 每日", channelBudgetScopeUser, channelBudgetWindowDaily, "", userDaily, now))
	}
	if userWeekly > 0 {
		config.Budgets = append(config.Budgets, row(legacyUserWeeklyBudgetID, "个人 · 每周", channelBudgetScopeUser, channelBudgetWindowWeekly, "", userWeekly, now))
	}
	if v1.PoolDailyQuotaLimit > 0 {
		config.Budgets = append(config.Budgets, row(legacyPoolDailyBudgetID, "池子 · 每日", channelBudgetScopePool, channelBudgetWindowDaily, "", v1.PoolDailyQuotaLimit, now))
	}
	if v1.PoolWeeklyQuotaLimit > 0 {
		config.Budgets = append(config.Budgets, row(legacyPoolWeeklyBudgetID, "池子 · 每周", channelBudgetScopePool, channelBudgetWindowWeekly, "", v1.PoolWeeklyQuotaLimit, now))
	}
	for _, rule := range v1.Rules {
		config.Schedules = append(config.Schedules, dto.ChannelBudgetSchedule{ID: rule.ID, Name: rule.Name, Enabled: rule.Enabled, Kind: rule.Kind, StartLocal: rule.StartLocal, EndLocal: rule.EndLocal, StartAt: rule.StartAt, EndAt: rule.EndAt, StartWeekday: rule.StartWeekday, EndWeekday: rule.EndWeekday, StartTime: rule.StartTime, EndTime: rule.EndTime, CreatedAt: rule.CreatedAt})
		name := []rune(rule.Name)
		if len(name) > 70 {
			name = name[:70]
		}
		prefix := string(name) + " · "
		// 规则内明确填写的额度（含 0 = 时段内不限）才成为行；规则停用则行同样停用。
		items := []struct {
			suffix, scope, window string
			value                 *int64
		}{
			{"user-daily", channelBudgetScopeUser, channelBudgetWindowDaily, rule.UserDailyQuotaLimit},
			{"pool-daily", channelBudgetScopePool, channelBudgetWindowDaily, rule.PoolDailyQuotaLimit},
			{"user-occurrence", channelBudgetScopeUser, channelBudgetWindowOccurrence, rule.UserPeriodQuotaLimit},
			{"pool-occurrence", channelBudgetScopePool, channelBudgetWindowOccurrence, rule.PoolPeriodQuotaLimit},
		}
		labels := map[string]string{"user-daily": "个人每日", "pool-daily": "池子每日", "user-occurrence": "个人整段", "pool-occurrence": "池子整段"}
		for _, item := range items {
			if item.value == nil {
				continue
			}
			converted := row("rule-"+rule.ID+"-"+item.suffix, prefix+labels[item.suffix], item.scope, item.window, rule.ID, *item.value, rule.CreatedAt)
			converted.Enabled = rule.Enabled
			config.Budgets = append(config.Budgets, converted)
		}
	}
	return config
}

// MigrateChannelBudgetPolicies 一次性把 v1 策略、渠道列日/周额度与两张旧覆盖表并入 v2；幂等，单渠道失败只记录日志。
// @param ctx 启动上下文。
// @return 无；进度与失败写入系统日志。
func MigrateChannelBudgetPolicies(ctx context.Context) {
	now := time.Now()
	migrated, skipped, failed := 0, 0, 0
	for offset := 0; ; offset += 500 {
		items, err := model.ListChannelBudgetMigrationSources(ctx, offset, 500)
		if err != nil {
			common.SysError("预算策略迁移读取渠道失败: " + common.LocalLogPreview(err.Error()))
			return
		}
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			var probe struct {
				SchemaVersion int `json:"schema_version"`
			}
			if item.Config != "" && common.UnmarshalJsonStr(item.Config, &probe) == nil && probe.SchemaVersion == channelBudgetSchemaVersion {
				skipped++
				continue
			}
			var v1 dto.ChannelPeriodPolicyConfigV1
			if item.Config != "" || item.Revision > 0 {
				if err := common.UnmarshalJsonStr(item.Config, &v1); err != nil {
					failed++
					common.SysError(fmt.Sprintf("预算策略迁移解析 v1 失败: channel_id=%d error=%s", item.ChannelID, common.LocalLogPreview(err.Error())))
					continue
				}
				if v1.SchemaVersion != 1 {
					failed++
					common.SysError(fmt.Sprintf("预算策略迁移拒绝未知版本: channel_id=%d schema_version=%d", item.ChannelID, v1.SchemaVersion))
					continue
				}
			}
			if item.Config == "" && item.UserDailyQuotaLimit <= 0 && item.UserWeeklyQuotaLimit <= 0 {
				skipped++
				continue
			}
			config := convertChannelBudgetPolicyV1(v1, item.UserDailyQuotaLimit, item.UserWeeklyQuotaLimit, now.Unix())
			data, err := common.Marshal(config)
			if err != nil {
				failed++
				continue
			}
			policy := &model.ChannelPeriodPolicy{ChannelId: item.ChannelID, Revision: item.Revision, Config: string(data)}
			if err := model.ReplaceChannelPeriodPolicy(ctx, policy); err != nil {
				failed++
				common.SysError(fmt.Sprintf("预算策略迁移写入失败: channel_id=%d error=%s", item.ChannelID, common.LocalLogPreview(err.Error())))
				continue
			}
			invalidateChannelPeriodPolicyCache(ctx, item.ChannelID)
			migrated++
		}
	}
	overrides, err := model.MigrateChannelUserOverridesToBudgets(ctx, now.Unix())
	if err != nil {
		common.SysError("个人覆盖迁移失败: " + common.LocalLogPreview(err.Error()))
	}
	common.SysLog(fmt.Sprintf("预算策略迁移完成: migrated=%d skipped=%d failed=%d overrides=%d", migrated, skipped, failed, overrides))
}
