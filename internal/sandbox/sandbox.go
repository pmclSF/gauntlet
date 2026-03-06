// Package sandbox provides platform-specific process isolation wrappers.
// It unifies egress blocking (sandbox-exec on macOS, unshare --net on Linux)
// into a single shared implementation.
package sandbox

import (
	"fmt"
	"os/exec"
	"runtime"
)

// WrapWithEgressBlock wraps a command to block outbound network egress while
// allowing localhost (needed for the MITM proxy).
func WrapWithEgressBlock(cmd *exec.Cmd) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		return wrapDarwin(cmd)
	case "linux":
		return wrapLinux(cmd)
	default:
		return nil, fmt.Errorf("egress blocking not supported on %s — run in a container", runtime.GOOS)
	}
}

func wrapDarwin(cmd *exec.Cmd) (*exec.Cmd, error) {
	// sandbox-exec denies outbound network except localhost (needed for MITM proxy).
	profile := `(version 1)(allow default)(deny network-outbound)(allow network-outbound (remote ip "localhost:*"))(allow network-outbound (remote ip "127.0.0.1:*"))`
	args := []string{"-p", profile, cmd.Path}
	args = append(args, cmd.Args[1:]...)
	wrapped := exec.Command("sandbox-exec", args...)
	copyCmd(cmd, wrapped)
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
	copyCmd(cmd, wrapped)
	return wrapped, nil
}

// copyCmd transfers Dir, Env, and stdio from src to dst.
func copyCmd(src, dst *exec.Cmd) {
	dst.Dir = src.Dir
	dst.Env = src.Env
	dst.Stdin = src.Stdin
	dst.Stdout = src.Stdout
	dst.Stderr = src.Stderr
}
