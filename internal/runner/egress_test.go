package runner

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pmclSF/gauntlet/internal/tut"
)

func TestCheckEgressBlocked_OpenWhenSocketConnects(t *testing.T) {
	origDial := dialEgressProbeFn
	origTargets := egressProbeTargets
	t.Cleanup(func() {
		dialEgressProbeFn = origDial
		egressProbeTargets = origTargets
	})

	egressProbeTargets = []string{"1.1.1.1:443"}
	dialEgressProbeFn = func(ctx context.Context, network, address string) (net.Conn, error) {
		return &fakeConn{}, nil
	}

	if got := CheckEgressBlocked(); got != EgressOpen {
		t.Fatalf("CheckEgressBlocked() = %s, want open", got.String())
	}
}

func TestCheckEgressBlocked_OpenWhenConnectionRefused(t *testing.T) {
	origDial := dialEgressProbeFn
	origTargets := egressProbeTargets
	t.Cleanup(func() {
		dialEgressProbeFn = origDial
		egressProbeTargets = origTargets
	})

	egressProbeTargets = []string{"1.1.1.1:443"}
	dialEgressProbeFn = func(ctx context.Context, network, address string) (net.Conn, error) {
		return nil, syscall.ECONNREFUSED
	}

	if got := CheckEgressBlocked(); got != EgressOpen {
		t.Fatalf("CheckEgressBlocked() = %s, want open", got.String())
	}
}

func TestCheckEgressBlocked_BlockedOnExplicitDeny(t *testing.T) {
	origDial := dialEgressProbeFn
	origTargets := egressProbeTargets
	t.Cleanup(func() {
		dialEgressProbeFn = origDial
		egressProbeTargets = origTargets
	})

	egressProbeTargets = []string{"1.1.1.1:443", "8.8.8.8:53"}
	dialEgressProbeFn = func(ctx context.Context, network, address string) (net.Conn, error) {
		return nil, syscall.EPERM
	}

	if got := CheckEgressBlocked(); got != EgressBlocked {
		t.Fatalf("CheckEgressBlocked() = %s, want blocked", got.String())
	}
}

func TestCheckEgressBlocked_UnknownOnAmbiguousError(t *testing.T) {
	origDial := dialEgressProbeFn
	origTargets := egressProbeTargets
	t.Cleanup(func() {
		dialEgressProbeFn = origDial
		egressProbeTargets = origTargets
	})

	egressProbeTargets = []string{"1.1.1.1:443"}
	dialEgressProbeFn = func(ctx context.Context, network, address string) (net.Conn, error) {
		return nil, errors.New("i/o timeout")
	}

	if got := CheckEgressBlocked(); got != EgressUnknown {
		t.Fatalf("CheckEgressBlocked() = %s, want unknown", got.String())
	}
}

type fakeConn struct{}

func (c *fakeConn) Read(p []byte) (n int, err error)  { return 0, nil }
func (c *fakeConn) Write(p []byte) (n int, err error) { return len(p), nil }
func (c *fakeConn) Close() error                      { return nil }
func (c *fakeConn) LocalAddr() net.Addr               { return fakeAddr("local") }
func (c *fakeConn) RemoteAddr() net.Addr              { return fakeAddr("remote") }
func (c *fakeConn) SetDeadline(t time.Time) error     { return nil }
func (c *fakeConn) SetReadDeadline(t time.Time) error { return nil }
func (c *fakeConn) SetWriteDeadline(t time.Time) error {
	return nil
}

type fakeAddr string

func (a fakeAddr) Network() string { return "tcp" }
func (a fakeAddr) String() string  { return string(a) }

func TestEgressEnforcement_BlocksOutboundAttemptAndCapturesArtifact(t *testing.T) {
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("sandbox-exec"); err != nil {
			t.Skip("sandbox-exec not available on host")
		}
	}
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("unshare"); err != nil {
			t.Skip("unshare not available on host")
		}
	}

	origCheck := checkEgressBlockedFn
	checkEgressBlockedFn = func() EgressStatus { return EgressBlocked }
	t.Cleanup(func() { checkEgressBlockedFn = origCheck })

	root := t.TempDir()
	evalsDir := filepath.Join(root, "evals")
	suiteDir := filepath.Join(evalsDir, "smoke")
	toolsDir := filepath.Join(evalsDir, "world", "tools")
	dbDir := filepath.Join(evalsDir, "world", "databases")
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		t.Fatalf("mkdir suite: %v", err)
	}
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("mkdir tools: %v", err)
	}
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("mkdir db: %v", err)
	}
	scenarioYAML := `scenario: egress_blocked
input:
  messages:
    - role: user
      content: test egress
assertions: []
`
	if err := os.WriteFile(filepath.Join(suiteDir, "egress.yaml"), []byte(scenarioYAML), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	scriptPath := filepath.Join(root, "agent.sh")
	script := `#!/bin/sh
cat >/dev/null
python3 - <<'PY'
import json
import socket
import sys
try:
    s = socket.create_connection(("1.1.1.1", 80), timeout=1.0)
    s.close()
    print(json.dumps({"status":"egress_open"}))
    sys.exit(0)
except Exception as exc:
    msg = f"blocked connection attempt: {exc}"
    print(json.dumps({"status":"egress_blocked","detail":msg}))
    sys.exit(1)
PY
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	runDir := filepath.Join(root, "runs")
	r := NewRunner(Config{
		Suite:     "smoke",
		Mode:      "pr_ci",
		EvalsDir:  evalsDir,
		OutputDir: runDir,
		TUTConfig: tut.Config{
			Command: scriptPath,
			Adapter: "cli",
			WorkDir: root,
		},
	})
	r.Adapter = &tut.CLIAdapter{}

	result, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("runner run failed: %v", err)
	}
	if len(result.Scenarios) != 1 {
		t.Fatalf("expected 1 scenario, got %d", len(result.Scenarios))
	}
	scenario := result.Scenarios[0]
	if scenario.Status == "passed" {
		t.Fatalf("scenario unexpectedly passed: %+v", scenario)
	}
	foundExitAssertion := false
	for _, assertion := range scenario.Assertions {
		if assertion.AssertionType == "tut_exit_nonzero" {
			foundExitAssertion = true
			break
		}
	}
	if !foundExitAssertion {
		t.Fatalf("expected tut_exit_nonzero assertion, got %+v", scenario.Assertions)
	}

	artifactPath := filepath.Join(runDir, scenario.Name, "pr_output.json")
	artifactRaw, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		t.Fatalf("read artifact %s: %v", artifactPath, readErr)
	}
	artifactText := strings.ToLower(string(artifactRaw))
	if strings.Contains(artifactText, "blocked connection attempt") ||
		strings.Contains(artifactText, "sandbox-exec") ||
		strings.Contains(artifactText, "unshare") ||
		strings.Contains(artifactText, "network address") {
		return
	}

	for _, assertion := range scenario.Assertions {
		msg := strings.ToLower(assertion.Message)
		if strings.Contains(msg, "egress block") ||
			strings.Contains(msg, "operation not permitted") ||
			strings.Contains(msg, "network is unreachable") {
			return
		}
	}
	t.Fatalf("expected egress-related evidence in artifact or assertions, artifact:\n%s\nassertions: %+v", string(artifactRaw), scenario.Assertions)
}
