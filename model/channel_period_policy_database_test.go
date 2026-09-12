package model

import (
	"os"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// 验证新增三表的重入迁移、版本竞争、特批 upsert 和缺口时间单调性。
// 外部方言只接收专用测试 DSN，表名前缀隔离，未配置时明确跳过。
func TestChannelPeriodPolicyDatabaseContract(t *testing.T) {
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
			db, err := gorm.Open(dialector, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "codex_period_contract_"}})
			require.NoError(t, err)
			old := DB
			DB = db
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			tables := []any{&ChannelPeriodPolicy{}, &ChannelUserPeriodOverride{}, &ChannelUserBudgetOverride{}, &ChannelQuotaTracking{}}
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(tables...))
				DB = old
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(tables...))
			require.NoError(t, db.AutoMigrate(tables...))
			current, err := GetChannelPeriodPolicy(t.Context(), 80)
			require.NoError(t, err)
			assert.Nil(t, current)
			policy := &ChannelPeriodPolicy{ChannelId: 80, Config: `{"schema_version":1}`, UpdatedBy: 1}
			require.NoError(t, ReplaceChannelPeriodPolicy(t.Context(), policy))
			assert.Equal(t, 1, policy.Revision)
			require.ErrorIs(t, ReplaceChannelPeriodPolicy(t.Context(), &ChannelPeriodPolicy{ChannelId: 80, Config: `{}`}), ErrChannelPeriodPolicyConflict)
			require.NoError(t, ReplaceChannelPeriodPolicy(t.Context(), policy))
			assert.Equal(t, 2, policy.Revision)
			require.ErrorIs(t, ReplaceChannelPeriodPolicy(t.Context(), &ChannelPeriodPolicy{ChannelId: 80, Revision: 1, Config: `{}`}), ErrChannelPeriodPolicyConflict)
			rule := strings.Repeat("a", 32)
			require.NoError(t, ReplaceChannelUserPeriodOverride(t.Context(), &ChannelUserPeriodOverride{ChannelId: 80, UserId: 7, RuleId: rule, UserPeriodQuotaLimit: 100, ExpiresAt: 200}))
			require.NoError(t, ReplaceChannelUserPeriodOverride(t.Context(), &ChannelUserPeriodOverride{ChannelId: 80, UserId: 7, RuleId: rule, UserPeriodQuotaLimit: 200, ExpiresAt: 0}))
			overrides, err := ListChannelUserPeriodOverrides(t.Context(), 80, 7, 300)
			require.NoError(t, err)
			require.Len(t, overrides, 1)
			assert.Equal(t, int64(200), overrides[0].UserPeriodQuotaLimit)
			assert.Zero(t, overrides[0].ExpiresAt)
			require.NoError(t, RecordChannelQuotaGap(t.Context(), 80, 200))
			require.NoError(t, RecordChannelQuotaGap(t.Context(), 80, 100))
			gap, err := GetChannelQuotaGap(t.Context(), 80)
			require.NoError(t, err)
			assert.Equal(t, int64(200), gap)
			require.NoError(t, ReplaceChannelUserBudgetOverride(t.Context(), &ChannelUserBudgetOverride{ChannelId: 80, UserId: 7, BudgetId: "legacy-user-daily", QuotaLimit: 300, ExpiresAt: 200}))
			require.NoError(t, ReplaceChannelUserBudgetOverride(t.Context(), &ChannelUserBudgetOverride{ChannelId: 80, UserId: 7, BudgetId: "legacy-user-daily", QuotaLimit: 400, ExpiresAt: 0}))
			budgetOverrides, err := ListActiveChannelUserBudgetOverrides(t.Context(), 80, 7, 300)
			require.NoError(t, err)
			require.Len(t, budgetOverrides, 1)
			assert.Equal(t, int64(400), budgetOverrides[0].QuotaLimit)
			require.NoError(t, DeleteChannelUserBudgetOverride(t.Context(), 80, 7, "legacy-user-daily"))
			budgetOverrides, err = ListActiveChannelUserBudgetOverrides(t.Context(), 80, 7, 300)
			require.NoError(t, err)
			assert.Empty(t, budgetOverrides)
			require.NoError(t, DeleteChannelUserPeriodOverride(t.Context(), 80, 7, rule))
			overrides, err = ListChannelUserPeriodOverrides(t.Context(), 80, 7, 300)
			require.NoError(t, err)
			assert.Empty(t, overrides)
		})
	}
}
