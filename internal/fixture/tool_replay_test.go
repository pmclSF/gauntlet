package fixture

import (
	"encoding/json"
	"testing"
	"time"
)

func TestToolReplay_AppliesDelayAndResponseCodeEnvelope(t *testing.T) {
	store := NewStore(t.TempDir())
	args := map[string]interface{}{"order_id": "ord-001"}
	canonical, err := CanonicalizeToolCall("order_lookup", args)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	hash := HashCanonical(canonical)

	if err := store.PutToolFixture(&ToolFixture{
		FixtureID:     hash,
		HashVersion:   1,
		CanonicalHash: hash,
		ToolName:      "order_lookup",
		ArgsHash:      hash,
		Args:          mustMarshal(args),
		Response:      json.RawMessage(`{"status":"error"}`),
		ResponseCode:  500,
		BehaviorDelay: 30,
	}); err != nil {
		t.Fatalf("put fixture: %v", err)
	}

	replay := &ToolReplay{Store: store, Suite: "smoke"}
	start := time.Now()
	raw, err := replay.Replay("order_lookup", args)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Fatalf("expected replay delay >= 20ms, got %v", elapsed)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal replay response: %v", err)
	}
	if got["status"] != "error" {
		t.Fatalf("status = %v, want error", got["status"])
	}
	if code, ok := got["response_code"].(float64); !ok || int(code) != 500 {
		t.Fatalf("response_code = %v, want 500", got["response_code"])
	}
	if len(replay.Traces) != 1 {
		t.Fatalf("trace count = %d, want 1", len(replay.Traces))
	}
	if replay.Traces[0].DurationMs != 30 {
		t.Fatalf("trace duration_ms = %d, want 30", replay.Traces[0].DurationMs)
	}
}

func TestToolReplay_WrapsScalarResponseWhenResponseCodePresent(t *testing.T) {
	store := NewStore(t.TempDir())
	args := map[string]interface{}{"id": "1"}
	canonical, err := CanonicalizeToolCall("lookup", args)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	hash := HashCanonical(canonical)

	if err := store.PutToolFixture(&ToolFixture{
		FixtureID:     hash,
		HashVersion:   1,
		CanonicalHash: hash,
		ToolName:      "lookup",
		ArgsHash:      hash,
		Args:          mustMarshal(args),
		Response:      json.RawMessage(`"ok"`),
		ResponseCode:  204,
	}); err != nil {
		t.Fatalf("put fixture: %v", err)
	}

	replay := &ToolReplay{Store: store, Suite: "smoke"}
	raw, err := replay.Replay("lookup", args)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal replay response: %v", err)
	}
	if got["body"] != "ok" {
		t.Fatalf("body = %v, want ok", got["body"])
	}
	if code, ok := got["response_code"].(float64); !ok || int(code) != 204 {
		t.Fatalf("response_code = %v, want 204", got["response_code"])
	}
}
