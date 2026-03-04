package ci

import (
	"encoding/json"
	"os"
	"strings"
)

// DetectMode detects the Gauntlet mode based on CI context.
func DetectMode() string {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		return "local"
	}

	eventName := os.Getenv("GITHUB_EVENT_NAME")
	switch eventName {
	case "pull_request":
		if isForkPullRequest() {
			return "fork_pr"
		}
		return "pr_ci"
	case "schedule":
		return "nightly"
	case "push":
		if isMainBranchPush() {
			return "nightly"
		}
		return "pr_ci"
	default:
		return "pr_ci"
	}
}

func isMainBranchPush() bool {
	ref := os.Getenv("GITHUB_REF")
	if ref == "refs/heads/main" || ref == "refs/heads/master" {
		return true
	}

	refName := os.Getenv("GITHUB_REF_NAME")
	return refName == "main" || refName == "master"
}

type pullRequestEvent struct {
	PullRequest struct {
		Head struct {
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"base"`
	} `json:"pull_request"`
}

func isForkPullRequest() bool {
	baseRepo := strings.TrimSpace(os.Getenv("GITHUB_REPOSITORY"))

	// Prefer direct env if provided by caller/test harness.
	if headRepo := strings.TrimSpace(os.Getenv("GITHUB_HEAD_REPO")); headRepo != "" {
		if baseRepo == "" {
			return true
		}
		return !strings.EqualFold(headRepo, baseRepo)
	}

	// Fall back to event payload JSON in GitHub Actions.
	eventPath := os.Getenv("GITHUB_EVENT_PATH")
	if eventPath != "" {
		data, err := os.ReadFile(eventPath)
		if err == nil {
			var evt pullRequestEvent
			if err := json.Unmarshal(data, &evt); err == nil {
				headRepo := strings.TrimSpace(evt.PullRequest.Head.Repo.FullName)
				baseFromEvent := strings.TrimSpace(evt.PullRequest.Base.Repo.FullName)
				if headRepo != "" && baseFromEvent != "" {
					return !strings.EqualFold(headRepo, baseFromEvent)
				}
				if headRepo != "" && baseRepo != "" {
					return !strings.EqualFold(headRepo, baseRepo)
				}
			}
		}
	}

	// If unavailable, default to same-repo PR for safety in local/test contexts.
	return false
}
