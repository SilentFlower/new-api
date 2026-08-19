package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserLimitSummariesReturnsOnlyRequestedPublicFields(t *testing.T) {
	truncateTables(t)
	users := []User{
		{Id: 9301, Username: "quota-alice", DisplayName: "Alice", Password: "secret-password", Email: "alice@example.com", AffCode: "quota-alice"},
		{Id: 9302, Username: "quota-bob", DisplayName: "Bob", Password: "secret-password", Email: "bob@example.com", AffCode: "quota-bob"},
	}
	require.NoError(t, DB.Create(&users).Error)

	summaries, err := GetUserLimitSummaries([]int{9302})
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, UserLimitSummary{ID: 9302, Username: "quota-bob", DisplayName: "Bob"}, summaries[0])
}
