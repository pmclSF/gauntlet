// Package tut defines the Target Under Test adapter interface.
// TUT adapters start and communicate with the agent being tested.
package tut

import (
	"context"
	"encoding/json"
	"time"

	"github.com/pmclSF/gauntlet/internal/scenario"
)

// IntegrationLevel describes how deeply the TUT is integrated with Gauntlet.
type IntegrationLevel string

const (
	// LevelBest is HTTP endpoint + @gauntlet.tool decorator.
	LevelBest IntegrationLevel = "best"
	// LevelGood is HTTP endpoint only (proxy intercepts model calls).
	LevelGood IntegrationLevel = "good"
	// LevelMinimal is CLI only — no hook, just egress + budget.
	LevelMinimal IntegrationLevel = "minimal"

	// CapabilityProtocolV1 is the current SDK capability negotiation protocol.
	CapabilityProtocolV1 = 1
)

// Config holds configuration for starting a TUT.
type Config struct {
	Command            string            `yaml:"command" json:"command"`
	Args               []string          `yaml:"args" json:"args"`
	WorkDir            string            `yaml:"work_dir" json:"work_dir"`
	Env                map[string]string `yaml:"env" json:"env"`
	Adapter            string            `yaml:"adapter" json:"adapter"` // "http", "cli", "minimal"
	HTTPPort           int               `yaml:"http_port" json:"http_port"`
	HTTPPath           string            `yaml:"http_path" json:"http_path"`
	StartupMs          int               `yaml:"startup_ms" json:"startup_ms"`
	ResourceLimits     ResourceLimits    `yaml:"resource_limits" json:"resource_limits"`
	Guardrails         Guardrails        `yaml:"guardrails" json:"guardrails"`
	BlockNetworkEgress bool              `yaml:"block_network_egress" json:"block_network_egress"`
	RestrictHostEnv    bool              `yaml:"restrict_host_env" json:"restrict_host_env"`
}

// ResourceLimits configures per-scenario process resource limits for the TUT.
type ResourceLimits struct {
	CPUSeconds int   `yaml:"cpu_seconds" json:"cpu_seconds"`
	MemoryMB   int64 `yaml:"memory_mb" json:"memory_mb"`
	OpenFiles  int   `yaml:"open_files" json:"open_files"`
}

func (r ResourceLimits) IsZero() bool {
	return r.CPUSeconds <= 0 && r.MemoryMB <= 0 && r.OpenFiles <= 0
}

// Guardrails configures optional hostile-payload hardening.
type Guardrails struct {
	HostilePayload bool `yaml:"hostile_payload" json:"hostile_payload"`
	// MaxProcesses sets RLIMIT_NPROC via shell ulimit -u when hostile guardrails
	// are enabled. Values <= 0 use a deterministic default.
	MaxProcesses int `yaml:"max_processes" json:"max_processes"`
}

// Adapter starts and manages a TUT process.
type Adapter interface {
	// Level returns the integration level.
	Level() IntegrationLevel
	// Start launches the TUT and returns a handle to interact with it.
	Start(ctx context.Context, config Config) (Handle, error)
}

// Handle is a running TUT instance.
type Handle interface {
	// Run sends input to the TUT and returns its output.
	Run(ctx context.Context, input scenario.Input) (*AgentOutput, error)
	// Traces returns structured trace events from the last run.
	Traces() []TraceEvent
	// Stop shuts down the TUT.
	Stop(ctx context.Context) error
}

// CapabilityProvider is implemented by handles that can report negotiated SDK
// capabilities discovered during execution.
type CapabilityProvider interface {
	Capabilities() *SDKCapabilities
}

// AgentOutput is the raw and parsed output from a TUT run.
type AgentOutput struct {
	Raw      []byte                 `json:"raw"`
	Parsed   map[string]interface{} `json:"parsed"`
	ExitCode int                    `json:"exit_code"`
	Duration time.Duration          `json:"duration"`
	StdErr   string                 `json:"stderr,omitempty"`
}

// TraceEvent is a structured event from TUT execution.
type TraceEvent struct {
	EventType       string                `json:"event_type"` // "tool_call", "model_call", "sdk_capabilities", "determinism_env"
	ToolName        string                `json:"tool_name,omitempty"`
	Args            json.RawMessage       `json:"args,omitempty"`
	Response        json.RawMessage       `json:"response,omitempty"`
	ModelCall       *ModelCallEvent       `json:"model_call,omitempty"`
	SDKCapabilities *SDKCapabilities      `json:"sdk_capabilities,omitempty"`
	DeterminismEnv  *DeterminismEnvReport `json:"determinism_env,omitempty"`
	Timestamp       time.Time             `json:"timestamp"`
	DurationMs      int                   `json:"duration_ms"`
}

// ModelCallEvent records details of a model API call.
type ModelCallEvent struct {
	ProviderFamily   string `json:"provider_family"`
	Model            string `json:"model"`
	CanonicalHash    string `json:"canonical_hash"`
	PromptTokens     int    `json:"prompt_tokens,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"`
}

// SDKCapabilities reports adapter instrumentation support negotiated from SDK
// runtime metadata.
type SDKCapabilities struct {
	ProtocolVersion int                             `json:"protocol_version"`
	SDK             string                          `json:"sdk,omitempty"`
	SDKVersion      string                          `json:"sdk_version,omitempty"`
	Runtime         string                          `json:"runtime,omitempty"`
	Adapters        map[string]SDKAdapterCapability `json:"adapters,omitempty"`
}

// SDKAdapterCapability describes support for one adapter family.
type SDKAdapterCapability struct {
	Enabled bool   `json:"enabled"`
	Patched bool   `json:"patched"`
	Reason  string `json:"reason,omitempty"`
}

// DeterminismEnvReport verifies whether runtime freeze controls were applied.
type DeterminismEnvReport struct {
	Language            string `json:"language,omitempty"`
	Runtime             string `json:"runtime,omitempty"`
	RequestedFreezeTime string `json:"requested_freeze_time,omitempty"`
	TimePatched         bool   `json:"time_patched"`
	RequestedTimezone   string `json:"requested_timezone,omitempty"`
	EffectiveTimezone   string `json:"effective_timezone,omitempty"`
	TimezoneApplied     bool   `json:"timezone_applied"`
	RequestedLocale     string `json:"requested_locale,omitempty"`
	EffectiveLocale     string `json:"effective_locale,omitempty"`
	LocaleApplied       bool   `json:"locale_applied"`
}

// ExtractSDKCapabilities returns the most recent sdk_capabilities report from
// the trace stream.
func ExtractSDKCapabilities(events []TraceEvent) *SDKCapabilities {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].EventType != "sdk_capabilities" {
			continue
		}
		if events[i].SDKCapabilities == nil {
			continue
		}
		return cloneSDKCapabilities(events[i].SDKCapabilities)
	}
	return nil
}

// ExtractDeterminismEnvReport returns the most recent determinism_env report
// from the trace stream.
func ExtractDeterminismEnvReport(events []TraceEvent) *DeterminismEnvReport {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].EventType != "determinism_env" {
			continue
		}
		if events[i].DeterminismEnv == nil {
			continue
		}
		report := *events[i].DeterminismEnv
		return &report
	}
	return nil
}

func cloneSDKCapabilities(in *SDKCapabilities) *SDKCapabilities {
	if in == nil {
		return nil
	}
	out := *in
	if len(in.Adapters) > 0 {
		out.Adapters = make(map[string]SDKAdapterCapability, len(in.Adapters))
		for key, value := range in.Adapters {
			out.Adapters[key] = value
		}
	}
	return &out
}
