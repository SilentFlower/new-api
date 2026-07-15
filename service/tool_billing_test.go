package service

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeToolCallQuotaWebSearch(t *testing.T) {
	usage := ToolCallUsage{
		ModelName:         "gpt-5",
		WebSearchCalls:    1,
		WebSearchToolName: ToolNameWebSearch,
	}

	result := ComputeToolCallQuota(usage, 2)
	require.Len(t, result.Items, 1)
	assert.Equal(t, common.QuotaRound(10.0/1000*common.QuotaPerUnit*2), result.TotalQuota)
	assert.Equal(t, ToolNameWebSearch, result.Items[0].Name)
	assert.Equal(t, 1, result.Items[0].CallCount)
	assert.Nil(t, result.QuotaClamp)
}

func TestComputeToolCallQuotaRejectsNonPositiveGroupRatio(t *testing.T) {
	usage := ToolCallUsage{
		ModelName:         "gpt-5",
		WebSearchCalls:    1,
		WebSearchToolName: ToolNameWebSearch,
	}

	assert.Equal(t, ToolCallResult{}, ComputeToolCallQuota(usage, 0))
	assert.Equal(t, ToolCallResult{}, ComputeToolCallQuota(usage, -1))
}

func TestComputeToolCallQuotaReportsSaturation(t *testing.T) {
	result := ComputeToolCallQuota(ToolCallUsage{
		ModelName:         "gpt-5",
		WebSearchCalls:    math.MaxInt,
		WebSearchToolName: ToolNameWebSearch,
	}, 1)

	assert.Equal(t, common.MaxQuota, result.TotalQuota)
	require.NotNil(t, result.QuotaClamp)
	assert.Equal(t, common.QuotaClampOverflow, result.QuotaClamp.Kind)
}
