package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPersistTokenLeakCandidatesIsIdempotentAndReopensMitigatedFinding(t *testing.T) {
	setupTokenLeakScanTestDB(t)
	t.Setenv("DINGTALK_TOKEN_LEAK_WEBHOOK_TOKEN", "")
	root := model.User{Id: 1, Username: "root", AffCode: "root-aff", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	owner := model.User{Id: 2, Username: "owner", AffCode: "owner-aff", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&root).Error)
	require.NoError(t, model.DB.Create(&owner).Error)
	token := &model.Token{Id: 11, UserId: owner.Id, Name: "production", Key: strings.Repeat("a", 48), Status: common.TokenStatusEnabled}
	require.NoError(t, model.DB.Create(token).Error)
	notifier := newTokenLeakNotifier(nil)
	candidate := githubCodeCandidate{
		RepositoryID:   101,
		RepositoryName: "public/repo",
		Path:           "config/example.txt",
		SHA:            strings.Repeat("b", 40),
		HTMLURL:        "https://github.com/public/repo/blob/main/config/example.txt",
	}

	count, err := persistTokenLeakCandidates(context.Background(), token, strings.Repeat("f", 64), []githubCodeCandidate{candidate}, notifier)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	findings, total, err := model.ListTokenLeakFindings("", 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, findings, 1)
	assert.Equal(t, model.TokenLeakFindingStatusOpen, findings[0].Status)
	assert.Equal(t, token.Name, findings[0].TokenName)

	notifications, err := model.ListTokenLeakNotificationsByFindingIDs([]int64{findings[0].ID})
	require.NoError(t, err)
	assert.Len(t, notifications, 2)

	_, err = persistTokenLeakCandidates(context.Background(), token, strings.Repeat("f", 64), []githubCodeCandidate{candidate}, notifier)
	require.NoError(t, err)
	notifications, err = model.ListTokenLeakNotificationsByFindingIDs([]int64{findings[0].ID})
	require.NoError(t, err)
	assert.Len(t, notifications, 2)

	require.NoError(t, model.UpdateTokenLeakFinding(findings[0].ID, map[string]any{
		"status":            model.TokenLeakFindingStatusMitigated,
		"mitigated_at":      int64(123),
		"mitigation_reason": "token_disabled",
	}))
	_, err = persistTokenLeakCandidates(context.Background(), token, strings.Repeat("f", 64), []githubCodeCandidate{candidate}, notifier)
	require.NoError(t, err)
	reopened, err := model.GetTokenLeakFindingByID(findings[0].ID)
	require.NoError(t, err)
	require.NotNil(t, reopened)
	assert.Equal(t, model.TokenLeakFindingStatusOpen, reopened.Status)
	assert.Equal(t, 1, reopened.ReopenCount)
	assert.Zero(t, reopened.MitigatedAt)
	notifications, err = model.ListTokenLeakNotificationsByFindingIDs([]int64{findings[0].ID})
	require.NoError(t, err)
	assert.Len(t, notifications, 4)

	_, err = persistTokenLeakCandidates(context.Background(), token, strings.Repeat("f", 64), []githubCodeCandidate{candidate}, notifier)
	require.NoError(t, err)
	notifications, err = model.ListTokenLeakNotificationsByFindingIDs([]int64{findings[0].ID})
	require.NoError(t, err)
	assert.Len(t, notifications, 4)

	require.NoError(t, model.UpdateTokenLeakFinding(findings[0].ID, map[string]any{
		"status":            model.TokenLeakFindingStatusMitigated,
		"mitigated_at":      int64(456),
		"mitigation_reason": "token_disabled",
	}))
	_, err = persistTokenLeakCandidates(context.Background(), token, strings.Repeat("f", 64), []githubCodeCandidate{candidate}, notifier)
	require.NoError(t, err)
	reopened, err = model.GetTokenLeakFindingByID(findings[0].ID)
	require.NoError(t, err)
	require.NotNil(t, reopened)
	assert.Equal(t, model.TokenLeakFindingStatusOpen, reopened.Status)
	assert.Equal(t, 2, reopened.ReopenCount)
	notifications, err = model.ListTokenLeakNotificationsByFindingIDs([]int64{findings[0].ID})
	require.NoError(t, err)
	require.Len(t, notifications, 6)
	triggerCounts := map[string]int{}
	for _, notification := range notifications {
		triggerCounts[notification.Trigger]++
	}
	assert.Equal(t, 2, triggerCounts[tokenLeakNotifyTriggerFirst])
	assert.Equal(t, 2, triggerCounts[tokenLeakReopenedNotificationTrigger(1)])
	assert.Equal(t, 2, triggerCounts[tokenLeakReopenedNotificationTrigger(2)])
}

func TestPersistTokenLeakCandidatesResumesInterruptedInitialNotification(t *testing.T) {
	setupTokenLeakScanTestDB(t)
	t.Setenv("DINGTALK_TOKEN_LEAK_WEBHOOK_TOKEN", "")
	root := model.User{Id: 1, Username: "root", Email: "root@example.com", AffCode: "root-aff", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	owner := model.User{Id: 2, Username: "owner", Email: "owner@example.com", AffCode: "owner-aff", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&root).Error)
	require.NoError(t, model.DB.Create(&owner).Error)
	token := &model.Token{Id: 12, UserId: owner.Id, Name: "production", Key: strings.Repeat("b", 48), Status: common.TokenStatusEnabled}
	require.NoError(t, model.DB.Create(token).Error)
	candidate := githubCodeCandidate{
		RepositoryID:   102,
		RepositoryName: "public/repo",
		Path:           "config/secret.txt",
		SHA:            strings.Repeat("c", 40),
		HTMLURL:        "https://github.com/public/repo/blob/main/config/secret.txt",
	}
	notifier := newTokenLeakNotifier(nil)
	sentByUserID := map[int]int{}
	notifier.sendUser = func(userID int, _ string, _ dto.UserSetting, _ dto.Notify) error {
		sentByUserID[userID]++
		return nil
	}
	notifier.getUserByID = func(int, bool) (*model.User, error) {
		return nil, errors.New("temporary user lookup failure")
	}

	count, err := persistTokenLeakCandidates(context.Background(), token, strings.Repeat("f", 64), []githubCodeCandidate{candidate}, notifier)
	require.Error(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, 1, sentByUserID[root.Id])
	assert.Zero(t, sentByUserID[owner.Id])

	notifier.getUserByID = model.GetUserById
	count, err = persistTokenLeakCandidates(context.Background(), token, strings.Repeat("f", 64), []githubCodeCandidate{candidate}, notifier)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, 1, sentByUserID[root.Id])
	assert.Equal(t, 1, sentByUserID[owner.Id])

	findings, total, err := model.ListTokenLeakFindings("", 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, findings, 1)
	notifications, err := model.ListTokenLeakNotificationsByFindingIDs([]int64{findings[0].ID})
	require.NoError(t, err)
	require.Len(t, notifications, 2)
	for _, notification := range notifications {
		assert.Equal(t, model.TokenLeakNotificationStatusSucceeded, notification.Status)
	}
}

func TestDisableTokenLeakFindingTokenDisablesTokenAndMitigatesAllFindings(t *testing.T) {
	setupTokenLeakScanTestDB(t)
	token := model.Token{
		Id:           21,
		UserId:       8,
		Key:          strings.Repeat("k", 48),
		Status:       common.TokenStatusEnabled,
		AccessedTime: 100,
	}
	require.NoError(t, model.DB.Create(&token).Error)
	for index := 0; index < 2; index++ {
		finding := model.TokenLeakFinding{
			FindingKey:     fmt.Sprintf("%064d", index+1),
			TokenID:        token.Id,
			UserID:         token.UserId,
			RepositoryID:   int64(index + 1),
			RepositoryName: "public/repo",
			FilePath:       fmt.Sprintf("file-%d.txt", index),
			Status:         model.TokenLeakFindingStatusOpen,
			FirstFoundAt:   100,
			LastFoundAt:    100,
		}
		require.NoError(t, model.CreateTokenLeakFinding(&finding))
	}
	findings, _, err := model.ListTokenLeakFindings("", 1, 20)
	require.NoError(t, err)

	tokenID, userID, err := DisableTokenLeakFindingToken(findings[0].ID)
	require.NoError(t, err)
	assert.Equal(t, token.Id, tokenID)
	assert.Equal(t, token.UserId, userID)
	updatedToken, err := model.GetTokenForLeakScanByID(token.Id)
	require.NoError(t, err)
	require.NotNil(t, updatedToken)
	assert.Equal(t, common.TokenStatusDisabled, updatedToken.Status)
	updatedFindings, _, err := model.ListTokenLeakFindings("", 1, 20)
	require.NoError(t, err)
	for _, finding := range updatedFindings {
		assert.Equal(t, model.TokenLeakFindingStatusMitigated, finding.Status)
		assert.Equal(t, "token_disabled", finding.MitigationReason)
	}
}

func TestDisableTokenLeakFindingTokenRollsBackWhenFindingUpdateFails(t *testing.T) {
	setupTokenLeakScanTestDB(t)
	token := model.Token{Id: 22, UserId: 9, Key: strings.Repeat("r", 48), Status: common.TokenStatusEnabled}
	require.NoError(t, model.DB.Create(&token).Error)
	finding := model.TokenLeakFinding{
		FindingKey:     strings.Repeat("1", 64),
		TokenID:        token.Id,
		UserID:         token.UserId,
		RepositoryID:   1,
		RepositoryName: "public/repo",
		FilePath:       "leak.txt",
		Status:         model.TokenLeakFindingStatusOpen,
		FirstFoundAt:   100,
		LastFoundAt:    100,
	}
	require.NoError(t, model.CreateTokenLeakFinding(&finding))
	require.NoError(t, model.DB.Exec(`CREATE TRIGGER fail_token_leak_finding_update
		BEFORE UPDATE ON token_leak_findings
		BEGIN
			SELECT RAISE(ABORT, 'forced finding update failure');
		END`).Error)

	_, _, err := DisableTokenLeakFindingToken(finding.ID)
	require.Error(t, err)
	storedToken, err := model.GetTokenForLeakScanByID(token.Id)
	require.NoError(t, err)
	require.NotNil(t, storedToken)
	assert.Equal(t, common.TokenStatusEnabled, storedToken.Status)
	storedFinding, err := model.GetTokenLeakFindingByID(finding.ID)
	require.NoError(t, err)
	require.NotNil(t, storedFinding)
	assert.Equal(t, model.TokenLeakFindingStatusOpen, storedFinding.Status)
}

func TestPersistTokenLeakCandidatesStopsBeforeWritingWhenDisabled(t *testing.T) {
	setupTokenLeakScanTestDB(t)
	operation_setting.GetTokenLeakScanSetting().Enabled = false
	token := &model.Token{Id: 23, UserId: 10, Key: strings.Repeat("s", 48), Status: common.TokenStatusEnabled}
	candidate := githubCodeCandidate{
		RepositoryID:   1,
		RepositoryName: "public/repo",
		Path:           "leak.txt",
		SHA:            strings.Repeat("a", 40),
		HTMLURL:        "https://github.com/public/repo/blob/main/leak.txt",
	}

	count, err := persistTokenLeakCandidates(context.Background(), token, strings.Repeat("f", 64), []githubCodeCandidate{candidate}, newTokenLeakNotifier(nil))
	require.ErrorIs(t, err, ErrTokenLeakScanDisabled)
	assert.Zero(t, count)
	_, total, listErr := model.ListTokenLeakFindings("", 1, 20)
	require.NoError(t, listErr)
	assert.Zero(t, total)
}

func TestGetTokenLeakScanStatusIncludesCoverageAndLatestManualTask(t *testing.T) {
	setupTokenLeakScanTestDB(t)
	now := common.GetTimestamp()
	tokens := []model.Token{
		{Id: 31, UserId: 1, Key: strings.Repeat("a", 48), Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true},
		{Id: 32, UserId: 1, Key: strings.Repeat("b", 48), Status: common.TokenStatusDisabled, ExpiredTime: -1},
		{Id: 33, UserId: 1, Key: strings.Repeat("c", 48), Status: common.TokenStatusEnabled, ExpiredTime: now - 1, UnlimitedQuota: true},
		{Id: 34, UserId: 1, Key: strings.Repeat("d", 48), Status: common.TokenStatusEnabled, ExpiredTime: -1},
	}
	require.NoError(t, model.DB.Create(&tokens).Error)
	require.NoError(t, model.SaveTokenLeakScanState(&model.TokenLeakScanState{TokenID: 31, UserID: 1, TokenStatus: common.TokenStatusEnabled, ScanStatus: model.TokenLeakScanStatusNotFound, SearchRequestCount: 1, LastScanCompletedAt: 100}))
	require.NoError(t, model.SaveTokenLeakScanState(&model.TokenLeakScanState{TokenID: 33, UserID: 1, TokenStatus: common.TokenStatusExpired, ScanStatus: model.TokenLeakScanStatusNotFound, SearchRequestCount: 1, LastScanCompletedAt: 300}))
	require.NoError(t, model.DB.Create(&model.SystemTask{TaskID: "scheduled", Type: model.SystemTaskTypeTokenLeakScan, Status: model.SystemTaskStatusSucceeded, Payload: "{}", State: "{}", Result: "{}"}).Error)
	require.NoError(t, model.DB.Create(&model.SystemTask{TaskID: "manual", Type: model.SystemTaskTypeTokenLeakScanManual, Status: model.SystemTaskStatusSucceeded, Payload: "{}", State: "{}", Result: "{}"}).Error)

	status, err := GetTokenLeakScanStatus()
	require.NoError(t, err)
	require.NotNil(t, status.LastTask)
	assert.Equal(t, model.SystemTaskTypeTokenLeakScanManual, status.LastTask.Type)
	require.NotNil(t, status.LastScheduledTask)
	assert.Equal(t, model.SystemTaskTypeTokenLeakScan, status.LastScheduledTask.Type)
	coverage := make(map[string]TokenLeakScanCoverageStatus, len(status.CoverageByStatus))
	for _, item := range status.CoverageByStatus {
		coverage[item.Status] = item
	}
	assert.Equal(t, 1, coverage[tokenLeakCoverageStatusEnabled].TotalTokens)
	assert.Zero(t, coverage[tokenLeakCoverageStatusEnabled].PendingTokens)
	assert.Equal(t, int64(100), coverage[tokenLeakCoverageStatusEnabled].LastScanCompletedAt)
	assert.Equal(t, 1, coverage[tokenLeakCoverageStatusDisabled].PendingTokens)
	assert.Equal(t, int64(300), coverage[tokenLeakCoverageStatusExpired].LastScanCompletedAt)
	assert.Equal(t, 1, coverage[tokenLeakCoverageStatusExhausted].PendingTokens)
	assert.Equal(t, "ok", status.GitHubAuthStatus)
	assert.Equal(t, int64(300), status.GitHubAuthCheckedAt)
}

func TestGetTokenLeakScanStatusClassifiesGitHubAuthentication(t *testing.T) {
	tests := []struct {
		name           string
		scanStatus     string
		errorCode      string
		expectedStatus string
	}{
		{name: "鉴权失败", scanStatus: model.TokenLeakScanStatusFailed, errorCode: "auth_failed", expectedStatus: "failed"},
		{name: "网络失败", scanStatus: model.TokenLeakScanStatusFailed, errorCode: "network_error", expectedStatus: "unknown"},
		{name: "服务不可用", scanStatus: model.TokenLeakScanStatusFailed, errorCode: "github_unavailable", expectedStatus: "unknown"},
		{name: "搜索不完整但鉴权成功", scanStatus: model.TokenLeakScanStatusIncomplete, errorCode: "search_incomplete", expectedStatus: "ok"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupTokenLeakScanTestDB(t)
			require.NoError(t, model.SaveTokenLeakScanState(&model.TokenLeakScanState{
				TokenID:             41,
				UserID:              1,
				TokenStatus:         common.TokenStatusEnabled,
				ScanStatus:          test.scanStatus,
				ErrorCode:           test.errorCode,
				SearchRequestCount:  1,
				LastScanCompletedAt: 1_700_000_000,
			}))

			status, err := GetTokenLeakScanStatus()
			require.NoError(t, err)
			assert.Equal(t, test.expectedStatus, status.GitHubAuthStatus)
			assert.Equal(t, int64(1_700_000_000), status.GitHubAuthCheckedAt)
		})
	}
}

func TestStartTokenLeakScanTaskReusesActiveTaskWithoutPersistingCredentials(t *testing.T) {
	setupTokenLeakScanTestDB(t)
	githubToken := "github-test-token"
	scanSecret := strings.Repeat("s", 32)
	t.Setenv("GITHUB_TOKEN_LEAK_SCAN_TOKEN", githubToken)
	t.Setenv("GITHUB_TOKEN_LEAK_SCAN_SECRET", scanSecret)

	first, created, err := StartTokenLeakScanTask(0)
	require.NoError(t, err)
	require.True(t, created)
	require.NotNil(t, first)
	assert.NotContains(t, first.Payload, githubToken)
	assert.NotContains(t, first.Payload, scanSecret)

	second, created, err := StartTokenLeakScanTask(0)
	require.NoError(t, err)
	require.False(t, created)
	require.NotNil(t, second)
	assert.Equal(t, first.TaskID, second.TaskID)
	assert.NotContains(t, second.Payload, githubToken)
	assert.NotContains(t, second.Payload, scanSecret)
}

func TestListTokenLeakFindingViewsNormalizesPagination(t *testing.T) {
	setupTokenLeakScanTestDB(t)

	page, err := ListTokenLeakFindingViews("", 0, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, page.Page)
	assert.Equal(t, 20, page.PageSize)
}

func TestIsTokenLeakScanHighPriority(t *testing.T) {
	now := int64(1_000)
	tests := []struct {
		name     string
		token    model.Token
		expected bool
	}{
		{
			name: "unlimited enabled token",
			token: model.Token{
				Status:         common.TokenStatusEnabled,
				ExpiredTime:    -1,
				UnlimitedQuota: true,
			},
			expected: true,
		},
		{
			name: "limited enabled token with quota",
			token: model.Token{
				Status:      common.TokenStatusEnabled,
				ExpiredTime: -1,
				RemainQuota: 1,
			},
			expected: true,
		},
		{
			name: "expired enabled token",
			token: model.Token{
				Status:         common.TokenStatusEnabled,
				ExpiredTime:    now - 1,
				UnlimitedQuota: true,
			},
		},
		{
			name: "exhausted enabled token",
			token: model.Token{
				Status:      common.TokenStatusEnabled,
				ExpiredTime: -1,
			},
		},
		{
			name: "disabled token",
			token: model.Token{
				Status:         common.TokenStatusDisabled,
				ExpiredTime:    -1,
				UnlimitedQuota: true,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, isTokenLeakScanHighPriority(&test.token, now))
		})
	}
}

func setupTokenLeakScanTestDB(t *testing.T) {
	t.Helper()
	oldDB := model.DB
	oldDatabaseType := common.MainDatabaseType()
	oldRedisEnabled := common.RedisEnabled
	oldScanEnabled := operation_setting.GetTokenLeakScanSetting().Enabled
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Token{},
		&model.TokenLeakScanState{},
		&model.TokenLeakFinding{},
		&model.TokenLeakNotification{},
		&model.SystemTask{},
	))
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	operation_setting.GetTokenLeakScanSetting().Enabled = true
	t.Cleanup(func() {
		model.DB = oldDB
		common.SetMainDatabaseType(oldDatabaseType)
		common.RedisEnabled = oldRedisEnabled
		operation_setting.GetTokenLeakScanSetting().Enabled = oldScanEnabled
	})
}
