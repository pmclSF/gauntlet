package runner

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
	"time"
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
