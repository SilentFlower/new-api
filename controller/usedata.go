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

	// 校验必填参数
	if startTimestamp == 0 || endTimestamp == 0 {
		common.ApiErrorMsg(c, "start_timestamp 和 end_timestamp 为必填参数")
		return
	}

	// 查询 Sheet 1 数据：按 API Key 汇总（从 logs 表聚合）
	summaryData, err := model.GetLogSummaryByKey(startTimestamp, endTimestamp, "", "")
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 查询 Sheet 2 数据：按 API Key + 模型明细（从 logs 表聚合）
	detailData, err := model.GetLogDetailByKeyModel(startTimestamp, endTimestamp, "", "")
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 查询 Sheet 3 数据：请求日志明细
	logs, err := model.GetLogsForExport(startTimestamp, endTimestamp, "", "")
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
	sheet1Headers := []string{"API Key 名称", "请求次数", "请求 Token 数", "请求额度"}
	for i, header := range sheet1Headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet1Name, cell, header)
	}
	for rowIdx, item := range summaryData {
		row := rowIdx + 2 // 从第2行开始写数据
		f.SetCellValue(sheet1Name, cellName(1, row), item.TokenName)
		f.SetCellValue(sheet1Name, cellName(2, row), item.Count)
		f.SetCellValue(sheet1Name, cellName(3, row), item.TokenUsed)
		f.SetCellValue(sheet1Name, cellName(4, row), formatQuotaValue(item.Quota))
	}

	// ========== Sheet 2：模型明细（按 Key 分组） ==========
	sheet2Name := "模型明细"
	f.NewSheet(sheet2Name)
	sheet2Headers := []string{"模型名称", "请求次数", "请求 Token 数", "请求额度"}

	// 按 token_name 分组
	keyGroups := make(map[string][]*model.LogDetailByKeyModel)
	var keyOrder []string
	for _, item := range detailData {
		if _, exists := keyGroups[item.TokenName]; !exists {
			keyOrder = append(keyOrder, item.TokenName)
		}
		keyGroups[item.TokenName] = append(keyGroups[item.TokenName], item)
	}

	// 创建加粗样式用于分组标题和小计行
	boldStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
	})

	row := 1
	for _, keyName := range keyOrder {
		items := keyGroups[keyName]

		// 分组标题行：API Key 名称
		f.SetCellValue(sheet2Name, cellName(1, row), "API Key: "+keyName)
		f.SetCellStyle(sheet2Name, cellName(1, row), cellName(1, row), boldStyle)
		row++

		// 表头行
		for i, header := range sheet2Headers {
			f.SetCellValue(sheet2Name, cellName(i+1, row), header)
			f.SetCellStyle(sheet2Name, cellName(i+1, row), cellName(i+1, row), boldStyle)
		}
		row++

		// 数据行 + 累计小计
		var totalCount, totalTokenUsed, totalQuota int
		for _, item := range items {
			f.SetCellValue(sheet2Name, cellName(1, row), item.ModelName)
			f.SetCellValue(sheet2Name, cellName(2, row), item.Count)
			f.SetCellValue(sheet2Name, cellName(3, row), item.TokenUsed)
			f.SetCellValue(sheet2Name, cellName(4, row), formatQuotaValue(item.Quota))
			totalCount += item.Count
			totalTokenUsed += item.TokenUsed
			totalQuota += item.Quota
			row++
		}

		// 小计行
		f.SetCellValue(sheet2Name, cellName(1, row), "小计")
		f.SetCellValue(sheet2Name, cellName(2, row), totalCount)
		f.SetCellValue(sheet2Name, cellName(3, row), totalTokenUsed)
		f.SetCellValue(sheet2Name, cellName(4, row), formatQuotaValue(totalQuota))
		f.SetCellStyle(sheet2Name, cellName(1, row), cellName(4, row), boldStyle)
		row++

		// 空行分隔
		row++
	}

	// ========== Sheet 3：请求日志 ==========
	sheet3Name := "请求日志"
	f.NewSheet(sheet3Name)
	sheet3Headers := []string{"时间", "API Key", "模型", "输入 Tokens", "输出 Tokens", "额度消耗", "耗时(s)", "是否流式", "渠道 ID", "请求 ID"}
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
		// 解析 Other JSON 获取缓存信息，对齐日志表展示方式
		inputDisplay := fmt.Sprintf("%d", logItem.PromptTokens)
		if logItem.Other != "" {
			if otherMap, err := common.StrToMap(logItem.Other); err == nil {
				cacheRead := getOtherInt(otherMap, "cache_tokens")
				// 缓存写入：优先使用分时段值之和，回退到总值
				cacheWrite5m := getOtherInt(otherMap, "cache_creation_tokens_5m")
				cacheWrite1h := getOtherInt(otherMap, "cache_creation_tokens_1h")
				cacheWrite := cacheWrite5m + cacheWrite1h
				if cacheWrite == 0 {
					cacheWrite = getOtherInt(otherMap, "cache_creation_tokens")
				}
				if cacheRead > 0 && cacheWrite > 0 {
					inputDisplay = fmt.Sprintf("%d (缓存读 %d · 写 %d)", logItem.PromptTokens, cacheRead, cacheWrite)
				} else if cacheRead > 0 {
					inputDisplay = fmt.Sprintf("%d (缓存读 %d)", logItem.PromptTokens, cacheRead)
				} else if cacheWrite > 0 {
					inputDisplay = fmt.Sprintf("%d (缓存写 %d)", logItem.PromptTokens, cacheWrite)
				}
			}
		}
		f.SetCellValue(sheet3Name, cellName(1, row), timeStr)
		f.SetCellValue(sheet3Name, cellName(2, row), logItem.TokenName)
		f.SetCellValue(sheet3Name, cellName(3, row), logItem.ModelName)
		f.SetCellValue(sheet3Name, cellName(4, row), inputDisplay)
		f.SetCellValue(sheet3Name, cellName(5, row), logItem.CompletionTokens)
		f.SetCellValue(sheet3Name, cellName(6, row), formatQuotaValue(logItem.Quota))
		f.SetCellValue(sheet3Name, cellName(7, row), logItem.UseTime)
		f.SetCellValue(sheet3Name, cellName(8, row), isStreamStr)
		f.SetCellValue(sheet3Name, cellName(9, row), logItem.ChannelId)
		f.SetCellValue(sheet3Name, cellName(10, row), logItem.RequestId)
	}

	// ========== 设置列宽 ==========
	// Sheet 1：汇总统计 — API Key 名称, 请求次数, 请求 Token 数, 请求额度
	f.SetColWidth(sheet1Name, "A", "A", 30) // API Key 名称
	f.SetColWidth(sheet1Name, "B", "B", 12) // 请求次数
	f.SetColWidth(sheet1Name, "C", "C", 16) // 请求 Token 数
	f.SetColWidth(sheet1Name, "D", "D", 14) // 请求额度

	// Sheet 2：模型明细 — 模型名称, 请求次数, 请求 Token 数, 请求额度
	f.SetColWidth(sheet2Name, "A", "A", 30) // 模型名称 / API Key 分组标题
	f.SetColWidth(sheet2Name, "B", "B", 12) // 请求次数
	f.SetColWidth(sheet2Name, "C", "C", 16) // 请求 Token 数
	f.SetColWidth(sheet2Name, "D", "D", 14) // 请求额度

	// Sheet 3：请求日志 — 时间, API Key, 模型, 输入 Tokens, 输出 Tokens, 额度消耗, 耗时(s), 是否流式, 渠道 ID, 请求 ID
	f.SetColWidth(sheet3Name, "A", "A", 20) // 时间
	f.SetColWidth(sheet3Name, "B", "B", 24) // API Key
	f.SetColWidth(sheet3Name, "C", "C", 28) // 模型
	f.SetColWidth(sheet3Name, "D", "D", 14) // 输入 Tokens
	f.SetColWidth(sheet3Name, "E", "E", 14) // 输出 Tokens
	f.SetColWidth(sheet3Name, "F", "F", 12) // 额度消耗
	f.SetColWidth(sheet3Name, "G", "G", 10) // 耗时(s)
	f.SetColWidth(sheet3Name, "H", "H", 10) // 是否流式
	f.SetColWidth(sheet3Name, "I", "I", 10) // 渠道 ID
	f.SetColWidth(sheet3Name, "J", "J", 38) // 请求 ID

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

// getOtherInt 从 Other JSON map 中安全提取整数值
func getOtherInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return int(f)
		}
	}
	return 0
}

// formatQuotaValue 将 quota 原始值转换为美元单位
// quota 存储的是内部单位值，需要除以 QuotaPerUnit 转换
func formatQuotaValue(quota int) float64 {
	return float64(quota) / common.QuotaPerUnit
}

// GetAllTokenNames 获取所有令牌名称列表（管理员接口）
// 用于数据看板搜索条件中的令牌下拉选择，返回去重后的令牌名称及其所属用户名
func GetAllTokenNames(c *gin.Context) {
	options, err := model.GetAllTokenNames()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    options,
	})
}

// GetSystemStats 获取系统级统计数据（管理员接口）
// 返回所有用户的余额、消耗额度、请求次数汇总，用于管理员数据看板
func GetSystemStats(c *gin.Context) {
	stats, err := model.GetSystemStats()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    stats,
	})
}

// GetUserQuotaDates 获取当前用户的配额统计数据（用户端接口）
// 支持按 API Key 名称过滤，时间跨度限制 1 个月
func GetQuotaDatesByUser(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	dates, err := model.GetQuotaDataGroupByUser(startTimestamp, endTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
}

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
