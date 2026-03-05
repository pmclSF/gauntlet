package tut

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/pmclSF/gauntlet/internal/scenario"
)

// rawSDKEvent matches the JSON format emitted by the Python SDK's events.py.
// Field names differ from TraceEvent (e.g. "type" vs "event_type").
type rawSDKEvent struct {
	GauntletEvent    bool            `json:"gauntlet_event"`
	Type             string          `json:"type"`
	Timestamp        float64         `json:"timestamp"`
	ToolName         string          `json:"tool_name,omitempty"`
	Args             json.RawMessage `json:"args,omitempty"`
	Result           json.RawMessage `json:"result,omitempty"`
	FixtureHit       bool            `json:"fixture_hit,omitempty"`
	CanonicalHash    string          `json:"canonical_hash,omitempty"`
	ProviderFamily   string          `json:"provider_family,omitempty"`
	Model            string          `json:"model,omitempty"`
	PromptTokens     int             `json:"prompt_tokens,omitempty"`
	CompletionTokens int             `json:"completion_tokens,omitempty"`
	DurationMs       int             `json:"duration_ms,omitempty"`
	Error            string          `json:"error,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
}

// parseTraceFile reads NDJSON trace events from a file written by the Python SDK.
func parseTraceFile(path string) ([]TraceEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []TraceEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB max line
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var raw rawSDKEvent
		if err := json.Unmarshal(line, &raw); err != nil {
			continue // skip malformed lines
		}
		if !raw.GauntletEvent {
			continue // not a gauntlet trace event
		}
		sec := int64(raw.Timestamp)
		nsec := int64((raw.Timestamp - float64(sec)) * 1e9)
		event := TraceEvent{
			EventType:  raw.Type,
			ToolName:   raw.ToolName,
			Args:       raw.Args,
			Response:   raw.Result,
			Timestamp:  time.Unix(sec, nsec),
			DurationMs: raw.DurationMs,
		}
		if raw.Type == "model_call" && (raw.ProviderFamily != "" || raw.Model != "" || raw.CanonicalHash != "") {
			promptTokens, completionTokens := extractModelCallTokens(raw)
			event.ModelCall = &ModelCallEvent{
				ProviderFamily:   raw.ProviderFamily,
				Model:            raw.Model,
				CanonicalHash:    raw.CanonicalHash,
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
			}
		} else if raw.Type == "sdk_capabilities" {
			var capabilities SDKCapabilities
			if len(raw.Result) > 0 && json.Unmarshal(raw.Result, &capabilities) == nil {
				event.SDKCapabilities = &capabilities
			}
		} else if raw.Type == "determinism_env" {
			var report DeterminismEnvReport
			if len(raw.Result) > 0 && json.Unmarshal(raw.Result, &report) == nil {
				event.DeterminismEnv = &report
			}
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

func extractModelCallTokens(raw rawSDKEvent) (int, int) {
	if raw.PromptTokens > 0 || raw.CompletionTokens > 0 {
		return raw.PromptTokens, raw.CompletionTokens
	}
	prompt, completion := extractTokenPair(raw.Metadata)
	if prompt > 0 || completion > 0 {
		return prompt, completion
	}
	return extractTokenPair(raw.Result)
}

func extractTokenPair(payload json.RawMessage) (int, int) {
	if len(payload) == 0 {
		return 0, 0
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return 0, 0
	}
	prompt := firstTokenValue(raw, [][]string{
		{"prompt_tokens"},
		{"usage", "prompt_tokens"},
		{"usage", "input_tokens"},
		{"usage", "inputTokens"},
		{"usageMetadata", "promptTokenCount"},
		{"meta", "billed_units", "input_tokens"},
	})
	completion := firstTokenValue(raw, [][]string{
		{"completion_tokens"},
		{"usage", "completion_tokens"},
		{"usage", "output_tokens"},
		{"usage", "outputTokens"},
		{"usageMetadata", "candidatesTokenCount"},
		{"meta", "billed_units", "output_tokens"},
	})
	return prompt, completion
}

func firstTokenValue(raw map[string]interface{}, paths [][]string) int {
	for _, path := range paths {
		if value, ok := tokenValueAtPath(raw, path); ok {
			return value
		}
	}
	return 0
}

func tokenValueAtPath(raw map[string]interface{}, path []string) (int, bool) {
	var cur interface{} = raw
	for _, part := range path {
		asMap, ok := cur.(map[string]interface{})
		if !ok {
			return 0, false
		}
		next, ok := asMap[part]
		if !ok {
			return 0, false
		}
		cur = next
	}
	switch n := cur.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		parsed, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
}

// CLIAdapter is the "Good" and "Minimal" integration level adapter.
// It runs the TUT as a subprocess with JSON on stdin/stdout.
type CLIAdapter struct {
	Minimal bool // if true, no @gauntlet.tool hook expected
}

func (a *CLIAdapter) Level() IntegrationLevel {
	if a.Minimal {
		return LevelMinimal
	}
	return LevelGood
}

func (a *CLIAdapter) Start(ctx context.Context, config Config) (Handle, error) {
	return &cliHandle{
		config: config,
		ctx:    ctx,
	}, nil
}

type cliHandle struct {
	config       Config
	ctx          context.Context
	traces       []TraceEvent
	capabilities *SDKCapabilities
}

func (h *cliHandle) Run(ctx context.Context, input scenario.Input) (*AgentOutput, error) {
	cmd := exec.CommandContext(ctx, h.config.Command, h.config.Args...)
	cmd.Dir = h.config.WorkDir
	cmd.Env = mergedProcessEnv(h.config.Env, h.config.RestrictHostEnv)

	// Create temp file for trace events so they don't mix with stdout.
	traceFile, err := os.CreateTemp("", "gauntlet-trace-*.ndjson")
	if err != nil {
		return nil, fmt.Errorf("failed to create trace file: %w", err)
	}
	tracePath := traceFile.Name()
	traceFile.Close()
	defer os.Remove(tracePath)
	cmd.Env = append(cmd.Env, "GAUNTLET_TRACE_FILE="+tracePath)

	payload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal input: %w", err)
	}
	cmd.Stdin = bytes.NewReader(payload)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if h.config.BlockNetworkEgress {
		wrapped, err := wrapWithEgressBlock(cmd)
		if err != nil {
			return nil, fmt.Errorf("failed to apply egress block to TUT command: %w", err)
		}
		cmd = wrapped
	}
	cmd, err = wrapWithHostilePayloadGuardrails(cmd, h.config.Guardrails)
	if err != nil {
		return nil, fmt.Errorf("failed to apply hostile payload guardrails: %w", err)
	}
	cmd, err = wrapWithResourceLimits(cmd, h.config.ResourceLimits)
	if err != nil {
		return nil, fmt.Errorf("failed to apply TUT resource limits: %w", err)
	}

	start := time.Now()
	err = cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("failed to run TUT: %w", err)
		}
	}

	// Parse trace events from the trace file.
	if traces, parseErr := parseTraceFile(tracePath); parseErr == nil {
		h.traces = traces
		h.capabilities = ExtractSDKCapabilities(traces)
	}

	var parsed map[string]interface{}
	_ = json.Unmarshal(stdout.Bytes(), &parsed)

	return &AgentOutput{
		Raw:      stdout.Bytes(),
		Parsed:   parsed,
		ExitCode: exitCode,
		Duration: duration,
		StdErr:   stderr.String(),
	}, nil
}

func (h *cliHandle) Traces() []TraceEvent {
	return h.traces
}

func (h *cliHandle) Capabilities() *SDKCapabilities {
	return cloneSDKCapabilities(h.capabilities)
}

func (h *cliHandle) Stop(ctx context.Context) error {
	return nil // CLI adapter runs per-invocation, nothing to stop
}
