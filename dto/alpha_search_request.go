package dto

import (
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// AlphaSearchRequest 描述 Alpha Search 入口校验所需的最小请求字段。
// 实际上游请求始终基于原始请求体构造，避免丢失协议后续新增的字段。
type AlphaSearchRequest struct {
	ID              *string `json:"id,omitempty"`
	Model           string  `json:"model"`
	MaxOutputTokens *uint   `json:"max_output_tokens,omitempty"`
}

// GetTokenCountMeta 返回空的 Token 计数信息；Alpha Search 只按工具调用次数计费。
// @return 不包含文本或最大 Token 数的计数元数据。
func (r *AlphaSearchRequest) GetTokenCountMeta() *types.TokenCountMeta {
	return &types.TokenCountMeta{}
}

// IsStream 表示 Alpha Search 是非流式 JSON 请求。
// @param c 当前 Gin 请求上下文。
// @return 始终返回 false。
func (r *AlphaSearchRequest) IsStream(c *gin.Context) bool {
	return false
}

// SetModelName 更新待转发的模型名。
// @param modelName 模型映射后的上游模型名。
func (r *AlphaSearchRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}
