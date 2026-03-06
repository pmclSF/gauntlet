package runner

import (
	"context"
	"errors"
	"net"
	"os"
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

