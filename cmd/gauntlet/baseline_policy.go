package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pmclSF/gauntlet/internal/baseline"
)

func newCheckBaselineApprovalCmd() *cobra.Command {
	var (
		baseRef       string
		headRef       string
		baselineDir   string
		requiredLabel string
		eventPath     string
		changedFiles  []string
		labels        []string
	)

	cmd := &cobra.Command{
		Use:   "check-baseline-approval",
		Short: "Require approval label when baseline files are changed in a PR",
		RunE: func(cmd *cobra.Command, args []string) error {
			requiredLabel = strings.TrimSpace(requiredLabel)
			if requiredLabel == "" {
				return fmt.Errorf("required label cannot be empty")
			}

			baselineDir = strings.TrimSpace(baselineDir)
			if baselineDir == "" {
				baselineDir = filepath.Join("evals", "baselines")
			}

			paths := append([]string{}, changedFiles...)
			if len(paths) == 0 {
				resolvedBase := resolveBaseRef(baseRef)
				resolvedHead := strings.TrimSpace(headRef)
				if resolvedHead == "" {
					resolvedHead = "HEAD"
				}
				diffPaths, err := gitDiffChangedFiles(resolvedBase, resolvedHead, baselineDir)
				if err != nil {
					return err
				}
				paths = diffPaths
			}

			changedBaselinePaths := filterBaselineChangedFiles(paths, baselineDir)
			if len(changedBaselinePaths) == 0 {
				fmt.Println("Baseline approval check passed: no baseline changes detected.")
				return nil
			}

			effectiveLabels := append([]string{}, labels...)
			if len(effectiveLabels) == 0 {
				ep := strings.TrimSpace(eventPath)
				if ep == "" {
					ep = strings.TrimSpace(os.Getenv("GITHUB_EVENT_PATH"))
				}
				if ep != "" {
					loaded, err := readPullRequestLabels(ep)
					if err != nil {
						return fmt.Errorf("baseline files changed but failed to read PR labels: %w", err)
					}
					effectiveLabels = loaded
				}
			}

			if !containsLabel(effectiveLabels, requiredLabel) {
				return fmt.Errorf(
					"baseline files changed (%d) but required approval label %q is missing\n  Changed files:\n%s\n  Add label %q to this PR and rerun checks",
					len(changedBaselinePaths),
					requiredLabel,
					formatChangedPaths(changedBaselinePaths),
					requiredLabel,
				)
			}

			fmt.Printf(
				"Baseline approval check passed: %d baseline file(s) changed and label %q is present.\n",
				len(changedBaselinePaths),
				requiredLabel,
			)
			return nil
		},
	}

	cmd.Flags().StringVar(&baseRef, "base-ref", "", "Base git ref for diff (default: origin/$GITHUB_BASE_REF or origin/main)")
	cmd.Flags().StringVar(&headRef, "head-ref", "HEAD", "Head git ref for diff")
	cmd.Flags().StringVar(&baselineDir, "baseline-dir", filepath.Join("evals", "baselines"), "Baseline directory path to monitor")
	cmd.Flags().StringVar(&requiredLabel, "required-label", baseline.DefaultBaselineApprovalLabel, "Required PR label when baseline files change")
	cmd.Flags().StringVar(&eventPath, "event-path", "", "Path to GitHub event payload JSON (defaults to $GITHUB_EVENT_PATH)")
	cmd.Flags().StringArrayVar(&changedFiles, "changed-file", nil, "Changed file path (repeatable); skips git diff when provided")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "PR label name (repeatable); skips event payload parsing when provided")

	return cmd
}

func resolveBaseRef(explicit string) string {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		return explicit
	}
	envBase := strings.TrimSpace(os.Getenv("GITHUB_BASE_REF"))
	if envBase != "" {
		if strings.Contains(envBase, "/") {
			return envBase
		}
		return "origin/" + envBase
	}
	return "origin/main"
}

func gitDiffChangedFiles(baseRef, headRef, baselineDir string) ([]string, error) {
	baseRef = strings.TrimSpace(baseRef)
	headRef = strings.TrimSpace(headRef)
	if baseRef == "" {
		return nil, fmt.Errorf("base ref is required when --changed-file is not provided")
	}
	if headRef == "" {
		headRef = "HEAD"
	}
	baselineDir = strings.TrimSpace(baselineDir)
	if baselineDir == "" {
		baselineDir = filepath.Join("evals", "baselines")
	}

	diffRange := fmt.Sprintf("%s...%s", baseRef, headRef)
	cmd := exec.Command("git", "diff", "--name-only", diffRange, "--", baselineDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff failed for %s: %w\n  Output: %s", diffRange, err, strings.TrimSpace(string(out)))
	}
	return splitLines(string(out)), nil
}

func filterBaselineChangedFiles(paths []string, baselineDir string) []string {
	prefix := normalizePath(baselineDir)
	if prefix == "" {
		prefix = "evals/baselines"
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		p := normalizePath(raw)
		if p == "" {
			continue
		}
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

func readPullRequestLabels(eventPath string) ([]string, error) {
	eventPath = strings.TrimSpace(eventPath)
	if eventPath == "" {
		return nil, fmt.Errorf("event payload path is empty")
	}
	data, err := os.ReadFile(eventPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read event payload %s: %w", eventPath, err)
	}
	var payload struct {
		PullRequest struct {
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse event payload %s: %w", eventPath, err)
	}
	seen := map[string]bool{}
	labels := make([]string, 0, len(payload.PullRequest.Labels))
	for _, label := range payload.PullRequest.Labels {
		name := strings.TrimSpace(label.Name)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		labels = append(labels, name)
	}
	sort.Strings(labels)
	return labels, nil
}

func containsLabel(labels []string, required string) bool {
	required = strings.ToLower(strings.TrimSpace(required))
	if required == "" {
		return false
	}
	for _, label := range labels {
		if strings.ToLower(strings.TrimSpace(label)) == required {
			return true
		}
	}
	return false
}

func splitLines(raw string) []string {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func formatChangedPaths(paths []string) string {
	if len(paths) == 0 {
		return "    - (none)"
	}
	var sb strings.Builder
	for _, p := range paths {
		sb.WriteString("    - ")
		sb.WriteString(p)
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.ToSlash(filepath.Clean(path))
	if strings.HasPrefix(path, "./") {
		path = strings.TrimPrefix(path, "./")
	}
	if path == "." {
		return ""
	}
	return path
}
