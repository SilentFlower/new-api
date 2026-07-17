package controller

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// parseDashboardTokenNames 解析数据看板令牌筛选参数，并兼容旧的 token_name 单值参数。
func parseDashboardTokenNames(c *gin.Context) []string {
	return parseDashboardQueryValues(c, "token_names", "token_name")
}

// parseDashboardGroups 解析数据看板分组筛选参数，并兼容旧的 group 单值参数。
func parseDashboardGroups(c *gin.Context) []string {
	return parseDashboardQueryValues(c, "groups", "group")
}

// parseDashboardQueryValues 解析重复 key / 括号数组 / 旧单值查询参数，并做 trim、去空、去重。
func parseDashboardQueryValues(c *gin.Context, multiName string, legacyName string) []string {
	values, hasValues := c.GetQueryArray(multiName)
	bracketValues, hasBracketValues := c.GetQueryArray(multiName + "[]")
	rawValues := make([]string, 0, len(values)+len(bracketValues)+1)
	rawValues = append(rawValues, values...)
	rawValues = append(rawValues, bracketValues...)
	if !hasValues && !hasBracketValues && legacyName != "" {
		rawValues = append(rawValues, c.Query(legacyName))
	}

	seen := make(map[string]struct{}, len(rawValues))
	normalized := make([]string, 0, len(rawValues))
	for _, value := range rawValues {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}
