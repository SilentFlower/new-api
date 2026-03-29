package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

// GetAllQuotaDates 获取所有配额统计数据（管理员接口）
// 支持按用户名和 API Key 名称过滤
func GetAllQuotaDates(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	dates, err := model.GetAllQuotaDates(startTimestamp, endTimestamp, username, tokenName)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
	return
}

// ExportQuotaDataExcel 导出数据看板 Excel 报表（管理员接口）
// 生成包含三个 Sheet 的 Excel 文件：
// - Sheet 1：按 API Key 汇总统计
// - Sheet 2：按 API Key + 模型明细
// - Sheet 3：请求日志明细
func ExportQuotaDataExcel(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")

	// 校验必填参数
	if startTimestamp == 0 || endTimestamp == 0 {
		common.ApiErrorMsg(c, "start_timestamp 和 end_timestamp 为必填参数")
		return
	}

	// 查询 Sheet 1 数据：按 API Key 汇总（从 logs 表聚合）
	summaryData, err := model.GetLogSummaryByKey(startTimestamp, endTimestamp, username, tokenName)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 查询 Sheet 2 数据：按 API Key + 模型明细（从 logs 表聚合）
	detailData, err := model.GetLogDetailByKeyModel(startTimestamp, endTimestamp, username, tokenName)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 查询 Sheet 3 数据：请求日志明细
	logs, err := model.GetLogsForExport(startTimestamp, endTimestamp, username, tokenName)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 创建 Excel 文件
	f := excelize.NewFile()
	defer f.Close()

	// ========== Sheet 1：汇总统计 ==========
	sheet1Name := "汇总统计"
	// 默认 Sheet 名为 "Sheet1"，重命名为汇总统计
	f.SetSheetName("Sheet1", sheet1Name)
	sheet1Headers := []string{"API Key 名称", "所属用户", "请求次数", "请求 Token 数", "请求额度"}
	for i, header := range sheet1Headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet1Name, cell, header)
	}
	for rowIdx, item := range summaryData {
		row := rowIdx + 2 // 从第2行开始写数据
		f.SetCellValue(sheet1Name, cellName(1, row), item.TokenName)
		f.SetCellValue(sheet1Name, cellName(2, row), item.Username)
		f.SetCellValue(sheet1Name, cellName(3, row), item.Count)
		f.SetCellValue(sheet1Name, cellName(4, row), item.TokenUsed)
		f.SetCellValue(sheet1Name, cellName(5, row), formatQuotaValue(item.Quota))
	}

	// ========== Sheet 2：模型明细 ==========
	sheet2Name := "模型明细"
	f.NewSheet(sheet2Name)
	sheet2Headers := []string{"API Key 名称", "所属用户", "模型名称", "请求次数", "请求 Token 数", "请求额度"}
	for i, header := range sheet2Headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet2Name, cell, header)
	}
	for rowIdx, item := range detailData {
		row := rowIdx + 2
		f.SetCellValue(sheet2Name, cellName(1, row), item.TokenName)
		f.SetCellValue(sheet2Name, cellName(2, row), item.Username)
		f.SetCellValue(sheet2Name, cellName(3, row), item.ModelName)
		f.SetCellValue(sheet2Name, cellName(4, row), item.Count)
		f.SetCellValue(sheet2Name, cellName(5, row), item.TokenUsed)
		f.SetCellValue(sheet2Name, cellName(6, row), formatQuotaValue(item.Quota))
	}

	// ========== Sheet 3：请求日志 ==========
	sheet3Name := "请求日志"
	f.NewSheet(sheet3Name)
	sheet3Headers := []string{"时间", "用户名", "API Key", "模型", "提示 Tokens", "完成 Tokens", "额度消耗", "耗时(s)", "是否流式", "渠道 ID", "请求 ID"}
	for i, header := range sheet3Headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet3Name, cell, header)
	}
	for rowIdx, logItem := range logs {
		row := rowIdx + 2
		// 将时间戳格式化为可读时间
		timeStr := time.Unix(logItem.CreatedAt, 0).Format("2006-01-02 15:04:05")
		isStreamStr := "否"
		if logItem.IsStream {
			isStreamStr = "是"
		}
		f.SetCellValue(sheet3Name, cellName(1, row), timeStr)
		f.SetCellValue(sheet3Name, cellName(2, row), logItem.Username)
		f.SetCellValue(sheet3Name, cellName(3, row), logItem.TokenName)
		f.SetCellValue(sheet3Name, cellName(4, row), logItem.ModelName)
		f.SetCellValue(sheet3Name, cellName(5, row), logItem.PromptTokens)
		f.SetCellValue(sheet3Name, cellName(6, row), logItem.CompletionTokens)
		f.SetCellValue(sheet3Name, cellName(7, row), formatQuotaValue(logItem.Quota))
		f.SetCellValue(sheet3Name, cellName(8, row), logItem.UseTime)
		f.SetCellValue(sheet3Name, cellName(9, row), isStreamStr)
		f.SetCellValue(sheet3Name, cellName(10, row), logItem.ChannelId)
		f.SetCellValue(sheet3Name, cellName(11, row), logItem.RequestId)
	}

	// 生成文件名，包含时间范围
	startDate := time.Unix(startTimestamp, 0).Format("20060102")
	endDate := time.Unix(endTimestamp, 0).Format("20060102")
	fileName := fmt.Sprintf("数据报表_%s_%s.xlsx", startDate, endDate)

	// 设置响应头并写入
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(fileName)))
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Cache-Control", "no-cache")

	if err := f.Write(c.Writer); err != nil {
		common.SysLog(fmt.Sprintf("导出 Excel 文件失败: %s", err.Error()))
		return
	}
}

// cellName 将列号和行号转换为 Excel 单元格名称（如 A1, B2）
func cellName(col, row int) string {
	name, _ := excelize.CoordinatesToCellName(col, row)
	return name
}

// formatQuotaValue 将 quota 原始值转换为美元单位
// quota 存储的是内部单位值，需要除以 QuotaPerUnit 转换
func formatQuotaValue(quota int) float64 {
	return float64(quota) / common.QuotaPerUnit
}

// GetUserQuotaDates 获取当前用户的配额统计数据（用户端接口）
// 支持按 API Key 名称过滤，时间跨度限制 1 个月
func GetUserQuotaDates(c *gin.Context) {
	userId := c.GetInt("id")
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	// 判断时间跨度是否超过 1 个月
	if endTimestamp-startTimestamp > 2592000 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "时间跨度不能超过 1 个月",
		})
		return
	}
	dates, err := model.GetQuotaDataByUserId(userId, startTimestamp, endTimestamp, tokenName)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
	return
}
