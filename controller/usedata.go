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

// 导出 Excel 青绿专业色板（与 PRD 方案 2 一致）。
const (
	exportColorHeader   = "0F766E"
	exportColorHeaderFG = "FFFFFF"
	exportColorGroup    = "E6F4F1"
	exportColorSubtotal = "F0FDFA"
	exportColorTotal    = "ECFDF5"
	exportColorBorder   = "CCE3DE"
	exportColorMeta     = "64748B"
	exportColorTitle    = "0F766E"
	exportColorText     = "111827"
)

// 每个 Sheet 的固定版式：R1 标题 / R2 元信息 / R3 空白 / R4 起业务内容。
const exportContentHeaderRow = 4

// exportExcelStyles 导出工作簿复用的样式 ID 集合。
type exportExcelStyles struct {
	title          int
	meta           int
	header         int
	text           int
	number         int
	money          int
	duration       int
	center         int
	groupTitle     int
	subtotalText   int
	subtotalNumber int
	subtotalMoney  int
	totalText      int
	totalNumber    int
	totalMoney     int
}

// ExportQuotaDataExcel 导出数据看板 Excel 报表（管理员接口）
// 生成包含三个 Sheet 的 Excel 文件：
// - Sheet 1：按分组 + API Key 汇总统计（含筛选感知合计）
// - Sheet 2：按分组 + API Key + 模型明细分段表
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

	styles, err := newExportExcelStyles(f)
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

	if err := setStreamColumnWidths(sheet1Writer, []float64{14, 18, 12, 16, 16}); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := setStreamColumnWidths(sheet2Writer, []float64{24, 12, 16, 16}); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := setStreamColumnWidths(sheet3Writer, []float64{20, 12, 14, 18, 28, 12, 14, 10, 10, 10, 22}); err != nil {
		common.ApiError(c, err)
		return
	}

	// 冻结到数据表头行，滚动时保留标题/元信息/表头。
	if err := setExportFreezePanes(sheet1Writer, exportContentHeaderRow); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := setExportFreezePanes(sheet2Writer, 3); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := setExportFreezePanes(sheet3Writer, exportContentHeaderRow); err != nil {
		common.ApiError(c, err)
		return
	}

	metaText := formatExportMetaSummary(startTimestamp, endTimestamp, groups, tokenNames)
	if err := writeExportSheetPreamble(sheet1Writer, 5, "数据看板导出 · 汇总统计", metaText, styles); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := writeExportSheetPreamble(sheet2Writer, 4, "数据看板导出 · 模型明细", metaText, styles); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := writeExportSheetPreamble(sheet3Writer, 11, "数据看板导出 · 请求日志", metaText, styles); err != nil {
		common.ApiError(c, err)
		return
	}

	sheet3Headers := []interface{}{"时间", "分组", "API Key", "模型", "输入 Tokens", "输出 Tokens", "额度消耗 (USD)", "耗时(s)", "是否流式", "渠道 ID", "请求 ID"}
	if err := writeExportHeaderRow(sheet3Writer, exportContentHeaderRow, sheet3Headers, styles.header); err != nil {
		common.ApiError(c, err)
		return
	}

	sheet3Row := exportContentHeaderRow + 1
	summaryData, detailData, err := model.ProcessLogsForExport(c.Request.Context(), startTimestamp, endTimestamp, "", tokenNames, groups, func(logItem *model.Log, cacheRead int, cacheWrite int) error {
		isStreamStr := "否"
		if logItem.IsStream {
			isStreamStr = "是"
		}
		inputTokenCell := styledCell(styles.number, logItem.PromptTokens)
		if cacheRead > 0 || cacheWrite > 0 {
			inputTokenCell = styledCell(styles.text, formatExportInputTokens(logItem.PromptTokens, cacheRead, cacheWrite))
		}
		values := []interface{}{
			styledCell(styles.text, time.Unix(logItem.CreatedAt, 0).Format("2006-01-02 15:04:05")),
			styledCell(styles.text, logItem.Group),
			styledCell(styles.text, logItem.TokenName),
			styledCell(styles.text, logItem.ModelName),
			inputTokenCell,
			styledCell(styles.number, logItem.CompletionTokens),
			styledCell(styles.money, formatQuotaValue(logItem.Quota)),
			styledCell(styles.duration, logItem.UseTime),
			styledCell(styles.center, isStreamStr),
			styledCell(styles.number, logItem.ChannelId),
			styledCell(styles.text, logItem.RequestId),
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
	if sheet3Row > exportContentHeaderRow+1 {
		if err := addExportDataTable(sheet3Writer, "ExportLogs", 1, 11, exportContentHeaderRow, sheet3Row-1); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if err := sheet3Writer.Flush(); err != nil {
		common.ApiError(c, err)
		return
	}

	sheet1Headers := []interface{}{"分组", "API Key 名称", "请求次数", "请求 Token 数", "请求额度 (USD)"}
	if err := writeExportHeaderRow(sheet1Writer, exportContentHeaderRow, sheet1Headers, styles.header); err != nil {
		common.ApiError(c, err)
		return
	}
	sheet1DataStart := exportContentHeaderRow + 1
	sheet1Row := sheet1DataStart
	for _, item := range summaryData {
		values := []interface{}{
			styledCell(styles.text, item.Group),
			styledCell(styles.text, item.TokenName),
			styledCell(styles.number, item.Count),
			styledCell(styles.number, item.TokenUsed),
			styledCell(styles.money, formatQuotaValue(item.Quota)),
		}
		if err := sheet1Writer.SetRow(cellName(1, sheet1Row), values); err != nil {
			common.ApiError(c, err)
			return
		}
		sheet1Row++
	}
	sheet1DataEnd := sheet1Row - 1
	if len(summaryData) > 0 {
		if err := addExportDataTable(sheet1Writer, "ExportSummary", 1, 5, exportContentHeaderRow, sheet1DataEnd); err != nil {
			common.ApiError(c, err)
			return
		}
		// 合计行放在筛选范围外，使用 SUBTOTAL 随 AutoFilter 可见行变化。
		totalValues := []interface{}{
			styledCell(styles.totalText, "合计"),
			styledCell(styles.totalText, ""),
			excelize.Cell{StyleID: styles.totalNumber, Formula: fmt.Sprintf("SUBTOTAL(109,C%d:C%d)", sheet1DataStart, sheet1DataEnd)},
			excelize.Cell{StyleID: styles.totalNumber, Formula: fmt.Sprintf("SUBTOTAL(109,D%d:D%d)", sheet1DataStart, sheet1DataEnd)},
			excelize.Cell{StyleID: styles.totalMoney, Formula: fmt.Sprintf("SUBTOTAL(109,E%d:E%d)", sheet1DataStart, sheet1DataEnd)},
		}
		if err := sheet1Writer.SetRow(cellName(1, sheet1Row), totalValues); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if err := sheet1Writer.Flush(); err != nil {
		common.ApiError(c, err)
		return
	}

	sheet2Headers := []interface{}{"模型名称", "请求次数", "请求 Token 数", "请求额度 (USD)"}
	sheet2Row := exportContentHeaderRow
	currentGroup := ""
	currentTokenName := ""
	hasCurrentGroup := false
	var totalCount, totalTokenUsed, totalQuota int
	for index, item := range detailData {
		groupChanged := !hasCurrentGroup || item.Group != currentGroup || item.TokenName != currentTokenName
		if groupChanged {
			if hasCurrentGroup {
				if err := writeExportSheet2Subtotal(sheet2Writer, sheet2Row, totalCount, totalTokenUsed, totalQuota, styles); err != nil {
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
			if err := sheet2Writer.MergeCell(cellName(1, sheet2Row), cellName(4, sheet2Row)); err != nil {
				common.ApiError(c, err)
				return
			}
			if err := sheet2Writer.SetRow(cellName(1, sheet2Row), []interface{}{
				styledCell(styles.groupTitle, title),
				styledCell(styles.groupTitle, ""),
				styledCell(styles.groupTitle, ""),
				styledCell(styles.groupTitle, ""),
			}); err != nil {
				common.ApiError(c, err)
				return
			}
			sheet2Row++
			if err := writeExportHeaderRow(sheet2Writer, sheet2Row, sheet2Headers, styles.header); err != nil {
				common.ApiError(c, err)
				return
			}
			sheet2Row++
		}

		values := []interface{}{
			styledCell(styles.text, item.ModelName),
			styledCell(styles.number, item.Count),
			styledCell(styles.number, item.TokenUsed),
			styledCell(styles.money, formatQuotaValue(item.Quota)),
		}
		if err := sheet2Writer.SetRow(cellName(1, sheet2Row), values); err != nil {
			common.ApiError(c, err)
			return
		}
		sheet2Row++
		totalCount += item.Count
		totalTokenUsed += item.TokenUsed
		totalQuota += item.Quota

		if index == len(detailData)-1 {
			if err := writeExportSheet2Subtotal(sheet2Writer, sheet2Row, totalCount, totalTokenUsed, totalQuota, styles); err != nil {
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

// setExportFreezePanes 冻结到指定表头行，使滚动时保留上方内容。
func setExportFreezePanes(writer *excelize.StreamWriter, headerRow int) error {
	return writer.SetPanes(&excelize.Panes{
		Freeze:      true,
		Split:       false,
		XSplit:      0,
		YSplit:      headerRow,
		TopLeftCell: cellName(1, headerRow+1),
		ActivePane:  "bottomLeft",
	})
}

// writeExportSheetPreamble 写入标题、元信息与空白分隔行。
func writeExportSheetPreamble(writer *excelize.StreamWriter, colCount int, title string, metaText string, styles exportExcelStyles) error {
	if colCount < 1 {
		colCount = 1
	}
	if err := writer.MergeCell(cellName(1, 1), cellName(colCount, 1)); err != nil {
		return err
	}
	titleRow := make([]interface{}, colCount)
	titleRow[0] = styledCell(styles.title, title)
	for i := 1; i < colCount; i++ {
		titleRow[i] = styledCell(styles.title, "")
	}
	if err := writer.SetRow(cellName(1, 1), titleRow); err != nil {
		return err
	}

	if err := writer.MergeCell(cellName(1, 2), cellName(colCount, 2)); err != nil {
		return err
	}
	metaRow := make([]interface{}, colCount)
	metaRow[0] = styledCell(styles.meta, metaText)
	for i := 1; i < colCount; i++ {
		metaRow[i] = styledCell(styles.meta, "")
	}
	if err := writer.SetRow(cellName(1, 2), metaRow); err != nil {
		return err
	}

	// 第 3 行空白，业务表头从第 4 行开始。
	return writer.SetRow(cellName(1, 3), []interface{}{""})
}

// writeExportHeaderRow 写入统一样式的数据表头行。
func writeExportHeaderRow(writer *excelize.StreamWriter, row int, headers []interface{}, styleID int) error {
	values := make([]interface{}, len(headers))
	for i, header := range headers {
		values[i] = styledCell(styleID, header)
	}
	return writer.SetRow(cellName(1, row), values)
}

// writeExportSheet2Subtotal 写入模型明细的静态小计行。
func writeExportSheet2Subtotal(writer *excelize.StreamWriter, row int, count int, tokenUsed int, quota int, styles exportExcelStyles) error {
	values := []interface{}{
		styledCell(styles.subtotalText, "小计"),
		styledCell(styles.subtotalNumber, count),
		styledCell(styles.subtotalNumber, tokenUsed),
		styledCell(styles.subtotalMoney, formatQuotaValue(quota)),
	}
	return writer.SetRow(cellName(1, row), values)
}

// addExportDataTable 为连续数据区添加带筛选的 Table（范围含表头，不含合计行）。
func addExportDataTable(writer *excelize.StreamWriter, name string, startCol int, endCol int, headerRow int, lastDataRow int) error {
	if lastDataRow < headerRow {
		return nil
	}
	ref := fmt.Sprintf("%s:%s", cellName(startCol, headerRow), cellName(endCol, lastDataRow))
	disableStripes := false
	return writer.AddTable(&excelize.Table{
		Range:          ref,
		Name:           name,
		StyleName:      "TableStyleMedium2",
		ShowRowStripes: &disableStripes,
	})
}

// formatExportMetaSummary 生成导出元信息文案。
func formatExportMetaSummary(startTimestamp int64, endTimestamp int64, groups []string, tokenNames []string) string {
	timeRange := fmt.Sprintf("%s ~ %s",
		time.Unix(startTimestamp, 0).Format("2006-01-02 15:04:05"),
		time.Unix(endTimestamp, 0).Format("2006-01-02 15:04:05"),
	)
	return fmt.Sprintf("时间范围：%s | 分组：%s | API Key：%s",
		timeRange,
		formatExportFilterSummary(groups),
		formatExportFilterSummary(tokenNames),
	)
}

// formatExportFilterSummary 将筛选列表格式化为摘要；空列表表示全部。
func formatExportFilterSummary(values []string) string {
	if len(values) == 0 {
		return "全部"
	}
	return strings.Join(values, ",")
}

// styledCell 构造带样式的流式单元格。
func styledCell(styleID int, value interface{}) excelize.Cell {
	return excelize.Cell{StyleID: styleID, Value: value}
}

// newExportExcelStyles 预创建导出所需样式。
func newExportExcelStyles(f *excelize.File) (exportExcelStyles, error) {
	var styles exportExcelStyles
	var err error

	thinBorder := []excelize.Border{
		{Type: "left", Color: exportColorBorder, Style: 1},
		{Type: "right", Color: exportColorBorder, Style: 1},
		{Type: "top", Color: exportColorBorder, Style: 1},
		{Type: "bottom", Color: exportColorBorder, Style: 1},
	}
	numberFmt := "#,##0"
	moneyFmt := "$#,##0.00"
	durationFmt := "0.00"

	if styles.title, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 14, Color: exportColorTitle},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
	}); err != nil {
		return styles, err
	}
	if styles.meta, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 9, Color: exportColorMeta},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
	}); err != nil {
		return styles, err
	}
	if styles.header, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: exportColorHeaderFG},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{exportColorHeader}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    thinBorder,
	}); err != nil {
		return styles, err
	}
	if styles.text, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Color: exportColorText},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
		Border:    thinBorder,
	}); err != nil {
		return styles, err
	}
	if styles.number, err = f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Color: exportColorText},
		Alignment:    &excelize.Alignment{Horizontal: "right", Vertical: "center"},
		Border:       thinBorder,
		CustomNumFmt: &numberFmt,
	}); err != nil {
		return styles, err
	}
	if styles.money, err = f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Color: exportColorText},
		Alignment:    &excelize.Alignment{Horizontal: "right", Vertical: "center"},
		Border:       thinBorder,
		CustomNumFmt: &moneyFmt,
	}); err != nil {
		return styles, err
	}
	if styles.duration, err = f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Color: exportColorText},
		Alignment:    &excelize.Alignment{Horizontal: "right", Vertical: "center"},
		Border:       thinBorder,
		CustomNumFmt: &durationFmt,
	}); err != nil {
		return styles, err
	}
	if styles.center, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Color: exportColorText},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:    thinBorder,
	}); err != nil {
		return styles, err
	}
	if styles.groupTitle, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: exportColorText},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{exportColorGroup}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
		Border:    thinBorder,
	}); err != nil {
		return styles, err
	}
	if styles.subtotalText, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: exportColorText},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{exportColorSubtotal}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
		Border:    thinBorder,
	}); err != nil {
		return styles, err
	}
	if styles.subtotalNumber, err = f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Bold: true, Color: exportColorText},
		Fill:         excelize.Fill{Type: "pattern", Color: []string{exportColorSubtotal}, Pattern: 1},
		Alignment:    &excelize.Alignment{Horizontal: "right", Vertical: "center"},
		Border:       thinBorder,
		CustomNumFmt: &numberFmt,
	}); err != nil {
		return styles, err
	}
	if styles.subtotalMoney, err = f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Bold: true, Color: exportColorText},
		Fill:         excelize.Fill{Type: "pattern", Color: []string{exportColorSubtotal}, Pattern: 1},
		Alignment:    &excelize.Alignment{Horizontal: "right", Vertical: "center"},
		Border:       thinBorder,
		CustomNumFmt: &moneyFmt,
	}); err != nil {
		return styles, err
	}
	if styles.totalText, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: exportColorText},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{exportColorTotal}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
		Border:    thinBorder,
	}); err != nil {
		return styles, err
	}
	if styles.totalNumber, err = f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Bold: true, Color: exportColorText},
		Fill:         excelize.Fill{Type: "pattern", Color: []string{exportColorTotal}, Pattern: 1},
		Alignment:    &excelize.Alignment{Horizontal: "right", Vertical: "center"},
		Border:       thinBorder,
		CustomNumFmt: &numberFmt,
	}); err != nil {
		return styles, err
	}
	if styles.totalMoney, err = f.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Bold: true, Color: exportColorText},
		Fill:         excelize.Fill{Type: "pattern", Color: []string{exportColorTotal}, Pattern: 1},
		Alignment:    &excelize.Alignment{Horizontal: "right", Vertical: "center"},
		Border:       thinBorder,
		CustomNumFmt: &moneyFmt,
	}); err != nil {
		return styles, err
	}
	return styles, nil
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
