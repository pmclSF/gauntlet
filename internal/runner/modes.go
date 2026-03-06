package runner

import (
	"strings"

	"github.com/pmclSF/gauntlet/internal/assertions"
	"github.com/pmclSF/gauntlet/internal/tut"
)

func modeRequiresBlockedEgress(mode string) bool {
	return mode == "pr_ci" || mode == "fork_pr"
}

func (r *Runner) buildTUTConfig(requiresBlockedEgress bool) tut.Config {
	cfg := r.Config.TUTConfig
	cfg.Env = cloneStringMap(cfg.Env)
	if r.Config.Mode == "fork_pr" || r.Config.Mode == "pr_ci" {
		cfg.RestrictHostEnv = true
		cfg.Env = stripSensitiveEnv(cfg.Env)
	}
	for _, kv := range r.Harness.Env() {
		if k, v, ok := splitEnvVar(kv); ok {
			cfg.Env[k] = v
		}
	}
	cfg.BlockNetworkEgress = requiresBlockedEgress
	return cfg
}

func stripSensitiveEnv(in map[string]string) map[string]string {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if isSensitiveEnvKey(k) {
			continue
		}
		out[k] = v
	}
	return out
}

func isSensitiveEnvKey(key string) bool {
	k := strings.ToUpper(strings.TrimSpace(key))
	if k == "" {
		return false
	}
	known := map[string]bool{
		"OPENAI_API_KEY":                 true,
		"ANTHROPIC_API_KEY":              true,
		"GOOGLE_API_KEY":                 true,
		"GOOGLE_APPLICATION_CREDENTIALS": true,
		"AWS_ACCESS_KEY_ID":              true,
		"AWS_SECRET_ACCESS_KEY":          true,
		"AWS_SESSION_TOKEN":              true,
		"COHERE_API_KEY":                 true,
	}
	if known[k] {
		return true
	}
	if strings.Contains(k, "API_KEY") ||
		strings.Contains(k, "SECRET") ||
		strings.Contains(k, "TOKEN") ||
		strings.Contains(k, "PASSWORD") {
		return true
	}
	return false
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func splitEnvVar(kv string) (string, string, bool) {
	parts := strings.SplitN(kv, "=", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func enforceAssertionMode(results []assertions.Result, hardGates, softSignals map[string]bool) []assertions.Result {
	if len(hardGates) == 0 && len(softSignals) == 0 {
		return results
	}
	for i := range results {
		name := strings.TrimSpace(results[i].AssertionType)
		if name == "" {
			continue
		}
		if softSignals[name] {
			results[i].Soft = true
			continue
		}
		if hardGates[name] {
			results[i].Soft = false
		}
	}
	return results
}
