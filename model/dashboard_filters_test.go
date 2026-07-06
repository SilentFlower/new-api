package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertDashboardFilterLog(t *testing.T, log Log) {
	t.Helper()
	require.NoError(t, LOG_DB.Create(&log).Error)
}

func TestGetAllQuotaDatesWithFiltersUsesTokenAndGroupIntersection(t *testing.T) {
	truncateTables(t)

	start := time.Now().Unix() - 3600
	end := time.Now().Unix() + 3600
	insertDashboardFilterLog(t, Log{
		UserId:           1,
		Username:         "alice",
		CreatedAt:        start + 60,
		Type:             LogTypeConsume,
		TokenName:        "key-a",
		ModelName:        "gpt-a",
		Quota:            100,
		PromptTokens:     10,
		CompletionTokens: 20,
		Group:            "vip",
	})
	insertDashboardFilterLog(t, Log{
		UserId:           2,
		Username:         "bob",
		CreatedAt:        start + 120,
		Type:             LogTypeConsume,
		TokenName:        "key-a",
		ModelName:        "gpt-a",
		Quota:            200,
		PromptTokens:     30,
		CompletionTokens: 40,
		Group:            "default",
	})
	insertDashboardFilterLog(t, Log{
		UserId:           3,
		Username:         "carol",
		CreatedAt:        start + 180,
		Type:             LogTypeConsume,
		TokenName:        "key-b",
		ModelName:        "gpt-b",
		Quota:            300,
		PromptTokens:     50,
		CompletionTokens: 60,
		Group:            "vip",
	})

	rows, err := GetAllQuotaDatesWithFilters(start, end, "", []string{"key-a"}, []string{"vip"})

	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "gpt-a", rows[0].ModelName)
	assert.Equal(t, "key-a", rows[0].TokenName)
	assert.Equal(t, 100, rows[0].Quota)
	assert.Equal(t, 30, rows[0].TokenUsed)
	assert.Equal(t, 1, rows[0].Count)
}

func TestGetQuotaDataGroupByUserWithFiltersUsesSameConditions(t *testing.T) {
	truncateTables(t)

	start := time.Now().Unix() - 3600
	end := time.Now().Unix() + 3600
	insertDashboardFilterLog(t, Log{
		UserId:           1,
		Username:         "alice",
		CreatedAt:        start + 60,
		Type:             LogTypeConsume,
		TokenName:        "key-a",
		ModelName:        "gpt-a",
		Quota:            100,
		PromptTokens:     10,
		CompletionTokens: 20,
		Group:            "vip",
	})
	insertDashboardFilterLog(t, Log{
		UserId:           2,
		Username:         "bob",
		CreatedAt:        start + 120,
		Type:             LogTypeConsume,
		TokenName:        "key-a",
		ModelName:        "gpt-a",
		Quota:            200,
		PromptTokens:     30,
		CompletionTokens: 40,
		Group:            "vip",
	})

	rows, err := GetQuotaDataGroupByUserWithFilters(start, end, "alice", []string{"key-a"}, []string{"vip"})

	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "alice", rows[0].Username)
	assert.Equal(t, 100, rows[0].Quota)
	assert.Equal(t, 30, rows[0].TokenUsed)
	assert.Equal(t, 1, rows[0].Count)
}

func TestSumUsedQuotaWithFiltersUsesTokenAndGroupIntersection(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	insertDashboardFilterLog(t, Log{
		UserId:           1,
		Username:         "alice",
		CreatedAt:        now,
		Type:             LogTypeConsume,
		TokenName:        "key-a",
		ModelName:        "gpt-a",
		Quota:            100,
		PromptTokens:     10,
		CompletionTokens: 20,
		Group:            "vip",
	})
	insertDashboardFilterLog(t, Log{
		UserId:           1,
		Username:         "alice",
		CreatedAt:        now,
		Type:             LogTypeConsume,
		TokenName:        "key-a",
		ModelName:        "gpt-a",
		Quota:            200,
		PromptTokens:     30,
		CompletionTokens: 40,
		Group:            "default",
	})

	stat, err := SumUsedQuotaWithFilters(LogTypeConsume, now-60, now+60, "", "alice", []string{"key-a"}, 0, []string{"vip"})

	require.NoError(t, err)
	assert.Equal(t, 100, stat.Quota)
	assert.Equal(t, 1, stat.Rpm)
	assert.Equal(t, 30, stat.Tpm)
}

func TestLogExportQueriesUseSameTokenAndGroupFilters(t *testing.T) {
	truncateTables(t)

	start := time.Now().Unix() - 3600
	end := time.Now().Unix() + 3600
	insertDashboardFilterLog(t, Log{
		UserId:           1,
		Username:         "alice",
		CreatedAt:        start + 60,
		Type:             LogTypeConsume,
		TokenName:        "key-a",
		ModelName:        "gpt-a",
		Quota:            100,
		PromptTokens:     10,
		CompletionTokens: 20,
		Group:            "vip",
	})
	insertDashboardFilterLog(t, Log{
		UserId:           2,
		Username:         "bob",
		CreatedAt:        start + 120,
		Type:             LogTypeConsume,
		TokenName:        "key-a",
		ModelName:        "gpt-a",
		Quota:            200,
		PromptTokens:     30,
		CompletionTokens: 40,
		Group:            "default",
	})
	insertDashboardFilterLog(t, Log{
		UserId:           3,
		Username:         "carol",
		CreatedAt:        start + 180,
		Type:             LogTypeConsume,
		TokenName:        "key-b",
		ModelName:        "gpt-b",
		Quota:            300,
		PromptTokens:     50,
		CompletionTokens: 60,
		Group:            "vip",
	})

	summaryRows, err := GetLogSummaryByKey(start, end, "", []string{"key-a"}, []string{"vip"})
	require.NoError(t, err)
	require.Len(t, summaryRows, 1)
	assert.Equal(t, "key-a", summaryRows[0].TokenName)
	assert.Equal(t, "alice", summaryRows[0].Username)
	assert.Equal(t, 100, summaryRows[0].Quota)
	assert.Equal(t, 30, summaryRows[0].TokenUsed)
	assert.Equal(t, 1, summaryRows[0].Count)

	detailRows, err := GetLogDetailByKeyModel(start, end, "", []string{"key-a"}, []string{"vip"})
	require.NoError(t, err)
	require.Len(t, detailRows, 1)
	assert.Equal(t, "key-a", detailRows[0].TokenName)
	assert.Equal(t, "gpt-a", detailRows[0].ModelName)
	assert.Equal(t, 100, detailRows[0].Quota)
	assert.Equal(t, 30, detailRows[0].TokenUsed)
	assert.Equal(t, 1, detailRows[0].Count)

	logRows, err := GetLogsForExport(start, end, "", []string{"key-a"}, []string{"vip"})
	require.NoError(t, err)
	require.Len(t, logRows, 1)
	assert.Equal(t, "key-a", logRows[0].TokenName)
	assert.Equal(t, "vip", logRows[0].Group)
	assert.Equal(t, 100, logRows[0].Quota)
}

func TestGetAllTokenNamesIncludesTokenGroup(t *testing.T) {
	truncateTables(t)

	user := User{
		Username: "alice",
		Password: "password123",
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&Token{
		UserId: user.Id,
		Key:    "token-key-a",
		Name:   "key-a",
		Group:  "vip",
	}).Error)

	options, err := GetAllTokenNames()

	require.NoError(t, err)
	require.Len(t, options, 1)
	assert.Equal(t, "key-a", options[0].Name)
	assert.Equal(t, "alice", options[0].Username)
	assert.Equal(t, "vip", options[0].Group)
}
