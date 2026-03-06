package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pmclSF/gauntlet/internal/discovery"
)

func TestGenerate_NoProposals(t *testing.T) {
	_, err := Generate(Config{})
	if err == nil || !strings.Contains(err.Error(), "no proposals") {
		t.Fatalf("expected 'no proposals' error, got: %v", err)
	}
}

func TestGenerate_NoToolProposals(t *testing.T) {
	// Proposals with no Tool field should be rejected
	_, err := Generate(Config{
		Proposals: []discovery.Proposal{
			{ID: "db_1", Name: "db_test", Description: "db only", Status: "pending", Source: "test", Database: "orders", SeedSet: "default"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "no tool proposals") {
		t.Fatalf("expected 'no tool proposals' error, got: %v", err)
	}
}

func TestGenerate_FallbackSignatures(t *testing.T) {
	tmpDir := t.TempDir()

	proposals := []discovery.Proposal{
		{ID: "t1", Name: "get_weather_nominal", Description: "test", Status: "pending", Source: "test", Tool: "get_weather", Variant: "nominal", Framework: "openai-agents"},
		{ID: "t2", Name: "lookup_order_nominal", Description: "test", Status: "pending", Source: "test", Tool: "lookup_order", Variant: "nominal"},
	}

	result, err := Generate(Config{
		RootDir:   tmpDir,
		EvalsDir:  filepath.Join(tmpDir, "evals"),
		Proposals: proposals,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Verify wrapper was created
	if result.WrapperPath == "" {
		t.Fatal("expected WrapperPath to be set")
	}
	wrapperContent, err := os.ReadFile(result.WrapperPath)
	if err != nil {
		t.Fatalf("failed to read wrapper: %v", err)
	}
	wrapper := string(wrapperContent)
	if !strings.Contains(wrapper, "@gauntlet.tool") {
		t.Error("wrapper should contain @gauntlet.tool decorator")
	}
	if !strings.Contains(wrapper, "get_weather") {
		t.Error("wrapper should contain get_weather tool")
	}
	if !strings.Contains(wrapper, "lookup_order") {
		t.Error("wrapper should contain lookup_order tool")
	}

	// Verify adapter was created
	if result.AdapterPath == "" {
		t.Fatal("expected AdapterPath to be set")
	}
	adapterContent, err := os.ReadFile(result.AdapterPath)
	if err != nil {
		t.Fatalf("failed to read adapter: %v", err)
	}
	adapter := string(adapterContent)
	if !strings.Contains(adapter, "gauntlet.connect()") {
		t.Error("adapter should contain gauntlet.connect()")
	}
	if !strings.Contains(adapter, "from gauntlet_tools import") {
		t.Error("adapter should import from gauntlet_tools")
	}

	// Verify world definitions were created
	if len(result.WorldFiles) != 2 {
		t.Fatalf("expected 2 world files, got %d", len(result.WorldFiles))
	}
	for _, wf := range result.WorldFiles {
		if _, err := os.Stat(wf); os.IsNotExist(err) {
			t.Errorf("world file does not exist: %s", wf)
		}
	}
}

func TestGenerate_SkipsExistingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Pre-create wrapper file
	wrapperPath := filepath.Join(tmpDir, "gauntlet_tools.py")
	if err := os.WriteFile(wrapperPath, []byte("# existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	proposals := []discovery.Proposal{
		{ID: "t1", Name: "test_nominal", Description: "test", Status: "pending", Source: "test", Tool: "test_tool", Variant: "nominal"},
	}

	result, err := Generate(Config{
		RootDir:   tmpDir,
		EvalsDir:  filepath.Join(tmpDir, "evals"),
		Proposals: proposals,
		Overwrite: false,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Wrapper should be skipped
	if result.WrapperPath != "" {
		t.Error("expected WrapperPath to be empty (skipped)")
	}
	if len(result.SkippedFiles) == 0 {
		t.Error("expected skipped files to be non-empty")
	}

	// Verify the existing file wasn't overwritten
	content, _ := os.ReadFile(wrapperPath)
	if string(content) != "# existing" {
		t.Error("existing wrapper file was overwritten")
	}
}

func TestGenerate_Overwrite(t *testing.T) {
	tmpDir := t.TempDir()

	// Pre-create wrapper file
	wrapperPath := filepath.Join(tmpDir, "gauntlet_tools.py")
	if err := os.WriteFile(wrapperPath, []byte("# existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	proposals := []discovery.Proposal{
		{ID: "t1", Name: "test_nominal", Description: "test", Status: "pending", Source: "test", Tool: "test_tool", Variant: "nominal"},
	}

	result, err := Generate(Config{
		RootDir:   tmpDir,
		EvalsDir:  filepath.Join(tmpDir, "evals"),
		Proposals: proposals,
		Overwrite: true,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Wrapper should be regenerated
	if result.WrapperPath == "" {
		t.Error("expected WrapperPath to be set (overwrite=true)")
	}

	content, _ := os.ReadFile(wrapperPath)
	if string(content) == "# existing" {
		t.Error("wrapper file should have been overwritten")
	}
}

func TestFileToImportPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"myapp/tools.py", "myapp.tools"},
		{"agent/tools/weather.py", "agent.tools.weather"},
		{"tools.py", "tools"},
		{"myapp/__init__.py", "myapp"},
		{"src/agents/tool_defs.py", "src.agents.tool_defs"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := fileToImportPath(tt.input)
			if got != tt.expected {
				t.Errorf("fileToImportPath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFilterToolProposals(t *testing.T) {
	proposals := []discovery.Proposal{
		{ID: "1", Tool: "get_weather", Variant: "nominal"},
		{ID: "2", Database: "orders"},
		{ID: "3", Tool: "lookup_order", Variant: "nominal"},
	}
	filtered := filterToolProposals(proposals)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 tool proposals, got %d", len(filtered))
	}
	if filtered[0].Tool != "get_weather" {
		t.Errorf("expected first tool to be get_weather, got %s", filtered[0].Tool)
	}
	if filtered[1].Tool != "lookup_order" {
		t.Errorf("expected second tool to be lookup_order, got %s", filtered[1].Tool)
	}
}

func TestFallbackSignatures(t *testing.T) {
	proposals := []discovery.Proposal{
		{ID: "1", Tool: "get_weather", Variant: "nominal", Framework: "openai-agents"},
		{ID: "2", Tool: "lookup", Variant: "nominal"},
	}
	sigs := fallbackSignatures(proposals)
	if len(sigs) != 2 {
		t.Fatalf("expected 2 signatures, got %d", len(sigs))
	}
	if sigs[0].Name != "get_weather" {
		t.Errorf("expected name get_weather, got %s", sigs[0].Name)
	}
	if sigs[0].Framework != "openai-agents" {
		t.Errorf("expected framework openai-agents, got %s", sigs[0].Framework)
	}
	if sigs[1].Name != "lookup" {
		t.Errorf("expected name lookup, got %s", sigs[1].Name)
	}
}

func TestGenerateWrapper_ValidPython(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "gauntlet_tools.py")

	sigs := []ToolSignature{
		{
			Name:     "get_weather",
			FuncName: "get_weather",
			File:     "myapp/tools.py",
			IsAsync:  false,
			Params: []ParamInfo{
				{Name: "city", Type: "str"},
			},
			ReturnType: "dict",
			Framework:  "openai-agents",
			Docstring:  "Get weather for a city.",
		},
		{
			Name:     "customer_balance",
			FuncName: "customer_balance",
			File:     "myapp/bank.py",
			IsAsync:  true,
			Params: []ParamInfo{
				{Name: "customer_id", Type: "int"},
			},
			ReturnType:    "str",
			Framework:     "pydantic-ai",
			HasRunContext: true,
			Docstring:     "Returns the customer's current account balance.\nThis is a multi-line docstring that should be truncated.",
		},
	}

	if err := generateWrapper(path, sigs); err != nil {
		t.Fatalf("generateWrapper failed: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read generated wrapper: %v", err)
	}
	wrapper := string(content)

	// Check structure
	if !strings.Contains(wrapper, "import gauntlet") {
		t.Error("wrapper should import gauntlet")
	}
	if !strings.Contains(wrapper, `@gauntlet.tool(name="get_weather")`) {
		t.Error("wrapper should have @gauntlet.tool decorator for get_weather")
	}
	if !strings.Contains(wrapper, `@gauntlet.tool(name="customer_balance")`) {
		t.Error("wrapper should have @gauntlet.tool decorator for customer_balance")
	}
	if !strings.Contains(wrapper, "async def customer_balance") {
		t.Error("wrapper should have async def for customer_balance")
	}
	if !strings.Contains(wrapper, "def get_weather") {
		t.Error("wrapper should have def for get_weather")
	}
	if !strings.Contains(wrapper, "city: str") {
		t.Error("wrapper should include typed parameter")
	}
	if !strings.Contains(wrapper, "-> dict") {
		t.Error("wrapper should include return type annotation")
	}
	if !strings.Contains(wrapper, "from myapp.tools import") {
		t.Error("wrapper should have correct import path")
	}
	// Multi-line docstring should be truncated to first line
	if strings.Contains(wrapper, "multi-line docstring") {
		t.Error("multi-line docstring should be truncated to first line")
	}
	if !strings.Contains(wrapper, "# Returns the customer's current account balance.") {
		t.Error("first line of docstring should appear as comment")
	}
}

func TestGenerateAdapter(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "gauntlet_adapter.py")

	if err := generateAdapter(path); err != nil {
		t.Fatalf("generateAdapter failed: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read adapter: %v", err)
	}
	adapter := string(content)

	if !strings.Contains(adapter, "gauntlet.connect()") {
		t.Error("adapter should call gauntlet.connect()")
	}
	if !strings.Contains(adapter, "from gauntlet_tools import") {
		t.Error("adapter should import gauntlet_tools")
	}
	if !strings.Contains(adapter, "json.load(sys.stdin)") {
		t.Error("adapter should read from stdin")
	}
	if !strings.Contains(adapter, "json.dump(result, sys.stdout)") {
		t.Error("adapter should write to stdout")
	}
}

func TestGenerateWorldDef(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "get_weather.yaml")

	sig := ToolSignature{
		Name:      "get_weather",
		FuncName:  "get_weather",
		File:      "tools.py",
		Framework: "openai-agents",
		Docstring: "Get weather for a city.",
	}

	if err := generateWorldDef(path, sig); err != nil {
		t.Fatalf("generateWorldDef failed: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read world def: %v", err)
	}
	yaml := string(content)

	if !strings.Contains(yaml, "tool: get_weather") {
		t.Error("world def should have tool name")
	}
	if !strings.Contains(yaml, "Get weather for a city.") {
		t.Error("world def should use docstring as description")
	}
	if !strings.Contains(yaml, "nominal") {
		t.Error("world def should have nominal state")
	}
}

func TestGenerateWorldDef_NoDocstring(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "my_tool.yaml")

	sig := ToolSignature{
		Name:      "my_tool",
		FuncName:  "my_tool",
		File:      "tools.py",
		Framework: "pydantic-ai",
	}

	if err := generateWorldDef(path, sig); err != nil {
		t.Fatalf("generateWorldDef failed: %v", err)
	}

	content, _ := os.ReadFile(path)
	yaml := string(content)

	if !strings.Contains(yaml, "Auto-discovered from pydantic-ai") {
		t.Error("world def should have auto-discovered description when no docstring")
	}
}
