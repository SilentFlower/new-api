package ratio_setting

import "strings"

const CompactModelSuffix = "-openai-compact"

func WithCompactModelSuffix(modelName string) string {
	if strings.HasSuffix(modelName, CompactModelSuffix) {
		return modelName
	}
	return modelName + CompactModelSuffix
}
