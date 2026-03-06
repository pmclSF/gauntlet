package tut

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pmclSF/gauntlet/internal/scenario"
)

func writeExecutableScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write script %s: %v", path, err)
	}
	return path
}

func reservePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer func() { _ = ln.Close() }()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("listener did not return TCP address")
	}
	return addr.Port
}

func TestCLIAdapter_RunAndTraceCapabilities(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script execution is unix-only")
	}

	tmp := t.TempDir()
	script := writeExecutableScript(t, tmp, "agent.sh", `#!/bin/sh
cat >/dev/null
echo '{"result":"ok"}'
if [ -n "${GAUNTLET_TRACE_FILE:-}" ]; then
  printf '%s\n' '{"gauntlet_event":true,"type":"sdk_capabilities","timestamp":1,"result":{"protocol_version":1,"adapters":{"openai":{"enabled":true,"patched":true}}}}' >> "$GAUNTLET_TRACE_FILE"
  printf '%s\n' '{"gauntlet_event":true,"type":"tool_call","timestamp":2,"tool_name":"order_lookup","args":{"order_id":"ord_1"},"result":{"status":"ok"}}' >> "$GAUNTLET_TRACE_FILE"
fi
`)

	adapter := &CLIAdapter{}
	handle, err := adapter.Start(context.Background(), Config{
		Command: script,
		WorkDir: tmp,
	})
	if err != nil {
		t.Fatalf("CLIAdapter.Start: %v", err)
	}

	out, err := handle.Run(context.Background(), scenario.Input{
		Messages: []scenario.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("CLI handle Run: %v", err)
	}
	if out.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", out.ExitCode)
	}
	if got := fmt.Sprint(out.Parsed["result"]); got != "ok" {
		t.Fatalf("parsed result = %q, want ok", got)
	}

	traces := handle.Traces()
	if len(traces) < 2 {
		t.Fatalf("expected at least 2 traces, got %d", len(traces))
	}
	capProvider, ok := handle.(CapabilityProvider)
	if !ok {
		t.Fatal("expected CLI handle to implement CapabilityProvider")
	}
	caps := capProvider.Capabilities()
	if caps == nil || !caps.Adapters["openai"].Patched {
		t.Fatalf("expected patched openai capability, got %+v", caps)
	}

	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("CLI handle Stop: %v", err)
	}
}

func TestCLIAdapter_RunNonZeroExitCodeDoesNotRaise(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script execution is unix-only")
	}

	tmp := t.TempDir()
	script := writeExecutableScript(t, tmp, "exit7.sh", `#!/bin/sh
echo '{"result":"bad"}'
exit 7
`)

	adapter := &CLIAdapter{}
	handle, err := adapter.Start(context.Background(), Config{
		Command: script,
		WorkDir: tmp,
	})
	if err != nil {
		t.Fatalf("CLIAdapter.Start: %v", err)
	}

	out, err := handle.Run(context.Background(), scenario.Input{})
	if err != nil {
		t.Fatalf("Run should not fail for non-zero process exit: %v", err)
	}
	if out.ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7", out.ExitCode)
	}
}

func TestHTTPAdapter_StartRunTracesStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script execution is unix-only")
	}

	port := reservePort(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	server := &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
	}
	go func() { _ = server.ListenAndServe() }()
	defer func() { _ = server.Shutdown(context.Background()) }()

	adapter := &HTTPAdapter{}
	handle, err := adapter.Start(context.Background(), Config{
		Command:   "/bin/sh",
		Args:      []string{"-c", "sleep 0.1"},
		HTTPPort:  port,
		HTTPPath:  "/run",
		StartupMs: 2000,
	})
	if err != nil {
		t.Fatalf("HTTPAdapter.Start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	out, err := handle.Run(context.Background(), scenario.Input{
		Payload: map[string]interface{}{"query": "status"},
	})
	if err != nil {
		t.Fatalf("HTTP handle Run: %v", err)
	}
	if got := fmt.Sprint(out.Parsed["ok"]); got != "true" {
		t.Fatalf("parsed ok = %q, want true", got)
	}

	hh, ok := handle.(*httpHandle)
	if !ok {
		t.Fatalf("expected *httpHandle, got %T", handle)
	}
	eventLine := `{"gauntlet_event":true,"type":"sdk_capabilities","timestamp":2,"result":{"protocol_version":1,"adapters":{"anthropic":{"enabled":true,"patched":true}}}}`
	if err := os.WriteFile(hh.tracePath, []byte(eventLine+"\n"), 0o644); err != nil {
		t.Fatalf("write trace file: %v", err)
	}
	traces := handle.Traces()
	if len(traces) != 1 || traces[0].EventType != "sdk_capabilities" {
		t.Fatalf("unexpected traces: %+v", traces)
	}
	capProvider, ok := handle.(CapabilityProvider)
	if !ok {
		t.Fatal("expected HTTP handle to implement CapabilityProvider")
	}
	caps := capProvider.Capabilities()
	if caps == nil || !caps.Adapters["anthropic"].Patched {
		t.Fatalf("expected anthropic capability report, got %+v", caps)
	}

	if err := handle.Stop(context.Background()); err != nil && !strings.Contains(err.Error(), "process already finished") {
		t.Fatalf("HTTP handle Stop: %v", err)
	}
}

func TestProcessWrappers_BuildStableCommands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command wrappers are unix-focused")
	}

	base := exec.Command("/bin/echo", "hello")
	base.Dir = "/tmp"
	base.Env = []string{"A=1"}

	wrappedLimits := wrapUnixResourceLimits(base, ResourceLimits{CPUSeconds: 2, OpenFiles: 64})
	if wrappedLimits.Path != "/bin/sh" {
		t.Fatalf("wrapUnixResourceLimits path = %q, want /bin/sh", wrappedLimits.Path)
	}
	if wrappedLimits.Dir != base.Dir {
		t.Fatalf("expected wrapped dir %q, got %q", base.Dir, wrappedLimits.Dir)
	}
	if len(wrappedLimits.Env) != 1 || wrappedLimits.Env[0] != "A=1" {
		t.Fatalf("expected wrapped env to be preserved, got %v", wrappedLimits.Env)
	}

	guarded := wrapWithMaxProcesses(base, 11)
	if guarded.Path != "/bin/sh" {
		t.Fatalf("wrapWithMaxProcesses path = %q, want /bin/sh", guarded.Path)
	}
	if !strings.Contains(strings.Join(guarded.Args, " "), "ulimit -u 11") {
		t.Fatalf("expected max process limit in args: %v", guarded.Args)
	}

	darwinWrapped, err := wrapDarwin(base)
	if err != nil {
		t.Fatalf("wrapDarwin: %v", err)
	}
	if filepath.Base(darwinWrapped.Path) != "sandbox-exec" {
		t.Fatalf("wrapDarwin path = %q, want sandbox-exec basename", darwinWrapped.Path)
	}

	linuxWrapped, err := wrapLinux(base)
	if err != nil {
		t.Fatalf("wrapLinux: %v", err)
	}
	if filepath.Base(linuxWrapped.Path) != "unshare" {
		t.Fatalf("wrapLinux path = %q, want unshare basename", linuxWrapped.Path)
	}
}
