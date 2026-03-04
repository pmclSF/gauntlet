package fixture

import (
	"os"
	"runtime"
	"strings"
)

// BuildProvenance returns provenance metadata for fixture creation.
// Headers may be nil; header keys are matched case-insensitively.
func BuildProvenance(headers map[string]string, source string) *Provenance {
	toolchains := map[string]string{
		"go": runtime.Version(),
	}
	sdkVersions := extractSDKVersions(headers)
	identity := firstNonEmpty(
		strings.TrimSpace(os.Getenv("GAUNTLET_RECORDER_IDENTITY")),
		strings.TrimSpace(os.Getenv("GITHUB_ACTOR")),
		strings.TrimSpace(os.Getenv("USER")),
		strings.TrimSpace(os.Getenv("USERNAME")),
		"unknown",
	)
	commit := firstNonEmpty(
		strings.TrimSpace(os.Getenv("GAUNTLET_COMMIT_SHA")),
		strings.TrimSpace(os.Getenv("GITHUB_SHA")),
		"unknown",
	)
	return &Provenance{
		CommitSHA:         commit,
		RecorderIdentity:  identity,
		ToolchainVersions: toolchains,
		SDKVersions:       sdkVersions,
		Source:            strings.TrimSpace(source),
	}
}

func extractSDKVersions(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}

	normalized := make(map[string]string, len(headers))
	for k, v := range headers {
		normalized[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}

	sdks := make(map[string]string)
	addIfSet(sdks, "user_agent", normalized["user-agent"])
	addIfSet(sdks, "x_stainless_package_version", normalized["x-stainless-package-version"])
	addIfSet(sdks, "anthropic_version", normalized["anthropic-version"])
	addIfSet(sdks, "x_goog_api_client", normalized["x-goog-api-client"])
	addIfSet(sdks, "x_sdk_version", normalized["x-sdk-version"])

	if len(sdks) == 0 {
		return nil
	}
	return sdks
}

func addIfSet(m map[string]string, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	m[key] = value
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
