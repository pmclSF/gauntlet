// Package fixture provides the content-addressed fixture store for
// recorded model and tool call responses. Fixtures are addressed by
// SHA-256 hash of the canonicalized request.
package fixture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ModelFixture is a recorded model call with canonical and raw forms.
type ModelFixture struct {
	FixtureID        string          `json:"fixture_id"`
	HashVersion      int             `json:"hash_version"`
	CanonicalHash    string          `json:"canonical_hash"`
	ProviderFamily   string          `json:"provider_family"`
	Model            string          `json:"model"`
	CanonicalRequest json.RawMessage `json:"canonical_request"`
	RawRequest       json.RawMessage `json:"raw_request"`
	Response         json.RawMessage `json:"response"`
	RecordedAt       time.Time       `json:"recorded_at"`
	RecordedBy       string          `json:"recorded_by"`
	IsBeta           bool            `json:"is_beta"`
}

// ToolFixture is a recorded tool call.
type ToolFixture struct {
	FixtureID     string          `json:"fixture_id"`
	HashVersion   int             `json:"hash_version"`
	CanonicalHash string          `json:"canonical_hash"`
	ToolName      string          `json:"tool_name"`
	ArgsHash      string          `json:"args_hash"`
	State         string          `json:"state"`
	Args          json.RawMessage `json:"args"`
	Response      json.RawMessage `json:"response"`
	ResponseCode  int             `json:"response_code,omitempty"`
	BehaviorDelay int             `json:"behavior_delay_ms,omitempty"`
	RecordedAt    time.Time       `json:"recorded_at"`
}

// ErrFixtureMiss is returned when no fixture matches the canonical hash.
// This is always a hard failure — never falls back to live calls.
type ErrFixtureMiss struct {
	FixtureType   string
	CanonicalHash string
	CanonicalJSON string
	RecordCmd     string
}

func (e *ErrFixtureMiss) Error() string {
	truncated := e.CanonicalJSON
	if len(truncated) > 500 {
		truncated = truncated[:500]
	}
	return fmt.Sprintf(`fixture miss: no recorded fixture for %s call
  Canonical hash: %s
  Canonical request (truncated): %s
  To record this fixture:
    %s
  See: docs/recording-fixtures.md`,
		e.FixtureType, e.CanonicalHash, truncated, e.RecordCmd)
}

// Store is a filesystem-backed content-addressed fixture store.
type Store struct {
	BaseDir string // e.g. "evals/fixtures"
}

// NewStore creates a new Store rooted at the given directory.
func NewStore(baseDir string) *Store {
	return &Store{BaseDir: baseDir}
}

// Hash computes SHA-256 of the given canonical JSON bytes.
func Hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// GetModelFixture retrieves a model fixture by canonical hash.
func (s *Store) GetModelFixture(hash string) (*ModelFixture, error) {
	path := filepath.Join(s.BaseDir, "models", hash+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // not found — caller will create ErrFixtureMiss
		}
		return nil, fmt.Errorf("failed to read model fixture %s: %w", path, err)
	}
	var f ModelFixture
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("failed to parse model fixture %s: %w", path, err)
	}
	return &f, nil
}

// PutModelFixture stores a model fixture by canonical hash.
func (s *Store) PutModelFixture(f *ModelFixture) error {
	dir := filepath.Join(s.BaseDir, "models")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create model fixtures directory: %w", err)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal model fixture: %w", err)
	}
	path := filepath.Join(dir, f.CanonicalHash+".json")
	return os.WriteFile(path, data, 0o644)
}

// GetToolFixture retrieves a tool fixture by tool name and canonical hash.
func (s *Store) GetToolFixture(toolName, hash string) (*ToolFixture, error) {
	path := filepath.Join(s.BaseDir, "tools", toolName, hash+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read tool fixture %s: %w", path, err)
	}
	var f ToolFixture
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("failed to parse tool fixture %s: %w", path, err)
	}
	return &f, nil
}

// PutToolFixture stores a tool fixture.
func (s *Store) PutToolFixture(f *ToolFixture) error {
	dir := filepath.Join(s.BaseDir, "tools", f.ToolName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create tool fixtures directory: %w", err)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tool fixture: %w", err)
	}
	path := filepath.Join(dir, f.CanonicalHash+".json")
	return os.WriteFile(path, data, 0o644)
}

// ListModelFixtures lists all model fixture hashes.
func (s *Store) ListModelFixtures() ([]string, error) {
	dir := filepath.Join(s.BaseDir, "models")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var hashes []string
	for _, e := range entries {
		name := e.Name()
		if filepath.Ext(name) == ".json" {
			hashes = append(hashes, name[:len(name)-5])
		}
	}
	return hashes, nil
}
