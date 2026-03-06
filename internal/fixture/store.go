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
	"sort"
	"strings"
	"time"

	"github.com/pmclSF/gauntlet/internal/proxy/providers"
)

// ModelFixture is a recorded model call with canonical and raw forms.
type ModelFixture struct {
	FixtureID         string            `json:"fixture_id"`
	HashVersion       int               `json:"hash_version"`
	CanonicalHash     string            `json:"canonical_hash"`
	ProviderFamily    string            `json:"provider_family"`
	Model             string            `json:"model"`
	CanonicalRequest  json.RawMessage   `json:"canonical_request"`
	RawRequest        json.RawMessage   `json:"raw_request"`
	Response          json.RawMessage   `json:"response"`
	RecordedAt        time.Time         `json:"recorded_at"`
	RecordedBy        string            `json:"recorded_by"`
	IsBeta            bool              `json:"is_beta"`
	Provenance        *Provenance       `json:"provenance,omitempty"`
	Signature         *FixtureSignature `json:"signature,omitempty"`
	Suite             string            `json:"suite,omitempty"`
	ScenarioSetSHA256 string            `json:"scenario_set_sha256,omitempty"`
}

// ToolFixture is a recorded tool call.
type ToolFixture struct {
	FixtureID         string            `json:"fixture_id"`
	HashVersion       int               `json:"hash_version"`
	CanonicalHash     string            `json:"canonical_hash"`
	ToolName          string            `json:"tool_name"`
	ArgsHash          string            `json:"args_hash"`
	State             string            `json:"state"`
	Args              json.RawMessage   `json:"args"`
	Response          json.RawMessage   `json:"response"`
	ResponseCode      int               `json:"response_code,omitempty"`
	BehaviorDelay     int               `json:"behavior_delay_ms,omitempty"`
	RecordedAt        time.Time         `json:"recorded_at"`
	Provenance        *Provenance       `json:"provenance,omitempty"`
	Signature         *FixtureSignature `json:"signature,omitempty"`
	Suite             string            `json:"suite,omitempty"`
	ScenarioSetSHA256 string            `json:"scenario_set_sha256,omitempty"`
}

// Provenance captures fixture creation metadata for auditability.
type Provenance struct {
	CommitSHA         string            `json:"commit_sha,omitempty"`
	RecorderIdentity  string            `json:"recorder_identity,omitempty"`
	ToolchainVersions map[string]string `json:"toolchain_versions,omitempty"`
	SDKVersions       map[string]string `json:"sdk_versions,omitempty"`
	Source            string            `json:"source,omitempty"`
}

// FixtureMissCandidate describes a nearby recorded model fixture candidate.
type FixtureMissCandidate struct {
	CanonicalHash  string `json:"canonical_hash"`
	ProviderFamily string `json:"provider_family,omitempty"`
	Model          string `json:"model,omitempty"`
	Distance       int    `json:"distance"`
}

// ErrFixtureMiss is returned when no fixture matches the canonical hash.
// This is always a hard failure — never falls back to live calls.
type ErrFixtureMiss struct {
	FixtureType      string
	ProviderFamily   string
	Model            string
	CanonicalHash    string
	CanonicalJSON    string
	RecordCmd        string
	Candidates       []FixtureMissCandidate
	ModelVersionHint string
}

func (e *ErrFixtureMiss) Error() string {
	truncated := e.CanonicalJSON
	if len(truncated) > 500 {
		truncated = truncated[:500]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "fixture miss: no recorded fixture for %s call\n", e.FixtureType)
	fmt.Fprintf(&b, "  Provider family: %s\n", fixtureMissDisplayValue(e.ProviderFamily))
	fmt.Fprintf(&b, "  Model: %s\n", fixtureMissDisplayValue(e.Model))
	fmt.Fprintf(&b, "  Canonical hash: %s\n", e.CanonicalHash)
	fmt.Fprintf(&b, "  Canonical request (truncated): %s\n", truncated)
	if len(e.Candidates) > 0 {
		b.WriteString("  Nearest recorded fixtures:\n")
		for _, candidate := range e.Candidates {
			fmt.Fprintf(
				&b,
				"    - hash=%s provider=%s model=%s distance=%d\n",
				candidate.CanonicalHash,
				fixtureMissDisplayValue(candidate.ProviderFamily),
				fixtureMissDisplayValue(candidate.Model),
				candidate.Distance,
			)
		}
	}
	if strings.TrimSpace(e.ModelVersionHint) != "" {
		fmt.Fprintf(&b, "  Hint: %s\n", strings.TrimSpace(e.ModelVersionHint))
	}
	fmt.Fprintf(&b, `  To record this fixture:
    %s
  See: docs/recording-fixtures.md`,
		e.RecordCmd)
	return b.String()
}

func fixtureMissDisplayValue(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "unknown"
	}
	return value
}

func normalizeFixtureMissKey(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// Store is a filesystem-backed content-addressed fixture store.
type Store struct {
	BaseDir                   string // e.g. "evals/fixtures"
	ExpectedSuite             string
	ExpectedScenarioSetSHA256 string
	fixtureSigner             *fixtureSigner
	requireFixtureSignatures  bool
	trustedKeyFingerprints    map[string]bool
	trustedRecorderIDs        map[string]bool
}

// NewStore creates a new Store rooted at the given directory.
func NewStore(baseDir string) *Store {
	return &Store{BaseDir: baseDir}
}

// SetReplayContext sets expected replay context metadata used for fixture
// context binding checks.
func (s *Store) SetReplayContext(suite, scenarioSetSHA256 string) {
	s.ExpectedSuite = strings.TrimSpace(suite)
	s.ExpectedScenarioSetSHA256 = strings.TrimSpace(scenarioSetSHA256)
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
	if err := s.validateModelFixture(path, hash, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// PutModelFixture stores a model fixture by canonical hash.
func (s *Store) PutModelFixture(f *ModelFixture) error {
	dir := filepath.Join(s.BaseDir, "models")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create model fixtures directory: %w", err)
	}
	if err := s.signModelFixture(f); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal model fixture: %w", err)
	}
	path := filepath.Join(dir, f.CanonicalHash+".json")
	if err := atomicWriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write model fixture %s: %w", path, err)
	}
	return nil
}

// GetToolFixture retrieves a tool fixture by tool name and canonical hash.
// Tries flat path (tools/<hash>.json) first, then namespaced (tools/<toolName>/<hash>.json).
func (s *Store) GetToolFixture(toolName, hash string) (*ToolFixture, error) {
	flatPath := filepath.Join(s.BaseDir, "tools", hash+".json")
	nsPath := filepath.Join(s.BaseDir, "tools", toolName, hash+".json")

	flatData, flatExists, err := readOptionalFile(flatPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read tool fixture %s: %w", flatPath, err)
	}
	nsData, nsExists, err := readOptionalFile(nsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read tool fixture %s: %w", nsPath, err)
	}
	if !flatExists && !nsExists {
		return nil, nil
	}

	data := flatData
	sourcePath := flatPath
	if !flatExists && nsExists {
		data = nsData
		sourcePath = nsPath
	}
	if flatExists && nsExists {
		if Hash(flatData) != Hash(nsData) {
			return nil, fmt.Errorf("fixture collision detected for tool %s hash %s: %s and %s differ", toolName, hash, flatPath, nsPath)
		}
		// Prefer flat path for deterministic precedence when contents are identical.
		data = flatData
		sourcePath = flatPath
	}

	var f ToolFixture
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("failed to parse tool fixture: %w", err)
	}
	if err := s.validateToolFixture(sourcePath, toolName, hash, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// PutToolFixture stores a tool fixture using flat layout (tools/<hash>.json).
func (s *Store) PutToolFixture(f *ToolFixture) error {
	dir := filepath.Join(s.BaseDir, "tools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create tool fixtures directory: %w", err)
	}
	if err := s.signToolFixture(f); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tool fixture: %w", err)
	}
	path := filepath.Join(dir, f.CanonicalHash+".json")
	if err := atomicWriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write tool fixture %s: %w", path, err)
	}
	return nil
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

// NearestModelFixtureCandidates returns up to limit nearby fixture candidates
// ordered by provider/model affinity, then canonical hash distance.
func (s *Store) NearestModelFixtureCandidates(providerFamily, model, requestedHash string, limit int) ([]FixtureMissCandidate, error) {
	if limit <= 0 {
		return nil, nil
	}

	dir := filepath.Join(s.BaseDir, "models")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list model fixtures for nearest-candidate lookup: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	requestedHash = normalizeFixtureMissKey(requestedHash)
	candidates := make([]FixtureMissCandidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		var meta struct {
			CanonicalHash  string `json:"canonical_hash"`
			ProviderFamily string `json:"provider_family"`
			Model          string `json:"model"`
		}
		if unmarshalErr := json.Unmarshal(data, &meta); unmarshalErr != nil {
			continue
		}
		hash := strings.TrimSpace(meta.CanonicalHash)
		if hash == "" {
			hash = strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		}
		hash = normalizeFixtureMissKey(hash)
		if hash == "" || hash == requestedHash {
			continue
		}
		candidates = append(candidates, FixtureMissCandidate{
			CanonicalHash:  hash,
			ProviderFamily: strings.TrimSpace(meta.ProviderFamily),
			Model:          strings.TrimSpace(meta.Model),
			Distance:       canonicalHashDistance(requestedHash, hash),
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		iRank := fixtureCandidateRank(candidates[i], providerFamily, model)
		jRank := fixtureCandidateRank(candidates[j], providerFamily, model)
		if iRank != jRank {
			return iRank < jRank
		}
		if candidates[i].Distance != candidates[j].Distance {
			return candidates[i].Distance < candidates[j].Distance
		}
		return candidates[i].CanonicalHash < candidates[j].CanonicalHash
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, nil
}

func fixtureCandidateRank(candidate FixtureMissCandidate, providerFamily, model string) int {
	requestedProvider := normalizeFixtureMissKey(providerFamily)
	requestedModel := normalizeFixtureMissKey(model)
	candidateProvider := normalizeFixtureMissKey(candidate.ProviderFamily)
	candidateModel := normalizeFixtureMissKey(candidate.Model)

	switch {
	case requestedProvider != "" && requestedModel != "" && candidateProvider == requestedProvider && candidateModel == requestedModel:
		return 0
	case requestedProvider != "" && candidateProvider == requestedProvider:
		return 1
	case requestedModel != "" && candidateModel == requestedModel:
		return 2
	default:
		return 3
	}
}

func canonicalHashDistance(requestedHash, candidateHash string) int {
	requestedHash = normalizeFixtureMissKey(requestedHash)
	candidateHash = normalizeFixtureMissKey(candidateHash)
	if requestedHash == "" || candidateHash == "" {
		if len(requestedHash) > len(candidateHash) {
			return len(requestedHash)
		}
		return len(candidateHash)
	}

	maxLen := len(requestedHash)
	if len(candidateHash) > maxLen {
		maxLen = len(candidateHash)
	}
	minLen := len(requestedHash)
	if len(candidateHash) < minLen {
		minLen = len(candidateHash)
	}
	distance := maxLen - minLen
	for i := 0; i < minLen; i++ {
		if requestedHash[i] != candidateHash[i] {
			distance++
		}
	}
	return distance
}

func readOptionalFile(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

func (s *Store) validateModelFixture(path, expectedHash string, f *ModelFixture) error {
	if strings.TrimSpace(f.CanonicalHash) == "" {
		return fmt.Errorf("model fixture %s missing canonical_hash", path)
	}
	if f.CanonicalHash != expectedHash {
		return fmt.Errorf("model fixture %s canonical_hash mismatch: file hash=%s fixture canonical_hash=%s", path, expectedHash, f.CanonicalHash)
	}
	var cr providers.CanonicalRequest
	if err := json.Unmarshal(f.CanonicalRequest, &cr); err != nil {
		return fmt.Errorf("model fixture %s malformed canonical_request: %w", path, err)
	}
	canonicalBytes, err := CanonicalizeRequest(&cr)
	if err != nil {
		return fmt.Errorf("model fixture %s canonicalization failed: %w", path, err)
	}
	recomputed := HashCanonical(canonicalBytes)
	if recomputed != expectedHash {
		return fmt.Errorf("model fixture %s hash collision or malformed canonical request: expected %s recomputed %s", path, expectedHash, recomputed)
	}
	if err := ValidateModelResponse(f.ProviderFamily, f.Response); err != nil {
		return fmt.Errorf("model fixture %s invalid response payload: %w", path, err)
	}
	if err := s.validateModelFixtureSignature(path, f); err != nil {
		return err
	}
	if err := s.validateFixtureContext(path, f.Suite, f.ScenarioSetSHA256); err != nil {
		return err
	}
	return nil
}

func (s *Store) validateToolFixture(path, requestedTool, expectedHash string, f *ToolFixture) error {
	if strings.TrimSpace(f.CanonicalHash) == "" {
		return fmt.Errorf("tool fixture %s missing canonical_hash", path)
	}
	if f.CanonicalHash != expectedHash {
		return fmt.Errorf("tool fixture %s canonical_hash mismatch: file hash=%s fixture canonical_hash=%s", path, expectedHash, f.CanonicalHash)
	}
	toolName := strings.TrimSpace(f.ToolName)
	if toolName == "" {
		toolName = strings.TrimSpace(requestedTool)
	}
	if toolName == "" {
		return fmt.Errorf("tool fixture %s missing tool_name", path)
	}
	if strings.TrimSpace(requestedTool) != "" && strings.TrimSpace(requestedTool) != toolName {
		return fmt.Errorf("tool fixture %s tool mismatch: requested=%s fixture=%s", path, requestedTool, toolName)
	}
	args := map[string]interface{}{}
	if len(f.Args) > 0 {
		if err := json.Unmarshal(f.Args, &args); err != nil {
			return fmt.Errorf("tool fixture %s malformed args: %w", path, err)
		}
	}
	canonicalBytes, err := CanonicalizeToolCall(toolName, args)
	if err != nil {
		return fmt.Errorf("tool fixture %s canonicalization failed: %w", path, err)
	}
	recomputed := HashCanonical(canonicalBytes)
	if recomputed != expectedHash {
		return fmt.Errorf("tool fixture %s hash collision or malformed canonical request: expected %s recomputed %s", path, expectedHash, recomputed)
	}
	if err := s.validateToolFixtureSignature(path, f); err != nil {
		return err
	}
	if err := s.validateFixtureContext(path, f.Suite, f.ScenarioSetSHA256); err != nil {
		return err
	}
	return nil
}

func (s *Store) validateFixtureContext(path, suite, scenarioSetSHA256 string) error {
	if s.ExpectedSuite != "" && strings.TrimSpace(suite) != "" && strings.TrimSpace(suite) != s.ExpectedSuite {
		return fmt.Errorf("fixture %s belongs to suite %q, expected %q", path, suite, s.ExpectedSuite)
	}
	if s.ExpectedScenarioSetSHA256 != "" &&
		strings.TrimSpace(scenarioSetSHA256) != "" &&
		strings.TrimSpace(scenarioSetSHA256) != s.ExpectedScenarioSetSHA256 {
		return fmt.Errorf("fixture %s scenario_set_sha256 mismatch: fixture=%s expected=%s", path, scenarioSetSHA256, s.ExpectedScenarioSetSHA256)
	}
	return nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	unlock, err := lockPath(path)
	if err != nil {
		return fmt.Errorf("failed to lock fixture path %s: %w", path, err)
	}
	defer unlock()

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		_ = tmp.Close()
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("failed to write temp file for %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp file for %s: %w", path, err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("failed to chmod temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to atomically replace %s: %w", path, err)
	}
	cleanup = false

	dirHandle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("failed to open fixture directory %s for sync: %w", dir, err)
	}
	defer dirHandle.Close()
	if err := dirHandle.Sync(); err != nil {
		return fmt.Errorf("failed to sync fixture directory %s: %w", dir, err)
	}

	return nil
}
