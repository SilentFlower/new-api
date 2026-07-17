package relay

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/websearch"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func shouldHandleClaudeWebSearchEmulation(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil {
		return false
	}
	return info.ChannelSetting.WebSearch.Enabled
}

func handleClaudeWebSearchEmulation(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) *types.NewAPIError {
	settings := info.ChannelSetting.WebSearch
	settings.Normalize()
	if !settings.Enabled {
		return types.NewErrorWithStatusCode(fmt.Errorf("渠道未启用 Claude Code WebSearch"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if err := settings.ValidateForRelay(); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	query := websearch.ExtractClaudeWebSearchQuery(request)
	if query == "" {
		return types.NewErrorWithStatusCode(fmt.Errorf("无法从最后一条 user 消息中提取 WebSearch 查询"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	httpClient, err := service.NewProxyHttpClient(info.ChannelSetting.Proxy)
	if err != nil {
		return types.NewErrorWithStatusCode(fmt.Errorf("WebSearch 代理配置错误: %w", err), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	provider, err := websearch.NewProvider(settings, httpClient)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	searchCtx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	searchResp, err := provider.Search(searchCtx, websearch.SearchRequest{
		Query:        query,
		MaxResults:   settings.MaxResults,
		SearchDepth:  settings.SearchDepth,
		Freshness:    settings.Freshness,
		ContentTypes: settings.ContentTypes,
	})
	if err != nil {
		return types.NewErrorWithStatusCode(fmt.Errorf("WebSearch provider %s 调用失败: %w", provider.Name(), err), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	if searchResp == nil {
		return types.NewErrorWithStatusCode(fmt.Errorf("WebSearch provider %s 返回空响应", provider.Name()), types.ErrorCodeEmptyResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}

	modelName := strings.TrimSpace(info.UpstreamModelName)
	if modelName == "" {
		modelName = request.Model
	}
	inputTokens := info.GetEstimatePromptTokens()
	if inputTokens <= 0 {
		inputTokens = len([]rune(query))/4 + 1
	}
	outputTokens := websearch.EstimateOutputTokens(query, searchResp.Results)
	messageID := "msg_" + info.RequestId
	toolUseID := "srvtoolu_" + info.RequestId
	usage := websearch.BuildUsage(inputTokens, outputTokens)

	info.SetFirstResponseTime()
	c.Set("claude_web_search_requests", 1)
	if request.IsStream(c) {
		info.IsStream = true
		if err := writeClaudeWebSearchStream(c, messageID, toolUseID, modelName, query, searchResp.Results, inputTokens, outputTokens); err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
		}
	} else {
		response := websearch.BuildClaudeWebSearchResponse(messageID, toolUseID, modelName, query, searchResp.Results, inputTokens, outputTokens)
		jsonData, err := common.Marshal(response)
		if err != nil {
			return types.NewError(err, types.ErrorCodeJsonMarshalFailed, types.ErrOptionWithSkipRetry())
		}
		c.Data(http.StatusOK, "application/json", jsonData)
	}
	service.PostTextConsumeQuota(c, info, usage, nil)
	return nil
}

func writeClaudeWebSearchStream(c *gin.Context, messageID string, toolUseID string, modelName string, query string, results []websearch.SearchResult, inputTokens int, outputTokens int) error {
	helper.SetEventStreamHeaders(c)
	c.Status(http.StatusOK)
	for _, event := range websearch.BuildClaudeWebSearchStreamEvents(messageID, toolUseID, modelName, query, results, inputTokens, outputTokens) {
		if err := helper.ClaudeData(c, *event); err != nil {
			return err
		}
	}
	return nil
}
