package claude

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/tidwall/gjson"
)

const flower2APILocalResponseHeader = "x-flower2api-local-response"

func takeFlower2APILocalSummaryReceipt(resp *http.Response, info *relaycommon.RelayInfo) bool {
	if resp == nil {
		return false
	}
	values := resp.Header.Values(flower2APILocalResponseHeader)
	// 回执只在这段上游连接上具有结算语义，避免经过客户端或下一层中继后被重新采用。
	resp.Header.Del(flower2APILocalResponseHeader)
	return resp.StatusCode == http.StatusOK && info.ChannelMeta != nil &&
		info.ChannelSetting.TrustFlower2APILocalResponse && len(values) == 1 && values[0] == dto.LocalResponseAGYAgentSummary
}

func explicitZeroFlower2APIUsage(raw gjson.Result) bool {
	// 普通 Messages 的缺省字段也会被解析为 0。只有上游明确给出的 0 才能覆盖用量兜底。
	if raw.Get("input_tokens").Raw != "0" || raw.Get("output_tokens").Raw != "0" {
		return false
	}
	var usage dto.ClaudeUsage
	return common.UnmarshalJsonStr(raw.Raw, &usage) == nil && !dto.HasClaudeUsageTokens(&usage) &&
		usage.BillingUsage == nil && (usage.ServerToolUse == nil || usage.ServerToolUse.WebSearchRequests == 0)
}

type localSummaryStream uint8

const (
	localSummaryAwaitStart localSummaryStream = iota
	localSummaryAwaitText
	localSummaryAwaitDelta
	localSummaryAwaitStop
	localSummaryComplete
	localSummaryInvalid
)

func (stream *localSummaryStream) observe(data string) {
	frame := gjson.Parse(data)
	switch frame.Get("type").String() {
	case "message_start":
		if *stream == localSummaryAwaitStart && explicitZeroFlower2APIUsage(frame.Get("message.usage")) {
			*stream = localSummaryAwaitText
			return
		}
	case "content_block_delta":
		if (*stream == localSummaryAwaitText || *stream == localSummaryAwaitDelta) && frame.Get("delta.type").String() == "text_delta" {
			if frame.Get("delta.text").String() != "" {
				*stream = localSummaryAwaitDelta
			}
			return
		}
	case "message_delta":
		if *stream == localSummaryAwaitDelta && frame.Get("delta.stop_reason").String() == "end_turn" && explicitZeroFlower2APIUsage(frame.Get("usage")) {
			*stream = localSummaryAwaitStop
			return
		}
	case "message_stop":
		if *stream == localSummaryAwaitStop {
			*stream = localSummaryComplete
			return
		}
	case "content_block_start":
		if (*stream == localSummaryAwaitText || *stream == localSummaryAwaitDelta) && frame.Get("content_block.type").String() == "text" {
			return
		}
	case "error":
	default:
		return
	}
	*stream = localSummaryInvalid
}

func isFlower2APILocalSummaryJSON(response *dto.ClaudeResponse, data []byte) bool {
	if response.Type != "message" || response.StopReason != "end_turn" || len(response.Content) == 0 {
		return false
	}
	for _, block := range response.Content {
		if block.Type != "text" || block.GetText() == "" {
			return false
		}
	}
	return explicitZeroFlower2APIUsage(gjson.GetBytes(data, "usage"))
}
