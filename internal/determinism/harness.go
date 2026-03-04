// Package determinism implements the determinism harness for Gauntlet.
// It injects time, RNG, locale, and timezone controls into the TUT
// environment and validates output for nondeterminism violations.
package determinism

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Harness holds the determinism controls to inject into TUT environment.
type Harness struct {
	FreezeTime time.Time
	RNGSeed    int64
	Locale     string
	Timezone   string
}

// NewHarness creates a Harness with default values.
func NewHarness() *Harness {
	return &Harness{
		FreezeTime: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
		RNGSeed:    42,
		Locale:     "en_US.UTF-8",
		Timezone:   "UTC",
	}
}

// Env returns the environment variables to inject into the TUT process.
func (h *Harness) Env() []string {
	return []string{
		fmt.Sprintf("GAUNTLET_FREEZE_TIME=%s", h.FreezeTime.Format(time.RFC3339)),
		fmt.Sprintf("GAUNTLET_RNG_SEED=%d", h.RNGSeed),
		fmt.Sprintf("GAUNTLET_LOCALE=%s", h.Locale),
		fmt.Sprintf("GAUNTLET_TIMEZONE=%s", h.Timezone),
		"GAUNTLET_ENABLED=1",
		"PYTHONDONTWRITEBYTECODE=1",
		"PYTHONHASHSEED=0",
		"PYTHONUNBUFFERED=1",
	}
}

// Warning represents a detected nondeterminism violation.
type Warning struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// directDatetimeImportRE matches "from datetime import datetime" and similar
// patterns that bypass our module-level patching of datetime.datetime.
var directDatetimeImportRE = regexp.MustCompile(
	`^\s*from\s+datetime\s+import\s+.*\b(datetime|date)\b`,
)

// ScanPythonImportWarnings scans Python files under dirs for imports that
// bypass Gauntlet's determinism patches (e.g. "from datetime import datetime").
// Returns warnings but does not block execution.
func ScanPythonImportWarnings(dirs []string) []Warning {
	var warnings []Warning
	seen := map[string]bool{}

	for _, dir := range dirs {
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".py") {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer f.Close()

			scanner := bufio.NewScanner(f)
			lineNum := 0
			for scanner.Scan() {
				lineNum++
				line := scanner.Text()
				if directDatetimeImportRE.MatchString(line) {
					key := path + ":" + line
					if seen[key] {
						continue
					}
					seen[key] = true
					warnings = append(warnings, Warning{
						Type: "nondeterminism.import",
						Message: fmt.Sprintf(
							"%s:%d: direct import '%s' bypasses Gauntlet's datetime patch; use 'import datetime' instead",
							path, lineNum, strings.TrimSpace(line)),
					})
				}
			}
			return nil
		})
	}
	return warnings
}
