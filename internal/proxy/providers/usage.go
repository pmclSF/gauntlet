package providers

import "encoding/json"

func extractUsageTokens(response []byte, promptPaths [][]string, completionPaths [][]string) (int, int) {
	var raw map[string]interface{}
	if err := json.Unmarshal(response, &raw); err != nil {
		return 0, 0
	}
	prompt := extractUsageTokenValue(raw, promptPaths)
	completion := extractUsageTokenValue(raw, completionPaths)
	return prompt, completion
}

func extractUsageTokenValue(raw map[string]interface{}, paths [][]string) int {
	for _, path := range paths {
		if value, ok := extractUsageTokenPath(raw, path); ok {
			return value
		}
	}
	return 0
}

func extractUsageTokenPath(raw map[string]interface{}, path []string) (int, bool) {
	var cur interface{} = raw
	for _, part := range path {
		asMap, ok := cur.(map[string]interface{})
		if !ok {
			return 0, false
		}
		next, ok := asMap[part]
		if !ok {
			return 0, false
		}
		cur = next
	}
	value, ok := toInt(cur)
	if !ok {
		return 0, false
	}
	return value, true
}
