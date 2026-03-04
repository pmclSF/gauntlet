package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// DefaultBaselineApprovalLabel is the default PR label required when baseline
	// files are modified.
	DefaultBaselineApprovalLabel = "gauntlet/baseline-approved"
)

// RollbackChange describes one baseline file mutation.
type RollbackChange struct {
	Path           string `json:"path"`
	Action         string `json:"action"` // created | updated
	PreviousSHA256 string `json:"previous_sha256,omitempty"`
	CurrentSHA256  string `json:"current_sha256"`
}

// RollbackManifest captures metadata needed to prepare a deterministic revert PR.
type RollbackManifest struct {
	ManifestVersion       int              `json:"manifest_version"`
	Suite                 string           `json:"suite,omitempty"`
	GeneratedAt           string           `json:"generated_at"`
	BaseRef               string           `json:"base_ref,omitempty"`
	RequiredApprovalLabel string           `json:"required_approval_label,omitempty"`
	Changes               []RollbackChange `json:"changes"`
}

// FileSHA256 computes the SHA-256 hash for a file.
// Returns exists=false when the file is missing.
func FileSHA256(path string) (hash string, exists bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to read file %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), true, nil
}

// WriteRollbackManifest writes a normalized rollback manifest JSON file.
func WriteRollbackManifest(path string, manifest *RollbackManifest) error {
	if manifest == nil {
		return fmt.Errorf("rollback manifest is nil")
	}
	normalized := normalizeRollbackManifest(*manifest)
	if normalized.ManifestVersion <= 0 {
		normalized.ManifestVersion = 1
	}
	if strings.TrimSpace(normalized.GeneratedAt) == "" {
		normalized.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create rollback manifest directory: %w", err)
	}
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal rollback manifest: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write rollback manifest %s: %w", path, err)
	}
	return nil
}

// WriteRollbackTemplate writes a markdown PR template for deterministic rollback.
func WriteRollbackTemplate(path string, manifest *RollbackManifest) error {
	if manifest == nil {
		return fmt.Errorf("rollback manifest is nil")
	}
	normalized := normalizeRollbackManifest(*manifest)
	if strings.TrimSpace(normalized.GeneratedAt) == "" {
		normalized.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create rollback template directory: %w", err)
	}
	content := buildRollbackTemplate(path, normalized)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write rollback template %s: %w", path, err)
	}
	return nil
}

func normalizeRollbackManifest(in RollbackManifest) RollbackManifest {
	out := in
	out.Suite = strings.TrimSpace(out.Suite)
	out.GeneratedAt = strings.TrimSpace(out.GeneratedAt)
	out.BaseRef = strings.TrimSpace(out.BaseRef)
	out.RequiredApprovalLabel = strings.TrimSpace(out.RequiredApprovalLabel)
	if out.RequiredApprovalLabel == "" {
		out.RequiredApprovalLabel = DefaultBaselineApprovalLabel
	}
	if out.BaseRef == "" {
		out.BaseRef = "origin/main"
	}

	seen := map[string]bool{}
	changes := make([]RollbackChange, 0, len(out.Changes))
	for _, change := range out.Changes {
		p := normalizePath(change.Path)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		action := strings.ToLower(strings.TrimSpace(change.Action))
		if action != "created" && action != "updated" {
			if strings.TrimSpace(change.PreviousSHA256) == "" {
				action = "created"
			} else {
				action = "updated"
			}
		}
		changes = append(changes, RollbackChange{
			Path:           p,
			Action:         action,
			PreviousSHA256: strings.TrimSpace(change.PreviousSHA256),
			CurrentSHA256:  strings.TrimSpace(change.CurrentSHA256),
		})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	out.Changes = changes
	return out
}

func buildRollbackTemplate(templatePath string, manifest RollbackManifest) string {
	var sb strings.Builder

	suite := manifest.Suite
	if suite == "" {
		suite = "all"
	}
	branchDate := "00000000"
	if ts, err := time.Parse(time.RFC3339, manifest.GeneratedAt); err == nil {
		branchDate = ts.UTC().Format("20060102")
	}
	branchName := fmt.Sprintf("revert/gauntlet-baselines-%s-%s", suite, branchDate)
	prTitle := fmt.Sprintf("revert: gauntlet baseline update (%s)", suite)

	sb.WriteString("# Revert Gauntlet Baseline Update\n\n")
	sb.WriteString(fmt.Sprintf("- Suite: `%s`\n", suite))
	sb.WriteString(fmt.Sprintf("- Generated: `%s`\n", manifest.GeneratedAt))
	sb.WriteString(fmt.Sprintf("- Base ref: `%s`\n", manifest.BaseRef))
	sb.WriteString(fmt.Sprintf("- Required approval label: `%s`\n\n", manifest.RequiredApprovalLabel))

	sb.WriteString("## Files To Revert\n")
	if len(manifest.Changes) == 0 {
		sb.WriteString("- _No changed files recorded._\n\n")
	} else {
		for _, change := range manifest.Changes {
			sb.WriteString(fmt.Sprintf("- `%s` (`%s`", change.Path, change.Action))
			if change.PreviousSHA256 != "" {
				sb.WriteString(fmt.Sprintf(", previous `%s`", change.PreviousSHA256))
			}
			if change.CurrentSHA256 != "" {
				sb.WriteString(fmt.Sprintf(", current `%s`", change.CurrentSHA256))
			}
			sb.WriteString(")\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Suggested Commands\n")
	sb.WriteString("```bash\n")
	sb.WriteString(fmt.Sprintf("git switch -c %s\n", branchName))
	if len(manifest.Changes) > 0 {
		sb.WriteString(fmt.Sprintf("git checkout %s --", manifest.BaseRef))
		for _, change := range manifest.Changes {
			sb.WriteString(" \\\n  ")
			sb.WriteString(change.Path)
		}
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("git commit -m %q\n", prTitle))
	sb.WriteString(fmt.Sprintf("gh pr create --title %q --body-file %s\n", prTitle, normalizePath(templatePath)))
	sb.WriteString("```\n")

	return sb.String()
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
