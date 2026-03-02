package ci

import "os"

// DetectMode detects the Gauntlet mode based on CI context.
func DetectMode() string {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		return "local"
	}

	// Fork PR detection
	eventName := os.Getenv("GITHUB_EVENT_NAME")
	if eventName == "pull_request" {
		headRepo := os.Getenv("GITHUB_HEAD_REF")
		baseRepo := os.Getenv("GITHUB_REPOSITORY")
		// If head and base repos differ, it's a fork PR
		if headRepo != "" && baseRepo != "" {
			return "fork_pr"
		}
		return "pr_ci"
	}

	if eventName == "schedule" {
		return "nightly"
	}

	return "pr_ci"
}
