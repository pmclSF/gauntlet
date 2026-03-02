package providers

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// GoogleNormalizer handles requests to Google AI Studio and Vertex AI.
// Detected by: hostname contains googleapis.com
type GoogleNormalizer struct{}

func (n *GoogleNormalizer) Family() string { return "google" }

func (n *GoogleNormalizer) Detect(hostname, path string, body []byte) bool {
	return strings.Contains(hostname, "googleapis.com")
}

func (n *GoogleNormalizer) Normalize(hostname, path string, headers map[string]string, body []byte) (*CanonicalRequest, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("google: failed to parse request body: %w", err)
	}

	cr := &CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "google",
		Extra:                    make(map[string]interface{}),
	}

	// Model is typically in the URL path for Google APIs
	// e.g. /v1beta/models/gemini-pro:generateContent
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "models" && i+1 < len(parts) {
			modelPart := parts[i+1]
			// Strip :generateContent suffix
			if idx := strings.Index(modelPart, ":"); idx > 0 {
				modelPart = modelPart[:idx]
			}
			cr.Model = modelPart
			break
		}
	}

	// System instruction
	if si, ok := raw["systemInstruction"].(map[string]interface{}); ok {
		if parts, ok := si["parts"].([]interface{}); ok {
			var texts []string
			for _, p := range parts {
				if part, ok := p.(map[string]interface{}); ok {
					if text, ok := part["text"].(string); ok {
						texts = append(texts, text)
					}
				}
			}
			cr.System = strings.Join(texts, "\n")
		}
	}

	// Contents -> Messages
	if contents, ok := raw["contents"].([]interface{}); ok {
		for _, c := range contents {
			if content, ok := c.(map[string]interface{}); ok {
				role, _ := content["role"].(string)
				// Map Google roles to canonical
				switch role {
				case "MODEL":
					role = "assistant"
				case "USER":
					role = "user"
				}
				if parts, ok := content["parts"].([]interface{}); ok {
					var text string
					for _, p := range parts {
						if part, ok := p.(map[string]interface{}); ok {
							if t, ok := part["text"].(string); ok {
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

	// Tools -> functionDeclarations
	if tools, ok := raw["tools"].([]interface{}); ok {
		for _, t := range tools {
			if tool, ok := t.(map[string]interface{}); ok {
				if fds, ok := tool["functionDeclarations"].([]interface{}); ok {
					for _, fd := range fds {
						if fn, ok := fd.(map[string]interface{}); ok {
							name, _ := fn["name"].(string)
							desc, _ := fn["description"].(string)
							params, _ := fn["parameters"].(map[string]interface{})
							cr.Tools = append(cr.Tools, CanonicalTool{
								Name:        name,
								Description: desc,
								Parameters:  params,
							})
						}
					}
				}
			}
		}
		sort.Slice(cr.Tools, func(i, j int) bool {
			return cr.Tools[i].Name < cr.Tools[j].Name
		})
	}

	// Generation config -> sampling
	if gc, ok := raw["generationConfig"].(map[string]interface{}); ok {
		if t, ok := gc["temperature"]; ok {
			if f, ok := toFloat64(t); ok {
				cr.Sampling.Temperature = &f
			}
		}
		if mt, ok := gc["maxOutputTokens"]; ok {
			if i, ok := toInt(mt); ok {
				cr.Sampling.MaxTokens = &i
			}
		}
		if tp, ok := gc["topP"]; ok {
			if f, ok := toFloat64(tp); ok {
				cr.Sampling.TopP = &f
			}
		}
		if stop, ok := gc["stopSequences"].([]interface{}); ok {
			for _, s := range stop {
				if str, ok := s.(string); ok {
					cr.Sampling.Stop = append(cr.Sampling.Stop, str)
				}
			}
		}
	}

	return cr, nil
}

func (n *GoogleNormalizer) DenormalizeResponse(canonical []byte, providerFamily string) ([]byte, error) {
	return canonical, nil
}
