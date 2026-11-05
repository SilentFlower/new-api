package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// useResponsesWebSocketOptionTestDB 隔离首帧设置的数据库、环境缺省与运行时选项。
// @param t 测试上下文。
// @return 已创建 Options 表的内存数据库。
func useResponsesWebSocketOptionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := useFrontendOptionMigrationDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{"ResponsesWebSocketFirstFrameTimeoutSeconds": "120"}
	common.OptionMapRWMutex.Unlock()
	previousTimeout := constant.ResponsesWebSocketFirstFrameTimeoutSeconds
	constant.ResponsesWebSocketFirstFrameTimeoutSeconds = 120
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
		constant.ResponsesWebSocketFirstFrameTimeoutSeconds = previousTimeout
	})
	return db
}

// TestResponsesWebSocketOptionRejectsInvalidValue 验证单项和批量保存均拒绝非法期限。
// @param t 测试上下文。
// @return 无返回值。
func TestResponsesWebSocketOptionRejectsInvalidValue(t *testing.T) {
	const key = "ResponsesWebSocketFirstFrameTimeoutSeconds"
	for _, bulk := range []bool{false, true} {
		for _, value := range []string{"", "0", "-1", "1.5", "invalid", "9223372037", "18446744073709551615"} {
			name := "单项/" + value
			if bulk {
				name = "批量/" + value
			}
			t.Run(name, func(t *testing.T) {
				db := useResponsesWebSocketOptionTestDB(t)
				require.NoError(t, db.Create(&Option{Key: key, Value: "120"}).Error)

				var err error
				if bulk {
					err = UpdateOptionsBulk(map[string]string{key: value})
				} else {
					err = UpdateOption(key, value)
				}

				assert.Error(t, err)
				assert.Equal(t, "120", requireOptionValue(t, db, key))
				common.OptionMapRWMutex.RLock()
				actual := common.OptionMap[key]
				common.OptionMapRWMutex.RUnlock()
				assert.Equal(t, "120", actual)
			})
		}
	}
}

// TestResponsesWebSocketOptionPersistence 验证环境缺省、站点覆盖和重启重载。
// @param t 测试上下文。
// @return 无返回值。
func TestResponsesWebSocketOptionPersistence(t *testing.T) {
	const key = "ResponsesWebSocketFirstFrameTimeoutSeconds"
	db := useResponsesWebSocketOptionTestDB(t)
	InitOptionMap()
	assert.Equal(t, "120", common.OptionMap[key])
	require.NoError(t, UpdateOption(key, "180"))
	assert.Equal(t, "180", requireOptionValue(t, db, key))

	// 重建内存配置模拟进程重新加载，数据库值应继续覆盖环境缺省。
	InitOptionMap()
	assert.Equal(t, "180", common.OptionMap[key])
	assert.Equal(t, 120, constant.ResponsesWebSocketFirstFrameTimeoutSeconds)
}

// TestResponsesWebSocketOptionSyncRejectsInvalidHistory 验证历史非法值不会污染运行时设置。
// @param t 测试上下文。
// @return 无返回值。
func TestResponsesWebSocketOptionSyncRejectsInvalidHistory(t *testing.T) {
	const key = "ResponsesWebSocketFirstFrameTimeoutSeconds"
	db := useResponsesWebSocketOptionTestDB(t)
	require.NoError(t, db.Create(&Option{Key: key, Value: "0"}).Error)

	loadOptionsFromDatabase()
	assert.Equal(t, "120", common.OptionMap[key])
	InitOptionMap()
	assert.Equal(t, "120", common.OptionMap[key])
}

// TestResponsesWebSocketOptionSaveFailurePreservesValue 验证数据库拒绝写入时不发布新期限。
// @param t 测试上下文。
// @return 无返回值。
func TestResponsesWebSocketOptionSaveFailurePreservesValue(t *testing.T) {
	const key = "ResponsesWebSocketFirstFrameTimeoutSeconds"
	db := useResponsesWebSocketOptionTestDB(t)
	require.NoError(t, db.Create(&Option{Key: key, Value: "120"}).Error)
	// 仅在 SQLite 测试连接设为只读，以复现真实持久化失败并继续核对旧记录。
	require.NoError(t, db.Exec("PRAGMA query_only = ON").Error)
	t.Cleanup(func() { require.NoError(t, db.Exec("PRAGMA query_only = OFF").Error) })

	err := UpdateOption(key, "180")

	assert.Error(t, err)
	assert.Equal(t, "120", requireOptionValue(t, db, key))
	assert.Equal(t, "120", common.OptionMap[key])
}
