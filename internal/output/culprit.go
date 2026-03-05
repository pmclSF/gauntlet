package output

import "github.com/pmclSF/gauntlet/internal/assertions"

// ClassifyCulprit attempts to identify the most likely cause of a failure.
func ClassifyCulprit(results []assertions.Result, worldTools map[string]string) *Culprit {
	if len(results) == 0 {
		return nil
	}

	// Find the first hard-gate failure
	for _, r := range results {
		if r.Passed || r.Soft {
			continue
		}

		switch r.DocketHint {
		case "tool.forbidden":
			return &Culprit{
				Class:      "agent.prompt",
				Confidence: "high",
				Reasoning:  "Agent called a forbidden tool, suggesting the prompt or planner logic needs adjustment",
			}
		case "planner.premature_finalize":
			// Check if any tool was in a non-nominal state
			for tool, state := range worldTools {
				if state != "nominal" {
					return &Culprit{
						Class:      "tool.state." + state,
						Confidence: "high",
						Reasoning:  "Tool " + tool + " was in " + state + " state; agent failed to handle this correctly",
					}
				}
			}
			return &Culprit{
				Class:      "agent.planner",
				Confidence: "medium",
				Reasoning:  "Agent terminated before completing the required tool sequence",
			}
		case "tool.timeout_retrycap", "planner.retry_storm":
			for tool, state := range worldTools {
				if state == "timeout" || state == "server_error" {
					return &Culprit{
						Class:      "tool.state." + state,
						Confidence: "high",
						Reasoning:  "Tool " + tool + " was in " + state + " state; agent retried too many times",
					}
				}
			}
			return &Culprit{
				Class:      "agent.retry_logic",
				Confidence: "medium",
				Reasoning:  "Agent exceeded retry limits",
			}
		case "output.schema_mismatch":
			return &Culprit{
				Class:      "agent.output",
				Confidence: "high",
				Reasoning:  "Agent output does not match expected schema",
			}
		case "output.invalid_json":
			return &Culprit{
				Class:      "agent.output",
				Confidence: "high",
				Reasoning:  "Agent produced invalid JSON output",
			}
		case "tool.args_invalid":
			return &Culprit{
				Class:      "agent.tool_use",
				Confidence: "medium",
				Reasoning:  "Agent called a tool with invalid arguments",
			}
		}
	}

	return &Culprit{
		Class:      "unknown",
		Confidence: "low",
		Reasoning:  "Could not determine a specific culprit",
	}
}
