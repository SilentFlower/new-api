package channel

import (
	"context"
	"net/http"
)

// TaskContextFetcher 表示支持请求上下文取消的任务查询适配器。
type TaskContextFetcher interface {
	// FetchTaskWithContext 使用指定上下文查询任务状态。
	// @param ctx 控制上游请求取消和超时的上下文。
	// @param baseUrl 上游基础地址。
	// @param key 上游鉴权信息。
	// @param body 查询参数。
	// @param proxy 可选代理地址。
	// @return 上游 HTTP 响应和请求错误。
	FetchTaskWithContext(ctx context.Context, baseUrl, key string, body map[string]any, proxy string) (*http.Response, error)
}

// FetchTaskWithContext 优先通过上下文感知接口查询任务，并兼容现有任务适配器。
// @param ctx 控制上游请求取消和超时的上下文。
// @param adaptor 任务适配器。
// @param baseUrl 上游基础地址。
// @param key 上游鉴权信息。
// @param body 查询参数。
// @param proxy 可选代理地址。
// @return 上游 HTTP 响应和请求错误。
func FetchTaskWithContext(ctx context.Context, adaptor TaskAdaptor, baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	if contextFetcher, ok := adaptor.(TaskContextFetcher); ok {
		return contextFetcher.FetchTaskWithContext(ctx, baseUrl, key, body, proxy)
	}
	return adaptor.FetchTask(baseUrl, key, body, proxy)
}
