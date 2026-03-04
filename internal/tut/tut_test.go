package tut

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// --- IntegrationLevel tests ---

func TestIntegrationLevelConstants(t *testing.T) {
	tests := []struct {
		level IntegrationLevel
		want  string
	}{
		{LevelBest, "best"},
		{LevelGood, "good"},
		{LevelMinimal, "minimal"},
	}
	for _, tt := range tests {
		if string(tt.level) != tt.want {
			t.Errorf("IntegrationLevel %q: got %q, want %q", tt.want, string(tt.level), tt.want)
		}
	}
}

// --- Config tests ---

func TestConfigFieldsFromJSON(t *testing.T) {
	input := `{
		"command": "python",
		"args": ["-m", "myagent"],
		"work_dir": "/tmp/agent",
		"env": {"API_KEY": "test"},
		"adapter": "http",
		"http_port": 9000,
		"http_path": "/api/run",
		"startup_ms": 3000
	}`

	var cfg Config
	if err := json.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("failed to unmarshal Config: %v", err)
	}

	if cfg.Command != "python" {
		t.Errorf("Command: got %q, want %q", cfg.Command, "python")
	}
	if len(cfg.Args) != 2 || cfg.Args[0] != "-m" || cfg.Args[1] != "myagent" {
		t.Errorf("Args: got %v, want [-m myagent]", cfg.Args)
	}
	if cfg.WorkDir != "/tmp/agent" {
		t.Errorf("WorkDir: got %q, want %q", cfg.WorkDir, "/tmp/agent")
	}
	if cfg.Env["API_KEY"] != "test" {
		t.Errorf("Env[API_KEY]: got %q, want %q", cfg.Env["API_KEY"], "test")
	}
	if cfg.Adapter != "http" {
		t.Errorf("Adapter: got %q, want %q", cfg.Adapter, "http")
	}
	if cfg.HTTPPort != 9000 {
		t.Errorf("HTTPPort: got %d, want 9000", cfg.HTTPPort)
	}
	if cfg.HTTPPath != "/api/run" {
		t.Errorf("HTTPPath: got %q, want %q", cfg.HTTPPath, "/api/run")
	}
	if cfg.StartupMs != 3000 {
		t.Errorf("StartupMs: got %d, want 3000", cfg.StartupMs)
	}
}

func TestConfigMarshalJSON(t *testing.T) {
	cfg := Config{
		Command:  "node",
		Args:     []string{"index.js"},
		Adapter:  "cli",
		HTTPPort: 8080,
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal Config: %v", err)
	}

	var roundtrip Config
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("failed to unmarshal Config: %v", err)
	}

	if roundtrip.Command != cfg.Command {
		t.Errorf("roundtrip Command: got %q, want %q", roundtrip.Command, cfg.Command)
	}
	if roundtrip.Adapter != cfg.Adapter {
		t.Errorf("roundtrip Adapter: got %q, want %q", roundtrip.Adapter, cfg.Adapter)
	}
	if roundtrip.HTTPPort != cfg.HTTPPort {
		t.Errorf("roundtrip HTTPPort: got %d, want %d", roundtrip.HTTPPort, cfg.HTTPPort)
	}
}

// --- AgentOutput tests ---

func TestAgentOutputJSONMarshal(t *testing.T) {
	ao := AgentOutput{
		Raw:      []byte(`{"result": "ok"}`),
		Parsed:   map[string]interface{}{"result": "ok"},
		ExitCode: 0,
		Duration: 1500 * time.Millisecond,
		StdErr:   "some warning",
	}

	data, err := json.Marshal(ao)
	if err != nil {
		t.Fatalf("failed to marshal AgentOutput: %v", err)
	}

	var roundtrip AgentOutput
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("failed to unmarshal AgentOutput: %v", err)
	}

	if roundtrip.ExitCode != 0 {
		t.Errorf("ExitCode: got %d, want 0", roundtrip.ExitCode)
	}
	if roundtrip.StdErr != "some warning" {
		t.Errorf("StdErr: got %q, want %q", roundtrip.StdErr, "some warning")
	}
	if string(roundtrip.Raw) != `{"result": "ok"}` {
		t.Errorf("Raw: got %q", string(roundtrip.Raw))
	}
	if roundtrip.Parsed["result"] != "ok" {
		t.Errorf("Parsed[result]: got %v, want %q", roundtrip.Parsed["result"], "ok")
	}
}

func TestAgentOutputJSONUnmarshal(t *testing.T) {
	input := `{
		"raw": "aGVsbG8=",
		"parsed": {"key": "value"},
		"exit_code": 1,
		"duration": 2000000000,
		"stderr": "error output"
	}`

	var ao AgentOutput
	if err := json.Unmarshal([]byte(input), &ao); err != nil {
		t.Fatalf("failed to unmarshal AgentOutput: %v", err)
	}

	if ao.ExitCode != 1 {
		t.Errorf("ExitCode: got %d, want 1", ao.ExitCode)
	}
	if ao.StdErr != "error output" {
		t.Errorf("StdErr: got %q, want %q", ao.StdErr, "error output")
	}
	if ao.Parsed["key"] != "value" {
		t.Errorf("Parsed[key]: got %v, want %q", ao.Parsed["key"], "value")
	}
}

func TestAgentOutputOmitEmptyStderr(t *testing.T) {
	ao := AgentOutput{
		ExitCode: 0,
		StdErr:   "",
	}
	data, err := json.Marshal(ao)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	if _, ok := m["stderr"]; ok {
		t.Error("stderr should be omitted when empty (omitempty tag)")
	}
}

// --- TraceEvent tests ---

func TestTraceEventToolCall(t *testing.T) {
	te := TraceEvent{
		EventType:  "tool_call",
		ToolName:   "search_files",
		Args:       json.RawMessage(`{"pattern": "*.go"}`),
		Response:   json.RawMessage(`{"files": ["main.go"]}`),
		Timestamp:  time.Now(),
		DurationMs: 50,
	}

	data, err := json.Marshal(te)
	if err != nil {
		t.Fatalf("failed to marshal TraceEvent: %v", err)
	}

	var roundtrip TraceEvent
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("failed to unmarshal TraceEvent: %v", err)
	}

	if roundtrip.EventType != "tool_call" {
		t.Errorf("EventType: got %q, want %q", roundtrip.EventType, "tool_call")
	}
	if roundtrip.ToolName != "search_files" {
		t.Errorf("ToolName: got %q, want %q", roundtrip.ToolName, "search_files")
	}
	if roundtrip.DurationMs != 50 {
		t.Errorf("DurationMs: got %d, want 50", roundtrip.DurationMs)
	}
}

func TestTraceEventModelCall(t *testing.T) {
	mce := &ModelCallEvent{
		ProviderFamily: "openai",
		Model:          "gpt-4",
		CanonicalHash:  "abc123",
	}
	te := TraceEvent{
		EventType:  "model_call",
		ModelCall:  mce,
		Timestamp:  time.Now(),
		DurationMs: 200,
	}

	data, err := json.Marshal(te)
	if err != nil {
		t.Fatalf("failed to marshal TraceEvent: %v", err)
	}

	var roundtrip TraceEvent
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("failed to unmarshal TraceEvent: %v", err)
	}

	if roundtrip.EventType != "model_call" {
		t.Errorf("EventType: got %q, want %q", roundtrip.EventType, "model_call")
	}
	if roundtrip.ModelCall == nil {
		t.Fatal("ModelCall: expected non-nil")
	}
	if roundtrip.ModelCall.ProviderFamily != "openai" {
		t.Errorf("ModelCall.ProviderFamily: got %q, want %q", roundtrip.ModelCall.ProviderFamily, "openai")
	}
	if roundtrip.ModelCall.Model != "gpt-4" {
		t.Errorf("ModelCall.Model: got %q, want %q", roundtrip.ModelCall.Model, "gpt-4")
	}
	if roundtrip.ModelCall.CanonicalHash != "abc123" {
		t.Errorf("ModelCall.CanonicalHash: got %q, want %q", roundtrip.ModelCall.CanonicalHash, "abc123")
	}
}

func TestTraceEventOmitEmpty(t *testing.T) {
	te := TraceEvent{
		EventType:  "model_call",
		Timestamp:  time.Now(),
		DurationMs: 100,
	}

	data, err := json.Marshal(te)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	if _, ok := m["tool_name"]; ok {
		t.Error("tool_name should be omitted when empty (omitempty tag)")
	}
	if _, ok := m["args"]; ok {
		t.Error("args should be omitted when empty (omitempty tag)")
	}
	if _, ok := m["response"]; ok {
		t.Error("response should be omitted when empty (omitempty tag)")
	}
}

// --- Adapter level tests ---

func TestHTTPAdapterLevel(t *testing.T) {
	a := &HTTPAdapter{}
	if a.Level() != LevelBest {
		t.Errorf("HTTPAdapter.Level(): got %q, want %q", a.Level(), LevelBest)
	}
}

func TestCLIAdapterLevelGood(t *testing.T) {
	a := &CLIAdapter{Minimal: false}
	if a.Level() != LevelGood {
		t.Errorf("CLIAdapter.Level() (non-minimal): got %q, want %q", a.Level(), LevelGood)
	}
}

func TestCLIAdapterLevelMinimal(t *testing.T) {
	a := &CLIAdapter{Minimal: true}
	if a.Level() != LevelMinimal {
		t.Errorf("CLIAdapter.Level() (minimal): got %q, want %q", a.Level(), LevelMinimal)
	}
}

// --- ModelCallEvent tests ---

func TestModelCallEventJSON(t *testing.T) {
	mce := ModelCallEvent{
		ProviderFamily: "anthropic",
		Model:          "claude-3-opus",
		CanonicalHash:  "xyz789",
	}

	data, err := json.Marshal(mce)
	if err != nil {
		t.Fatalf("failed to marshal ModelCallEvent: %v", err)
	}

	var roundtrip ModelCallEvent
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("failed to unmarshal ModelCallEvent: %v", err)
	}

	if roundtrip.ProviderFamily != "anthropic" {
		t.Errorf("ProviderFamily: got %q, want %q", roundtrip.ProviderFamily, "anthropic")
	}
	if roundtrip.Model != "claude-3-opus" {
		t.Errorf("Model: got %q, want %q", roundtrip.Model, "claude-3-opus")
	}
	if roundtrip.CanonicalHash != "xyz789" {
		t.Errorf("CanonicalHash: got %q, want %q", roundtrip.CanonicalHash, "xyz789")
	}
}

func TestMergedProcessEnv_RestrictHostEnv(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("SECRET_TOKEN", "topsecret")

	env := mergedProcessEnv(map[string]string{"CUSTOM_VAR": "1"}, true)
	m := make(map[string]string)
	for _, kv := range env {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}

	if m["CUSTOM_VAR"] != "1" {
		t.Fatalf("CUSTOM_VAR = %q, want 1", m["CUSTOM_VAR"])
	}
	if _, ok := m["PATH"]; !ok {
		t.Fatal("PATH should be retained in restricted env")
	}
	if _, ok := m["SECRET_TOKEN"]; ok {
		t.Fatal("SECRET_TOKEN should not be inherited in restricted env")
	}
}

func TestMergedProcessEnv_InheritHostEnv(t *testing.T) {
	t.Setenv("SECRET_TOKEN", "topsecret")
	env := mergedProcessEnv(map[string]string{"CUSTOM_VAR": "1"}, false)

	m := make(map[string]string)
	for _, kv := range env {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}

	if m["CUSTOM_VAR"] != "1" {
		t.Fatalf("CUSTOM_VAR = %q, want 1", m["CUSTOM_VAR"])
	}
	if m["SECRET_TOKEN"] != "topsecret" {
		t.Fatalf("SECRET_TOKEN = %q, want topsecret", m["SECRET_TOKEN"])
	}
}
