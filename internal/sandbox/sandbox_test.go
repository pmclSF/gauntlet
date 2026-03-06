package sandbox

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWrapDarwin_UsesSandboxExec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	base := exec.Command("/bin/echo", "hello")
	base.Dir = "/tmp"
	base.Env = []string{"A=1"}

	wrapped, err := wrapDarwin(base)
	if err != nil {
		t.Fatalf("wrapDarwin: %v", err)
	}
	if filepath.Base(wrapped.Path) != "sandbox-exec" {
		t.Fatalf("wrapDarwin path = %q, want sandbox-exec basename", wrapped.Path)
	}
	if wrapped.Dir != base.Dir {
		t.Fatalf("expected dir %q, got %q", base.Dir, wrapped.Dir)
	}
	if len(wrapped.Env) != 1 || wrapped.Env[0] != "A=1" {
		t.Fatalf("expected env preserved, got %v", wrapped.Env)
	}
	// Profile must allow localhost for proxy
	args := strings.Join(wrapped.Args, " ")
	if !strings.Contains(args, "localhost") {
		t.Fatalf("expected localhost allow in profile, got args: %s", args)
	}
}

func TestWrapLinux_UsesUnshare(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	base := exec.Command("/bin/echo", "hello")
	base.Dir = "/tmp"
	base.Env = []string{"B=2"}

	wrapped, err := wrapLinux(base)
	if err != nil {
		t.Fatalf("wrapLinux: %v", err)
	}
	if filepath.Base(wrapped.Path) != "unshare" {
		t.Fatalf("wrapLinux path = %q, want unshare basename", wrapped.Path)
	}
	if wrapped.Dir != base.Dir {
		t.Fatalf("expected dir %q, got %q", base.Dir, wrapped.Dir)
	}
	// Must bring up loopback for proxy
	args := strings.Join(wrapped.Args, " ")
	if !strings.Contains(args, "ip link set lo up") {
		t.Fatalf("expected loopback bringup in args: %s", args)
	}
}

func TestCopyCmd_PreservesFields(t *testing.T) {
	src := exec.Command("/bin/true")
	src.Dir = "/home"
	src.Env = []string{"X=1", "Y=2"}

	dst := exec.Command("/bin/false")
	copyCmd(src, dst)

	if dst.Dir != "/home" {
		t.Fatalf("Dir not copied: %q", dst.Dir)
	}
	if len(dst.Env) != 2 {
		t.Fatalf("Env not copied: %v", dst.Env)
	}
}
