package common

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"

	"github.com/stretchr/testify/assert"
)

func TestResolveBillingModelName(t *testing.T) {
	tests := []struct {
		name          string
		originModel   string
		upstreamModel string
		useUpstream   bool
		isModelMapped bool
		wantModel     string
	}{
		{
			name:          "开关关闭时使用原始模型",
			originModel:   "origin-model",
			upstreamModel: "upstream-model",
			isModelMapped: true,
			wantModel:     "origin-model",
		},
		{
			name:          "未发生映射时使用原始模型",
			originModel:   "origin-model",
			upstreamModel: "upstream-model",
			useUpstream:   true,
			wantModel:     "origin-model",
		},
		{
			name:          "映射后使用上游模型",
			originModel:   "origin-model",
			upstreamModel: "upstream-model",
			useUpstream:   true,
			isModelMapped: true,
			wantModel:     "upstream-model",
		},
		{
			name:          "Compact 映射保留计费后缀",
			originModel:   "origin-model-openai-compact",
			upstreamModel: "upstream-model",
			useUpstream:   true,
			isModelMapped: true,
			wantModel:     "upstream-model-openai-compact",
		},
		{
			name:          "上游模型为空时使用原始模型",
			originModel:   "origin-model",
			useUpstream:   true,
			isModelMapped: true,
			wantModel:     "origin-model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &RelayInfo{
				OriginModelName: tt.originModel,
				ChannelMeta: &ChannelMeta{
					UpstreamModelName: tt.upstreamModel,
					IsModelMapped:     tt.isModelMapped,
					ChannelSetting: dto.ChannelSettings{
						UseUpstreamModelForBilling: tt.useUpstream,
					},
				},
			}

			assert.Equal(t, tt.wantModel, info.ResolveBillingModelName())
		})
	}

	var nilInfo *RelayInfo
	assert.Empty(t, nilInfo.ResolveBillingModelName())
}

func TestBillingModelNameFreezeAndClear(t *testing.T) {
	info := &RelayInfo{
		OriginModelName: "origin-model",
		ChannelMeta: &ChannelMeta{
			UpstreamModelName: "upstream-model",
			IsModelMapped:     true,
			ChannelSetting: dto.ChannelSettings{
				UseUpstreamModelForBilling: true,
			},
		},
	}

	assert.Equal(t, "upstream-model", info.BillingModelName())
	info.FreezeBillingModelName(" frozen-model ")
	assert.Equal(t, "frozen-model", info.FrozenBillingModelName())
	assert.Equal(t, "frozen-model", info.BillingModelName())

	info.UpstreamModelName = "changed-upstream-model"
	assert.Equal(t, "frozen-model", info.BillingModelName())

	info.ClearBillingModelName()
	assert.Empty(t, info.FrozenBillingModelName())
	assert.Equal(t, "changed-upstream-model", info.BillingModelName())
}
