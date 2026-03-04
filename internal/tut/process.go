package tut

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
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
	for k, v := range envMap {
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

func wrapDarwin(cmd *exec.Cmd) (*exec.Cmd, error) {
	// sandbox-exec applies a deny-network policy for the child process.
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
	// unshare --net executes in an isolated network namespace.
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
