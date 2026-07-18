package middleware

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"

	"github.com/gin-gonic/gin"
)

// detectAndStoreResponsesCompactMode 只负责 Distributor 层的 Compact mode 检测边界。
// build 分支 Compact 协议细节集中在独立文件，distributor.go 仅保留调用点以降低上游同步冲突。
func detectAndStoreResponsesCompactMode(c *gin.Context) error {
	if !strings.HasPrefix(c.Request.URL.Path, "/v1/responses") {
		return nil
	}

	var requestBody []byte
	if !strings.HasPrefix(c.Request.URL.Path, "/v1/responses/compact") {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return err
		}
		requestBody, err = storage.Bytes()
		if err != nil {
			return err
		}
	}
	compactMode := relayhelper.DetectResponsesCompactMode(
		c.Request.Method,
		c.Request.URL.Path,
		c.Request.Header,
		requestBody,
		relayhelper.ResponsesTransportHTTP,
	)
	common.SetContextKey(c, constant.ContextKeyResponsesCompactMode, compactMode)
	return nil
}
