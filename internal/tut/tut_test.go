package tut

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		"startup_ms": 3000,
		"resource_limits": {"cpu_seconds": 9, "memory_mb": 256, "open_files": 128},
		"guardrails": {"hostile_payload": true, "max_processes": 64}
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
	if cfg.ResourceLimits.CPUSeconds != 9 || cfg.ResourceLimits.MemoryMB != 256 || cfg.ResourceLimits.OpenFiles != 128 {
		t.Errorf("ResourceLimits: got %+v, want cpu=9 memory=256 open_files=128", cfg.ResourceLimits)
	}
	if !cfg.Guardrails.HostilePayload || cfg.Guardrails.MaxProcesses != 64 {
		t.Errorf("Guardrails: got %+v, want hostile_payload=true max_processes=64", cfg.Guardrails)
	}
}

func TestConfigMarshalJSON(t *testing.T) {
	cfg := Config{
		Command:  "node",
		Args:     []string{"index.js"},
		Adapter:  "cli",
		HTTPPort: 8080,
		ResourceLimits: ResourceLimits{
			CPUSeconds: 5,
			MemoryMB:   128,
			OpenFiles:  64,
		},
		Guardrails: Guardrails{
			HostilePayload: true,
			MaxProcesses:   32,
		},
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
	if roundtrip.ResourceLimits != cfg.ResourceLimits {
		t.Errorf("roundtrip ResourceLimits: got %+v, want %+v", roundtrip.ResourceLimits, cfg.ResourceLimits)
	}
	if roundtrip.Guardrails != cfg.Guardrails {
		t.Errorf("roundtrip Guardrails: got %+v, want %+v", roundtrip.Guardrails, cfg.Guardrails)
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

func TestParseTraceFile_ModelCallMetadata(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "trace.ndjson")
	line := `{"gauntlet_event":true,"type":"model_call","timestamp":1735689600.25,"provider_family":"openai","model":"gpt-4o","canonical_hash":"abc123","args":{"endpoint":"/v1/chat/completions"},"result":{"id":"resp_1"},"duration_ms":17}`
	if err := os.WriteFile(tracePath, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	events, err := parseTraceFile(tracePath)
	if err != nil {
		t.Fatalf("parseTraceFile: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	event := events[0]
	if event.EventType != "model_call" {
		t.Fatalf("EventType = %q, want model_call", event.EventType)
	}
	if event.ModelCall == nil {
		t.Fatal("expected model call metadata")
	}
	if event.ModelCall.ProviderFamily != "openai" {
		t.Fatalf("ProviderFamily = %q, want openai", event.ModelCall.ProviderFamily)
	}
	if event.ModelCall.Model != "gpt-4o" {
		t.Fatalf("Model = %q, want gpt-4o", event.ModelCall.Model)
	}
	if event.ModelCall.CanonicalHash != "abc123" {
		t.Fatalf("CanonicalHash = %q, want abc123", event.ModelCall.CanonicalHash)
	}
}

func TestParseTraceFile_ModelCallTokenUsage(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "trace.ndjson")
	line := `{"gauntlet_event":true,"type":"model_call","timestamp":1735689600.25,"provider_family":"openai","model":"gpt-4o","canonical_hash":"abc123","result":{"id":"resp_1","usage":{"prompt_tokens":42,"completion_tokens":9}},"duration_ms":17}`
	if err := os.WriteFile(tracePath, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	events, err := parseTraceFile(tracePath)
	if err != nil {
		t.Fatalf("parseTraceFile: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	event := events[0]
	if event.ModelCall == nil {
		t.Fatal("expected model call metadata")
	}
	if event.ModelCall.PromptTokens != 42 {
		t.Fatalf("PromptTokens = %d, want 42", event.ModelCall.PromptTokens)
	}
	if event.ModelCall.CompletionTokens != 9 {
		t.Fatalf("CompletionTokens = %d, want 9", event.ModelCall.CompletionTokens)
	}
}

func TestParseTraceFile_SDKCapabilities(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "trace.ndjson")
	line := `{"gauntlet_event":true,"type":"sdk_capabilities","timestamp":1735689600.5,"result":{"protocol_version":1,"sdk":"gauntlet-python","runtime":"python3.11","adapters":{"openai":{"enabled":true,"patched":false,"reason":"openai_not_installed"}}}}`
	if err := os.WriteFile(tracePath, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	events, err := parseTraceFile(tracePath)
	if err != nil {
		t.Fatalf("parseTraceFile: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	event := events[0]
	if event.EventType != "sdk_capabilities" {
		t.Fatalf("EventType = %q, want sdk_capabilities", event.EventType)
	}
	if event.SDKCapabilities == nil {
		t.Fatal("expected sdk capabilities payload")
	}
	if event.SDKCapabilities.ProtocolVersion != CapabilityProtocolV1 {
		t.Fatalf("ProtocolVersion = %d, want %d", event.SDKCapabilities.ProtocolVersion, CapabilityProtocolV1)
	}
	if event.SDKCapabilities.Adapters["openai"].Reason != "openai_not_installed" {
		t.Fatalf("openai reason = %q", event.SDKCapabilities.Adapters["openai"].Reason)
	}
}

func TestParseTraceFile_DeterminismEnvReport(t *testing.T) {
	tracePath := filepath.Join(t.TempDir(), "trace.ndjson")
	line := `{"gauntlet_event":true,"type":"determinism_env","timestamp":1735689600.6,"result":{"language":"python","runtime":"python3.11","requested_timezone":"UTC","effective_timezone":"UTC","timezone_applied":true,"requested_locale":"en_US.UTF-8","effective_locale":"en_US.UTF-8","locale_applied":true,"requested_freeze_time":"2025-01-15T10:00:00Z","time_patched":true}}`
	if err := os.WriteFile(tracePath, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}

	events, err := parseTraceFile(tracePath)
	if err != nil {
		t.Fatalf("parseTraceFile: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	event := events[0]
	if event.EventType != "determinism_env" {
		t.Fatalf("EventType = %q, want determinism_env", event.EventType)
	}
	if event.DeterminismEnv == nil {
		t.Fatal("expected determinism environment report")
	}
	if !event.DeterminismEnv.TimezoneApplied {
		t.Fatal("expected timezone_applied=true")
	}
	if event.DeterminismEnv.EffectiveTimezone != "UTC" {
		t.Fatalf("effective timezone = %q, want UTC", event.DeterminismEnv.EffectiveTimezone)
	}
}

func TestExtractSDKCapabilitiesReturnsMostRecent(t *testing.T) {
	events := []TraceEvent{
		{
			EventType: "sdk_capabilities",
			SDKCapabilities: &SDKCapabilities{
				ProtocolVersion: CapabilityProtocolV1,
				SDK:             "gauntlet-python",
				Adapters: map[string]SDKAdapterCapability{
					"openai": {Enabled: true, Patched: false, Reason: "first"},
				},
			},
		},
		{
			EventType: "sdk_capabilities",
			SDKCapabilities: &SDKCapabilities{
				ProtocolVersion: CapabilityProtocolV1,
				SDK:             "gauntlet-python",
				Adapters: map[string]SDKAdapterCapability{
					"openai": {Enabled: true, Patched: true},
				},
			},
		},
	}

	report := ExtractSDKCapabilities(events)
	if report == nil {
		t.Fatal("expected capability report")
	}
	if !report.Adapters["openai"].Patched {
		t.Fatal("expected most recent capabilities payload to be returned")
	}
}

func TestExtractDeterminismEnvReportReturnsMostRecent(t *testing.T) {
	events := []TraceEvent{
		{
			EventType: "determinism_env",
			DeterminismEnv: &DeterminismEnvReport{
				Language:          "python",
				EffectiveTimezone: "UTC",
			},
		},
		{
			EventType: "determinism_env",
			DeterminismEnv: &DeterminismEnvReport{
				Language:          "python",
				EffectiveTimezone: "Europe/Berlin",
			},
		},
	}

	report := ExtractDeterminismEnvReport(events)
	if report == nil {
		t.Fatal("expected determinism env report")
	}
	if report.EffectiveTimezone != "Europe/Berlin" {
		t.Fatalf("effective timezone = %q, want Europe/Berlin", report.EffectiveTimezone)
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

func TestBuildResourceLimitScript(t *testing.T) {
	script := buildResourceLimitScript(ResourceLimits{
		CPUSeconds: 7,
		MemoryMB:   256,
		OpenFiles:  128,
	})
	required := []string{
		"ulimit -n 128",
		"ulimit -t 7",
		"ulimit -v 262144",
		"exec \"$@\"",
	}
	for _, fragment := range required {
		if !strings.Contains(script, fragment) {
			t.Fatalf("script missing %q:\n%s", fragment, script)
		}
	}
}

func TestBuildGuardrailScript(t *testing.T) {
	script := buildGuardrailScript(42)
	required := []string{
		"set -eu",
		"ulimit -u 42",
		"exec \"$@\"",
	}
	for _, fragment := range required {
		if !strings.Contains(script, fragment) {
			t.Fatalf("script missing %q:\n%s", fragment, script)
		}
	}
}

func TestWrapWithResourceLimits_NoLimitsReturnsOriginalCommand(t *testing.T) {
	cmd := exec.Command("echo", "hello")
	wrapped, err := wrapWithResourceLimits(cmd, ResourceLimits{})
	if err != nil {
		t.Fatalf("wrapWithResourceLimits: %v", err)
	}
	if wrapped != cmd {
		t.Fatal("expected original command when no limits configured")
	}
}

func TestWrapWithHostilePayloadGuardrails_DisabledReturnsOriginalCommand(t *testing.T) {
	cmd := exec.Command("echo", "hello")
	wrapped, err := wrapWithHostilePayloadGuardrails(cmd, Guardrails{})
	if err != nil {
		t.Fatalf("wrapWithHostilePayloadGuardrails: %v", err)
	}
	if wrapped != cmd {
		t.Fatal("expected original command when hostile payload guardrails disabled")
	}
}

func TestWrapWithHostilePayloadGuardrails_EnabledBehavior(t *testing.T) {
	cmd := exec.Command("echo", "hello")
	wrapped, err := wrapWithHostilePayloadGuardrails(cmd, Guardrails{HostilePayload: true, MaxProcesses: 12})
	if runtime.GOOS != "linux" {
		if err == nil {
			t.Fatal("expected unsupported OS error when hostile payload guardrails are enabled")
		}
		return
	}
	if err != nil {
		// Some linux environments might not expose unshare in PATH.
		if strings.Contains(err.Error(), "require 'unshare'") {
			t.Skipf("skipping due to missing unshare: %v", err)
		}
		t.Fatalf("wrapWithHostilePayloadGuardrails: %v", err)
	}
	if filepath.Base(wrapped.Path) != "unshare" && filepath.Base(wrapped.Path) != "sh" {
		t.Fatalf("expected unshare/sh wrapper, got %q", wrapped.Path)
	}
}
