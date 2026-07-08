package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func statisticTestOther(values map[string]interface{}) string {
	return common.MapToJsonStr(values)
}

func resetStatisticMigrationOption(t *testing.T) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = map[string]string{}
		common.OptionMapRWMutex.Unlock()
	})
}

func TestStatisticTokenUsedForLogAnthropicCache(t *testing.T) {
	tests := []struct {
		name  string
		other map[string]interface{}
		want  int
	}{
		{
			name: "Anthropic 缓存读写计入总量",
			other: map[string]interface{}{
				"usage_semantic":        "anthropic",
				"cache_tokens":          12,
				"cache_creation_tokens": 8,
			},
			want: 150,
		},
		{
			name: "Claude 标记兼容旧日志",
			other: map[string]interface{}{
				"claude":                true,
				"cache_tokens":          "12",
				"cache_creation_tokens": "8",
			},
			want: 150,
		},
		{
			name: "拆分缓存写优先避免双算",
			other: map[string]interface{}{
				"usage_semantic":           "anthropic",
				"cache_tokens":             12,
				"cache_creation_tokens":    100,
				"cache_creation_tokens_5m": 3,
				"cache_creation_tokens_1h": 5,
			},
			want: 150,
		},
		{
			name: "非 Anthropic 缓存字段不重复计入",
			other: map[string]interface{}{
				"cache_tokens":          12,
				"cache_creation_tokens": 8,
			},
			want: 130,
		},
		{
			name:  "无效 Other 回退原始输入输出",
			other: nil,
			want:  130,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := &Log{
				PromptTokens:     100,
				CompletionTokens: 30,
			}
			if tt.other != nil {
				log.Other = statisticTestOther(tt.other)
			} else {
				log.Other = "{"
			}
			assert.Equal(t, tt.want, statisticTokenUsedForLog(log))
		})
	}
}

func TestLogAggregatesIncludeAnthropicCacheTokens(t *testing.T) {
	truncateTables(t)

	start := time.Now().Unix() - 3600
	end := time.Now().Unix() + 3600
	insertDashboardFilterLog(t, Log{
		UserId:           1,
		Username:         "alice",
		CreatedAt:        start + 60,
		Type:             LogTypeConsume,
		TokenName:        "key-a",
		ModelName:        "claude-sonnet",
		Quota:            100,
		PromptTokens:     10,
		CompletionTokens: 20,
		Group:            "vip",
		Other: statisticTestOther(map[string]interface{}{
			"usage_semantic":        "anthropic",
			"cache_tokens":          5,
			"cache_creation_tokens": 7,
		}),
	})

	rows, err := GetAllQuotaDatesWithFilters(start, end, "", []string{"key-a"}, []string{"vip"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 42, rows[0].TokenUsed)

	summaryRows, err := GetLogSummaryByKey(start, end, "", []string{"key-a"}, []string{"vip"})
	require.NoError(t, err)
	require.Len(t, summaryRows, 1)
	assert.Equal(t, 42, summaryRows[0].TokenUsed)

	tokenRows, err := GetTokenQuotaData(999, start, end)
	require.NoError(t, err)
	require.Empty(t, tokenRows)
}

func TestTokenQuotaDataIncludesAnthropicCacheTokens(t *testing.T) {
	truncateTables(t)

	start := time.Now().Unix() - 3600
	end := time.Now().Unix() + 3600
	insertDashboardFilterLog(t, Log{
		UserId:           1,
		Username:         "alice",
		TokenId:          88,
		CreatedAt:        start + 60,
		Type:             LogTypeConsume,
		TokenName:        "key-a",
		ModelName:        "claude-sonnet",
		Quota:            100,
		PromptTokens:     10,
		CompletionTokens: 20,
		Group:            "vip",
		Other: statisticTestOther(map[string]interface{}{
			"claude":                   true,
			"cache_tokens":             5,
			"cache_creation_tokens":    100,
			"cache_creation_tokens_5m": 3,
			"cache_creation_tokens_1h": 4,
		}),
	})

	rows, err := GetTokenQuotaData(88, start, end)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 42, rows[0].TokenUsed)

	stat, err := GetTokenLogStat(88, start, end)
	require.NoError(t, err)
	assert.Equal(t, 42, stat.TotalTokens)
	assert.Equal(t, 10, stat.PromptTokens)
	assert.Equal(t, 20, stat.CompletionTokens)
}

func TestQuotaDataTokenUsedMigrationAddsCacheDeltaOnce(t *testing.T) {
	truncateTables(t)
	resetStatisticMigrationOption(t)

	createdAt := time.Now().Unix() - 3600
	bucket := hourBucketTimestamp(createdAt)
	require.NoError(t, DB.Create(&QuotaData{
		UserID:    1,
		Username:  "alice",
		ModelName: "claude-sonnet",
		TokenName: "key-a",
		CreatedAt: bucket,
		TokenUsed: 30,
		Count:     1,
		Quota:     100,
	}).Error)
	insertDashboardFilterLog(t, Log{
		UserId:           1,
		Username:         "alice",
		CreatedAt:        createdAt,
		Type:             LogTypeConsume,
		TokenName:        "key-a",
		ModelName:        "claude-sonnet",
		Quota:            100,
		PromptTokens:     10,
		CompletionTokens: 20,
		Other: statisticTestOther(map[string]interface{}{
			"usage_semantic":        "anthropic",
			"cache_tokens":          5,
			"cache_creation_tokens": 7,
		}),
	})

	require.NoError(t, migrateQuotaDataTokenUsedStats())
	var quotaData QuotaData
	require.NoError(t, DB.Table("quota_data").Where("user_id = ? AND token_name = ?", 1, "key-a").First(&quotaData).Error)
	assert.Equal(t, 42, quotaData.TokenUsed)

	require.NoError(t, migrateQuotaDataTokenUsedStats())
	require.NoError(t, DB.Table("quota_data").Where("user_id = ? AND token_name = ?", 1, "key-a").First(&quotaData).Error)
	assert.Equal(t, 42, quotaData.TokenUsed)
}
