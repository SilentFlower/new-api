package model

import (
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestChannelLimitParentDatabaseMigrationAndConcurrentTopology(t *testing.T) {
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
			db, err := gorm.Open(dialector, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "codex_parent_contract_"}})
			require.NoError(t, err)
			previousDB := DB
			oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
			DB = db
			switch dialect {
			case "sqlite":
				common.SetMainDatabaseType(common.DatabaseTypeSQLite)
			case "mysql":
				common.SetMainDatabaseType(common.DatabaseTypeMySQL)
			case "postgres":
				common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
			}
			sqlDB, err := db.DB()
			require.NoError(t, err)
			if dialect == "sqlite" {
				sqlDB.SetMaxOpenConns(1)
			}
			tables := []any{&Channel{}, &Ability{}, &ChannelLimitParent{}}
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(tables...))
				DB = previousDB
				common.SetDatabaseTypes(oldMain, oldLog)
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(tables...))
			require.NoError(t, db.AutoMigrate(tables...))
			for _, id := range []int{81, 82, 83, 84, 85} {
				require.NoError(t, db.Create(&Channel{Id: id, Name: "关系合同渠道"}).Error)
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			for _, input := range []ChannelLimitParent{{ChannelId: 82, ParentChannelId: 81, UpdatedBy: 1}, {ChannelId: 81, ParentChannelId: 83, UpdatedBy: 1}} {
				go func(value ChannelLimitParent) {
					<-start
					results <- ReplaceChannelLimitParent(t.Context(), &value)
				}(input)
			}
			close(start)
			first, second := <-results, <-results
			if first == nil {
				require.ErrorIs(t, second, ErrInvalidChannelLimitParent)
			} else {
				require.ErrorIs(t, first, ErrInvalidChannelLimitParent)
				require.NoError(t, second)
			}
			var relation ChannelLimitParent
			require.NoError(t, db.Where("parent_channel_id > 0").First(&relation).Error)
			assert.Equal(t, 1, relation.Revision)
			_, err = BatchDeleteChannels([]int{relation.ParentChannelId})
			require.ErrorIs(t, err, ErrChannelLimitParentInUse)
			require.ErrorIs(t, ReplaceChannelLimitParent(t.Context(), &ChannelLimitParent{ChannelId: relation.ChannelId, UpdatedBy: 1}), ErrChannelLimitParentConflict)
			relation.ParentChannelId = 0
			require.NoError(t, ReplaceChannelLimitParent(t.Context(), &relation))
			stored, err := GetChannelLimitParent(t.Context(), relation.ChannelId)
			require.NoError(t, err)
			assert.Equal(t, 2, stored.Revision)
			assert.Zero(t, stored.ParentChannelId)

			// 绑定与父删除竞争时只能有一个生效，不能留下指向已删除渠道的关系。
			deleteStart := make(chan struct{})
			bound, deleted := make(chan error, 1), make(chan error, 1)
			go func() {
				<-deleteStart
				bound <- ReplaceChannelLimitParent(t.Context(), &ChannelLimitParent{ChannelId: 84, ParentChannelId: 85, UpdatedBy: 1})
			}()
			go func() {
				<-deleteStart
				_, err := BatchDeleteChannels([]int{85})
				deleted <- err
			}()
			close(deleteStart)
			bindErr, deleteErr := <-bound, <-deleted
			if bindErr == nil {
				require.ErrorIs(t, deleteErr, ErrChannelLimitParentInUse)
			} else {
				require.ErrorIs(t, bindErr, ErrInvalidChannelLimitParent)
				require.NoError(t, deleteErr)
			}
			var orphaned int64
			require.NoError(t, db.Model(&ChannelLimitParent{}).Where("parent_channel_id > 0 AND parent_channel_id NOT IN (?)", db.Model(&Channel{}).Select("id")).Count(&orphaned).Error)
			assert.Zero(t, orphaned)
			exerciseChannelStatusCleanupRace(t, db, DeleteDisabledChannel)
		})
	}
}
