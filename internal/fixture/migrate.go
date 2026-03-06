package fixture

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pmclSF/gauntlet/internal/proxy/providers"
)

// MigrationOptions controls fixture hash migration behavior.
type MigrationOptions struct {
	FromVersion    int
	ToVersion      int
	DryRun         bool
	ReportPath     string
	ManifestPath   string
	SigningKeyPath string
	Now            func() time.Time
}

// MigrationSummary captures high-level migration counts.
type MigrationSummary struct {
	Scanned        int `json:"scanned"`
	Eligible       int `json:"eligible"`
	Migrated       int `json:"migrated"`
	Rehashed       int `json:"rehashed"`
	VersionOnly    int `json:"version_only"`
	SkippedVersion int `json:"skipped_version"`
}

// MigrationEntry is a per-fixture migration action.
type MigrationEntry struct {
	FixtureType string `json:"fixture_type"` // model | tool
	SourcePath  string `json:"source_path"`
	TargetPath  string `json:"target_path"`
	OldHash     string `json:"old_hash"`
	NewHash     string `json:"new_hash"`
	OldVersion  int    `json:"old_version"`
	NewVersion  int    `json:"new_version"`
	Action      string `json:"action"` // rehash | version_only
}

// MigrationReport is the dry-run/apply migration report artifact.
type MigrationReport struct {
	GeneratedAt  string           `json:"generated_at"`
	StoreBaseDir string           `json:"store_base_dir"`
	FromVersion  int              `json:"from_version"`
	ToVersion    int              `json:"to_version"`
	DryRun       bool             `json:"dry_run"`
	Summary      MigrationSummary `json:"summary"`
	Entries      []MigrationEntry `json:"entries"`
	ReportPath   string           `json:"report_path,omitempty"`
	ManifestPath string           `json:"manifest_path,omitempty"`
}

// ManifestSignature stores a cryptographic signature over the manifest payload.
type ManifestSignature struct {
	Algorithm      string `json:"algorithm"`
	KeyPath        string `json:"key_path"`
	KeyFingerprint string `json:"key_fingerprint"`
	PublicKeyPEM   string `json:"public_key_pem"`
	PayloadSHA256  string `json:"payload_sha256"`
	Value          string `json:"value"`
}

// MigrationManifest is the signed migration manifest artifact.
type MigrationManifest struct {
	Version      int               `json:"version"`
	GeneratedAt  string            `json:"generated_at"`
	StoreBaseDir string            `json:"store_base_dir"`
	FromVersion  int               `json:"from_version"`
	ToVersion    int               `json:"to_version"`
	DryRun       bool              `json:"dry_run"`
	Summary      MigrationSummary  `json:"summary"`
	Entries      []MigrationEntry  `json:"entries"`
	Signature    ManifestSignature `json:"signature"`
}

type migrationPayload struct {
	Version      int              `json:"version"`
	GeneratedAt  string           `json:"generated_at"`
	StoreBaseDir string           `json:"store_base_dir"`
	FromVersion  int              `json:"from_version"`
	ToVersion    int              `json:"to_version"`
	DryRun       bool             `json:"dry_run"`
	Summary      MigrationSummary `json:"summary"`
	Entries      []MigrationEntry `json:"entries"`
}

type plannedWrite struct {
	sourcePath string
	targetPath string
	content    []byte
}

// MigrateFixtures recomputes fixture hashes and updates hash versions.
// It can run in dry-run mode and always emits a report and signed manifest.
func MigrateFixtures(store *Store, opts MigrationOptions) (*MigrationReport, error) {
	if store == nil || strings.TrimSpace(store.BaseDir) == "" {
		return nil, fmt.Errorf("fixture store is required")
	}
	if opts.FromVersion <= 0 || opts.ToVersion <= 0 {
		return nil, fmt.Errorf("hash versions must be positive (from=%d, to=%d)", opts.FromVersion, opts.ToVersion)
	}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn().UTC()

	report := &MigrationReport{
		GeneratedAt:  now.Format(time.RFC3339),
		StoreBaseDir: filepath.Clean(store.BaseDir),
		FromVersion:  opts.FromVersion,
		ToVersion:    opts.ToVersion,
		DryRun:       opts.DryRun,
	}

	var writes []plannedWrite
	var err error
	reportPath, manifestPath, keyPath := resolveMigrationArtifactPaths(store, opts, now)
	report.ReportPath = reportPath
	report.ManifestPath = manifestPath

	writes, err = appendModelMigrations(store, opts, report, writes, keyPath)
	if err != nil {
		return nil, err
	}
	writes, err = appendToolMigrations(store, opts, report, writes, keyPath)
	if err != nil {
		return nil, err
	}
	sort.Slice(report.Entries, func(i, j int) bool {
		if report.Entries[i].FixtureType == report.Entries[j].FixtureType {
			return report.Entries[i].SourcePath < report.Entries[j].SourcePath
		}
		return report.Entries[i].FixtureType < report.Entries[j].FixtureType
	})

	if err := validateTargetConflicts(writes); err != nil {
		return nil, err
	}

	if !opts.DryRun {
		if err := applyWrites(writes); err != nil {
			return nil, err
		}
	}

	manifest, err := buildSignedManifest(report, keyPath)
	if err != nil {
		return nil, err
	}
	if err := writeJSONFile(reportPath, report); err != nil {
		return nil, err
	}
	if err := writeJSONFile(manifestPath, manifest); err != nil {
		return nil, err
	}

	return report, nil
}

func appendModelMigrations(store *Store, opts MigrationOptions, report *MigrationReport, writes []plannedWrite, signingKeyPath string) ([]plannedWrite, error) {
	modelDir := filepath.Join(store.BaseDir, "models")
	entries, err := os.ReadDir(modelDir)
	if err != nil {
		if os.IsNotExist(err) {
			return writes, nil
		}
		return nil, fmt.Errorf("failed to read model fixtures directory %s: %w", modelDir, err)
	}
	for _, ent := range entries {
		if ent.IsDir() || filepath.Ext(ent.Name()) != ".json" {
			continue
		}
		path := filepath.Join(modelDir, ent.Name())
		report.Summary.Scanned++

		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read model fixture %s: %w", path, err)
		}
		var f ModelFixture
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("failed to parse model fixture %s: %w", path, err)
		}
		if f.HashVersion != opts.FromVersion {
			report.Summary.SkippedVersion++
			continue
		}
		report.Summary.Eligible++

		newHash, canonicalBytes, err := recomputeModelFixtureHash(path, &f)
		if err != nil {
			return nil, err
		}
		targetPath := filepath.Join(modelDir, newHash+".json")

		entry := MigrationEntry{
			FixtureType: "model",
			SourcePath:  relativePath(store.BaseDir, path),
			TargetPath:  relativePath(store.BaseDir, targetPath),
			OldHash:     f.CanonicalHash,
			NewHash:     newHash,
			OldVersion:  f.HashVersion,
			NewVersion:  opts.ToVersion,
			Action:      "version_only",
		}
		if f.CanonicalHash != newHash || filepath.Base(path) != newHash+".json" {
			entry.Action = "rehash"
		}
		report.Entries = append(report.Entries, entry)
		report.Summary.Migrated++
		if entry.Action == "rehash" {
			report.Summary.Rehashed++
		} else {
			report.Summary.VersionOnly++
		}

		f.HashVersion = opts.ToVersion
		f.CanonicalHash = newHash
		f.FixtureID = newHash
		f.CanonicalRequest = canonicalBytes
		if f.Provenance == nil {
			f.Provenance = BuildProvenance(nil, "migrate_fixtures")
		}
		if err := signModelFixtureWithKeyPath(&f, signingKeyPath); err != nil {
			return nil, fmt.Errorf("failed to sign migrated model fixture %s: %w", path, err)
		}
		content, err := json.MarshalIndent(&f, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to marshal migrated model fixture %s: %w", path, err)
		}
		writes = append(writes, plannedWrite{
			sourcePath: path,
			targetPath: targetPath,
			content:    content,
		})
	}
	return writes, nil
}

func appendToolMigrations(store *Store, opts MigrationOptions, report *MigrationReport, writes []plannedWrite, signingKeyPath string) ([]plannedWrite, error) {
	toolsDir := filepath.Join(store.BaseDir, "tools")
	var paths []string
	err := filepath.WalkDir(toolsDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || filepath.Ext(d.Name()) != ".json" {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return writes, nil
		}
		return nil, fmt.Errorf("failed to walk tool fixtures directory %s: %w", toolsDir, err)
	}
	sort.Strings(paths)

	for _, path := range paths {
		report.Summary.Scanned++

		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read tool fixture %s: %w", path, err)
		}
		var f ToolFixture
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("failed to parse tool fixture %s: %w", path, err)
		}
		if f.HashVersion != opts.FromVersion {
			report.Summary.SkippedVersion++
			continue
		}
		report.Summary.Eligible++

		toolName := strings.TrimSpace(f.ToolName)
		if toolName == "" {
			parent := filepath.Base(filepath.Dir(path))
			if parent != "tools" {
				toolName = parent
			}
		}
		if toolName == "" {
			return nil, fmt.Errorf("tool fixture %s missing tool_name and cannot infer from path", path)
		}
		newHash, err := recomputeToolFixtureHash(path, toolName, f.Args)
		if err != nil {
			return nil, err
		}
		targetPath := filepath.Join(filepath.Dir(path), newHash+".json")

		entry := MigrationEntry{
			FixtureType: "tool",
			SourcePath:  relativePath(store.BaseDir, path),
			TargetPath:  relativePath(store.BaseDir, targetPath),
			OldHash:     f.CanonicalHash,
			NewHash:     newHash,
			OldVersion:  f.HashVersion,
			NewVersion:  opts.ToVersion,
			Action:      "version_only",
		}
		if f.CanonicalHash != newHash || filepath.Base(path) != newHash+".json" {
			entry.Action = "rehash"
		}
		report.Entries = append(report.Entries, entry)
		report.Summary.Migrated++
		if entry.Action == "rehash" {
			report.Summary.Rehashed++
		} else {
			report.Summary.VersionOnly++
		}

		f.ToolName = toolName
		f.HashVersion = opts.ToVersion
		f.CanonicalHash = newHash
		f.FixtureID = newHash
		f.ArgsHash = newHash
		if f.Provenance == nil {
			f.Provenance = BuildProvenance(nil, "migrate_fixtures")
		}
		if err := signToolFixtureWithKeyPath(&f, signingKeyPath); err != nil {
			return nil, fmt.Errorf("failed to sign migrated tool fixture %s: %w", path, err)
		}
		content, err := json.MarshalIndent(&f, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to marshal migrated tool fixture %s: %w", path, err)
		}
		writes = append(writes, plannedWrite{
			sourcePath: path,
			targetPath: targetPath,
			content:    content,
		})
	}
	return writes, nil
}

func recomputeModelFixtureHash(path string, f *ModelFixture) (string, []byte, error) {
	if len(f.CanonicalRequest) == 0 {
		return "", nil, fmt.Errorf("model fixture %s missing canonical_request", path)
	}
	var cr providers.CanonicalRequest
	if err := json.Unmarshal(f.CanonicalRequest, &cr); err != nil {
		return "", nil, fmt.Errorf("model fixture %s has malformed canonical_request: %w", path, err)
	}
	canonicalBytes, err := CanonicalizeRequest(&cr)
	if err != nil {
		return "", nil, fmt.Errorf("model fixture %s canonicalization failed: %w", path, err)
	}
	return HashCanonical(canonicalBytes), canonicalBytes, nil
}

func recomputeToolFixtureHash(path, toolName string, argsRaw json.RawMessage) (string, error) {
	args := map[string]interface{}{}
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			return "", fmt.Errorf("tool fixture %s has malformed args: %w", path, err)
		}
	}
	canonicalBytes, err := CanonicalizeToolCall(toolName, args)
	if err != nil {
		return "", fmt.Errorf("tool fixture %s canonicalization failed: %w", path, err)
	}
	return HashCanonical(canonicalBytes), nil
}

func validateTargetConflicts(writes []plannedWrite) error {
	targetToDigest := make(map[string]string, len(writes))
	sourceSet := make(map[string]bool, len(writes))
	for _, w := range writes {
		sourceSet[filepath.Clean(w.sourcePath)] = true
	}

	for _, w := range writes {
		target := filepath.Clean(w.targetPath)
		d := sha256.Sum256(w.content)
		digest := hex.EncodeToString(d[:])
		if prev, ok := targetToDigest[target]; ok && prev != digest {
			return fmt.Errorf("migration collision: multiple fixtures map to %s with different content", target)
		}
		targetToDigest[target] = digest
	}

	for _, w := range writes {
		target := filepath.Clean(w.targetPath)
		if sourceSet[target] {
			continue
		}
		existing, err := os.ReadFile(target)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("failed to inspect target fixture %s: %w", target, err)
		}
		d := sha256.Sum256(existing)
		existingDigest := hex.EncodeToString(d[:])
		if targetToDigest[target] != existingDigest {
			return fmt.Errorf("migration collision: target fixture %s already exists with different content", target)
		}
	}
	return nil
}

func applyWrites(writes []plannedWrite) error {
	if len(writes) == 0 {
		return nil
	}
	sort.Slice(writes, func(i, j int) bool {
		return writes[i].targetPath < writes[j].targetPath
	})
	targetSet := make(map[string]bool, len(writes))
	for _, w := range writes {
		targetSet[filepath.Clean(w.targetPath)] = true
	}

	for _, w := range writes {
		dir := filepath.Dir(w.targetPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create fixture directory %s: %w", dir, err)
		}
		if err := os.WriteFile(w.targetPath, w.content, 0o644); err != nil {
			return fmt.Errorf("failed to write migrated fixture %s: %w", w.targetPath, err)
		}
	}

	sort.Slice(writes, func(i, j int) bool {
		return writes[i].sourcePath < writes[j].sourcePath
	})
	for _, w := range writes {
		source := filepath.Clean(w.sourcePath)
		target := filepath.Clean(w.targetPath)
		if source == target || targetSet[source] {
			continue
		}
		if err := os.Remove(source); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove old fixture %s: %w", source, err)
		}
	}
	return nil
}

func resolveMigrationArtifactPaths(store *Store, opts MigrationOptions, now time.Time) (reportPath, manifestPath, keyPath string) {
	modeTag := "apply"
	if opts.DryRun {
		modeTag = "dry-run"
	}
	timestamp := now.Format("20060102-150405")
	migrationDir := filepath.Join(store.BaseDir, "migrations")

	reportPath = strings.TrimSpace(opts.ReportPath)
	if reportPath == "" {
		reportPath = filepath.Join(migrationDir, fmt.Sprintf("fixture-migration-v%d-to-v%d-%s-%s-report.json", opts.FromVersion, opts.ToVersion, modeTag, timestamp))
	}

	manifestPath = strings.TrimSpace(opts.ManifestPath)
	if manifestPath == "" {
		manifestPath = filepath.Join(migrationDir, fmt.Sprintf("fixture-migration-v%d-to-v%d-%s-%s-manifest.json", opts.FromVersion, opts.ToVersion, modeTag, timestamp))
	}

	keyPath = strings.TrimSpace(opts.SigningKeyPath)
	if keyPath == "" {
		keyPath = filepath.Join(filepath.Dir(store.BaseDir), ".gauntlet", "fixture-signing-key.pem")
	}

	return filepath.Clean(reportPath), filepath.Clean(manifestPath), filepath.Clean(keyPath)
}

func buildSignedManifest(report *MigrationReport, signingKeyPath string) (*MigrationManifest, error) {
	payload := migrationPayload{
		Version:      1,
		GeneratedAt:  report.GeneratedAt,
		StoreBaseDir: report.StoreBaseDir,
		FromVersion:  report.FromVersion,
		ToVersion:    report.ToVersion,
		DryRun:       report.DryRun,
		Summary:      report.Summary,
		Entries:      report.Entries,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal manifest payload: %w", err)
	}
	priv, pubPEM, err := loadOrCreateSigningKey(signingKeyPath)
	if err != nil {
		return nil, err
	}
	signature := ed25519.Sign(priv, payloadJSON)
	pub := priv.Public().(ed25519.PublicKey)
	pubHash := sha256.Sum256(pub)
	payloadHash := sha256.Sum256(payloadJSON)

	return &MigrationManifest{
		Version:      payload.Version,
		GeneratedAt:  payload.GeneratedAt,
		StoreBaseDir: payload.StoreBaseDir,
		FromVersion:  payload.FromVersion,
		ToVersion:    payload.ToVersion,
		DryRun:       payload.DryRun,
		Summary:      payload.Summary,
		Entries:      payload.Entries,
		Signature: ManifestSignature{
			Algorithm:      "ed25519",
			KeyPath:        filepath.Clean(signingKeyPath),
			KeyFingerprint: hex.EncodeToString(pubHash[:]),
			PublicKeyPEM:   string(pubPEM),
			PayloadSHA256:  hex.EncodeToString(payloadHash[:]),
			Value:          base64.StdEncoding.EncodeToString(signature),
		},
	}, nil
}

func loadOrCreateSigningKey(path string) (ed25519.PrivateKey, []byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		priv, pubPEM, err := parseSigningKey(data)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse signing key %s: %w", path, err)
		}
		return priv, pubPEM, nil
	}
	if !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("failed to read signing key %s: %w", path, err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate signing key: %w", err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal signing key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})

	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal signing public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, fmt.Errorf("failed to create signing key directory: %w", err)
	}
	if err := os.WriteFile(path, privPEM, 0o600); err != nil {
		return nil, nil, fmt.Errorf("failed to write signing key %s: %w", path, err)
	}
	pubPath := path + ".pub.pem"
	if err := os.WriteFile(pubPath, pubPEM, 0o644); err != nil {
		return nil, nil, fmt.Errorf("failed to write signing public key %s: %w", pubPath, err)
	}
	return priv, pubPEM, nil
}

func parseSigningKey(data []byte) (ed25519.PrivateKey, []byte, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, nil, fmt.Errorf("invalid PEM")
	}
	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, nil, err
	}
	priv, ok := keyAny.(ed25519.PrivateKey)
	if !ok {
		return nil, nil, fmt.Errorf("key is not ed25519 private key")
	}
	pub := priv.Public().(ed25519.PublicKey)
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	return priv, pubPEM, nil
}

func writeJSONFile(path string, value interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", path, err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

func relativePath(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(path))
	}
	return filepath.ToSlash(filepath.Clean(rel))
}
