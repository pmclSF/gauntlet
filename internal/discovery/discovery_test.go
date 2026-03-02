package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Engine.Discover: finds tool variants from YAML files
// ---------------------------------------------------------------------------

func TestEngine_Discover_FindsToolVariants(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a tool directory with a YAML tool definition.
	toolDir := filepath.Join(tmpDir, "tools")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatalf("mkdir tools: %v", err)
	}

	toolYAML := `states:
  happy_path:
    description: "normal case"
  error_case:
    description: "error scenario"
`
	if err := os.WriteFile(filepath.Join(toolDir, "order_lookup.yaml"), []byte(toolYAML), 0o644); err != nil {
		t.Fatalf("write tool yaml: %v", err)
	}

	engine := NewEngine(DiscoveryConfig{
		RootDir:  tmpDir,
		ToolDirs: []string{"tools"},
	})

	proposals, err := engine.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(proposals) != 2 {
		t.Fatalf("expected 2 proposals, got %d", len(proposals))
	}

	// Check that both variants are present.
	variants := map[string]bool{}
	for _, p := range proposals {
		variants[p.Variant] = true
		if p.Tool != "order_lookup" {
			t.Errorf("proposal.Tool = %q, want %q", p.Tool, "order_lookup")
		}
		if p.Status != "pending" {
			t.Errorf("proposal.Status = %q, want %q", p.Status, "pending")
		}
		if p.Source != "tool_definition" {
			t.Errorf("proposal.Source = %q, want %q", p.Source, "tool_definition")
		}
	}

	if !variants["happy_path"] {
		t.Error("expected variant happy_path")
	}
	if !variants["error_case"] {
		t.Error("expected variant error_case")
	}
}

func TestEngine_Discover_WithDBSchemas(t *testing.T) {
	tmpDir := t.TempDir()

	// Create DB schema directory.
	dbDir := filepath.Join(tmpDir, "schemas")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("mkdir schemas: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dbDir, "orders.yaml"), []byte("tables:\n  orders:\n    id: int"), 0o644); err != nil {
		t.Fatalf("write db schema: %v", err)
	}

	engine := NewEngine(DiscoveryConfig{
		RootDir:     tmpDir,
		DBSchemaDir: "schemas",
	})

	proposals, err := engine.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals))
	}

	p := proposals[0]
	if p.Source != "db_schema" {
		t.Errorf("proposal.Source = %q, want %q", p.Source, "db_schema")
	}
}

func TestEngine_Discover_ExcludedToolIsSkipped(t *testing.T) {
	tmpDir := t.TempDir()

	toolDir := filepath.Join(tmpDir, "tools")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatalf("mkdir tools: %v", err)
	}

	toolYAML := `states:
  happy_path:
    description: "normal case"
`
	if err := os.WriteFile(filepath.Join(toolDir, "order_lookup.yaml"), []byte(toolYAML), 0o644); err != nil {
		t.Fatalf("write tool yaml: %v", err)
	}

	engine := NewEngine(DiscoveryConfig{
		RootDir:      tmpDir,
		ToolDirs:     []string{"tools"},
		ExcludeTools: []string{"order_lookup"},
	})

	proposals, err := engine.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(proposals) != 0 {
		t.Errorf("expected 0 proposals when tool is excluded, got %d", len(proposals))
	}
}

func TestEngine_Discover_MissingToolDirIsSkipped(t *testing.T) {
	tmpDir := t.TempDir()

	engine := NewEngine(DiscoveryConfig{
		RootDir:  tmpDir,
		ToolDirs: []string{"nonexistent"},
	})

	proposals, err := engine.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(proposals) != 0 {
		t.Errorf("expected 0 proposals for missing dir, got %d", len(proposals))
	}
}

// ---------------------------------------------------------------------------
// SaveProposals and LoadProposals: round-trip
// ---------------------------------------------------------------------------

func TestSaveAndLoadProposals_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "proposals.yaml")

	original := []Proposal{
		{
			ID:          "disc-tool1-happy",
			Name:        "tool1_happy",
			Description: "Test tool1 in happy state",
			Tool:        "tool1",
			Variant:     "happy",
			Tags:        []string{"auto-discovered", "tool-variant"},
			Status:      "pending",
			Source:      "tool_definition",
		},
		{
			ID:          "disc-tool2-error",
			Name:        "tool2_error",
			Description: "Test tool2 in error state",
			Tool:        "tool2",
			Variant:     "error",
			Tags:        []string{"auto-discovered"},
			Status:      "approved",
			Source:      "tool_definition",
		},
	}

	if err := SaveProposals(original, path); err != nil {
		t.Fatalf("SaveProposals: %v", err)
	}

	loaded, err := LoadProposals(path)
	if err != nil {
		t.Fatalf("LoadProposals: %v", err)
	}

	if len(loaded) != len(original) {
		t.Fatalf("loaded %d proposals, want %d", len(loaded), len(original))
	}

	for i := range original {
		if loaded[i].ID != original[i].ID {
			t.Errorf("proposal[%d].ID = %q, want %q", i, loaded[i].ID, original[i].ID)
		}
		if loaded[i].Name != original[i].Name {
			t.Errorf("proposal[%d].Name = %q, want %q", i, loaded[i].Name, original[i].Name)
		}
		if loaded[i].Tool != original[i].Tool {
			t.Errorf("proposal[%d].Tool = %q, want %q", i, loaded[i].Tool, original[i].Tool)
		}
		if loaded[i].Variant != original[i].Variant {
			t.Errorf("proposal[%d].Variant = %q, want %q", i, loaded[i].Variant, original[i].Variant)
		}
		if loaded[i].Status != original[i].Status {
			t.Errorf("proposal[%d].Status = %q, want %q", i, loaded[i].Status, original[i].Status)
		}
		if loaded[i].Source != original[i].Source {
			t.Errorf("proposal[%d].Source = %q, want %q", i, loaded[i].Source, original[i].Source)
		}
		if loaded[i].Description != original[i].Description {
			t.Errorf("proposal[%d].Description = %q, want %q", i, loaded[i].Description, original[i].Description)
		}
	}
}

func TestLoadProposals_FileNotFound(t *testing.T) {
	_, err := LoadProposals("/nonexistent/path/proposals.yaml")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestSaveProposals_EmptyList(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "empty.yaml")

	if err := SaveProposals([]Proposal{}, path); err != nil {
		t.Fatalf("SaveProposals: %v", err)
	}

	loaded, err := LoadProposals(path)
	if err != nil {
		t.Fatalf("LoadProposals: %v", err)
	}

	if len(loaded) != 0 {
		t.Errorf("expected 0 proposals, got %d", len(loaded))
	}
}

// ---------------------------------------------------------------------------
// isExcluded: filters correctly
// ---------------------------------------------------------------------------

func TestIsExcluded(t *testing.T) {
	engine := NewEngine(DiscoveryConfig{
		ExcludeTools: []string{"tool_a", "tool_b"},
	})

	tests := []struct {
		toolName string
		want     bool
	}{
		{"tool_a", true},
		{"tool_b", true},
		{"tool_c", false},
		{"", false},
		{"TOOL_A", false}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			got := engine.isExcluded(tt.toolName)
			if got != tt.want {
				t.Errorf("isExcluded(%q) = %v, want %v", tt.toolName, got, tt.want)
			}
		})
	}
}

func TestIsExcluded_EmptyList(t *testing.T) {
	engine := NewEngine(DiscoveryConfig{})

	if engine.isExcluded("anything") {
		t.Error("isExcluded should return false when exclude list is empty")
	}
}
