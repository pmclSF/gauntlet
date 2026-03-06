package fixture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestPutToolFixture_ConcurrentWritersProduceValidFixture(t *testing.T) {
	store := NewStore(t.TempDir())
	args := map[string]interface{}{"order_id": "ord-001"}
	canonical, err := CanonicalizeToolCall("order_lookup", args)
	if err != nil {
		t.Fatalf("canonicalize tool call: %v", err)
	}
	hash := HashCanonical(canonical)

	var wg sync.WaitGroup
	errCh := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fixture := &ToolFixture{
				FixtureID:     hash,
				HashVersion:   1,
				CanonicalHash: hash,
				ToolName:      "order_lookup",
				ArgsHash:      hash,
				Args:          mustMarshal(args),
				Response:      json.RawMessage(`{"ok":true}`),
				ResponseCode:  200,
			}
			if err := store.PutToolFixture(fixture); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent PutToolFixture failed: %v", err)
	}

	path := filepath.Join(store.BaseDir, "tools", hash+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var parsed ToolFixture
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("fixture is corrupted JSON: %v\nraw=%s", err, string(data))
	}
	if parsed.CanonicalHash != hash {
		t.Fatalf("canonical_hash = %q, want %q", parsed.CanonicalHash, hash)
	}
	if parsed.ToolName != "order_lookup" {
		t.Fatalf("tool_name = %q, want order_lookup", parsed.ToolName)
	}
}
