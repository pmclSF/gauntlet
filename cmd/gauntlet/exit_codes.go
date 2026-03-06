package main

import "strings"

const (
	ExitSuccess      = 0 // all scenarios passed
	ExitFailure      = 1 // one or more scenarios failed assertions
	ExitError        = 2 // gauntlet encountered an execution/runtime error
	ExitInvalidInput = 3 // invalid flags, missing files, or invalid yaml/schema input
)

func exitCodeForError(err error) int {
	if err == nil {
		return ExitSuccess
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "unknown command"),
		strings.Contains(msg, "unknown flag"),
		strings.Contains(msg, "required flag"),
		strings.Contains(msg, "validation failed"),
		strings.Contains(msg, "failed to parse scenario file"),
		strings.Contains(msg, "schema validation failed"),
		strings.Contains(msg, "policy file not found"),
		strings.Contains(msg, "invalid runner mode"),
		strings.Contains(msg, "invalid model mode"),
		strings.Contains(msg, "no scenario files found"),
		strings.Contains(msg, "duplicate scenario name"):
		return ExitInvalidInput
	default:
		return ExitError
	}
}
