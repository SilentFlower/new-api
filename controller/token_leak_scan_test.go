package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTokenLeakControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalRedisEnabled := common.RedisEnabled
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()

	gin.SetMode(gin.TestMode)
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Token{},
		&model.TokenLeakFinding{},
		&model.TokenLeakNotification{},
		&model.Log{},
	))
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
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
	})
	return db
}

func TestDisableTokenLeakFindingTokenReturnsSafeResponseAndRecordsAudit(t *testing.T) {
	db := setupTokenLeakControllerTestDB(t)
	root := model.User{Id: 1, Username: "root", Password: "placeholder", AffCode: "root-aff", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	owner := model.User{Id: 2, Username: "owner", Password: "placeholder", AffCode: "owner-aff", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&root).Error)
	require.NoError(t, db.Create(&owner).Error)
	fullToken := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUV"
	token := model.Token{Id: 51, UserId: owner.Id, Name: "production", Key: fullToken, Status: common.TokenStatusEnabled}
	require.NoError(t, db.Create(&token).Error)
	fingerprint := strings.Repeat("f", 64)
	finding := model.TokenLeakFinding{
		FindingKey:       strings.Repeat("1", 64),
		TokenID:          token.Id,
		UserID:           owner.Id,
		TokenName:        token.Name,
		TokenFingerprint: fingerprint,
		RepositoryID:     1,
		RepositoryName:   "public/repo",
		FilePath:         "config/key.txt",
		Status:           model.TokenLeakFindingStatusOpen,
		FirstFoundAt:     100,
		LastFoundAt:      100,
	}
	require.NoError(t, model.CreateTokenLeakFinding(&finding))

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/token-leak-scan/findings/%d/disable-token", finding.ID), nil)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", finding.ID)}}
	c.Set("id", root.Id)
	c.Set("username", root.Username)
	c.Set("role", root.Role)

	DisableTokenLeakFindingToken(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), fullToken)
	assert.NotContains(t, recorder.Body.String(), fingerprint)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			TokenID int `json:"token_id"`
			Status  int `json:"status"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, token.Id, response.Data.TokenID)
	assert.Equal(t, common.TokenStatusDisabled, response.Data.Status)

	var logs []model.Log
	require.NoError(t, db.Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, model.LogTypeManage, logs[0].Type)
	assert.NotContains(t, logs[0].Content+logs[0].Other, fullToken)
	assert.NotContains(t, logs[0].Content+logs[0].Other, fingerprint)
	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	op, ok := other["op"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "token_leak.disable", op["action"])
}

func TestGetTokenLeakScanFindingsExposesNameWithoutSensitiveIdentity(t *testing.T) {
	setupTokenLeakControllerTestDB(t)
	fingerprint := strings.Repeat("e", 64)
	finding := model.TokenLeakFinding{
		FindingKey:       strings.Repeat("2", 64),
		TokenID:          61,
		UserID:           71,
		TokenName:        "production",
		TokenFingerprint: fingerprint,
		RepositoryID:     2,
		RepositoryName:   "public/repo",
		FilePath:         "config/key.txt",
		HTMLURL:          "https://github.com/public/repo/blob/main/config/key.txt",
		Status:           model.TokenLeakFindingStatusOpen,
		FirstFoundAt:     100,
		LastFoundAt:      100,
	}
	require.NoError(t, model.CreateTokenLeakFinding(&finding))

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/token-leak-scan/findings?status=open", nil)
	GetTokenLeakScanFindings(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"token_name":"production"`)
	assert.NotContains(t, recorder.Body.String(), fingerprint)
}

func TestGetTokenLeakScanFindingsRejectsInvalidStatus(t *testing.T) {
	setupTokenLeakControllerTestDB(t)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/token-leak-scan/findings?status=invalid", nil)

	GetTokenLeakScanFindings(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
}
