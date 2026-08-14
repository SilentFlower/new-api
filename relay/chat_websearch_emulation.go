package relay

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/websearch"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func shouldHandleChatWebSearchEmulation(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil {
		return false
	}
	return info.ChannelSetting.WebSearch.Enabled
}

func handleChatWebSearchEmulation(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) *types.NewAPIError {
	if !info.ChannelSetting.WebSearch.Enabled {
		return types.NewErrorWithStatusCode(fmt.Errorf("渠道未启用 Chat Completions WebSearch"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	query := websearch.ExtractChatWebSearchQuery(request)
	if query == "" {
		return types.NewErrorWithStatusCode(fmt.Errorf("无法从最后一条 user 消息中提取 WebSearch 查询"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	searchResp, newAPIError := executeChannelWebSearch(c, info, query)
	if newAPIError != nil {
		return newAPIError
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
	usage := websearch.BuildChatUsage(inputTokens, outputTokens)
	responseID := helper.GetResponseID(c)
	created := common.GetTimestamp()

	info.SetFirstResponseTime()
	common.SetContextKey(c, constant.ContextKeyChatWebSearchLocalEmulation, true)
	c.Set("claude_web_search_requests", 1)
	if request.IsStream(c.Request) {
		info.IsStream = true
		if err := writeChatWebSearchStream(c, responseID, created, modelName, query, searchResp.Results, usage, info.ShouldIncludeUsage); err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
		}
	} else {
		response := websearch.BuildChatWebSearchResponse(responseID, created, modelName, query, searchResp.Results, inputTokens, outputTokens)
		jsonData, err := common.Marshal(response)
		if err != nil {
			return types.NewError(err, types.ErrorCodeJsonMarshalFailed, types.ErrOptionWithSkipRetry())
		}
		c.Data(http.StatusOK, "application/json", jsonData)
	}
	service.PostTextConsumeQuota(c, info, usage, nil)
	return nil
}

func writeChatWebSearchStream(c *gin.Context, responseID string, created int64, modelName string, query string, results []websearch.SearchResult, usage *dto.Usage, includeUsage bool) error {
	helper.SetEventStreamHeaders(c)
	c.Status(http.StatusOK)
	if err := helper.ObjectData(c, helper.GenerateStartEmptyResponse(responseID, created, modelName, nil)); err != nil {
		return err
	}
	content := websearch.BuildTextSummary(query, results)
	if err := helper.ObjectData(c, &dto.ChatCompletionsStreamResponse{
		Id:      responseID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   modelName,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index: 0,
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Content: common.GetPointer(content),
				},
			},
		},
	}); err != nil {
		return err
	}
	if err := helper.ObjectData(c, helper.GenerateStopResponse(responseID, created, modelName, "stop")); err != nil {
		return err
	}
	if includeUsage && usage != nil {
		if err := helper.ObjectData(c, helper.GenerateFinalUsageResponse(responseID, created, modelName, *usage)); err != nil {
			return err
		}
	}
	helper.Done(c)
	return nil
}
