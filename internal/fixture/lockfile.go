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
)

const DefaultReplayLockfileName = "replay.lock.json"

// ReplayLockfile captures a deterministic fixture index used for replay
// integrity checks.
type ReplayLockfile struct {
	Version           int           `json:"version"`
	GeneratedAt       string        `json:"generated_at"`
	FixturesDir       string        `json:"fixtures_dir"`
	Suite             string        `json:"suite,omitempty"`
	ScenarioSetSHA256 string        `json:"scenario_set_sha256,omitempty"`
	Entries           []ReplayEntry `json:"entries"`
	IndexSHA256       string        `json:"index_sha256"`
}

// ReplayEntry is a single fixture index row.
type ReplayEntry struct {
	Path          string `json:"path"`
	FixtureType   string `json:"fixture_type"` // model | tool
	CanonicalHash string `json:"canonical_hash"`
	SHA256        string `json:"sha256"`
	Size          int64  `json:"size"`
}

// ScenarioSetDigest returns a deterministic digest for a suite's scenario names.
func ScenarioSetDigest(names []string) string {
	items := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		n := strings.TrimSpace(name)
		if n == "" {
			continue
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		items = append(items, n)
	}
	sort.Strings(items)
	sum := sha256.Sum256([]byte(strings.Join(items, "\n")))
	return hex.EncodeToString(sum[:])
}

// WriteReplayLockfile creates/updates replay.lock.json for fixtures.
func WriteReplayLockfile(store *Store, suite, scenarioSetSHA256, outPath string, now time.Time) (*ReplayLockfile, string, error) {
	if store == nil || strings.TrimSpace(store.BaseDir) == "" {
		return nil, "", fmt.Errorf("fixture store is required")
	}
	if now.IsZero() {
		now = time.Now()
	}
	lock, err := buildReplayLockfile(store, strings.TrimSpace(suite), strings.TrimSpace(scenarioSetSHA256), now.UTC())
	if err != nil {
		return nil, "", err
	}

	path := strings.TrimSpace(outPath)
	if path == "" {
		path = filepath.Join(store.BaseDir, DefaultReplayLockfileName)
	}
	path = filepath.Clean(path)
	if err := writeJSONFile(path, lock); err != nil {
		return nil, "", err
	}
	return lock, path, nil
}

// VerifyReplayLockfile validates replay.lock.json against current fixture files.
func VerifyReplayLockfile(store *Store, expectedSuite, expectedScenarioSetSHA256, path string) error {
	if store == nil || strings.TrimSpace(store.BaseDir) == "" {
		return fmt.Errorf("fixture store is required")
	}
	lockPath := strings.TrimSpace(path)
	if lockPath == "" {
		lockPath = filepath.Join(store.BaseDir, DefaultReplayLockfileName)
	}
	lockPath = filepath.Clean(lockPath)

	data, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("replay lockfile missing at %s", lockPath)
		}
		return fmt.Errorf("failed to read replay lockfile %s: %w", lockPath, err)
	}
	var lock ReplayLockfile
	if err := json.Unmarshal(data, &lock); err != nil {
		return fmt.Errorf("failed to parse replay lockfile %s: %w", lockPath, err)
	}
	if lock.Version != 1 {
		return fmt.Errorf("unsupported replay lockfile version %d in %s", lock.Version, lockPath)
	}

	expSuite := strings.TrimSpace(expectedSuite)
	if expSuite != "" && strings.TrimSpace(lock.Suite) != "" && strings.TrimSpace(lock.Suite) != expSuite {
		return fmt.Errorf("replay lockfile suite mismatch: lockfile=%q expected=%q", lock.Suite, expSuite)
	}
	expScenarioDigest := strings.TrimSpace(expectedScenarioSetSHA256)
	if expScenarioDigest != "" && strings.TrimSpace(lock.ScenarioSetSHA256) != "" && strings.TrimSpace(lock.ScenarioSetSHA256) != expScenarioDigest {
		return fmt.Errorf("replay lockfile scenario_set_sha256 mismatch: lockfile=%s expected=%s", lock.ScenarioSetSHA256, expScenarioDigest)
	}

	actual, err := buildReplayLockfile(store, strings.TrimSpace(lock.Suite), strings.TrimSpace(lock.ScenarioSetSHA256), time.Time{})
	if err != nil {
		return err
	}
	// Keep metadata from lockfile and compare deterministic index.
	actual.GeneratedAt = lock.GeneratedAt
	actual.FixturesDir = lock.FixturesDir
	if !equalReplayEntries(lock.Entries, actual.Entries) || lock.IndexSHA256 != actual.IndexSHA256 {
		return fmt.Errorf("replay lockfile integrity mismatch for %s", lockPath)
	}
	return nil
}

func buildReplayLockfile(store *Store, suite, scenarioSetSHA256 string, now time.Time) (*ReplayLockfile, error) {
	entries, err := collectReplayEntries(store, suite, scenarioSetSHA256)
	if err != nil {
		return nil, err
	}
	lock := &ReplayLockfile{
		Version:           1,
		GeneratedAt:       "",
		FixturesDir:       normalizeSlashPath(store.BaseDir),
		Suite:             suite,
		ScenarioSetSHA256: scenarioSetSHA256,
		Entries:           entries,
	}
	if !now.IsZero() {
		lock.GeneratedAt = now.Format(time.RFC3339)
	}
	indexHash, err := computeReplayIndexHash(entries)
	if err != nil {
		return nil, err
	}
	lock.IndexSHA256 = indexHash
	return lock, nil
}

func collectReplayEntries(store *Store, suite, scenarioSetSHA256 string) ([]ReplayEntry, error) {
	var entries []ReplayEntry
	collisionGuard := map[string]string{}

	modelDir := filepath.Join(store.BaseDir, "models")
	modelFiles, err := listJSONFiles(modelDir)
	if err != nil {
		return nil, err
	}
	for _, path := range modelFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read model fixture %s: %w", path, err)
		}
		var f ModelFixture
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("failed to parse model fixture %s: %w", path, err)
		}
		expectedHash := strings.TrimSuffix(filepath.Base(path), ".json")
		tmpStore := *store
		tmpStore.SetReplayContext(suite, scenarioSetSHA256)
		if err := tmpStore.validateModelFixture(path, expectedHash, &f); err != nil {
			return nil, err
		}
		d := sha256.Sum256(data)
		entry := ReplayEntry{
			Path:          relativePath(store.BaseDir, path),
			FixtureType:   "model",
			CanonicalHash: expectedHash,
			SHA256:        hex.EncodeToString(d[:]),
			Size:          int64(len(data)),
		}
		if err := recordCollision(collisionGuard, entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	toolsDir := filepath.Join(store.BaseDir, "tools")
	toolFiles, err := listJSONFiles(toolsDir)
	if err != nil {
		return nil, err
	}
	for _, path := range toolFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read tool fixture %s: %w", path, err)
		}
		var f ToolFixture
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("failed to parse tool fixture %s: %w", path, err)
		}
		expectedHash := strings.TrimSuffix(filepath.Base(path), ".json")
		requestedTool := strings.TrimSpace(f.ToolName)
		if requestedTool == "" {
			parent := filepath.Base(filepath.Dir(path))
			if parent != "tools" {
				requestedTool = parent
			}
		}
		tmpStore := *store
		tmpStore.SetReplayContext(suite, scenarioSetSHA256)
		if err := tmpStore.validateToolFixture(path, requestedTool, expectedHash, &f); err != nil {
			return nil, err
		}
		d := sha256.Sum256(data)
		entry := ReplayEntry{
			Path:          relativePath(store.BaseDir, path),
			FixtureType:   "tool",
			CanonicalHash: expectedHash,
			SHA256:        hex.EncodeToString(d[:]),
			Size:          int64(len(data)),
		}
		if err := recordCollision(collisionGuard, entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Path == entries[j].Path {
			return entries[i].FixtureType < entries[j].FixtureType
		}
		return entries[i].Path < entries[j].Path
	})
	return entries, nil
}

func listJSONFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() || filepath.Ext(d.Name()) != ".json" {
			return nil
		}
		// Ignore lockfile itself if walking base fixtures directory by mistake.
		if filepath.Base(path) == DefaultReplayLockfileName {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to scan fixture directory %s: %w", dir, err)
	}
	sort.Strings(files)
	return files, nil
}

func recordCollision(guard map[string]string, entry ReplayEntry) error {
	key := entry.FixtureType + ":" + entry.CanonicalHash
	if prevPath, ok := guard[key]; ok && prevPath != entry.Path {
		return fmt.Errorf("fixture hash collision detected for %s across %s and %s", key, prevPath, entry.Path)
	}
	guard[key] = entry.Path
	return nil
}

func computeReplayIndexHash(entries []ReplayEntry) (string, error) {
	payload, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("failed to marshal replay lockfile entries: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func equalReplayEntries(a, b []ReplayEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func normalizeSlashPath(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	return strings.ReplaceAll(clean, `\`, "/")
}
