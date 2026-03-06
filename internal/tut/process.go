package tut

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func mergedProcessEnv(overrides map[string]string, restrictHostEnv bool) []string {
	// Preserve current process environment (PATH, HOME, etc.) and apply overrides.
	envMap := make(map[string]string)
	if restrictHostEnv {
		for _, key := range []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP", "SHELL", "LANG", "LC_ALL", "LC_CTYPE", "TERM", "USER"} {
			if val, ok := os.LookupEnv(key); ok {
				envMap[key] = val
			}
		}
	} else {
		for _, kv := range os.Environ() {
			for i := 0; i < len(kv); i++ {
				if kv[i] == '=' {
					envMap[kv[:i]] = kv[i+1:]
					break
				}
			}
		}
	}

	for k, v := range overrides {
		envMap[k] = v
	}

	env := make([]string, 0, len(envMap))
	keys := make([]string, 0, len(envMap))
	for k := range envMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := envMap[k]
		env = append(env, k+"="+v)
	}
	return env
}

func wrapWithEgressBlock(cmd *exec.Cmd) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		return wrapDarwin(cmd)
	case "linux":
		return wrapLinux(cmd)
	default:
		return nil, fmt.Errorf("egress blocking not supported on %s — run in a container", runtime.GOOS)
	}
}

func wrapWithResourceLimits(cmd *exec.Cmd, limits ResourceLimits) (*exec.Cmd, error) {
	if limits.IsZero() {
		return cmd, nil
	}
	switch runtime.GOOS {
	case "darwin", "linux":
		return wrapUnixResourceLimits(cmd, limits), nil
	default:
		return nil, fmt.Errorf("resource limits are not supported on %s", runtime.GOOS)
	}
}

func wrapWithHostilePayloadGuardrails(cmd *exec.Cmd, guardrails Guardrails) (*exec.Cmd, error) {
	if !guardrails.HostilePayload {
		return cmd, nil
	}
	maxProcs := guardrails.MaxProcesses
	if maxProcs <= 0 {
		maxProcs = 64
	}

	switch runtime.GOOS {
	case "linux":
		// If command is already unshare-wrapped (e.g. from egress isolation), add
		// process-count guardrail without nesting another unshare.
		if filepath.Base(cmd.Path) == "unshare" {
			return wrapWithMaxProcesses(cmd, maxProcs), nil
		}
		if _, err := exec.LookPath("unshare"); err != nil {
			return nil, fmt.Errorf("hostile payload guardrails require 'unshare' on linux: %w", err)
		}
		script := buildGuardrailScript(maxProcs)
		args := []string{
			"--fork",
			"--pid",
			"--mount-proc",
			"--ipc",
			"--uts",
			"--",
			"/bin/sh",
			"-c",
			script,
			"gauntlet-hostile-guardrail",
			cmd.Path,
		}
		args = append(args, cmd.Args[1:]...)
		wrapped := exec.Command("unshare", args...)
		wrapped.Dir = cmd.Dir
		wrapped.Env = cmd.Env
		wrapped.Stdin = cmd.Stdin
		wrapped.Stdout = cmd.Stdout
		wrapped.Stderr = cmd.Stderr
		return wrapped, nil
	default:
		return nil, fmt.Errorf("hostile payload guardrails are currently supported only on linux")
	}
}

func wrapUnixResourceLimits(cmd *exec.Cmd, limits ResourceLimits) *exec.Cmd {
	script := buildResourceLimitScript(limits)
	args := []string{"-c", script, "gauntlet-resource-wrapper", cmd.Path}
	args = append(args, cmd.Args[1:]...)
	wrapped := exec.Command("/bin/sh", args...)
	wrapped.Dir = cmd.Dir
	wrapped.Env = cmd.Env
	wrapped.Stdin = cmd.Stdin
	wrapped.Stdout = cmd.Stdout
	wrapped.Stderr = cmd.Stderr
	return wrapped
}

func buildResourceLimitScript(limits ResourceLimits) string {
	lines := []string{"set -eu"}
	if limits.OpenFiles > 0 {
		lines = append(lines, fmt.Sprintf("ulimit -n %d", limits.OpenFiles))
	}
	if limits.CPUSeconds > 0 {
		lines = append(lines, fmt.Sprintf("ulimit -t %d", limits.CPUSeconds))
	}
	if limits.MemoryMB > 0 {
		memoryKB := limits.MemoryMB * 1024
		lines = append(lines, fmt.Sprintf("(ulimit -v %d >/dev/null 2>&1 || ulimit -d %d >/dev/null 2>&1 || (echo \"gauntlet: requested memory_mb limit is unsupported by /bin/sh ulimit\" >&2; exit 73))", memoryKB, memoryKB))
	}
	lines = append(lines, "exec \"$@\"")
	return strings.Join(lines, "\n")
}

func buildGuardrailScript(maxProcs int) string {
	lines := []string{
		"set -eu",
		fmt.Sprintf("ulimit -u %d", maxProcs),
		"exec \"$@\"",
	}
	return strings.Join(lines, "\n")
}

func wrapWithMaxProcesses(cmd *exec.Cmd, maxProcs int) *exec.Cmd {
	script := buildGuardrailScript(maxProcs)
	args := []string{"-c", script, "gauntlet-guardrail-maxprocs", cmd.Path}
	args = append(args, cmd.Args[1:]...)
	wrapped := exec.Command("/bin/sh", args...)
	wrapped.Dir = cmd.Dir
	wrapped.Env = cmd.Env
	wrapped.Stdin = cmd.Stdin
	wrapped.Stdout = cmd.Stdout
	wrapped.Stderr = cmd.Stderr
	return wrapped
}

func wrapDarwin(cmd *exec.Cmd) (*exec.Cmd, error) {
	// sandbox-exec denies outbound network except localhost (needed for MITM proxy).
	profile := `(version 1)(allow default)(deny network-outbound)(allow network-outbound (remote ip "localhost:*"))(allow network-outbound (remote ip "127.0.0.1:*"))`
	args := []string{"-p", profile, cmd.Path}
	args = append(args, cmd.Args[1:]...)
	wrapped := exec.Command("sandbox-exec", args...)
	wrapped.Dir = cmd.Dir
	wrapped.Env = cmd.Env
	wrapped.Stdin = cmd.Stdin
	wrapped.Stdout = cmd.Stdout
	wrapped.Stderr = cmd.Stderr
	return wrapped, nil
}

func wrapLinux(cmd *exec.Cmd) (*exec.Cmd, error) {
	// unshare --net creates an isolated network namespace.
	// We bring up the loopback interface so the TUT can reach the MITM proxy.
	shell := fmt.Sprintf("ip link set lo up 2>/dev/null; exec %q", cmd.Path)
	for _, a := range cmd.Args[1:] {
		shell += fmt.Sprintf(" %q", a)
	}
	wrapped := exec.Command("unshare", "--net", "sh", "-c", shell)
	wrapped.Dir = cmd.Dir
	wrapped.Env = cmd.Env
	wrapped.Stdin = cmd.Stdin
	wrapped.Stdout = cmd.Stdout
	wrapped.Stderr = cmd.Stderr
	return wrapped, nil
}
