package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func newDashboardQueryTestContext(target string) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return ctx
}

func TestParseDashboardQueryValuesRepeatedAndBracket(t *testing.T) {
	ctx := newDashboardQueryTestContext("/api/data/export?token_names=alpha&token_names=beta&token_names[]=%20beta%20&token_names[]=&token_name=legacy")

	values := parseDashboardTokenNames(ctx)

	assert.Equal(t, []string{"alpha", "beta"}, values)
}

func TestParseDashboardQueryValuesLegacyFallback(t *testing.T) {
	ctx := newDashboardQueryTestContext("/api/data/export?group=%20vip%20")

	values := parseDashboardGroups(ctx)

	assert.Equal(t, []string{"vip"}, values)
}

func TestParseDashboardQueryValuesNewEmptyDoesNotFallback(t *testing.T) {
	ctx := newDashboardQueryTestContext("/api/data/export?groups=&groups=%20&group=legacy")

	values := parseDashboardGroups(ctx)

	assert.Empty(t, values)
}

func TestExportQuotaDataExcelIncludesGroupsAndPreservesTokenStatistics(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	start := int64(1700000000)
	end := start + 3600
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId:           1,
		Username:         "alice",
		CreatedAt:        start + 60,
		Type:             model.LogTypeConsume,
		TokenName:        "key-a",
		ModelName:        "claude-sonnet",
		Quota:            100,
		PromptTokens:     10,
		CompletionTokens: 20,
		UseTime:          2,
		IsStream:         true,
		ChannelId:        9,
		Group:            "vip",
		RequestId:        "req-vip",
		Other: common.MapToJsonStr(map[string]interface{}{
			"usage_semantic":        "anthropic",
			"cache_tokens":          5,
			"cache_creation_tokens": 7,
		}),
	}).Error)
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId:           2,
		Username:         "bob",
		CreatedAt:        start + 120,
		Type:             model.LogTypeConsume,
		TokenName:        "key-a",
		ModelName:        "gpt-a",
		Quota:            200,
		PromptTokens:     30,
		CompletionTokens: 40,
		Group:            "default",
		RequestId:        "req-default",
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	target := fmt.Sprintf("/api/data/export?start_timestamp=%d&end_timestamp=%d&groups=vip&token_names=key-a", start, end)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)

	ExportQuotaDataExcel(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", recorder.Header().Get("Content-Type"))
	assert.Contains(t, recorder.Header().Get("Content-Disposition"), time.Unix(start, 0).Format("20060102"))

	workbook, err := excelize.OpenReader(bytes.NewReader(recorder.Body.Bytes()))
	require.NoError(t, err)
	defer workbook.Close()

	summaryRows, err := workbook.GetRows("汇总统计")
	require.NoError(t, err)
	require.Len(t, summaryRows, 2)
	assert.Equal(t, []string{"分组", "API Key 名称", "请求次数", "请求 Token 数", "请求额度"}, summaryRows[0])
	require.GreaterOrEqual(t, len(summaryRows[1]), 4)
	assert.Equal(t, []string{"vip", "key-a", "1", "42"}, summaryRows[1][:4])

	detailRows, err := workbook.GetRows("模型明细")
	require.NoError(t, err)
	require.Len(t, detailRows, 4)
	assert.Equal(t, "分组: vip / API Key: key-a", detailRows[0][0])
	assert.Equal(t, []string{"模型名称", "请求次数", "请求 Token 数", "请求额度"}, detailRows[1])
	require.GreaterOrEqual(t, len(detailRows[2]), 3)
	assert.Equal(t, []string{"claude-sonnet", "1", "42"}, detailRows[2][:3])
	assert.Equal(t, "小计", detailRows[3][0])

	logRows, err := workbook.GetRows("请求日志")
	require.NoError(t, err)
	require.Len(t, logRows, 2)
	assert.Equal(t, []string{"时间", "分组", "API Key", "模型", "输入 Tokens", "输出 Tokens", "额度消耗", "耗时(s)", "是否流式", "渠道 ID", "请求 ID"}, logRows[0])
	require.Len(t, logRows[1], 11)
	assert.Equal(t, "vip", logRows[1][1])
	assert.Equal(t, "key-a", logRows[1][2])
	assert.Equal(t, "claude-sonnet", logRows[1][3])
	assert.Equal(t, "10 (缓存读 5 · 写 7)", logRows[1][4])
	assert.Equal(t, "20", logRows[1][5])
	assert.Equal(t, "是", logRows[1][8])
	assert.Equal(t, "req-vip", logRows[1][10])
}
