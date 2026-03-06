package ctxversion

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// NewSnapshot: creation
// ---------------------------------------------------------------------------

func TestNewSnapshot_Creation(t *testing.T) {
	parts := map[Part]PartHash{
		PartModel:  {Hash: "abc123", Version: "gpt-4"},
		PartPrompt: {Hash: "def456"},
		PartTools:  {Hash: "ghi789"},
	}

	snap := NewSnapshot("run-1", "smoke", "order_lookup", parts)

	if snap.RunID != "run-1" {
		t.Errorf("RunID = %q, want %q", snap.RunID, "run-1")
	}
	if snap.Suite != "smoke" {
		t.Errorf("Suite = %q, want %q", snap.Suite, "smoke")
	}
	if snap.Scenario != "order_lookup" {
		t.Errorf("Scenario = %q, want %q", snap.Scenario, "order_lookup")
	}
	if snap.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
	if len(snap.Parts) != 3 {
		t.Errorf("len(Parts) = %d, want 3", len(snap.Parts))
	}
	if snap.Parts[PartModel].Hash != "abc123" {
		t.Errorf("Parts[model].Hash = %q, want %q", snap.Parts[PartModel].Hash, "abc123")
	}
	if snap.Parts[PartModel].Version != "gpt-4" {
		t.Errorf("Parts[model].Version = %q, want %q", snap.Parts[PartModel].Version, "gpt-4")
	}
}

func TestNewSnapshot_EmptyParts(t *testing.T) {
	snap := NewSnapshot("run-2", "full", "scenario1", map[Part]PartHash{})

	if snap.RunID != "run-2" {
		t.Errorf("RunID = %q, want %q", snap.RunID, "run-2")
	}
	if len(snap.Parts) != 0 {
		t.Errorf("len(Parts) = %d, want 0", len(snap.Parts))
	}
}

// ---------------------------------------------------------------------------
// Compare: detects changed/unchanged parts
// ---------------------------------------------------------------------------

func TestCompare_DetectsChangedParts(t *testing.T) {
	before := NewSnapshot("run-1", "smoke", "test", map[Part]PartHash{
		PartModel:   {Hash: "hash1"},
		PartPrompt:  {Hash: "hash2"},
		PartTools:   {Hash: "hash3"},
		PartData:    {Hash: "hash4"},
		PartPlanner: {Hash: "hash5"},
	})

	after := NewSnapshot("run-2", "smoke", "test", map[Part]PartHash{
		PartModel:   {Hash: "hash1"},         // unchanged
		PartPrompt:  {Hash: "hash2-changed"}, // changed
		PartTools:   {Hash: "hash3"},          // unchanged
		PartData:    {Hash: "hash4-changed"},  // changed
		PartPlanner: {Hash: "hash5"},          // unchanged
	})

	diffs := Compare(before, after)

	if len(diffs) != 5 {
		t.Fatalf("expected 5 diffs (one per part), got %d", len(diffs))
	}

	expected := map[Part]bool{
		PartModel:   false,
		PartPrompt:  true,
		PartTools:   false,
		PartData:    true,
		PartPlanner: false,
	}

	for _, d := range diffs {
		wantChanged, ok := expected[d.Part]
		if !ok {
			t.Errorf("unexpected part: %q", d.Part)
			continue
		}
		if d.Changed != wantChanged {
			t.Errorf("diff for %q: Changed = %v, want %v", d.Part, d.Changed, wantChanged)
		}
	}
}

func TestCompare_AllUnchanged(t *testing.T) {
	parts := map[Part]PartHash{
		PartModel:   {Hash: "aaa"},
		PartPrompt:  {Hash: "bbb"},
		PartTools:   {Hash: "ccc"},
		PartData:    {Hash: "ddd"},
		PartPlanner: {Hash: "eee"},
	}

	before := NewSnapshot("run-1", "smoke", "test", parts)
	after := NewSnapshot("run-2", "smoke", "test", parts)

	diffs := Compare(before, after)
	for _, d := range diffs {
		if d.Changed {
			t.Errorf("part %q should not be changed", d.Part)
		}
	}
}

func TestCompare_MissingPartsInBefore(t *testing.T) {
	before := NewSnapshot("run-1", "smoke", "test", map[Part]PartHash{})
	after := NewSnapshot("run-2", "smoke", "test", map[Part]PartHash{
		PartModel: {Hash: "hash1"},
	})

	diffs := Compare(before, after)
	for _, d := range diffs {
		if d.Part == PartModel {
			if !d.Changed {
				t.Error("model should be changed when missing from before")
			}
			if d.Before != "" {
				t.Errorf("Before = %q, want empty", d.Before)
			}
			if d.After != "hash1" {
				t.Errorf("After = %q, want %q", d.After, "hash1")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// ChangedParts: returns only changed parts
// ---------------------------------------------------------------------------

func TestChangedParts_ReturnsOnlyChanged(t *testing.T) {
	before := NewSnapshot("run-1", "smoke", "test", map[Part]PartHash{
		PartModel:   {Hash: "h1"},
		PartPrompt:  {Hash: "h2"},
		PartTools:   {Hash: "h3"},
		PartData:    {Hash: "h4"},
		PartPlanner: {Hash: "h5"},
	})

	after := NewSnapshot("run-2", "smoke", "test", map[Part]PartHash{
		PartModel:   {Hash: "h1"},
		PartPrompt:  {Hash: "h2-new"},
		PartTools:   {Hash: "h3"},
		PartData:    {Hash: "h4"},
		PartPlanner: {Hash: "h5-new"},
	})

	changed := ChangedParts(before, after)

	if len(changed) != 2 {
		t.Fatalf("expected 2 changed parts, got %d: %v", len(changed), changed)
	}

	changedSet := map[Part]bool{}
	for _, p := range changed {
		changedSet[p] = true
	}

	if !changedSet[PartPrompt] {
		t.Error("expected prompt to be in changed parts")
	}
	if !changedSet[PartPlanner] {
		t.Error("expected planner to be in changed parts")
	}
}

func TestChangedParts_NoneChanged(t *testing.T) {
	parts := map[Part]PartHash{
		PartModel: {Hash: "same"},
	}
	before := NewSnapshot("r1", "s", "sc", parts)
	after := NewSnapshot("r2", "s", "sc", parts)

	changed := ChangedParts(before, after)
	// Parts not present in either snapshot have empty hash on both sides -> unchanged.
	// Only PartModel is present and identical -> unchanged.
	for _, p := range changed {
		t.Errorf("unexpected changed part: %q", p)
	}
}

// ---------------------------------------------------------------------------
// HashContent: deterministic
// ---------------------------------------------------------------------------

func TestHashContent_Deterministic(t *testing.T) {
	content := []byte("hello world")

	h1 := HashContent(content)
	h2 := HashContent(content)

	if h1 != h2 {
		t.Errorf("HashContent not deterministic: %q != %q", h1, h2)
	}

	// SHA-256 of "hello world" is well-known.
	expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if h1 != expected {
		t.Errorf("HashContent = %q, want %q", h1, expected)
	}
}

func TestHashContent_DifferentInputsDifferentHashes(t *testing.T) {
	h1 := HashContent([]byte("input A"))
	h2 := HashContent([]byte("input B"))

	if h1 == h2 {
		t.Error("different inputs should produce different hashes")
	}
}

func TestHashContent_EmptyInput(t *testing.T) {
	h := HashContent([]byte{})
	if h == "" {
		t.Error("hash of empty input should not be empty")
	}
	// SHA-256 of empty string.
	expected := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if h != expected {
		t.Errorf("HashContent(empty) = %q, want %q", h, expected)
	}
}

// ---------------------------------------------------------------------------
// Save and LoadSnapshot: round-trip
// ---------------------------------------------------------------------------

func TestSaveAndLoadSnapshot_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	original := NewSnapshot("run-42", "smoke", "order_test", map[Part]PartHash{
		PartModel:   {Hash: "model-hash", Version: "gpt-4", Detail: "model detail"},
		PartPrompt:  {Hash: "prompt-hash"},
		PartTools:   {Hash: "tools-hash", Version: "v2"},
		PartData:    {Hash: "data-hash"},
		PartPlanner: {Hash: "planner-hash"},
	})

	if err := original.Save(tmpDir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(tmpDir, "smoke_order_test.json")
	loaded, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}

	if loaded.RunID != original.RunID {
		t.Errorf("RunID = %q, want %q", loaded.RunID, original.RunID)
	}
	if loaded.Suite != original.Suite {
		t.Errorf("Suite = %q, want %q", loaded.Suite, original.Suite)
	}
	if loaded.Scenario != original.Scenario {
		t.Errorf("Scenario = %q, want %q", loaded.Scenario, original.Scenario)
	}

	for _, part := range AllParts() {
		origHash := original.Parts[part]
		loadedHash := loaded.Parts[part]
		if origHash.Hash != loadedHash.Hash {
			t.Errorf("Parts[%s].Hash = %q, want %q", part, loadedHash.Hash, origHash.Hash)
		}
		if origHash.Version != loadedHash.Version {
			t.Errorf("Parts[%s].Version = %q, want %q", part, loadedHash.Version, origHash.Version)
		}
		if origHash.Detail != loadedHash.Detail {
			t.Errorf("Parts[%s].Detail = %q, want %q", part, loadedHash.Detail, origHash.Detail)
		}
	}
}

func TestSave_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "nested", "deep")

	snap := NewSnapshot("run-1", "smoke", "test", map[Part]PartHash{})
	if err := snap.Save(subDir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(subDir, "smoke_test.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected file %s to exist", path)
	}
}

func TestLoadSnapshot_FileNotFound(t *testing.T) {
	_, err := LoadSnapshot("/nonexistent/path/snapshot.json")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestAllParts(t *testing.T) {
	parts := AllParts()
	if len(parts) != 5 {
		t.Errorf("AllParts returned %d parts, want 5", len(parts))
	}

	expected := map[Part]bool{
		PartModel: true, PartPrompt: true, PartTools: true,
		PartData: true, PartPlanner: true,
	}
	for _, p := range parts {
		if !expected[p] {
			t.Errorf("unexpected part: %q", p)
		}
	}
}
