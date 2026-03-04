package tut

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"time"

	"github.com/gauntlet-dev/gauntlet/internal/scenario"
)

// HTTPAdapter is the "Best" and "Good" integration level adapter.
// It starts the TUT as a subprocess and communicates via HTTP.
type HTTPAdapter struct{}

func (a *HTTPAdapter) Level() IntegrationLevel { return LevelBest }

func (a *HTTPAdapter) Start(ctx context.Context, config Config) (Handle, error) {
	cmd := exec.CommandContext(ctx, config.Command, config.Args...)
	cmd.Dir = config.WorkDir

	// Build environment.
	cmd.Env = mergedProcessEnv(config.Env, config.RestrictHostEnv)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if config.BlockNetworkEgress {
		wrapped, err := wrapWithEgressBlock(cmd)
		if err != nil {
			return nil, fmt.Errorf("failed to apply egress block to TUT command: %w", err)
		}
		cmd = wrapped
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start TUT: %w", err)
	}

	// Wait for TUT to be ready
	startupTimeout := time.Duration(config.StartupMs) * time.Millisecond
	if startupTimeout == 0 {
		startupTimeout = 5 * time.Second
	}

	port := config.HTTPPort
	if port == 0 {
		port = 8000
	}
	baseURL := fmt.Sprintf("http://localhost:%d", port)
	path := config.HTTPPath
	if path == "" {
		path = "/run"
	}

	deadline := time.Now().Add(startupTimeout)
	ready := false
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("TUT did not become healthy at %s/health within %s (stderr: %s)", baseURL, startupTimeout, stderr.String())
	}

	return &httpHandle{
		cmd:     cmd,
		baseURL: baseURL,
		path:    path,
		stderr:  &stderr,
	}, nil
}

type httpHandle struct {
	cmd     *exec.Cmd
	baseURL string
	path    string
	stderr  *bytes.Buffer
	traces  []TraceEvent
}

func (h *httpHandle) Run(ctx context.Context, input scenario.Input) (*AgentOutput, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal input: %w", err)
	}

	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, "POST", h.baseURL+h.path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("TUT request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read TUT response: %w", err)
	}

	duration := time.Since(start)

	var parsed map[string]interface{}
	_ = json.Unmarshal(body, &parsed)

	return &AgentOutput{
		Raw:      body,
		Parsed:   parsed,
		ExitCode: 0,
		Duration: duration,
		StdErr:   h.stderr.String(),
	}, nil
}

func (h *httpHandle) Traces() []TraceEvent {
	return h.traces
}

func (h *httpHandle) Stop(ctx context.Context) error {
	if h.cmd.Process != nil {
		return h.cmd.Process.Kill()
	}
	return nil
}
