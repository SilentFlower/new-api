package relay

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// 最终出站再校验一次，覆盖预处理与适配器之后的模型专属字段裁剪。
func checkChannelLimitFallbackOutbound(c *gin.Context, info *relaycommon.RelayInfo, body []byte) *types.NewAPIError {
	if info.LimitFallback == nil {
		return nil
	}
	storage, err := common.GetBodyStorage(c)
	var original []byte
	if err == nil {
		original, err = storage.Bytes()
	}
	if err != nil || !service.ChannelLimitFallbackPreservesRequest(original, body) {
		return types.NewOpenAIError(errors.New("fallback target cannot preserve this request"), types.ErrorCodeChannelLimitFallbackUnavailable, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	return nil
}
