package model

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestWorkflowNamePhysicalMigration 验证旧窄列升级后可容纳业务名称，且五个名称列不固定为 64。
// @param t 测试上下文。
// @return 无。
func TestWorkflowNamePhysicalMigration(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		envName string
		open    func(string) (gorm.Dialector, error)
	}{
		{"mysql", "WORKFLOW_NAME_MIGRATION_MYSQL_DSN", func(dsn string) (gorm.Dialector, error) {
			return mysql.Open(dsn), nil
		}},
		{"postgres", "WORKFLOW_NAME_MIGRATION_POSTGRES_DSN", func(dsn string) (gorm.Dialector, error) {
			return postgres.Open(dsn), nil
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			dsn := os.Getenv(testCase.envName)
			if dsn == "" {
				t.Skipf("未设置 %s", testCase.envName)
			}
			dialector, err := testCase.open(dsn)
			require.NoError(t, err)
			recorder := &migrationSQLRecorder{}
			db, err := gorm.Open(dialector, &gorm.Config{Logger: recorder})
			require.NoError(t, err)
			var databaseName string
			query := "SELECT current_database()"
			if testCase.name == "mysql" {
				query = "SELECT DATABASE()"
			}
			require.NoError(t, db.Raw(query).Scan(&databaseName).Error)
			require.Equal(t, "workflow_name_migration_test", databaseName)

			for _, table := range []string{"quota_data", "logs", "tokens", "users"} {
				require.NoError(t, db.Exec("DROP TABLE IF EXISTS "+table).Error)
				t.Cleanup(func() { db.Exec("DROP TABLE IF EXISTS " + table) })
			}
			for _, statement := range []string{
				"CREATE TABLE users (id BIGINT PRIMARY KEY, username VARCHAR(20) UNIQUE, display_name VARCHAR(20), password VARCHAR(20) NOT NULL)",
				"CREATE TABLE tokens (id BIGINT PRIMARY KEY, name VARCHAR(50))",
				"CREATE TABLE logs (id BIGINT PRIMARY KEY, username VARCHAR(20), token_name VARCHAR(50))",
				"CREATE TABLE quota_data (id BIGINT PRIMARY KEY, username VARCHAR(20), token_name VARCHAR(50))",
				"INSERT INTO users (id, username, display_name, password) VALUES (1, '旧用户', '旧用户', 'old-password')",
				"INSERT INTO tokens (id, name) VALUES (1, 'old-token')",
				"INSERT INTO logs (id, username, token_name) VALUES (1, '旧用户', 'old-token')",
				"INSERT INTO quota_data (id, username, token_name) VALUES (1, '旧用户', 'old-token')",
			} {
				require.NoError(t, db.Exec(statement).Error)
			}
			require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &Log{}, &QuotaData{}))

			longName := strings.Repeat("测", UserNameMaxLength)
			longerName := strings.Repeat("测", UserNameMaxLength+1)
			for _, column := range []struct {
				table, name string
				allowLonger bool
			}{
				{"users", "username", true}, {"users", "display_name", true},
				{"tokens", "name", true}, {"logs", "username", true}, {"logs", "token_name", true},
				{"quota_data", "username", false}, {"quota_data", "token_name", false},
			} {
				columnTypes, err := db.Migrator().ColumnTypes(column.table)
				require.NoError(t, err)
				found := false
				for _, columnType := range columnTypes {
					if columnType.Name() != column.name {
						continue
					}
					length, ok := columnType.Length()
					if column.allowLonger {
						if ok {
							require.Greater(t, length, int64(UserNameMaxLength), "%s.%s 不应限制为 64", column.table, column.name)
						}
					} else {
						require.True(t, ok, "%s.%s 缺少列宽", column.table, column.name)
						require.EqualValues(t, UserNameMaxLength, length)
					}
					found = true
				}
				require.True(t, found, "%s.%s 不存在", column.table, column.name)
				require.NoError(t, db.Exec("UPDATE "+column.table+" SET "+column.name+" = ? WHERE id = 1", longName).Error)
				var storedName string
				require.NoError(t, db.Table(column.table).Select(column.name).Where("id = 1").Scan(&storedName).Error)
				require.Equal(t, longName, storedName)
				if column.allowLonger {
					require.NoError(t, db.Exec("UPDATE "+column.table+" SET "+column.name+" = ? WHERE id = 1", longerName).Error)
					require.NoError(t, db.Table(column.table).Select(column.name).Where("id = 1").Scan(&storedName).Error)
					require.Equal(t, longerName, storedName)
				}
			}
			if testCase.name == "mysql" {
				// PostgreSQL 的其他旧字段仍会重复发出 ALTER，此处守住生产 MySQL 的启动迁移。
				recorder.reset()
				require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &Log{}, &QuotaData{}))
				require.Empty(t, recorder.schemaMutations(), "目标列宽下再次迁移不应重复 ALTER TABLE")
			}
		})
	}
}
