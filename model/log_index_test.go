package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestLogAutoMigrateCreatesTokenStatsIndexes 验证公共日志统计所需复合索引会随日志表迁移创建。
func TestLogAutoMigrateCreatesTokenStatsIndexes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, db.AutoMigrate(&Log{}))
	require.True(t, db.Migrator().HasIndex(&Log{}, "idx_logs_token_created_at"))
	require.True(t, db.Migrator().HasIndex(&Log{}, "idx_logs_token_type_created_at"))
}
