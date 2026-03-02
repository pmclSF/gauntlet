package world

import "fmt"

// RequiredStates are the states that every tool definition should include.
// Missing states generate a warning, not an error.
var RequiredStates = []string{"nominal", "timeout", "server_error", "malformed_response"}

// ValidateToolDef checks that a tool definition has the recommended states.
func ValidateToolDef(td *ToolDef) []string {
	var warnings []string
	for _, state := range RequiredStates {
		if _, ok := td.States[state]; !ok {
			warnings = append(warnings, fmt.Sprintf("tool %s: missing recommended state '%s'", td.Tool, state))
		}
	}
	return warnings
}
