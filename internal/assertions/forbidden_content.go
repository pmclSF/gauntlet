package assertions

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ForbiddenContentAssertion fails when a regex pattern appears in selected fields.
type ForbiddenContentAssertion struct{}

func (a *ForbiddenContentAssertion) Type() string { return "forbidden_content" }
func (a *ForbiddenContentAssertion) IsSoft() bool { return false }

func (a *ForbiddenContentAssertion) Evaluate(ctx Context) Result {
	rawPattern, _ := specString(ctx.Spec, "pattern")
	pattern := strings.TrimSpace(rawPattern)
	if pattern == "" {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Message:       "forbidden_content: missing required field 'pattern'",
			DocketHint:    "assertion.spec_invalid",
		}
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Message:       fmt.Sprintf("forbidden_content: invalid regex %q: %v", pattern, err),
			DocketHint:    "assertion.spec_invalid",
		}
	}

	fields := specStringSlice(ctx.Spec, "fields")
	if len(fields) == 0 {
		fields = []string{"output"}
	}

	for _, field := range fields {
		switch field {
		case "output":
			outputText := strings.TrimSpace(string(ctx.Output.Raw))
			if outputText == "" && ctx.Output.Parsed != nil {
				if encoded, marshalErr := json.Marshal(ctx.Output.Parsed); marshalErr == nil {
					outputText = string(encoded)
				}
			}
			if loc := re.FindStringIndex(outputText); loc != nil {
				return Result{
					AssertionType: a.Type(),
					Passed:        false,
					Expected:      fmt.Sprintf("pattern %q absent from output", pattern),
					Actual:        fmt.Sprintf("matched output at [%d:%d]", loc[0], loc[1]),
					Message:       fmt.Sprintf("forbidden_content: pattern %q matched in output", pattern),
					DocketHint:    "output.forbidden_content",
				}
			}
		case "tool_args":
			for i, ev := range ctx.ToolTrace {
				if ev.EventType != "tool_call" || len(ev.Args) == 0 {
					continue
				}
				argsText := string(ev.Args)
				if loc := re.FindStringIndex(argsText); loc != nil {
					return Result{
						AssertionType: a.Type(),
						Passed:        false,
						Expected:      fmt.Sprintf("pattern %q absent from tool_args", pattern),
						Actual:        fmt.Sprintf("matched tool_args at call %d [%d:%d]", i, loc[0], loc[1]),
						Message:       fmt.Sprintf("forbidden_content: pattern %q matched in tool_args", pattern),
						DocketHint:    "tool.args_forbidden_content",
					}
				}
			}
		default:
			return Result{
				AssertionType: a.Type(),
				Passed:        false,
				Message:       fmt.Sprintf("forbidden_content: unsupported field %q (supported: output, tool_args)", field),
				DocketHint:    "assertion.spec_invalid",
			}
		}
	}

	return Result{
		AssertionType: a.Type(),
		Passed:        true,
		Message:       fmt.Sprintf("forbidden_content: pattern %q not found in checked fields", pattern),
	}
}
