package service

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// TestChannelBudgetMigrationDatabaseContract 在三种隔离测试库中验证完整旧数据迁移、覆盖复制及重复启动幂等。
// @param t 测试上下文；外部数据库仅使用 CHANNEL_PERIOD_TEST_*_DSN。
// @return 无。
func TestChannelBudgetMigrationDatabaseContract(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var dialector gorm.Dialector
			if dialect == "sqlite" {
				dialector = sqlite.Open(":memory:")
			} else {
				dsn := os.Getenv("CHANNEL_PERIOD_TEST_" + strings.ToUpper(dialect) + "_DSN")
				if dsn == "" {
					t.Skip("未配置专用测试数据库")
				}
				if dialect == "mysql" {
					dialector = mysql.Open(dsn)
				} else {
					dialector = postgres.Open(dsn)
				}
			}
			db, err := gorm.Open(dialector, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "codex_budget_migration_"}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			oldDB, oldRedis := model.DB, common.RedisEnabled
			model.DB, common.RedisEnabled = db, false
			tables := []any{&model.Channel{}, &model.ChannelPeriodPolicy{}, &model.ChannelUserLimitOverride{}, &model.ChannelUserPeriodOverride{}, &model.ChannelUserBudgetOverride{}}
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(tables...))
				model.DB, common.RedisEnabled = oldDB, oldRedis
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(tables...))
			require.NoError(t, db.AutoMigrate(tables...))
			require.NoError(t, db.Create(&model.Channel{Id: 80, Name: "旧策略", UserDailyQuotaLimit: common.GetPointer(100), UserWeeklyQuotaLimit: common.GetPointer(500)}).Error)
			require.NoError(t, db.Create(&model.Channel{Id: 81, Name: "仅旧列", UserDailyQuotaLimit: common.GetPointer(700)}).Error)
			ruleID := strings.Repeat("b", 32)
			v1 := dto.ChannelPeriodPolicyConfigV1{SchemaVersion: 1, PoolDailyQuotaLimit: 1000, Rules: []dto.ChannelPeriodRuleV1{
				{ID: ruleID, Name: "每周时段", Enabled: true, Kind: "weekly", StartWeekday: 0, EndWeekday: 1, StartTime: "00:00", EndTime: "00:00", CreatedAt: 1, UserPeriodQuotaLimit: common.GetPointer(int64(15))},
			}}
			raw, err := common.Marshal(v1)
			require.NoError(t, err)
			require.NoError(t, model.ReplaceChannelPeriodPolicy(t.Context(), &model.ChannelPeriodPolicy{ChannelId: 80, Config: string(raw), UpdatedBy: 1}))
			require.NoError(t, model.ReplaceChannelUserLimitOverride(&model.ChannelUserLimitOverride{ChannelId: 80, UserId: 7, UserDailyQuotaLimit: common.GetPointer(200), UserWeeklyQuotaLimit: common.GetPointer(900), UpdatedBy: 1}))
			require.NoError(t, model.ReplaceChannelUserPeriodOverride(t.Context(), &model.ChannelUserPeriodOverride{ChannelId: 80, UserId: 7, RuleId: ruleID, UserPeriodQuotaLimit: 30, UpdatedBy: 1}))

			MigrateChannelBudgetPolicies(t.Context())
			first, err := model.GetChannelPeriodPolicy(t.Context(), 80)
			require.NoError(t, err)
			require.NotNil(t, first)
			assert.Equal(t, 2, first.Revision)
			var migrated dto.ChannelPeriodPolicyConfig
			require.NoError(t, common.UnmarshalJsonStr(first.Config, &migrated))
			assert.Equal(t, 2, migrated.SchemaVersion)
			limits := make(map[string]int64)
			for _, row := range migrated.Budgets {
				limits[row.ID] = row.Limit
			}
			assert.Equal(t, map[string]int64{"legacy-user-daily": 100, "legacy-user-weekly": 500, "legacy-pool-daily": 1000, "rule-" + ruleID + "-user-occurrence": 15}, limits)
			require.Len(t, migrated.Schedules, 1)
			assert.Equal(t, ruleID, migrated.Schedules[0].ID)
			overrides, err := model.ListActiveChannelUserBudgetOverrides(t.Context(), 80, 7, time.Now().Unix())
			require.NoError(t, err)
			copied := make(map[string]int64)
			for _, item := range overrides {
				copied[item.BudgetId] = item.QuotaLimit
			}
			assert.Equal(t, map[string]int64{"legacy-user-daily": 200, "legacy-user-weekly": 900, "rule-" + ruleID + "-user-occurrence": 30}, copied)
			onlyColumns, err := GetChannelPeriodPolicy(t.Context(), 81)
			require.NoError(t, err)
			assert.Equal(t, 1, onlyColumns.Revision)
			require.Len(t, onlyColumns.Config.Budgets, 1)
			assert.Equal(t, int64(700), onlyColumns.Config.Budgets[0].Limit)

			// 管理员在迁移后更新过的新覆盖不能被再次启动时的旧数据覆盖。
			require.NoError(t, model.ReplaceChannelUserBudgetOverride(t.Context(), &model.ChannelUserBudgetOverride{ChannelId: 80, UserId: 7, BudgetId: "legacy-user-daily", QuotaLimit: 300, UpdatedBy: 1}))
			MigrateChannelBudgetPolicies(t.Context())
			again, err := model.GetChannelPeriodPolicy(t.Context(), 80)
			require.NoError(t, err)
			assert.Equal(t, first.Revision, again.Revision)
			assert.JSONEq(t, first.Config, again.Config)
			overrides, err = model.ListActiveChannelUserBudgetOverrides(t.Context(), 80, 7, time.Now().Unix())
			require.NoError(t, err)
			require.Len(t, overrides, 3)
			updated, err := model.GetChannelUserBudgetOverride(t.Context(), 80, 7, "legacy-user-daily")
			require.NoError(t, err)
			require.NotNil(t, updated)
			assert.Equal(t, int64(300), updated.QuotaLimit)
			// 撤销后再次启动不能从旧表复活提额，分页也不能暴露撤销占位记录。
			require.NoError(t, model.DeleteChannelUserBudgetOverride(t.Context(), 80, 7, "legacy-user-daily"))
			MigrateChannelBudgetPolicies(t.Context())
			deleted, err := model.GetChannelUserBudgetOverride(t.Context(), 80, 7, "legacy-user-daily")
			require.NoError(t, err)
			assert.Nil(t, deleted)
			page, total, err := model.ListActiveChannelUserBudgetOverridesPage(t.Context(), 80, time.Now().Unix(), 0, 20)
			require.NoError(t, err)
			assert.Equal(t, int64(2), total)
			assert.Len(t, page, 2)
			require.NoError(t, model.ReplaceChannelUserBudgetOverride(t.Context(), &model.ChannelUserBudgetOverride{ChannelId: 80, UserId: 7, BudgetId: "legacy-user-daily", QuotaLimit: 400, UpdatedBy: 1}))
			MigrateChannelBudgetPolicies(t.Context())
			granted, err := model.GetChannelUserBudgetOverride(t.Context(), 80, 7, "legacy-user-daily")
			require.NoError(t, err)
			require.NotNil(t, granted)
			assert.Equal(t, int64(400), granted.QuotaLimit)
		})
	}
}
