package relay

import (
	"context"
	"fmt"
	"net/http"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/websearch"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func executeChannelWebSearch(c *gin.Context, info *relaycommon.RelayInfo, query string) (*websearch.SearchResponse, *types.NewAPIError) {
	settings := info.ChannelSetting.WebSearch
	settings.Normalize()
	if err := settings.ValidateForRelay(); err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	httpClient, err := service.NewProxyHttpClient(info.ChannelSetting.Proxy)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("WebSearch 代理配置错误: %w", err), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	provider, err := websearch.NewProvider(settings, httpClient)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
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
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("WebSearch provider %s 调用失败: %w", provider.Name(), err), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	if searchResp == nil {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("WebSearch provider %s 返回空响应", provider.Name()), types.ErrorCodeEmptyResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	return searchResp, nil
}
