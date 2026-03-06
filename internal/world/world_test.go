package world

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pmclSF/gauntlet/internal/scenario"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// ValidateToolDef
// ---------------------------------------------------------------------------

func TestValidateToolDef_AllStatesPresent(t *testing.T) {
	td := &ToolDef{
		Tool: "order_lookup",
		States: map[string]*ToolStateDef{
			"nominal":            {},
			"timeout":            {},
			"server_error":       {},
			"malformed_response": {},
		},
	}
	warnings := ValidateToolDef(td)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
}

func TestValidateToolDef_MissingSingleState(t *testing.T) {
	td := &ToolDef{
		Tool: "refund_processor",
		States: map[string]*ToolStateDef{
			"nominal":      {},
			"timeout":      {},
			"server_error": {},
			// missing "malformed_response"
		},
	}
	warnings := ValidateToolDef(td)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "malformed_response") {
		t.Errorf("warning should mention malformed_response: %s", warnings[0])
	}
	if !strings.Contains(warnings[0], "refund_processor") {
		t.Errorf("warning should mention tool name: %s", warnings[0])
	}
}

func TestValidateToolDef_MissingAllStates(t *testing.T) {
	td := &ToolDef{
		Tool:   "empty_tool",
		States: map[string]*ToolStateDef{},
	}
	warnings := ValidateToolDef(td)
	if len(warnings) != len(RequiredStates) {
		t.Errorf("expected %d warnings, got %d: %v", len(RequiredStates), len(warnings), warnings)
	}
}

func TestValidateToolDef_NilStatesMap(t *testing.T) {
	td := &ToolDef{
		Tool:   "nil_states",
		States: nil,
	}
	warnings := ValidateToolDef(td)
	if len(warnings) != len(RequiredStates) {
		t.Errorf("expected %d warnings for nil States map, got %d", len(RequiredStates), len(warnings))
	}
}

func TestValidateToolDef_ExtraStatesAreAllowed(t *testing.T) {
	td := &ToolDef{
		Tool: "custom_tool",
		States: map[string]*ToolStateDef{
			"nominal":            {},
			"timeout":            {},
			"server_error":       {},
			"malformed_response": {},
			"rate_limited":       {},
			"partial_response":   {},
		},
	}
	warnings := ValidateToolDef(td)
	if len(warnings) != 0 {
		t.Errorf("extra states should not produce warnings, got %v", warnings)
	}
}

func TestValidateToolDef_MissingMultipleStates(t *testing.T) {
	td := &ToolDef{
		Tool: "partial_tool",
		States: map[string]*ToolStateDef{
			"nominal": {},
			// missing timeout, server_error, malformed_response
		},
	}
	warnings := ValidateToolDef(td)
	if len(warnings) != 3 {
		t.Errorf("expected 3 warnings, got %d: %v", len(warnings), warnings)
	}
}

// ---------------------------------------------------------------------------
// ValidateVariantPolicy
// ---------------------------------------------------------------------------

func TestValidateVariantPolicy_AllNominal(t *testing.T) {
	tools := map[string]string{
		"order_lookup":     "nominal",
		"refund_processor": "nominal",
		"inventory":        "nominal",
	}
	if err := ValidateVariantPolicy(tools, nil, false); err != nil {
		t.Errorf("all nominal should pass: %v", err)
	}
}

func TestValidateVariantPolicy_SingleFault(t *testing.T) {
	tools := map[string]string{
		"order_lookup":     "nominal",
		"refund_processor": "timeout",
		"inventory":        "nominal",
	}
	if err := ValidateVariantPolicy(tools, nil, false); err != nil {
		t.Errorf("single fault should pass: %v", err)
	}
}

func TestValidateVariantPolicy_MultiFaultRejected(t *testing.T) {
	tools := map[string]string{
		"order_lookup":     "timeout",
		"refund_processor": "server_error",
		"inventory":        "nominal",
	}
	err := ValidateVariantPolicy(tools, nil, false)
	if err == nil {
		t.Fatal("expected error for multi-fault without chaos")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "multi-fault") {
		t.Errorf("error should mention multi-fault: %s", errMsg)
	}
	if !strings.Contains(errMsg, "chaos: false") {
		t.Errorf("error should mention chaos: false: %s", errMsg)
	}
}

func TestValidateVariantPolicy_MultiFaultAllowedWithChaos(t *testing.T) {
	tools := map[string]string{
		"order_lookup":     "timeout",
		"refund_processor": "server_error",
		"inventory":        "malformed_response",
	}
	if err := ValidateVariantPolicy(tools, nil, true); err != nil {
		t.Errorf("multi-fault with chaos=true should pass: %v", err)
	}
}

func TestValidateVariantPolicy_EmptyTools(t *testing.T) {
	if err := ValidateVariantPolicy(map[string]string{}, nil, false); err != nil {
		t.Errorf("empty tools should pass: %v", err)
	}
}

func TestValidateVariantPolicy_NilTools(t *testing.T) {
	if err := ValidateVariantPolicy(nil, nil, false); err != nil {
		t.Errorf("nil tools should pass: %v", err)
	}
}

func TestValidateVariantPolicy_AllNonNominalChaos(t *testing.T) {
	tools := map[string]string{
		"a": "timeout",
		"b": "server_error",
		"c": "malformed_response",
	}
	if err := ValidateVariantPolicy(tools, nil, true); err != nil {
		t.Errorf("all non-nominal with chaos=true should pass: %v", err)
	}
}

func TestValidateVariantPolicy_MultiFaultChaosLogsWarning(t *testing.T) {
	var buf bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	}()

	tools := map[string]string{
		"order_lookup":     "timeout",
		"refund_processor": "server_error",
	}
	if err := ValidateVariantPolicy(tools, nil, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	msg := buf.String()
	if !strings.Contains(msg, "chaos=true with multi-fault scenario") {
		t.Fatalf("expected warning log, got: %q", msg)
	}
}

func TestValidateVariantPolicy_SingleToolNonNominal(t *testing.T) {
	tools := map[string]string{
		"solo": "server_error",
	}
	if err := ValidateVariantPolicy(tools, nil, false); err != nil {
		t.Errorf("single tool non-nominal should pass single-fault: %v", err)
	}
}

func TestValidateVariantPolicy_StandardDatabaseSeedIsNominal(t *testing.T) {
	databases := map[string]scenario.DBSpec{
		"orders_db": {SeedSets: []string{"standard_order"}},
	}
	if err := ValidateVariantPolicy(nil, databases, false); err != nil {
		t.Fatalf("standard seed set should be treated as nominal: %v", err)
	}
}

func TestValidateVariantPolicy_OneDatabaseFaultAllowed(t *testing.T) {
	databases := map[string]scenario.DBSpec{
		"orders_db": {SeedSets: []string{"conflicting_state"}},
	}
	if err := ValidateVariantPolicy(nil, databases, false); err != nil {
		t.Fatalf("single database fault should pass: %v", err)
	}
}

func TestValidateVariantPolicy_ToolAndDatabaseFaultRejected(t *testing.T) {
	tools := map[string]string{
		"order_lookup": "timeout",
	}
	databases := map[string]scenario.DBSpec{
		"orders_db": {SeedSets: []string{"conflicting_state"}},
	}
	err := ValidateVariantPolicy(tools, databases, false)
	if err == nil {
		t.Fatal("expected error when tool + database are both non-nominal")
	}
	msg := err.Error()
	if !strings.Contains(msg, "tool order_lookup") {
		t.Fatalf("expected tool detail in error, got: %s", msg)
	}
	if !strings.Contains(msg, "db orders_db") {
		t.Fatalf("expected db detail in error, got: %s", msg)
	}
}

// ---------------------------------------------------------------------------
// State assembly (Assemble + LoadToolDef + LoadDBDef)
// ---------------------------------------------------------------------------

func TestLoadToolDef_FromFile(t *testing.T) {
	dir := t.TempDir()
	content := `
tool: order_lookup
states:
  nominal:
    response_code: 200
    behavior: returns order info
    agent_expectation: should relay status
    response:
      order_id: "ord-001"
      status: "shipped"
  timeout:
    response_code: 504
    behavior: gateway timeout
    delay_ms: 30000
    error: "connection timed out"
`
	path := filepath.Join(dir, "order_lookup.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	td, err := LoadToolDef(path)
	if err != nil {
		t.Fatalf("LoadToolDef failed: %v", err)
	}
	if td.Tool != "order_lookup" {
		t.Errorf("Tool = %q, want %q", td.Tool, "order_lookup")
	}
	if len(td.States) != 2 {
		t.Fatalf("expected 2 states, got %d", len(td.States))
	}
	nominal := td.States["nominal"]
	if nominal == nil {
		t.Fatal("missing 'nominal' state")
	}
	if nominal.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", nominal.StatusCode)
	}
	if nominal.Behavior != "returns order info" {
		t.Errorf("Behavior = %q", nominal.Behavior)
	}
	if nominal.Response == nil {
		t.Error("expected nominal to have a response")
	}
	timeout := td.States["timeout"]
	if timeout == nil {
		t.Fatal("missing 'timeout' state")
	}
	if timeout.StatusCode != 504 {
		t.Errorf("timeout StatusCode = %d, want 504", timeout.StatusCode)
	}
	if timeout.DelayMs != 30000 {
		t.Errorf("timeout DelayMs = %d, want 30000", timeout.DelayMs)
	}
	if timeout.Error != "connection timed out" {
		t.Errorf("timeout Error = %q", timeout.Error)
	}
}

func TestLoadToolDef_LegacyStatusCodeAliasStillSupported(t *testing.T) {
	dir := t.TempDir()
	content := `
tool: order_lookup
states:
  nominal:
    status_code: 201
`
	path := filepath.Join(dir, "order_lookup.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	td, err := LoadToolDef(path)
	if err != nil {
		t.Fatalf("LoadToolDef failed: %v", err)
	}
	nominal := td.States["nominal"]
	if nominal == nil {
		t.Fatal("missing 'nominal' state")
	}
	if nominal.StatusCode != 201 {
		t.Fatalf("StatusCode = %d, want 201", nominal.StatusCode)
	}
}

func TestLoadToolDef_NonexistentFile(t *testing.T) {
	_, err := LoadToolDef("/nonexistent/tool.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestLoadDBDef_FromFile(t *testing.T) {
	dir := t.TempDir()
	content := `
database: orders_db
adapter: sqlite3
seed_sets:
  default:
    tables:
      orders:
        columns:
          id: TEXT PRIMARY KEY
          status: TEXT
        rows:
          - id: "1"
            status: shipped
          - id: "2"
            status: pending
`
	path := filepath.Join(dir, "orders_db.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	dd, err := LoadDBDef(path)
	if err != nil {
		t.Fatalf("LoadDBDef failed: %v", err)
	}
	if dd.Database != "orders_db" {
		t.Errorf("Database = %q", dd.Database)
	}
	if dd.Adapter != "sqlite3" {
		t.Errorf("Adapter = %q", dd.Adapter)
	}
	if len(dd.SeedSets) != 1 {
		t.Fatalf("expected 1 seed set, got %d", len(dd.SeedSets))
	}
	ss := dd.SeedSets["default"]
	if ss == nil {
		t.Fatal("missing 'default' seed set")
	}
	if len(ss.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(ss.Tables))
	}
	orders := ss.Tables["orders"]
	if orders == nil {
		t.Fatal("missing 'orders' table")
	}
	if len(orders.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(orders.Rows))
	}
}

func TestAssemble_FromDirectories(t *testing.T) {
	toolsDir := t.TempDir()
	dbDir := t.TempDir()

	// Write a tool definition
	toolYAML := `
tool: weather_api
states:
  nominal:
    response_code: 200
`
	if err := os.WriteFile(filepath.Join(toolsDir, "weather_api.yaml"), []byte(toolYAML), 0644); err != nil {
		t.Fatalf("failed to write tool: %v", err)
	}

	// Write a DB definition
	dbYAML := `
database: users
adapter: sqlite3
seed_sets:
  default:
    table: users
    rows:
      - name: Alice
`
	if err := os.WriteFile(filepath.Join(dbDir, "users.yaml"), []byte(dbYAML), 0644); err != nil {
		t.Fatalf("failed to write DB: %v", err)
	}

	state, err := Assemble(toolsDir, dbDir)
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	if len(state.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(state.Tools))
	}
	if _, ok := state.Tools["weather_api"]; !ok {
		t.Error("expected tool 'weather_api'")
	}
	if len(state.Databases) != 1 {
		t.Fatalf("expected 1 database, got %d", len(state.Databases))
	}
	if _, ok := state.Databases["users"]; !ok {
		t.Error("expected database 'users'")
	}
}

func TestAssemble_EmptyDirectories(t *testing.T) {
	toolsDir := t.TempDir()
	dbDir := t.TempDir()
	state, err := Assemble(toolsDir, dbDir)
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	if len(state.Tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(state.Tools))
	}
	if len(state.Databases) != 0 {
		t.Errorf("expected 0 databases, got %d", len(state.Databases))
	}
}

func TestAssemble_NonexistentDirsAreOK(t *testing.T) {
	// Empty string dirs should not trigger errors
	state, err := Assemble("", "")
	if err != nil {
		t.Fatalf("Assemble with empty dirs failed: %v", err)
	}
	if state == nil {
		t.Fatal("expected non-nil state")
	}
}

func TestAssemble_MultipleToolFiles(t *testing.T) {
	toolsDir := t.TempDir()

	for _, name := range []string{"api_a", "api_b", "api_c"} {
		content, _ := yaml.Marshal(&ToolDef{
			Tool: name,
			States: map[string]*ToolStateDef{
				"nominal": {StatusCode: 200},
			},
		})
		if err := os.WriteFile(filepath.Join(toolsDir, name+".yaml"), content, 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	state, err := Assemble(toolsDir, "")
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	if len(state.Tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(state.Tools))
	}
}

// ---------------------------------------------------------------------------
// ToolStateDef JSON response field
// ---------------------------------------------------------------------------

func TestToolStateDef_ResponseRoundTrip(t *testing.T) {
	td := &ToolStateDef{
		Response: map[string]interface{}{"order_id": "123", "total": 42.50},
	}
	data, err := json.Marshal(td)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundTripped ToolStateDef
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	resp, ok := roundTripped.Response.(map[string]interface{})
	if !ok {
		t.Fatalf("Response type = %T, want map", roundTripped.Response)
	}
	if resp["order_id"] != "123" {
		t.Errorf("order_id = %v, want 123", resp["order_id"])
	}
}
