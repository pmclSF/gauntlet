// Package behavioral implements Layer 2 (behavioral) and Layer 3 (embedding)
// assertions for the Gauntlet 4-layer assertion model.
//
// Layer 1: Structural — JSON schema, required fields (in assertions package)
// Layer 2: Behavioral — tool sequence, retry cap, forbidden tool (hard gate)
// Layer 3: Embedding — semantic similarity via embeddings (soft gate)
// Layer 4: Judge — LLM-as-judge evaluation (soft gate, nightly only)
package behavioral

import (
	"fmt"
	"math"
	"strings"
)

// Layer represents an assertion layer.
type Layer int

const (
	LayerStructural Layer = 1
	LayerBehavioral Layer = 2
	LayerEmbedding  Layer = 3
	LayerJudge      Layer = 4
)

// AssertionResult is the result of a behavioral or embedding assertion.
type AssertionResult struct {
	Layer      Layer   `json:"layer"`
	Type       string  `json:"type"`
	Passed     bool    `json:"passed"`
	Message    string  `json:"message"`
	Score      float64 `json:"score,omitempty"`
	Threshold  float64 `json:"threshold,omitempty"`
	IsHardGate bool    `json:"is_hard_gate"`
}

// ToolTrace represents the sequence of tool calls made by an agent.
type ToolTrace struct {
	Calls []ToolCall `json:"calls"`
}

// ToolCall is a single tool invocation.
type ToolCall struct {
	Name   string                 `json:"name"`
	Args   map[string]interface{} `json:"args"`
	Result interface{}            `json:"result,omitempty"`
}

// CheckToolSequence verifies that a required tool sequence appears in the trace.
func CheckToolSequence(trace *ToolTrace, required []string) AssertionResult {
	actual := make([]string, len(trace.Calls))
	for i, c := range trace.Calls {
		actual[i] = c.Name
	}

	idx := 0
	for _, call := range actual {
		if idx < len(required) && call == required[idx] {
			idx++
		}
	}

	if idx == len(required) {
		return AssertionResult{
			Layer:      LayerBehavioral,
			Type:       "tool_sequence",
			Passed:     true,
			Message:    "Required tool sequence found",
			IsHardGate: true,
		}
	}

	return AssertionResult{
		Layer:      LayerBehavioral,
		Type:       "tool_sequence",
		Passed:     false,
		Message:    fmt.Sprintf("Missing tools in sequence: expected %v, got %v (matched %d/%d)", required, actual, idx, len(required)),
		IsHardGate: true,
	}
}

// CheckForbiddenTool verifies that forbidden tools were not called.
func CheckForbiddenTool(trace *ToolTrace, forbidden []string) AssertionResult {
	forbiddenSet := make(map[string]bool)
	for _, f := range forbidden {
		forbiddenSet[f] = true
	}

	for _, call := range trace.Calls {
		if forbiddenSet[call.Name] {
			return AssertionResult{
				Layer:      LayerBehavioral,
				Type:       "forbidden_tool",
				Passed:     false,
				Message:    fmt.Sprintf("Forbidden tool called: %s", call.Name),
				IsHardGate: true,
			}
		}
	}

	return AssertionResult{
		Layer:      LayerBehavioral,
		Type:       "forbidden_tool",
		Passed:     true,
		Message:    "No forbidden tools called",
		IsHardGate: true,
	}
}

// CheckRetryCap verifies that no tool is called more than N times consecutively.
func CheckRetryCap(trace *ToolTrace, maxRetries int) AssertionResult {
	if maxRetries <= 0 {
		maxRetries = 3
	}

	if len(trace.Calls) == 0 {
		return AssertionResult{
			Layer:      LayerBehavioral,
			Type:       "retry_cap",
			Passed:     true,
			Message:    "No tool calls to check",
			IsHardGate: true,
		}
	}

	consecutive := 1
	for i := 1; i < len(trace.Calls); i++ {
		if trace.Calls[i].Name == trace.Calls[i-1].Name {
			consecutive++
			if consecutive > maxRetries {
				return AssertionResult{
					Layer:      LayerBehavioral,
					Type:       "retry_cap",
					Passed:     false,
					Message:    fmt.Sprintf("Tool %s called %d times consecutively (max %d)", trace.Calls[i].Name, consecutive, maxRetries),
					IsHardGate: true,
				}
			}
		} else {
			consecutive = 1
		}
	}

	return AssertionResult{
		Layer:      LayerBehavioral,
		Type:       "retry_cap",
		Passed:     true,
		Message:    fmt.Sprintf("All tools within retry cap of %d", maxRetries),
		IsHardGate: true,
	}
}

// CosineSimilarity computes cosine similarity between two vectors.
func CosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// CheckEmbeddingSimilarity is a Layer 3 embedding-based assertion.
// It compares the output embedding to a reference embedding.
func CheckEmbeddingSimilarity(outputEmbedding, referenceEmbedding []float64, threshold float64) AssertionResult {
	if threshold == 0 {
		threshold = 0.85
	}

	score := CosineSimilarity(outputEmbedding, referenceEmbedding)

	return AssertionResult{
		Layer:      LayerEmbedding,
		Type:       "embedding_similarity",
		Passed:     score >= threshold,
		Message:    fmt.Sprintf("Cosine similarity: %.4f (threshold: %.4f)", score, threshold),
		Score:      score,
		Threshold:  threshold,
		IsHardGate: false,
	}
}

// TokenOverlap computes the Jaccard similarity of token sets.
func TokenOverlap(output, reference string) float64 {
	outputTokens := tokenize(output)
	refTokens := tokenize(reference)

	if len(outputTokens) == 0 || len(refTokens) == 0 {
		return 0
	}

	intersection := 0
	for token := range outputTokens {
		if refTokens[token] {
			intersection++
		}
	}

	union := len(outputTokens) + len(refTokens) - intersection
	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}

func tokenize(text string) map[string]bool {
	tokens := make(map[string]bool)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		word = strings.Trim(word, ".,!?;:'\"()[]{}")
		if word != "" {
			tokens[word] = true
		}
	}
	return tokens
}
