package relay

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

var alphaSearchHopByHopHeaders = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Proxy-Connection":    {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

// AlphaSearchHelper 将 Alpha Search 请求透明转发到所选上游，并在成功后结算工具调用费用。
// @param c 当前 Gin 请求上下文。
// @param info 当前 Relay 请求信息。
// @return 上游调用失败时返回统一 Relay 错误，成功时返回 nil。
func AlphaSearchHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	jsonData, newAPIError := prepareAlphaSearchRequestBody(c, info)
	if newAPIError != nil {
		return newAPIError
	}
	body, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	defer closer.Close()
	info.UpstreamRequestBodySize = body.Size()

	req, err := newAlphaSearchUpstreamRequest(c, info, adaptor, body, body.Size())
	if err != nil {
		return types.NewOpenAIError(errors.New("upstream error: build request failed"), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	httpResp, err := channel.DoRequestWithoutErrorLog(c, req, info)
	if err != nil {
		newAPIError := types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
		newAPIError.SetMessage("upstream error: do request failed")
		return newAPIError
	}
	defer service.CloseResponseBodyGracefully(httpResp)

	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		statusCode := httpResp.StatusCode
		newAPIError := service.RelayErrorHandlerWithoutBodyLog(c.Request.Context(), httpResp)
		message := fmt.Sprintf("bad response status code %d", statusCode)
		oaiError := newAPIError.ToOpenAIError()
		oaiError.Message = message
		newAPIError.RelayError = oaiError
		newAPIError.SetMessage(message)
		service.ResetStatusCode(newAPIError, c.GetString("status_code_mapping"))
		return newAPIError
	}

	responseBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	info.SetFirstResponseTime()
	service.IOCopyBytesGracefully(c, safeAlphaSearchResponse(c, httpResp), responseBody)
	service.PostToolCallConsumeQuota(c, info)
	return nil
}

func prepareAlphaSearchRequestBody(c *gin.Context, info *relaycommon.RelayInfo) ([]byte, *types.NewAPIError) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
	}
	rawBody, err := storage.Bytes()
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
	}
	jsonData, err := sjson.SetBytes(append([]byte(nil), rawBody...), "model", info.UpstreamModelName)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			return nil, newAPIErrorFromParamOverride(err)
		}
	}
	return jsonData, nil
}

func buildAlphaSearchUpstreamURL(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor) (string, error) {
	requestURL := "/v1/alpha/search"
	var fullRequestURL string
	if info.ApiType == appconstant.APITypeAdvancedCustom {
		resolvedURL, err := adaptor.GetRequestURL(info)
		if err != nil {
			return "", err
		}
		fullRequestURL = resolvedURL
	} else {
		if info.ApiType == appconstant.APITypeCodex {
			requestURL = "/backend-api/codex/alpha/search"
		}
		fullRequestURL = relaycommon.GetFullRequestURL(info.ChannelBaseUrl, requestURL, info.ChannelType)
	}

	parsedURL, err := url.Parse(fullRequestURL)
	if err != nil {
		return "", err
	}
	query := parsedURL.Query()
	for key, values := range c.Request.URL.Query() {
		if _, exists := query[key]; exists {
			// 渠道路由中的固定查询参数可能承载上游鉴权，不能被客户端同名值污染。
			continue
		}
		for _, value := range values {
			query.Add(key, value)
		}
	}
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func newAlphaSearchUpstreamRequest(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor, body io.Reader, bodySize int64) (*http.Request, error) {
	fullRequestURL, err := buildAlphaSearchUpstreamURL(c, info, adaptor)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, fullRequestURL, body)
	if err != nil {
		return nil, err
	}
	req.ContentLength = bodySize
	headers := req.Header
	if err := adaptor.SetupRequestHeader(c, &headers, info); err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	headerOverride, err := channel.ResolveHeaderOverride(info, c)
	if err != nil {
		return nil, err
	}
	for key, value := range headerOverride {
		req.Header.Set(key, value)
		if strings.EqualFold(key, "Host") {
			req.Host = value
		}
	}
	channel.ApplyUpstreamBodyMetadata(req, body)
	info.UpstreamRequestURLPath = req.URL.EscapedPath()
	return req, nil
}

func safeAlphaSearchResponse(c *gin.Context, response *http.Response) *http.Response {
	cloned := new(http.Response)
	*cloned = *response
	cloned.Header = make(http.Header)

	connectionHeaders := make(map[string]struct{})
	for _, value := range response.Header.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			name = http.CanonicalHeaderKey(strings.TrimSpace(name))
			if name != "" {
				connectionHeaders[name] = struct{}{}
			}
		}
	}
	for key, values := range response.Header {
		canonicalKey := http.CanonicalHeaderKey(key)
		if _, skip := alphaSearchHopByHopHeaders[canonicalKey]; skip {
			continue
		}
		if _, skip := connectionHeaders[canonicalKey]; skip {
			continue
		}
		if !service.ShouldCopyUpstreamHeader(c, key, values) {
			continue
		}
		for _, value := range values {
			cloned.Header.Add(key, value)
		}
	}
	return cloned
}

// buildAlphaSearchRequestBody 在模型发生映射时只改写 model，并保留所有未知字段。
func buildAlphaSearchRequestBody(rawBody []byte, originModel, upstreamModel string) ([]byte, error) {
	if len(rawBody) == 0 {
		return nil, errors.New("empty alpha search request body")
	}
	if upstreamModel == "" || upstreamModel == originModel {
		return rawBody, nil
	}
	var body map[string]any
	if err := common.Unmarshal(rawBody, &body); err != nil {
		return nil, err
	}
	body["model"] = upstreamModel
	return common.Marshal(body)
}
