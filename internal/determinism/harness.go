// Package determinism implements the determinism harness for Gauntlet.
// It injects time, RNG, locale, and timezone controls into the TUT
// environment and validates output for nondeterminism violations.
package determinism

import (
	"fmt"
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
