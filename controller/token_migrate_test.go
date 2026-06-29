package controller

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTokenMigrateControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalRedisEnabled := common.RedisEnabled
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL

	common.RedisEnabled = false
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))

	model.DB = db
	model.LOG_DB = db

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.RedisEnabled = originalRedisEnabled
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
	})

	return db
}

func TestMigrateSingleTokenInTxEnablesRecordIpLogForNewUser(t *testing.T) {
	db := setupTokenMigrateControllerTestDB(t)

	srcUser := &model.User{
		Username:    "root-user",
		Password:    "placeholder",
		DisplayName: "root-user",
		Role:        common.RoleRootUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AffCode:     "src-aff",
	}
	require.NoError(t, db.Create(srcUser).Error)

	token := &model.Token{
		UserId:         srcUser.Id,
		Key:            "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUV",
		Status:         common.TokenStatusEnabled,
		Name:           "alice-cop",
		CreatedTime:    1,
		AccessedTime:   1,
		ExpiredTime:    -1,
		RemainQuota:    12345,
		UnlimitedQuota: false,
		Group:          "biz_2",
	}
	require.NoError(t, db.Create(token).Error)

	newUsername, newUserId, err := migrateSingleTokenInTx(nil, srcUser, token, map[string]bool{})
	require.NoError(t, err)
	require.Equal(t, "alice-cop", newUsername)
	require.NotZero(t, newUserId)

	var newUser model.User
	require.NoError(t, db.First(&newUser, "id = ?", newUserId).Error)
	assert.Equal(t, "biz_2", newUser.Group)
	assert.Equal(t, 12345, newUser.Quota)
	assert.True(t, newUser.GetSetting().RecordIpLog)

	var migratedToken model.Token
	require.NoError(t, db.First(&migratedToken, "id = ?", token.Id).Error)
	assert.Equal(t, newUserId, migratedToken.UserId)
}
