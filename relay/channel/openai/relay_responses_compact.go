package openai

import (
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// OaiResponsesCompactionHandler 处理 unary Compact JSON，并按需桥接为 Responses SSE。
// @param c 当前 Gin 请求上下文。
// @param resp 上游 HTTP 响应。
// @param info 当前 Relay 请求信息；历史 body bridge 依赖其中的客户端流式标记。
// @return 归一化 usage 和响应处理错误。
func OaiResponsesCompactionHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var compactResp dto.OpenAIResponsesCompactionResponse
	if err := common.Unmarshal(responseBody, &compactResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := compactResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	if info != nil && info.ResponsesClientStream {
		if err := helper.WriteResponsesCompactSSECompleted(c, responseBody, compactResp.Output); err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
	} else {
		service.IOCopyBytesGracefully(c, resp, responseBody)
	}

	usage := dto.Usage{}
	if compactResp.Usage != nil {
		usage.PromptTokens = compactResp.Usage.InputTokens
		usage.CompletionTokens = compactResp.Usage.OutputTokens
		usage.TotalTokens = compactResp.Usage.TotalTokens
		if compactResp.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = compactResp.Usage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.CacheWriteTokens = compactResp.Usage.InputTokensDetails.CacheWriteTokens
		}
	}

	return &usage, nil
}
