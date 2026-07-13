package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

func parseFlowQuotaTimeRange(c *gin.Context) (int64, int64, bool) {
	startTimestamp, err := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	if err != nil || startTimestamp <= 0 {
		common.ApiErrorMsg(c, "invalid start_timestamp")
		return 0, 0, false
	}
	endTimestamp, err := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if err != nil || endTimestamp <= 0 {
		common.ApiErrorMsg(c, "invalid end_timestamp")
		return 0, 0, false
	}
	if endTimestamp < startTimestamp {
		common.ApiErrorMsg(c, "invalid time range")
		return 0, 0, false
	}
	return startTimestamp, endTimestamp, true
}

// GetAllQuotaDates 获取所有配额统计数据（管理员接口）
// 支持按用户名、API Key 名称和分组过滤。
func GetAllQuotaDates(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenNames := parseDashboardTokenNames(c)
	groups := parseDashboardGroups(c)
	dates, err := model.GetAllQuotaDatesWithFilters(startTimestamp, endTimestamp, username, tokenNames, groups)
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
// - Sheet 1：按分组 + API Key 汇总统计
// - Sheet 2：按分组 + API Key + 模型明细
// - Sheet 3：请求日志明细
// @param c Gin 请求上下文
// @return 无返回值，成功时直接写入 Excel 文件响应
func ExportQuotaDataExcel(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenNames := parseDashboardTokenNames(c)
	groups := parseDashboardGroups(c)

	// 校验必填参数
	if startTimestamp == 0 || endTimestamp == 0 {
		common.ApiErrorMsg(c, "start_timestamp 和 end_timestamp 为必填参数")
		return
	}

	f := excelize.NewFile()
	defer f.Close()

	sheet1Name := "汇总统计"
	if err := f.SetSheetName("Sheet1", sheet1Name); err != nil {
		common.ApiError(c, err)
		return
	}
	sheet2Name := "模型明细"
	if _, err := f.NewSheet(sheet2Name); err != nil {
		common.ApiError(c, err)
		return
	}
	sheet3Name := "请求日志"
	if _, err := f.NewSheet(sheet3Name); err != nil {
		common.ApiError(c, err)
		return
	}

	boldStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	sheet1Writer, err := f.NewStreamWriter(sheet1Name)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	sheet2Writer, err := f.NewStreamWriter(sheet2Name)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	sheet3Writer, err := f.NewStreamWriter(sheet3Name)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if err := setStreamColumnWidths(sheet1Writer, []float64{18, 30, 12, 16, 14}); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := setStreamColumnWidths(sheet2Writer, []float64{42, 12, 16, 14}); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := setStreamColumnWidths(sheet3Writer, []float64{20, 18, 24, 28, 22, 14, 12, 10, 10, 10, 38}); err != nil {
		common.ApiError(c, err)
		return
	}

	sheet3Headers := []interface{}{"时间", "分组", "API Key", "模型", "输入 Tokens", "输出 Tokens", "额度消耗", "耗时(s)", "是否流式", "渠道 ID", "请求 ID"}
	if err := sheet3Writer.SetRow("A1", sheet3Headers); err != nil {
		common.ApiError(c, err)
		return
	}

	sheet3Row := 2
	summaryData, detailData, err := model.ProcessLogsForExport(c.Request.Context(), startTimestamp, endTimestamp, "", tokenNames, groups, func(logItem *model.Log, cacheRead int, cacheWrite int) error {
		isStreamStr := "否"
		if logItem.IsStream {
			isStreamStr = "是"
		}
		values := []interface{}{
			time.Unix(logItem.CreatedAt, 0).Format("2006-01-02 15:04:05"),
			logItem.Group,
			logItem.TokenName,
			logItem.ModelName,
			formatExportInputTokens(logItem.PromptTokens, cacheRead, cacheWrite),
			logItem.CompletionTokens,
			formatQuotaValue(logItem.Quota),
			logItem.UseTime,
			isStreamStr,
			logItem.ChannelId,
			logItem.RequestId,
		}
		if err := sheet3Writer.SetRow(cellName(1, sheet3Row), values); err != nil {
			return err
		}
		sheet3Row++
		return nil
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := sheet3Writer.Flush(); err != nil {
		common.ApiError(c, err)
		return
	}

	sheet1Headers := []interface{}{"分组", "API Key 名称", "请求次数", "请求 Token 数", "请求额度"}
	if err := sheet1Writer.SetRow("A1", sheet1Headers); err != nil {
		common.ApiError(c, err)
		return
	}
	for rowIdx, item := range summaryData {
		values := []interface{}{item.Group, item.TokenName, item.Count, item.TokenUsed, formatQuotaValue(item.Quota)}
		if err := sheet1Writer.SetRow(cellName(1, rowIdx+2), values); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if err := sheet1Writer.Flush(); err != nil {
		common.ApiError(c, err)
		return
	}

	sheet2Headers := []interface{}{"模型名称", "请求次数", "请求 Token 数", "请求额度"}
	sheet2Row := 1
	currentGroup := ""
	currentTokenName := ""
	hasCurrentGroup := false
	var totalCount, totalTokenUsed, totalQuota int
	for index, item := range detailData {
		groupChanged := !hasCurrentGroup || item.Group != currentGroup || item.TokenName != currentTokenName
		if groupChanged {
			if hasCurrentGroup {
				values := []interface{}{"小计", totalCount, totalTokenUsed, formatQuotaValue(totalQuota)}
				if err := sheet2Writer.SetRow(cellName(1, sheet2Row), values, excelize.RowOpts{StyleID: boldStyle}); err != nil {
					common.ApiError(c, err)
					return
				}
				sheet2Row += 2
			}

			currentGroup = item.Group
			currentTokenName = item.TokenName
			hasCurrentGroup = true
			totalCount = 0
			totalTokenUsed = 0
			totalQuota = 0

			title := fmt.Sprintf("分组: %s / API Key: %s", currentGroup, currentTokenName)
			if err := sheet2Writer.SetRow(cellName(1, sheet2Row), []interface{}{title}, excelize.RowOpts{StyleID: boldStyle}); err != nil {
				common.ApiError(c, err)
				return
			}
			sheet2Row++
			if err := sheet2Writer.SetRow(cellName(1, sheet2Row), sheet2Headers, excelize.RowOpts{StyleID: boldStyle}); err != nil {
				common.ApiError(c, err)
				return
			}
			sheet2Row++
		}

		values := []interface{}{item.ModelName, item.Count, item.TokenUsed, formatQuotaValue(item.Quota)}
		if err := sheet2Writer.SetRow(cellName(1, sheet2Row), values); err != nil {
			common.ApiError(c, err)
			return
		}
		sheet2Row++
		totalCount += item.Count
		totalTokenUsed += item.TokenUsed
		totalQuota += item.Quota

		if index == len(detailData)-1 {
			values := []interface{}{"小计", totalCount, totalTokenUsed, formatQuotaValue(totalQuota)}
			if err := sheet2Writer.SetRow(cellName(1, sheet2Row), values, excelize.RowOpts{StyleID: boldStyle}); err != nil {
				common.ApiError(c, err)
				return
			}
		}
	}
	if err := sheet2Writer.Flush(); err != nil {
		common.ApiError(c, err)
		return
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
		logger.LogError(c.Request.Context(), fmt.Sprintf("导出 Excel 文件失败: %s", err.Error()))
		return
	}
}

// parseDashboardTokenNames 解析数据看板令牌筛选参数，并兼容旧的 token_name 单值参数。
func parseDashboardTokenNames(c *gin.Context) []string {
	return parseDashboardQueryValues(c, "token_names", "token_name")
}

// parseDashboardGroups 解析数据看板分组筛选参数，并兼容旧的 group 单值参数。
func parseDashboardGroups(c *gin.Context) []string {
	return parseDashboardQueryValues(c, "groups", "group")
}

// parseDashboardQueryValues 解析重复 key / 括号数组 / 旧单值查询参数，并做 trim、去空、去重。
func parseDashboardQueryValues(c *gin.Context, multiName string, legacyName string) []string {
	values, hasValues := c.GetQueryArray(multiName)
	bracketValues, hasBracketValues := c.GetQueryArray(multiName + "[]")
	rawValues := make([]string, 0, len(values)+len(bracketValues)+1)
	rawValues = append(rawValues, values...)
	rawValues = append(rawValues, bracketValues...)
	if !hasValues && !hasBracketValues && legacyName != "" {
		rawValues = append(rawValues, c.Query(legacyName))
	}

	seen := make(map[string]struct{}, len(rawValues))
	normalized := make([]string, 0, len(rawValues))
	for _, value := range rawValues {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

// cellName 将列号和行号转换为 Excel 单元格名称（如 A1, B2）
func cellName(col, row int) string {
	name, _ := excelize.CoordinatesToCellName(col, row)
	return name
}

func setStreamColumnWidths(writer *excelize.StreamWriter, widths []float64) error {
	for index, width := range widths {
		column := index + 1
		if err := writer.SetColWidth(column, column, width); err != nil {
			return err
		}
	}
	return nil
}

func formatExportInputTokens(promptTokens int, cacheRead int, cacheWrite int) string {
	if cacheRead > 0 && cacheWrite > 0 {
		return fmt.Sprintf("%d (缓存读 %d · 写 %d)", promptTokens, cacheRead, cacheWrite)
	}
	if cacheRead > 0 {
		return fmt.Sprintf("%d (缓存读 %d)", promptTokens, cacheRead)
	}
	if cacheWrite > 0 {
		return fmt.Sprintf("%d (缓存写 %d)", promptTokens, cacheWrite)
	}
	return fmt.Sprintf("%d", promptTokens)
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
	username := c.Query("username")
	tokenNames := parseDashboardTokenNames(c)
	groups := parseDashboardGroups(c)
	dates, err := model.GetQuotaDataGroupByUserWithFilters(startTimestamp, endTimestamp, username, tokenNames, groups)
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
	tokenNames := parseDashboardTokenNames(c)
	// 判断时间跨度是否超过 1 个月
	if endTimestamp-startTimestamp > 2592000 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "时间跨度不能超过 1 个月",
		})
		return
	}
	dates, err := model.GetQuotaDataByUserIdWithFilters(userId, startTimestamp, endTimestamp, tokenNames)
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

func GetAllFlowQuotaDates(c *gin.Context) {
	startTimestamp, endTimestamp, ok := parseFlowQuotaTimeRange(c)
	if !ok {
		return
	}
	username := c.Query("username")
	dates, err := model.GetFlowQuotaData(startTimestamp, endTimestamp, username, 0, c.GetInt("role"))
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

func GetUserFlowQuotaDates(c *gin.Context) {
	userId := c.GetInt("id")
	startTimestamp, endTimestamp, ok := parseFlowQuotaTimeRange(c)
	if !ok {
		return
	}
	if endTimestamp-startTimestamp > 2592000 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "时间跨度不能超过 1 个月",
		})
		return
	}
	dates, err := model.GetFlowQuotaData(startTimestamp, endTimestamp, "", userId, common.RoleCommonUser)
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
