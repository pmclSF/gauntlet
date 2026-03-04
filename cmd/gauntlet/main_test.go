package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gauntlet-dev/gauntlet/internal/runner"
)

func TestSplitCSVFlag(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "empty",
			in:   "",
			want: []string{},
		},
		{
			name: "single",
			in:   "evals/world/tools",
			want: []string{"evals/world/tools"},
		},
		{
			name: "trim and dedupe",
			in:   " tools ,python ,tools, , python ",
			want: []string{"tools", "python"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitCSVFlag(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("splitCSVFlag(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

func TestEnsureAutoDiscoverySuite_GeneratesScenarios(t *testing.T) {
	root := t.TempDir()
	evals := filepath.Join(root, "evals")
	suiteDir := filepath.Join(evals, "smoke")
	toolsDir := filepath.Join(evals, "world", "tools")
	dbDir := filepath.Join(evals, "world", "databases")
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		t.Fatalf("mkdir suite: %v", err)
	}
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("mkdir tools: %v", err)
	}
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("mkdir db: %v", err)
	}
	if err := os.WriteFile(filepath.Join(toolsDir, "order_lookup.yaml"), []byte(`
tool: order_lookup
states:
  nominal:
    response: {status: "ok"}
  timeout:
    error: "timeout"
`), 0o644); err != nil {
		t.Fatalf("write tool: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dbDir, "orders_db.yaml"), []byte(`
database: orders_db
seed_sets:
  standard_order:
    tables: {}
`), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir root: %v", err)
	}

	res, err := ensureAutoDiscoverySuite(runner.Config{
		Suite:    "smoke",
		EvalsDir: "evals",
	}, false)
	if err != nil {
		t.Fatalf("ensureAutoDiscoverySuite: %v", err)
	}
	if res.GeneratedScenarios == 0 {
		t.Fatal("expected auto-discovery to generate scenarios")
	}
}
