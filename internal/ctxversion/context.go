// Package ctxversion implements context versioning for Gauntlet.
// It tracks the 5 moving parts that can cause a scenario to flip:
// model, prompt, tools, data, and planner.
package ctxversion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Part is one of the 5 moving parts.
type Part string

const (
	PartModel   Part = "model"
	PartPrompt  Part = "prompt"
	PartTools   Part = "tools"
	PartData    Part = "data"
	PartPlanner Part = "planner"
)

// AllParts returns all 5 moving parts.
func AllParts() []Part {
	return []Part{PartModel, PartPrompt, PartTools, PartData, PartPlanner}
}

// Snapshot captures the hash of each moving part at a point in time.
type Snapshot struct {
	RunID     string            `json:"run_id"`
	Timestamp time.Time         `json:"timestamp"`
	Suite     string            `json:"suite"`
	Scenario  string            `json:"scenario"`
	Parts     map[Part]PartHash `json:"parts"`
}

// PartHash is the hash and metadata for a single part.
type PartHash struct {
	Hash    string `json:"hash"`
	Version string `json:"version,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// Diff represents a change between two snapshots.
type Diff struct {
	Part     Part   `json:"part"`
	Before   string `json:"before"`
	After    string `json:"after"`
	Changed  bool   `json:"changed"`
}

// NewSnapshot creates a snapshot with all parts hashed.
func NewSnapshot(runID, suite, scenario string, parts map[Part]PartHash) *Snapshot {
	return &Snapshot{
		RunID:     runID,
		Timestamp: time.Now(),
		Suite:     suite,
		Scenario:  scenario,
		Parts:     parts,
	}
}

// HashContent computes a SHA-256 hash of arbitrary content.
func HashContent(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

// Compare returns the diffs between two snapshots.
func Compare(before, after *Snapshot) []Diff {
	var diffs []Diff
	for _, part := range AllParts() {
		beforeHash := ""
		afterHash := ""
		if h, ok := before.Parts[part]; ok {
			beforeHash = h.Hash
		}
		if h, ok := after.Parts[part]; ok {
			afterHash = h.Hash
		}
		diffs = append(diffs, Diff{
			Part:    part,
			Before:  beforeHash,
			After:   afterHash,
			Changed: beforeHash != afterHash,
		})
	}
	return diffs
}

// ChangedParts returns only the parts that changed between snapshots.
func ChangedParts(before, after *Snapshot) []Part {
	var changed []Part
	for _, d := range Compare(before, after) {
		if d.Changed {
			changed = append(changed, d.Part)
		}
	}
	return changed
}

// Save writes a snapshot to disk.
func (s *Snapshot) Save(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s_%s.json", s.Suite, s.Scenario))
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadSnapshot reads a snapshot from disk.
func LoadSnapshot(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Snapshot
	return &s, json.Unmarshal(data, &s)
}
