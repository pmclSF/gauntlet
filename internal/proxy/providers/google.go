package providers

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

	if isGeminiStreamingPath(path) {
		cr.Extra["original_path_was_streaming"] = true
	}

	// Model is typically in the URL path for Google APIs
	// e.g. /v1beta/models/gemini-2.0-flash:generateContent
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "models" && i+1 < len(parts) {
			modelPart := parts[i+1]
			if idx := strings.Index(modelPart, ":"); idx > 0 {
				modelPart = modelPart[:idx]
			}
			cr.Model = modelPart
			break
		}
	}

	// System instruction
	if si, ok := raw["systemInstruction"].(map[string]interface{}); ok {
		if rawParts, ok := si["parts"].([]interface{}); ok {
			var texts []string
			for _, p := range rawParts {
				part, ok := p.(map[string]interface{})
				if !ok {
					continue
				}
				if text, ok := part["text"].(string); ok && strings.TrimSpace(text) != "" {
					texts = append(texts, text)
				}
			}
			cr.System = strings.Join(texts, "\n")
		}
	}

	// Contents -> canonical messages
	if contents, ok := raw["contents"].([]interface{}); ok {
		for _, c := range contents {
			content, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			role, _ := content["role"].(string)
			// Map Google roles to canonical
			switch role {
			case "MODEL":
				role = "assistant"
			case "USER":
				role = "user"
			}

			messageContent := ""
			var normalizedParts []CanonicalContentPart
			if rawParts, ok := content["parts"].([]interface{}); ok {
				for _, rawPart := range rawParts {
					part, ok := rawPart.(map[string]interface{})
					if !ok {
						continue
					}
					if text, ok := part["text"].(string); ok {
						normalizedParts = append(normalizedParts, CanonicalContentPart{
							Type: "text",
							Text: text,
						})
						messageContent += text
					}
					if inlineData, ok := part["inlineData"].(map[string]interface{}); ok {
						encoded, _ := inlineData["data"].(string)
						decoded, err := decodeBase64Any(encoded)
						if err != nil {
							continue
						}
						sum := sha256.Sum256(decoded)
						mime, _ := inlineData["mimeType"].(string)
						normalizedParts = append(normalizedParts, CanonicalContentPart{
							Type:      "image",
							ImageHash: hex.EncodeToString(sum[:]),
							MimeType:  mime,
						})
					}
					if fileData, ok := part["fileData"].(map[string]interface{}); ok {
						fileURI, _ := fileData["fileUri"].(string)
						mime, _ := fileData["mimeType"].(string)
						if strings.TrimSpace(fileURI) == "" {
							continue
						}
						normalizedParts = append(normalizedParts, CanonicalContentPart{
							Type:      "image",
							ImageHash: fileURI,
							MimeType:  mime,
						})
					}
				}
			}

			contentValue := collapseCanonicalParts(normalizedParts)
			if str, ok := contentValue.(string); ok && str == "" {
				contentValue = messageContent
			}
			cr.Messages = append(cr.Messages, CanonicalMessage{
				Role:    role,
				Content: contentValue,
			})
		}
	}

	// Tools -> functionDeclarations
	if tools, ok := raw["tools"].([]interface{}); ok {
		for _, t := range tools {
			tool, ok := t.(map[string]interface{})
			if !ok {
				continue
			}
			if fds, ok := tool["functionDeclarations"].([]interface{}); ok {
				for _, fd := range fds {
					fn, ok := fd.(map[string]interface{})
					if !ok {
						continue
					}
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

func (n *GoogleNormalizer) ExtractUsage(response []byte) (promptTokens int, completionTokens int) {
	return extractUsageTokens(
		response,
		[][]string{
			{"prompt_tokens"},
			{"usageMetadata", "promptTokenCount"},
			{"usage", "prompt_tokens"},
		},
		[][]string{
			{"completion_tokens"},
			{"usageMetadata", "candidatesTokenCount"},
			{"usage", "completion_tokens"},
		},
	)
}

func (n *GoogleNormalizer) NormalizeResponseForFixture(response []byte) ([]byte, error) {
	return normalizeGeminiResponse(response)
}

func normalizeGeminiResponse(response []byte) ([]byte, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(response, &obj); err == nil {
		return marshalGeminiResponse(sanitizeGeminiResponse(obj))
	}

	var list []interface{}
	if err := json.Unmarshal(response, &list); err == nil {
		merged := mergeGeminiChunks(list)
		return marshalGeminiResponse(merged)
	}

	chunks := parseNDJSONObjects(response)
	if len(chunks) > 0 {
		merged := mergeGeminiChunks(chunks)
		return marshalGeminiResponse(merged)
	}

	// If response isn't parseable, keep raw payload to avoid breaking replay.
	return response, nil
}

func marshalGeminiResponse(v map[string]interface{}) ([]byte, error) {
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("google: failed to marshal normalized response: %w", err)
	}
	return out, nil
}

func sanitizeGeminiResponse(root map[string]interface{}) map[string]interface{} {
	prompt, completion := geminiUsageFromResponse(root)
	delete(root, "usageMetadata")
	delete(root, "createTime")
	delete(root, "updateTime")
	delete(root, "responseId")
	if prompt > 0 {
		root["prompt_tokens"] = prompt
	}
	if completion > 0 {
		root["completion_tokens"] = completion
	}
	return root
}

func geminiUsageFromResponse(root map[string]interface{}) (int, int) {
	usage, ok := root["usageMetadata"].(map[string]interface{})
	if !ok {
		return 0, 0
	}
	prompt, _ := toInt(usage["promptTokenCount"])
	completion, _ := toInt(usage["candidatesTokenCount"])
	return prompt, completion
}

func mergeGeminiChunks(chunks []interface{}) map[string]interface{} {
	mergedText := strings.Builder{}
	role := ""
	promptTokens := 0
	completionTokens := 0

	for _, rawChunk := range chunks {
		chunk, ok := rawChunk.(map[string]interface{})
		if !ok {
			continue
		}

		p, c := geminiUsageFromResponse(chunk)
		if p > 0 {
			promptTokens = p
		}
		if c > 0 {
			completionTokens = c
		}

		candidates, ok := chunk["candidates"].([]interface{})
		if !ok || len(candidates) == 0 {
			continue
		}
		candidate, ok := candidates[0].(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := candidate["content"].(map[string]interface{})
		if !ok {
			continue
		}
		if rawRole, ok := content["role"].(string); ok && strings.TrimSpace(rawRole) != "" {
			role = rawRole
		}
		parts, ok := content["parts"].([]interface{})
		if !ok {
			continue
		}
		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]interface{})
			if !ok {
				continue
			}
			text, _ := part["text"].(string)
			if text != "" {
				mergedText.WriteString(text)
			}
		}
	}

	content := map[string]interface{}{
		"parts": []interface{}{
			map[string]interface{}{
				"text": mergedText.String(),
			},
		},
	}
	if strings.TrimSpace(role) != "" {
		content["role"] = role
	}
	merged := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content": content,
			},
		},
	}
	if promptTokens > 0 {
		merged["prompt_tokens"] = promptTokens
	}
	if completionTokens > 0 {
		merged["completion_tokens"] = completionTokens
	}
	return merged
}

func parseNDJSONObjects(raw []byte) []interface{} {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 1024*1024), 4*1024*1024)
	var out []interface{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}
		out = append(out, chunk)
	}
	return out
}

func isGeminiStreamingPath(path string) bool {
	return strings.Contains(path, ":streamGenerateContent")
}

func rewriteGeminiStreamingPath(path string) string {
	if !isGeminiStreamingPath(path) {
		return path
	}
	return strings.Replace(path, ":streamGenerateContent", ":generateContent", 1)
}

// RewriteGeminiStreamingPath rewrites streamGenerateContent requests to the
// non-streaming generateContent endpoint for deterministic fixture recording.
func RewriteGeminiStreamingPath(path string) string {
	return rewriteGeminiStreamingPath(path)
}
