package fixture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// ToolReplay intercepts tool calls and serves responses from the fixture store.
type ToolReplay struct {
	Store  *Store
	Suite  string
	Traces []ToolCallTrace
}

// ToolCallTrace records a single tool call for assertion evaluation.
type ToolCallTrace struct {
	ToolName     string          `json:"tool_name"`
	Args         json.RawMessage `json:"args"`
	ArgsHash     string          `json:"args_hash"`
	Response     json.RawMessage `json:"response"`
	ResponseHash string          `json:"response_hash"`
	FixtureUsed  string          `json:"fixture_used"`
	DurationMs   int             `json:"duration_ms"`
	Timestamp    time.Time       `json:"timestamp"`
}

// Replay looks up a fixture for the given tool call.
// Returns the fixture response or ErrFixtureMiss.
// The real tool function is never called.
func (r *ToolReplay) Replay(toolName string, args map[string]interface{}) (json.RawMessage, error) {
	canonical, err := CanonicalizeToolCall(toolName, args)
	if err != nil {
		return nil, fmt.Errorf("failed to canonicalize tool call %s: %w", toolName, err)
	}

	hash := HashCanonical(canonical)

	fixture, err := r.Store.GetToolFixture(toolName, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to load tool fixture for %s: %w", toolName, err)
	}
	if fixture == nil {
		return nil, &ErrFixtureMiss{
			FixtureType:   "tool:" + toolName,
			CanonicalHash: hash,
			CanonicalJSON: string(canonical),
			RecordCmd:     fmt.Sprintf("GAUNTLET_MODEL_MODE=live gauntlet record --suite %s", r.Suite),
		}
	}

	// Simulate delay if configured
	if fixture.BehaviorDelay > 0 {
		time.Sleep(time.Duration(fixture.BehaviorDelay) * time.Millisecond)
	}

	response := applyResponseCodeEnvelope(fixture.Response, fixture.ResponseCode)

	// Record trace
	r.Traces = append(r.Traces, ToolCallTrace{
		ToolName:    toolName,
		Args:        mustMarshal(args),
		ArgsHash:    hash,
		Response:    response,
		FixtureUsed: fixture.CanonicalHash,
		DurationMs:  fixture.BehaviorDelay,
		Timestamp:   time.Now(),
	})

	return response, nil
}

// Reset clears recorded traces for a new scenario.
func (r *ToolReplay) Reset() {
	r.Traces = nil
}

func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

func applyResponseCodeEnvelope(raw json.RawMessage, responseCode int) json.RawMessage {
	if responseCode <= 0 {
		return raw
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		envelope := map[string]interface{}{"response_code": responseCode}
		return mustMarshal(envelope)
	}

	var decoded interface{}
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return raw
	}

	switch v := decoded.(type) {
	case map[string]interface{}:
		if _, exists := v["response_code"]; !exists {
			v["response_code"] = responseCode
		}
		return mustMarshal(v)
	default:
		envelope := map[string]interface{}{
			"response_code": responseCode,
			"body":          decoded,
		}
		return mustMarshal(envelope)
	}
}
