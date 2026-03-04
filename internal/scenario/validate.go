package scenario

import (
	"encoding/json"
	"fmt"
	"strings"

	schemas "github.com/gauntlet-dev/gauntlet/schema"
	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"
)

func validateScenarioDocument(path string, data []byte) error {
	var raw interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		// Keep parser error behavior in LoadFile as the canonical parse failure.
		return nil
	}

	normalized, err := toJSONCompatible(raw)
	if err != nil {
		return fmt.Errorf("scenario file %s: failed to normalize YAML for schema validation: %w", path, err)
	}

	docBytes, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("scenario file %s: failed to marshal document for schema validation: %w", path, err)
	}

	schemaLoader := gojsonschema.NewBytesLoader(schemas.ScenarioSchema())
	docLoader := gojsonschema.NewBytesLoader(docBytes)
	result, err := gojsonschema.Validate(schemaLoader, docLoader)
	if err != nil {
		return fmt.Errorf("scenario file %s: schema validation failed: %w", path, err)
	}
	if result.Valid() {
		return nil
	}

	const maxErrors = 6
	var parts []string
	for i, ve := range result.Errors() {
		if i >= maxErrors {
			parts = append(parts, fmt.Sprintf("... and %d more", len(result.Errors())-maxErrors))
			break
		}
		field := ve.Field()
		if field == "(root)" {
			field = "root"
		}
		parts = append(parts, fmt.Sprintf("%s: %s", field, ve.Description()))
	}

	return fmt.Errorf("scenario file %s: schema validation failed: %s", path, strings.Join(parts, "; "))
}

func toJSONCompatible(v interface{}) (interface{}, error) {
	switch node := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(node))
		for k, val := range node {
			conv, err := toJSONCompatible(val)
			if err != nil {
				return nil, err
			}
			out[k] = conv
		}
		return out, nil
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(node))
		for k, val := range node {
			key, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("non-string key %v", k)
			}
			conv, err := toJSONCompatible(val)
			if err != nil {
				return nil, err
			}
			out[key] = conv
		}
		return out, nil
	case []interface{}:
		out := make([]interface{}, len(node))
		for i, val := range node {
			conv, err := toJSONCompatible(val)
			if err != nil {
				return nil, err
			}
			out[i] = conv
		}
		return out, nil
	default:
		return node, nil
	}
}
