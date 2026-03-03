package runner

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// EgressStatus represents the state of network egress blocking.
type EgressStatus int

const (
	EgressBlocked EgressStatus = iota // Network is blocked
	EgressOpen                        // Network is accessible
	EgressUnknown                     // Could not determine
)

// checkEgressBlockedFn is overridden in tests to make mode enforcement deterministic.
var checkEgressBlockedFn = CheckEgressBlocked

func (s EgressStatus) String() string {
	switch s {
	case EgressBlocked:
		return "blocked"
	case EgressOpen:
		return "open"
	default:
		return "unknown"
	}
}

// InCIContext returns true if running in a CI environment.
func InCIContext() bool {
	return os.Getenv("CI") == "true" || os.Getenv("GITHUB_ACTIONS") == "true"
}

// CheckEgressBlocked performs a canary DNS lookup to detect if network is blocked.
func CheckEgressBlocked() EgressStatus {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resolver := &net.Resolver{}
	_, err := resolver.LookupHost(ctx, "dns.google")
	if err != nil {
		return EgressBlocked
	}
	return EgressOpen
}

// WrapWithEgressBlock wraps a command to block network egress.
// Uses platform-specific mechanisms.
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
	// Use sandbox-exec with a deny-network profile
	profile := `(version 1)(allow default)(deny network*)`
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
	// Use unshare --net to create a new network namespace
	args := []string{"--net", cmd.Path}
	args = append(args, cmd.Args[1:]...)
	wrapped := exec.Command("unshare", args...)
	wrapped.Dir = cmd.Dir
	wrapped.Env = cmd.Env
	wrapped.Stdin = cmd.Stdin
	wrapped.Stdout = cmd.Stdout
	wrapped.Stderr = cmd.Stderr
	return wrapped, nil
}
