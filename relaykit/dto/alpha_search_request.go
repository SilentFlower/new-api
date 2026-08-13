package dto

import (
	"encoding/json"
	"net/http"

	"github.com/QuantumNous/new-api/relaykit/types"
)

// AlphaSearchRequest is the Codex standalone web search request.
// RawBody preserves the original JSON so unknown fields are forwarded intact.
type AlphaSearchRequest struct {
	ID              *string         `json:"id,omitempty"`
	Model           string          `json:"model"`
	Stream          *bool           `json:"stream,omitempty"`
	MaxOutputTokens *uint           `json:"max_output_tokens,omitempty"`
	RawBody         json.RawMessage `json:"-"`
}

func (r *AlphaSearchRequest) GetTokenCountMeta() *types.TokenCountMeta {
	combineText := ""
	if len(r.RawBody) > 0 {
		combineText = string(r.RawBody)
	}
	return &types.TokenCountMeta{
		CombineText: combineText,
		TokenType:   types.TokenTypeTokenizer,
	}
}

func (r *AlphaSearchRequest) IsStream(_ *http.Request) bool {
	return false
}

func (r *AlphaSearchRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}
