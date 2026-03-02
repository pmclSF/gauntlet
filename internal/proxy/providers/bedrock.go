package providers

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// BedrockNormalizer handles AWS Bedrock Converse API requests.
// Detected by: hostname contains .amazonaws.com AND path contains /converse
type BedrockNormalizer struct{}

func (n *BedrockNormalizer) Family() string { return "bedrock_converse" }

func (n *BedrockNormalizer) Detect(hostname, path string, body []byte) bool {
	return strings.Contains(hostname, ".amazonaws.com") && strings.Contains(path, "/converse")
}

func (n *BedrockNormalizer) Normalize(hostname, path string, headers map[string]string, body []byte) (*CanonicalRequest, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("bedrock: failed to parse request body: %w", err)
	}

	cr := &CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "bedrock_converse",
		Extra:                    make(map[string]interface{}),
	}

	// Model ID from path: /model/<modelId>/converse
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "model" && i+1 < len(parts) {
			cr.Model = parts[i+1]
			break
		}
	}

	// System
	if sys, ok := raw["system"].([]interface{}); ok {
		var texts []string
		for _, s := range sys {
			if block, ok := s.(map[string]interface{}); ok {
				if text, ok := block["text"].(string); ok {
					texts = append(texts, text)
				}
			}
		}
		cr.System = strings.Join(texts, "\n")
	}

	// Messages
	if msgs, ok := raw["messages"].([]interface{}); ok {
		for _, m := range msgs {
			if msg, ok := m.(map[string]interface{}); ok {
				role, _ := msg["role"].(string)
				// Bedrock content is an array of content blocks
				if content, ok := msg["content"].([]interface{}); ok {
					var text string
					for _, c := range content {
						if block, ok := c.(map[string]interface{}); ok {
							if t, ok := block["text"].(string); ok {
								text += t
							}
						}
					}
					cr.Messages = append(cr.Messages, CanonicalMessage{
						Role:    role,
						Content: text,
					})
				}
			}
		}
	}

	// Tool config
	if tc, ok := raw["toolConfig"].(map[string]interface{}); ok {
		if tools, ok := tc["tools"].([]interface{}); ok {
			for _, t := range tools {
				if tool, ok := t.(map[string]interface{}); ok {
					if spec, ok := tool["toolSpec"].(map[string]interface{}); ok {
						name, _ := spec["name"].(string)
						desc, _ := spec["description"].(string)
						schema, _ := spec["inputSchema"].(map[string]interface{})
						params, _ := schema["json"].(map[string]interface{})
						cr.Tools = append(cr.Tools, CanonicalTool{
							Name:        name,
							Description: desc,
							Parameters:  params,
						})
					}
				}
			}
			sort.Slice(cr.Tools, func(i, j int) bool {
				return cr.Tools[i].Name < cr.Tools[j].Name
			})
		}
	}

	// Inference config
	if ic, ok := raw["inferenceConfig"].(map[string]interface{}); ok {
		if t, ok := ic["temperature"]; ok {
			if f, ok := toFloat64(t); ok {
				cr.Sampling.Temperature = &f
			}
		}
		if mt, ok := ic["maxTokens"]; ok {
			if i, ok := toInt(mt); ok {
				cr.Sampling.MaxTokens = &i
			}
		}
		if tp, ok := ic["topP"]; ok {
			if f, ok := toFloat64(tp); ok {
				cr.Sampling.TopP = &f
			}
		}
		if stop, ok := ic["stopSequences"].([]interface{}); ok {
			for _, s := range stop {
				if str, ok := s.(string); ok {
					cr.Sampling.Stop = append(cr.Sampling.Stop, str)
				}
			}
		}
	}

	return cr, nil
}

func (n *BedrockNormalizer) DenormalizeResponse(canonical []byte, providerFamily string) ([]byte, error) {
	return canonical, nil
}
