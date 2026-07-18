package helper

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// GetAndValidateAlphaSearchRequest 解析并校验 Alpha Search 调度和计费边界字段。
// @param c 当前 Gin 请求上下文。
// @return 最小请求对象；请求非法时返回错误。
func GetAndValidateAlphaSearchRequest(c *gin.Context) (*dto.AlphaSearchRequest, error) {
	request := &dto.AlphaSearchRequest{}
	if err := common.UnmarshalBodyReusable(c, request); err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.Model) == "" {
		return nil, errors.New("model is required")
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	modelFieldCount := 0
	gjson.ParseBytes(body).ForEach(func(key, value gjson.Result) bool {
		if key.String() == "model" {
			modelFieldCount++
		}
		return true
	})
	if modelFieldCount != 1 {
		return nil, errors.New("model must be specified exactly once")
	}
	if exceedsMaxTokensLimit(request.MaxOutputTokens) {
		return nil, errors.New("max_output_tokens is invalid")
	}
	return request, nil
}
