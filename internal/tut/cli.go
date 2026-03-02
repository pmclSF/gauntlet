package tut

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/gauntlet-dev/gauntlet/internal/scenario"
)

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
	config Config
	ctx    context.Context
	traces []TraceEvent
}

func (h *cliHandle) Run(ctx context.Context, input scenario.Input) (*AgentOutput, error) {
	cmd := exec.CommandContext(ctx, h.config.Command, h.config.Args...)
	cmd.Dir = h.config.WorkDir

	env := make([]string, 0, len(h.config.Env))
	for k, v := range h.config.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env

	payload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal input: %w", err)
	}
	cmd.Stdin = bytes.NewReader(payload)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

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

func (h *cliHandle) Stop(ctx context.Context) error {
	return nil // CLI adapter runs per-invocation, nothing to stop
}
