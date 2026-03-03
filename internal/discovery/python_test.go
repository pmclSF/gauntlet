package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverFromPythonTools_ModuleClassAndMultipleDecorators(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "agent")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	code := `import gauntlet
from gauntlet import tool as gt

def retry(fn):
    return fn

@gauntlet.tool(name="order_lookup")
def lookup_order(order_id):
    return {"order_id": order_id}

class Billing:
    @retry
    @gauntlet.tool
    def payment_lookup(self, payment_id):
        return {"payment_id": payment_id}

@gt(name="cancel_order")
async def cancel(order_id):
    return {"ok": True}
`
	if err := os.WriteFile(filepath.Join(srcDir, "tools.py"), []byte(code), 0o644); err != nil {
		t.Fatalf("write tools.py: %v", err)
	}

	engine := NewEngine(DiscoveryConfig{
		RootDir:    root,
		PythonDirs: []string{"agent"},
	})

	proposals, err := engine.discoverFromPythonTools()
	if err != nil {
		t.Fatalf("discoverFromPythonTools: %v", err)
	}

	names := map[string]bool{}
	for _, p := range proposals {
		if p.Source != "python_tool_ast" {
			t.Fatalf("unexpected source %q", p.Source)
		}
		names[p.Tool] = true
	}
	if !names["order_lookup"] {
		t.Fatalf("missing order_lookup in proposals: %#v", proposals)
	}
	if !names["payment_lookup"] {
		t.Fatalf("missing payment_lookup in proposals: %#v", proposals)
	}
	if !names["cancel_order"] {
		t.Fatalf("missing cancel_order in proposals: %#v", proposals)
	}
}

func TestDiscoverFromPythonTools_ResolvesImportedToolModule(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "pkg")
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir pkg: %v", err)
	}
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("mkdir app: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "__init__.py"), []byte(""), 0o644); err != nil {
		t.Fatalf("write __init__: %v", err)
	}

	lib := `import gauntlet

@gauntlet.tool(name="inventory_lookup")
def lookup_inventory(sku):
    return {"sku": sku}
`
	if err := os.WriteFile(filepath.Join(pkgDir, "tool_lib.py"), []byte(lib), 0o644); err != nil {
		t.Fatalf("write tool_lib.py: %v", err)
	}

	importer := `from pkg.tool_lib import lookup_inventory

def run():
    return lookup_inventory("sku-1")
`
	if err := os.WriteFile(filepath.Join(appDir, "agent.py"), []byte(importer), 0o644); err != nil {
		t.Fatalf("write agent.py: %v", err)
	}

	engine := NewEngine(DiscoveryConfig{
		RootDir:    root,
		PythonDirs: []string{"app"},
	})

	proposals, err := engine.discoverFromPythonTools()
	if err != nil {
		t.Fatalf("discoverFromPythonTools: %v", err)
	}
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal from imported tool module, got %d (%#v)", len(proposals), proposals)
	}
	if proposals[0].Tool != "inventory_lookup" {
		t.Fatalf("tool = %q, want inventory_lookup", proposals[0].Tool)
	}
}

func TestResolveImportModule(t *testing.T) {
	tests := []struct {
		current string
		raw     string
		want    string
	}{
		{"pkg.agent", "pkg.tools", "pkg.tools"},
		{"pkg.agent", ".tools", "pkg.tools"},
		{"pkg.sub.agent", "..tools", "pkg.tools"},
	}
	for _, tt := range tests {
		if got := resolveImportModule(tt.current, tt.raw); got != tt.want {
			t.Fatalf("resolveImportModule(%q, %q) = %q, want %q", tt.current, tt.raw, got, tt.want)
		}
	}
}
