package relay

import (
	"fmt"
	"github.com/MoeForge/moego/ojson"
	"io"
	"net/http"
	"reflect"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/requestplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
)

// PrepareResponsesRequest applies the same model, conversion and channel rules
// for HTTP and WebSocket requests. The caller closes closer after the attempt;
// passthrough bodies remain owned by the incoming request's BodyStorage.
// The returned adaptor retains route/conversion state for DoRequest/DoResponse.
func PrepareResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, req *dto.OpenAIResponsesRequest) (relaychannel.Adaptor, common.ReplayableBody, io.Closer, *types.NewAPIError) {
	return prepareResponsesRequest(c, info, req, nil)
}

var responsesFieldNames = dto.GetJSONFieldNames(reflect.TypeFor[dto.OpenAIResponsesRequest]())

func prepareResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, req *dto.OpenAIResponsesRequest, rawSocketBody []byte) (relaychannel.Adaptor, common.ReplayableBody, io.Closer, *types.NewAPIError) {
	info.InitChannelMeta(c)
	if info.RelayMode == relayconstant.RelayModeResponsesCompact &&
		!common.SupportsResponsesCompact(info.ChannelType, info.ApiType) {
		return nil, nil, nil, types.NewErrorWithStatusCode(
			fmt.Errorf("unsupported endpoint %q for api type %d", "/v1/responses/compact", info.ApiType),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	request, err := common.DeepCopy(req)
	if err != nil {
		return nil, nil, nil, types.NewError(fmt.Errorf("failed to copy responses request: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if err := helper.ModelMappedHelper(c, info, request); err != nil {
		return nil, nil, nil, types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	if err := helper.ApplyReasoningModelSuffix(c, info, request); err != nil {
		return nil, nil, nil, newConvertRequestFailedError(c, info, err)
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return nil, nil, nil, types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return nil, nil, nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
		}
		syncAdvancedChannelReasoningEffortFromRequest(info, req)
		if apiErr := requestplugin.PassThrough(c, info, storage); apiErr != nil {
			return nil, nil, nil, apiErr
		}
		body := common.NewReplayableBodyReader(storage)
		return adaptor, body, io.NopCloser(body), nil
	}

	convertedRequest, err := adaptor.ConvertOpenAIResponsesRequest(c, info, *request)
	if err != nil {
		return nil, nil, nil, newConvertRequestFailedError(c, info, err)
	}
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return nil, nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if len(rawSocketBody) != 0 {
		// 保留客户端的扩展字段。已知字段仍采用适配器结果，随后统一执行禁用规则与插件检查。
		var original map[string]common.RawMessage
		if err = common.Unmarshal(rawSocketBody, &original); err != nil {
			return nil, nil, nil, newConvertRequestFailedError(c, info, err)
		}
		doc := ojson.NewDoc(jsonData)
		for key, raw := range original {
			if _, known := responsesFieldNames[key]; known {
				continue
			}
			if err = doc.SetRawBytes(ojson.Escape(key), raw); err != nil {
				return nil, nil, nil, newConvertRequestFailedError(c, info, err)
			}
		}
		jsonData = doc.Bytes()
	}
	jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return nil, nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	jsonData, apiErr := requestplugin.Outbound(c, info, jsonData)
	if apiErr != nil {
		return nil, nil, nil, apiErr
	}
	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			return nil, nil, nil, newAPIErrorFromParamOverride(err)
		}
	}
	if finalizer, ok := adaptor.(relaychannel.ResponsesRequestFinalizer); ok {
		jsonData, err = finalizer.FinalizeResponsesRequest(info, jsonData)
		if err != nil {
			return nil, nil, nil, newConvertRequestFailedError(c, info, err)
		}
	}
	// egress reads the body as it is sent, after the channel finalized it.
	if apiErr = requestplugin.Egress(c, info, jsonData); apiErr != nil {
		return nil, nil, nil, apiErr
	}

	syncAdvancedChannelReasoningEffortFromBody(info, jsonData)
	logger.LogDebug(c, "requestBody: %s", jsonData)
	body, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
	if err != nil {
		return nil, nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	return adaptor, body, closer, nil
}
