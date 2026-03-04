package fixture

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValidateModelResponse checks that a provider response is valid JSON and
// matches minimal deterministic structure expectations before fixture storage.
func ValidateModelResponse(providerFamily string, response []byte) error {
	var root map[string]interface{}
	if err := json.Unmarshal(response, &root); err != nil {
		return fmt.Errorf("response is not valid JSON: %w", err)
	}
	if len(root) == 0 {
		return fmt.Errorf("response object is empty")
	}
	if _, ok := root["error"]; ok {
		return nil // provider error payloads are valid fixtures
	}

	switch strings.ToLower(strings.TrimSpace(providerFamily)) {
	case "openai_compatible":
		if !hasNonEmptyArray(root, "choices") {
			return fmt.Errorf("openai_compatible response missing non-empty choices array")
		}
	case "anthropic":
		if !hasNonEmptyArray(root, "content") && !hasNonEmptyString(root, "completion") {
			return fmt.Errorf("anthropic response missing content array/completion")
		}
	case "google":
		if !hasNonEmptyArray(root, "candidates") {
			return fmt.Errorf("google response missing non-empty candidates array")
		}
	case "cohere":
		if !hasNonEmptyArray(root, "generations") && !hasNonEmptyString(root, "text") {
			return fmt.Errorf("cohere response missing generations/text")
		}
	case "bedrock_converse":
		if _, ok := root["output"]; !ok && !hasNonEmptyArray(root, "results") {
			return fmt.Errorf("bedrock_converse response missing output/results")
		}
	case "unknown":
		// Accept generic valid JSON object for unknown providers.
	default:
		// Unknown provider family string is treated conservatively as generic object.
	}

	return nil
}

func hasNonEmptyArray(root map[string]interface{}, key string) bool {
	v, ok := root[key]
	if !ok {
		return false
	}
	items, ok := v.([]interface{})
	return ok && len(items) > 0
}

func hasNonEmptyString(root map[string]interface{}, key string) bool {
	v, ok := root[key]
	if !ok {
		return false
	}
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) != ""
}
