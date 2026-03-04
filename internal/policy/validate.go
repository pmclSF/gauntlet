package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	schemas "github.com/gauntlet-dev/gauntlet/schema"
	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v3"
)

func strictPolicyFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GAUNTLET_POLICY_STRICT"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func validatePolicyDocument(path string, data []byte, opts LoadOptions) error {
	var raw interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		// Keep YAML parser errors from LoadWithOptions as canonical parse failures.
		return nil
	}
	normalized, err := toJSONCompatible(raw)
	if err != nil {
		return fmt.Errorf("policy file %s: failed to normalize YAML for schema validation: %w", path, err)
	}
	docBytes, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("policy file %s: failed to marshal document for schema validation: %w", path, err)
	}

	schemaBytes := schemas.GauntletPolicySchema()
	if !opts.Strict {
		schemaBytes, err = relaxAdditionalProperties(schemaBytes)
		if err != nil {
			return fmt.Errorf("policy file %s: failed to build relaxed schema: %w", path, err)
		}
	}

	result, err := gojsonschema.Validate(
		gojsonschema.NewBytesLoader(schemaBytes),
		gojsonschema.NewBytesLoader(docBytes),
	)
	if err != nil {
		return fmt.Errorf("policy file %s: schema validation failed: %w", path, err)
	}
	if result.Valid() {
		return nil
	}

	var root yaml.Node
	_ = yaml.Unmarshal(data, &root)

	const maxErrors = 8
	parts := make([]string, 0, len(result.Errors()))
	for i, ve := range result.Errors() {
		if i >= maxErrors {
			parts = append(parts, fmt.Sprintf("... and %d more", len(result.Errors())-maxErrors))
			break
		}
		field := normalizeSchemaErrorField(ve.Field())
		line := findSchemaErrorLine(&root, field, ve.Description())
		if line > 0 {
			parts = append(parts, fmt.Sprintf("%s (line %d): %s", field, line, ve.Description()))
		} else {
			parts = append(parts, fmt.Sprintf("%s: %s", field, ve.Description()))
		}
	}
	return fmt.Errorf("policy file %s: schema validation failed: %s", path, strings.Join(parts, "; "))
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

func relaxAdditionalProperties(schemaBytes []byte) ([]byte, error) {
	var raw interface{}
	if err := json.Unmarshal(schemaBytes, &raw); err != nil {
		return nil, err
	}
	relaxAdditionalPropsNode(raw)
	return json.Marshal(raw)
}

func relaxAdditionalPropsNode(node interface{}) {
	switch v := node.(type) {
	case map[string]interface{}:
		for key, child := range v {
			if key == "additionalProperties" {
				if b, ok := child.(bool); ok && !b {
					v[key] = true
					continue
				}
			}
			relaxAdditionalPropsNode(child)
		}
	case []interface{}:
		for _, child := range v {
			relaxAdditionalPropsNode(child)
		}
	}
}

func normalizeSchemaErrorField(field string) string {
	field = strings.TrimSpace(field)
	switch field {
	case "", "(root)":
		return "root"
	default:
		field = strings.TrimPrefix(field, "(root).")
		if field == "" {
			return "root"
		}
		return "root." + field
	}
}

func findSchemaErrorLine(root *yaml.Node, field, description string) int {
	doc := yamlRoot(root)
	if doc == nil {
		return 0
	}
	tokens := schemaFieldTokens(field)
	line := lineForPath(doc, tokens)

	unknownKey := extractAdditionalPropertyName(description)
	if unknownKey == "" {
		return line
	}
	parent := nodeForPath(doc, tokens)
	if parent == nil || parent.Kind != yaml.MappingNode {
		return line
	}
	for i := 0; i+1 < len(parent.Content); i += 2 {
		key := parent.Content[i]
		if key.Value == unknownKey {
			return key.Line
		}
	}
	return line
}

func yamlRoot(root *yaml.Node) *yaml.Node {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		return root.Content[0]
	}
	return root
}

func schemaFieldTokens(field string) []string {
	field = strings.TrimSpace(field)
	field = strings.TrimPrefix(field, "root")
	field = strings.TrimPrefix(field, ".")
	if field == "" {
		return nil
	}
	parts := strings.Split(field, ".")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func lineForPath(node *yaml.Node, tokens []string) int {
	if node == nil {
		return 0
	}
	current := node
	line := current.Line
	for _, token := range tokens {
		switch current.Kind {
		case yaml.MappingNode:
			found := false
			for i := 0; i+1 < len(current.Content); i += 2 {
				k := current.Content[i]
				v := current.Content[i+1]
				if k.Value == token {
					line = k.Line
					current = v
					found = true
					break
				}
			}
			if !found {
				return line
			}
		case yaml.SequenceNode:
			idx, err := strconv.Atoi(token)
			if err != nil || idx < 0 || idx >= len(current.Content) {
				return line
			}
			current = current.Content[idx]
			if current.Line > 0 {
				line = current.Line
			}
		default:
			return line
		}
	}
	if line <= 0 {
		return current.Line
	}
	return line
}

func nodeForPath(node *yaml.Node, tokens []string) *yaml.Node {
	current := node
	for _, token := range tokens {
		switch current.Kind {
		case yaml.MappingNode:
			found := false
			for i := 0; i+1 < len(current.Content); i += 2 {
				k := current.Content[i]
				v := current.Content[i+1]
				if k.Value == token {
					current = v
					found = true
					break
				}
			}
			if !found {
				return nil
			}
		case yaml.SequenceNode:
			idx, err := strconv.Atoi(token)
			if err != nil || idx < 0 || idx >= len(current.Content) {
				return nil
			}
			current = current.Content[idx]
		default:
			return nil
		}
	}
	return current
}

func extractAdditionalPropertyName(desc string) string {
	const suffix = " is not allowed"
	idx := strings.Index(desc, "Additional property ")
	if idx == -1 {
		return ""
	}
	start := idx + len("Additional property ")
	rest := strings.TrimSpace(desc[start:])
	end := strings.Index(rest, suffix)
	if end == -1 {
		return ""
	}
	name := strings.TrimSpace(rest[:end])
	name = strings.Trim(name, `"'`)
	return name
}
