package docket

import (
	"sort"

	"github.com/gauntlet-dev/gauntlet/internal/assertions"
)

// Classify assigns docket tags to a set of assertion results.
// Returns all matching tags and the primary (highest-precedence) tag.
func Classify(results []assertions.Result) (tags []string, primary string) {
	tagSet := make(map[string]bool)

	for _, r := range results {
		if r.Passed {
			continue
		}
		hint := r.DocketHint
		if hint == "" {
			hint = TagUnknown
		}
		tagSet[hint] = true
	}

	if len(tagSet) == 0 {
		return nil, ""
	}

	for tag := range tagSet {
		tags = append(tags, tag)
	}

	// Sort by precedence
	sort.Slice(tags, func(i, j int) bool {
		return Precedence(tags[i]) < Precedence(tags[j])
	})

	return tags, tags[0]
}
