package model

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelUserBudgetOverride 保存单个用户对一条个人预算行的提额；唯一键 (channel_id, user_id, budget_id)。
type ChannelUserBudgetOverride struct {
	Id         int    `json:"id"`
	ChannelId  int    `json:"channel_id" gorm:"uniqueIndex:idx_channel_user_budget_override"`
	UserId     int    `json:"user_id" gorm:"uniqueIndex:idx_channel_user_budget_override"`
	BudgetId   string `json:"budget_id" gorm:"type:varchar(64);uniqueIndex:idx_channel_user_budget_override"`
	QuotaLimit int64  `json:"limit" gorm:"column:quota_limit;type:bigint"`
	ExpiresAt  int64  `json:"expires_at" gorm:"type:bigint;index"`
	UpdatedBy  int    `json:"updated_by"`
	CreatedAt  int64  `json:"created_at" gorm:"type:bigint"`
	UpdatedAt  int64  `json:"updated_at" gorm:"type:bigint"`
}

// GetChannelUserBudgetOverride 读取单条提额的持久化快照，供替换与撤销审计使用。
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param userID 用户 ID。
// @param budgetID 预算行 ID。
// @return 当前记录；不存在或已撤销时返回 nil，读取故障返回错误。
func GetChannelUserBudgetOverride(ctx context.Context, channelID, userID int, budgetID string) (*ChannelUserBudgetOverride, error) {
	if DB == nil {
		return nil, errors.New("个人预算覆盖数据库未初始化")
	}
	var item ChannelUserBudgetOverride
	err := DB.WithContext(ctx).Where("channel_id = ? AND user_id = ? AND budget_id = ?", channelID, userID, budgetID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && item.QuotaLimit == 0 {
		return nil, nil
	}
	return &item, err
}

// ListActiveChannelUserBudgetOverrides 查询用户在渠道内当前有效的全部行级提额。
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param userID 用户 ID。
// @param now 当前 Unix 秒。
// @return 有效记录及真实数据库错误。
func ListActiveChannelUserBudgetOverrides(ctx context.Context, channelID, userID int, now int64) ([]ChannelUserBudgetOverride, error) {
	if DB == nil {
		return nil, errors.New("个人预算覆盖数据库未初始化")
	}
	var items []ChannelUserBudgetOverride
	err := DB.WithContext(ctx).Where("channel_id = ? AND user_id = ? AND quota_limit > 0 AND (expires_at = 0 OR expires_at > ?)", channelID, userID, now).Order("budget_id ASC").Find(&items).Error
	return items, err
}

// ListActiveChannelUserBudgetOverridesPage 分页返回渠道内当前有效的行级提额。
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param now 当前 Unix 秒。
// @param offset 分页偏移。
// @param limit 分页大小。
// @return 记录、总数及错误。
func ListActiveChannelUserBudgetOverridesPage(ctx context.Context, channelID int, now int64, offset, limit int) ([]ChannelUserBudgetOverride, int64, error) {
	if DB == nil {
		return nil, 0, errors.New("个人预算覆盖数据库未初始化")
	}
	query := DB.WithContext(ctx).Model(&ChannelUserBudgetOverride{}).Where("channel_id = ? AND quota_limit > 0 AND (expires_at = 0 OR expires_at > ?)", channelID, now)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []ChannelUserBudgetOverride
	err := query.Order("user_id ASC, budget_id ASC").Offset(offset).Limit(limit).Find(&items).Error
	return items, total, err
}

// ReplaceChannelUserBudgetOverride 按唯一键写入或覆盖一条行级提额。
// @param ctx 请求上下文。
// @param item 待保存记录。
// @return 数据库错误。
func ReplaceChannelUserBudgetOverride(ctx context.Context, item *ChannelUserBudgetOverride) error {
	if DB == nil || item == nil || item.ChannelId <= 0 || item.UserId <= 0 || item.BudgetId == "" {
		return gorm.ErrInvalidData
	}
	now := time.Now().Unix()
	item.CreatedAt, item.UpdatedAt = now, now
	return DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}, {Name: "user_id"}, {Name: "budget_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"quota_limit", "expires_at", "updated_by", "updated_at"}),
	}).Create(item).Error
}

// DeleteChannelUserBudgetOverride 撤销一条行级提额，保留非生效记录防止旧表在重启迁移时将其重新导入。
// @param ctx 请求上下文。
// @param channelID 渠道 ID。
// @param userID 用户 ID。
// @param budgetID 预算行 id。
// @return 数据库错误。
func DeleteChannelUserBudgetOverride(ctx context.Context, channelID, userID int, budgetID string) error {
	if DB == nil {
		return errors.New("个人预算覆盖数据库未初始化")
	}
	// 唯一键占位同时保护“撤销先完成、旧迁移后写入”的并发顺序；查询不会返回零额度记录。
	return ReplaceChannelUserBudgetOverride(ctx, &ChannelUserBudgetOverride{ChannelId: channelID, UserId: userID, BudgetId: budgetID})
}

// ChannelBudgetMigrationSource 是一个渠道参与预算迁移所需的全部输入。
type ChannelBudgetMigrationSource struct {
	ChannelID            int
	UserDailyQuotaLimit  int64
	UserWeeklyQuotaLimit int64
	Revision             int
	Config               string
}

// ListChannelBudgetMigrationSources 分页返回渠道的旧日/周列与当前策略正文。
// @param ctx 启动上下文。
// @param offset 分页偏移。
// @param limit 分页大小。
// @return 迁移输入及错误。
func ListChannelBudgetMigrationSources(ctx context.Context, offset, limit int) ([]ChannelBudgetMigrationSource, error) {
	var channels []Channel
	if err := DB.WithContext(ctx).Select("id", "user_daily_quota_limit", "user_weekly_quota_limit").Order("id ASC").Offset(offset).Limit(limit).Find(&channels).Error; err != nil {
		return nil, err
	}
	if len(channels) == 0 {
		return nil, nil
	}
	ids := make([]int, len(channels))
	for i, channel := range channels {
		ids[i] = channel.Id
	}
	var policies []ChannelPeriodPolicy
	if err := DB.WithContext(ctx).Where("channel_id IN ?", ids).Find(&policies).Error; err != nil {
		return nil, err
	}
	policyByChannel := make(map[int]ChannelPeriodPolicy, len(policies))
	for _, policy := range policies {
		policyByChannel[policy.ChannelId] = policy
	}
	items := make([]ChannelBudgetMigrationSource, 0, len(channels))
	for _, channel := range channels {
		item := ChannelBudgetMigrationSource{ChannelID: channel.Id}
		if channel.UserDailyQuotaLimit != nil && *channel.UserDailyQuotaLimit > 0 {
			item.UserDailyQuotaLimit = int64(*channel.UserDailyQuotaLimit)
		}
		if channel.UserWeeklyQuotaLimit != nil && *channel.UserWeeklyQuotaLimit > 0 {
			item.UserWeeklyQuotaLimit = int64(*channel.UserWeeklyQuotaLimit)
		}
		if policy, ok := policyByChannel[channel.Id]; ok {
			item.Revision, item.Config = policy.Revision, policy.Config
		}
		items = append(items, item)
	}
	return items, nil
}

// MigrateChannelUserOverridesToBudgets 把旧个人日/周覆盖与规则整段特批复制为行级提额；已存在的记录不覆盖。
// @param ctx 启动上下文。
// @param now 当前 Unix 秒，已过期记录跳过。
// @return 新写入的记录数及错误。
func MigrateChannelUserOverridesToBudgets(ctx context.Context, now int64) (int, error) {
	inserted := 0
	insert := func(item ChannelUserBudgetOverride) error {
		item.CreatedAt, item.UpdatedAt = now, now
		result := DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&item)
		if result.Error != nil {
			return result.Error
		}
		inserted += int(result.RowsAffected)
		return nil
	}
	var limits []ChannelUserLimitOverride
	if err := DB.WithContext(ctx).Where("expires_at = 0 OR expires_at > ?", now).Find(&limits).Error; err != nil {
		return inserted, err
	}
	for _, old := range limits {
		if old.UserDailyQuotaLimit != nil && *old.UserDailyQuotaLimit > 0 {
			if err := insert(ChannelUserBudgetOverride{ChannelId: old.ChannelId, UserId: old.UserId, BudgetId: "legacy-user-daily", QuotaLimit: int64(*old.UserDailyQuotaLimit), ExpiresAt: old.ExpiresAt, UpdatedBy: old.UpdatedBy}); err != nil {
				return inserted, err
			}
		}
		if old.UserWeeklyQuotaLimit != nil && *old.UserWeeklyQuotaLimit > 0 {
			if err := insert(ChannelUserBudgetOverride{ChannelId: old.ChannelId, UserId: old.UserId, BudgetId: "legacy-user-weekly", QuotaLimit: int64(*old.UserWeeklyQuotaLimit), ExpiresAt: old.ExpiresAt, UpdatedBy: old.UpdatedBy}); err != nil {
				return inserted, err
			}
		}
	}
	var periods []ChannelUserPeriodOverride
	if err := DB.WithContext(ctx).Where("expires_at = 0 OR expires_at > ?", now).Find(&periods).Error; err != nil {
		return inserted, err
	}
	for _, old := range periods {
		if old.UserPeriodQuotaLimit <= 0 {
			continue
		}
		if err := insert(ChannelUserBudgetOverride{ChannelId: old.ChannelId, UserId: old.UserId, BudgetId: "rule-" + old.RuleId + "-user-occurrence", QuotaLimit: old.UserPeriodQuotaLimit, ExpiresAt: old.ExpiresAt, UpdatedBy: old.UpdatedBy}); err != nil {
			return inserted, err
		}
	}
	return inserted, nil
}
