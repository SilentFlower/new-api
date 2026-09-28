package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateMigrationUsernameAccepts64UnicodeCodePoints(t *testing.T) {
	name := strings.Repeat("工作", 32)
	require.NoError(t, ValidateMigrationUsername(name))
	require.ErrorContains(t, ValidateMigrationUsername(name+"流"), "64")
	require.Error(t, ValidateMigrationUsername(" 工作流"))
	require.Error(t, ValidateMigrationUsername("工作\n流"))
}

func TestBuildExplicitMigratedUsernameKeepsExactNameAndRejectsConflict(t *testing.T) {
	truncateUsers(t)
	name := "智能标书生成工作流"
	got, err := BuildExplicitMigratedUsername(DB, name, map[string]bool{})
	require.NoError(t, err)
	require.Equal(t, name, got)

	seedUserForUsername(t, name)
	_, err = BuildExplicitMigratedUsername(DB, name, map[string]bool{})
	require.ErrorContains(t, err, "已存在")
	_, err = BuildExplicitMigratedUsername(DB, "另一工作流", map[string]bool{"另一工作流": true})
	require.ErrorContains(t, err, "本批次")
}
