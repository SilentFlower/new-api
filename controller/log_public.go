package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetLogByKeyPaged 按 API Key 分页查询公共日志，并隐藏管理员敏感字段。
// @param c Gin 请求上下文
// @return 无返回值，响应直接写入上下文
func GetLogByKeyPaged(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	if tokenId == 0 {
		common.ApiErrorMsg(c, "无效的令牌")
		return
	}

	pageInfo := common.GetPageQuery(c)
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	modelName := c.Query("model_name")
	requestId := c.Query("request_id")

	logs, total, err := model.GetLogsByTokenId(tokenId, logType, startTimestamp, endTimestamp, modelName, requestId, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
}

func parseTokenLogFilterParams(c *gin.Context, tokenId int) model.TokenLogFilterParams {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	return model.TokenLogFilterParams{
		TokenID:        tokenId,
		LogType:        logType,
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		ModelName:      c.Query("model_name"),
		RequestID:      c.Query("request_id"),
	}
}

// GetLogStatByKey 按 API Key 和公共筛选条件查询统计卡片数据。
// @param c Gin 请求上下文
// @return 无返回值，响应直接写入上下文
func GetLogStatByKey(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	if tokenId == 0 {
		common.ApiErrorMsg(c, "无效的令牌")
		return
	}

	stat, err := model.GetTokenLogStatWithFilters(parseTokenLogFilterParams(c, tokenId))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, stat)
}

// GetLogChartDataByKey 按 API Key 和公共筛选条件查询模型分布及消耗趋势。
// @param c Gin 请求上下文
// @return 无返回值，响应直接写入上下文
func GetLogChartDataByKey(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	if tokenId == 0 {
		common.ApiErrorMsg(c, "无效的令牌")
		return
	}

	params := parseTokenLogFilterParams(c, tokenId)

	// 时间跨度限制 1 个月
	if params.EndTimestamp-params.StartTimestamp > 2592000 {
		common.ApiErrorMsg(c, "时间跨度不能超过 1 个月")
		return
	}

	// 查询模型调用分布数据（饼图）
	modelStats, err := model.GetTokenModelStatsWithFilters(params)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 查询消耗趋势数据（折线图）
	quotaData, err := model.GetTokenQuotaDataWithFilters(params)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"model_stats": modelStats,
		"quota_data":  quotaData,
	})
}
