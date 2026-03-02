// Package tut defines the Target Under Test adapter interface.
// TUT adapters start and communicate with the agent being tested.
package tut

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gauntlet-dev/gauntlet/internal/scenario"
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
)

// Config holds configuration for starting a TUT.
type Config struct {
	Command    string            `yaml:"command" json:"command"`
	Args       []string          `yaml:"args" json:"args"`
	WorkDir    string            `yaml:"work_dir" json:"work_dir"`
	Env        map[string]string `yaml:"env" json:"env"`
	Adapter    string            `yaml:"adapter" json:"adapter"` // "http", "cli", "minimal"
	HTTPPort   int               `yaml:"http_port" json:"http_port"`
	HTTPPath   string            `yaml:"http_path" json:"http_path"`
	StartupMs  int               `yaml:"startup_ms" json:"startup_ms"`
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
	EventType  string           `json:"event_type"` // "tool_call", "model_call"
	ToolName   string           `json:"tool_name,omitempty"`
	Args       json.RawMessage  `json:"args,omitempty"`
	Response   json.RawMessage  `json:"response,omitempty"`
	ModelCall  *ModelCallEvent  `json:"model_call,omitempty"`
	Timestamp  time.Time        `json:"timestamp"`
	DurationMs int              `json:"duration_ms"`
}

// ModelCallEvent records details of a model API call.
type ModelCallEvent struct {
	ProviderFamily string `json:"provider_family"`
	Model          string `json:"model"`
	CanonicalHash  string `json:"canonical_hash"`
}
