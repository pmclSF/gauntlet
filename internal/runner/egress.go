package runner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
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

var egressProbeTargets = []string{
	"1.1.1.1:443", // Cloudflare
	"8.8.8.8:53",  // Google DNS
	"9.9.9.9:53",  // Quad9 DNS
}

var dialEgressProbeFn = func(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 350 * time.Millisecond}
	return dialer.DialContext(ctx, network, address)
}

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

// CheckEgressBlocked probes outbound TCP sockets directly (without DNS) to
// classify whether egress appears open, blocked, or unknown.
func CheckEgressBlocked() EgressStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	sawBlockedSignal := false
	for _, target := range egressProbeTargets {
		conn, err := dialEgressProbeFn(ctx, "tcp", target)
		if err == nil {
			_ = conn.Close()
			return EgressOpen
		}
		if isConnectionRefused(err) {
			return EgressOpen
		}
		if isLikelyBlockedEgress(err) {
			sawBlockedSignal = true
			continue
		}
		// Ambiguous network failures should not be interpreted as blocked.
		return EgressUnknown
	}
	if sawBlockedSignal {
		return EgressBlocked
	}
	return EgressUnknown
}

func isConnectionRefused(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "connection refused")
}

func isLikelyBlockedEgress(err error) bool {
	if errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.EACCES) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EHOSTUNREACH) {
		return true
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "operation not permitted") ||
		strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "no route to host")
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
	// TODO(stage1): Duplicates sandbox-exec wrapping in tut/process.go — unify into shared package.
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
	// TODO(stage1): Duplicates unshare wrapping in tut/process.go — unify into shared package.
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
