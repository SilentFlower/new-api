package controller

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const claudeTokenCountingBeta = "token-counting-2024-11-01"

var claudeCountTokensDoRequest = channel.DoRequest

var claudeCountTokensGenerationFields = []string{
	"max_tokens",
	"max_tokens_to_sample",
	"stream",
	"temperature",
	"top_p",
	"top_k",
	"stop",
	"stop_sequences",
}

var claudeCountTokensHopByHopHeaders = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"content-length":      {},
}

// RelayClaudeCountTokens 处理 Claude token counting 透传请求。
// 参数 c 为当前 Gin 请求上下文，包含认证、渠道选择和原始请求信息。
func RelayClaudeCountTokens(c *gin.Context) {
	newAPIError := relayClaudeCountTokens(c)
	if newAPIError == nil {
		return
	}
	newAPIError.SetMessage(common.MessageWithRequestId(newAPIError.Error(), c.GetString(common.RequestIdKey)))
	c.JSON(newAPIError.StatusCode, gin.H{
		"type":  "error",
		"error": newAPIError.ToClaudeError(),
	})
}

func relayClaudeCountTokens(c *gin.Context) *types.NewAPIError {
	body, err := readAndValidateClaudeCountTokensBody(c)
	if err != nil {
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		}
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	info, err := buildClaudeCountTokensRelayInfo(c, body)
	if err != nil {
		return types.NewError(err, types.ErrorCodeGenRelayInfoFailed, types.ErrOptionWithSkipRetry())
	}

	requestBytes, err := buildClaudeCountTokensRequestBody(body, info)
	if err != nil {
		if fixedErr, ok := relaycommon.AsParamOverrideReturnError(err); ok {
			return relaycommon.NewAPIErrorFromParamOverride(fixedErr)
		}
		if errors.Is(err, errClaudeCountTokensParamOverride) {
			return types.NewError(err, types.ErrorCodeChannelParamOverrideInvalid, types.ErrOptionWithSkipRetry())
		}
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	requestURL, err := buildClaudeCountTokensURL(info)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, requestURL, bytes.NewReader(requestBytes))
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if err = buildClaudeCountTokensHeaders(c, req, info); err != nil {
		return types.NewError(err, types.ErrorCodeChannelHeaderOverrideInvalid, types.ErrOptionWithSkipRetry())
	}
	concurrencyGuard, apiErr := acquireChannelUserConcurrencyGuard(c)
	if apiErr != nil {
		return apiErr
	}
	req = req.WithContext(c.Request.Context())
	defer func() {
		_ = finishChannelUserConcurrencyGuard(c, concurrencyGuard, nil)
	}()

	resp, err := claudeCountTokensDoRequest(c, req, info)
	if err != nil {
		apiErr = finishChannelUserConcurrencyGuard(c, concurrencyGuard, types.NewError(err, types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry()))
		concurrencyGuard = nil
		return apiErr
	}
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		apiErr = finishChannelUserConcurrencyGuard(c, concurrencyGuard, types.NewError(err, types.ErrorCodeReadResponseBodyFailed, types.ErrOptionWithSkipRetry()))
		concurrencyGuard = nil
		return apiErr
	}
	apiErr = finishChannelUserConcurrencyGuard(c, concurrencyGuard, nil)
	concurrencyGuard = nil
	if apiErr != nil {
		return apiErr
	}

	copyClaudeCountTokensResponseHeaders(c, resp.Header)
	contentType := resp.Header.Get("Content-Type")
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/json"
	}
	c.Data(resp.StatusCode, contentType, responseBody)
	return nil
}

var errClaudeCountTokensParamOverride = errors.New("Claude count_tokens 参数覆盖失败")

func readAndValidateClaudeCountTokensBody(c *gin.Context) (map[string]any, error) {
	var body map[string]any
	if err := common.UnmarshalBodyReusable(c, &body); err != nil {
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			return nil, fmt.Errorf("请求体过大: %w", err)
		}
		return nil, fmt.Errorf("无效的 Claude count_tokens 请求体: %w", err)
	}
	if body == nil {
		return nil, errors.New("Claude count_tokens 请求体必须是 JSON 对象")
	}
	model, ok := body["model"].(string)
	if !ok || strings.TrimSpace(model) == "" {
		return nil, errors.New("Claude count_tokens 请求缺少 model")
	}
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) == 0 {
		return nil, errors.New("Claude count_tokens 请求缺少 messages")
	}
	return body, nil
}

func buildClaudeCountTokensRelayInfo(c *gin.Context, body map[string]any) (*relaycommon.RelayInfo, error) {
	modelName := strings.TrimSpace(body["model"].(string))
	if common.GetContextKeyString(c, constant.ContextKeyOriginalModel) == "" {
		common.SetContextKey(c, constant.ContextKeyOriginalModel, modelName)
	}
	info := relaycommon.GenRelayInfoClaude(c, nil)
	info.InitRequestConversionChain()
	if info.OriginModelName == "" {
		info.OriginModelName = modelName
	}
	info.InitChannelMeta(c)
	if info.ChannelMeta == nil {
		return nil, errors.New("Claude count_tokens 渠道信息为空")
	}
	if info.UpstreamModelName == "" {
		info.UpstreamModelName = info.OriginModelName
	}
	if err := helper.ModelMappedHelper(c, info, nil); err != nil {
		return nil, err
	}
	if info.UpstreamModelName != "" {
		body["model"] = info.UpstreamModelName
	}
	return info, nil
}

func buildClaudeCountTokensRequestBody(body map[string]any, info *relaycommon.RelayInfo) ([]byte, error) {
	removeClaudeCountTokensGenerationFields(body)

	requestBytes, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	if info != nil && len(info.ParamOverride) > 0 {
		requestBytes, err = relaycommon.ApplyParamOverrideWithRelayInfo(requestBytes, info)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errClaudeCountTokensParamOverride, err)
		}
		var overridden map[string]any
		if err = common.Unmarshal(requestBytes, &overridden); err != nil {
			return nil, err
		}
		removeClaudeCountTokensGenerationFields(overridden)
		requestBytes, err = common.Marshal(overridden)
		if err != nil {
			return nil, err
		}
	}
	return requestBytes, nil
}

func removeClaudeCountTokensGenerationFields(body map[string]any) {
	for _, field := range claudeCountTokensGenerationFields {
		delete(body, field)
	}
}

func buildClaudeCountTokensURL(info *relaycommon.RelayInfo) (string, error) {
	if info == nil || strings.TrimSpace(info.ChannelBaseUrl) == "" {
		return "", errors.New("Claude count_tokens 缺少上游地址")
	}
	requestURL := strings.TrimRight(info.ChannelBaseUrl, "/") + "/v1/messages/count_tokens"
	if !shouldAppendClaudeCountTokensBetaQuery(info) {
		return requestURL, nil
	}
	parsedURL, err := url.Parse(requestURL)
	if err != nil {
		return "", err
	}
	query := parsedURL.Query()
	query.Set("beta", "true")
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func shouldAppendClaudeCountTokensBetaQuery(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	return info.IsClaudeBetaQuery || info.ChannelOtherSettings.ClaudeBetaQuery
}

func buildClaudeCountTokensHeaders(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	if req == nil {
		return errors.New("Claude count_tokens 上游请求为空")
	}
	if info == nil {
		return errors.New("Claude count_tokens relay info 为空")
	}

	header := req.Header
	header.Set("Content-Type", "application/json")
	if accept := strings.TrimSpace(c.Request.Header.Get("Accept")); accept != "" {
		header.Set("Accept", accept)
	}
	if info.ApiKey != "" {
		header.Set("x-api-key", info.ApiKey)
	}
	anthropicVersion := strings.TrimSpace(c.Request.Header.Get("anthropic-version"))
	if anthropicVersion == "" {
		anthropicVersion = "2023-06-01"
	}
	header.Set("anthropic-version", anthropicVersion)
	header.Set("anthropic-beta", mergeAnthropicBetaHeader(c.Request.Header.Get("anthropic-beta"), claudeTokenCountingBeta))

	headerOverride, err := channel.ResolveHeaderOverride(info, c)
	if err != nil {
		return err
	}
	for key, value := range headerOverride {
		header.Set(key, value)
		if strings.EqualFold(key, "Host") {
			req.Host = value
		}
	}
	header.Set("anthropic-beta", mergeAnthropicBetaHeader(c.Request.Header.Get("anthropic-beta"), header.Get("anthropic-beta"), claudeTokenCountingBeta))
	return nil
}

func mergeAnthropicBetaHeader(headerValues ...string) string {
	seen := make(map[string]struct{})
	tokens := make([]string, 0)
	addToken := func(token string) {
		token = strings.TrimSpace(token)
		if token == "" {
			return
		}
		if _, ok := seen[token]; ok {
			return
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}
	for _, headerValue := range headerValues {
		for _, token := range strings.Split(headerValue, ",") {
			addToken(token)
		}
	}
	return strings.Join(tokens, ",")
}

func copyClaudeCountTokensResponseHeaders(c *gin.Context, headers http.Header) {
	for key, values := range headers {
		if _, skip := claudeCountTokensHopByHopHeaders[strings.ToLower(key)]; skip {
			continue
		}
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
}
