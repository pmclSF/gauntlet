package assertions

import (
	"encoding/json"
	"fmt"

	"github.com/xeipuuv/gojsonschema"
)

// OutputSchemaAssertion validates the TUT output against a JSON Schema.
// Hard gate — a failure blocks the PR.
type OutputSchemaAssertion struct{}

func (a *OutputSchemaAssertion) Type() string { return "output_schema" }
func (a *OutputSchemaAssertion) IsSoft() bool { return false }

func (a *OutputSchemaAssertion) Evaluate(ctx Context) Result {
	if ctx.Output.Parsed == nil {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Message:       "output is not valid JSON",
			DocketHint:    "output.invalid_json",
		}
	}

	// Get schema from context (would be passed via spec properties)
	schemaData := ctx.Baseline
	if schemaData != nil && schemaData.OutputSchema != nil {
		schemaBytes, err := json.Marshal(schemaData.OutputSchema)
		if err != nil {
			return Result{
				AssertionType: a.Type(),
				Passed:        false,
				Message:       fmt.Sprintf("failed to marshal output schema: %v", err),
				DocketHint:    "output.schema_mismatch",
			}
		}

		schemaLoader := gojsonschema.NewBytesLoader(schemaBytes)
		documentLoader := gojsonschema.NewGoLoader(ctx.Output.Parsed)

		result, err := gojsonschema.Validate(schemaLoader, documentLoader)
		if err != nil {
			return Result{
				AssertionType: a.Type(),
				Passed:        false,
				Message:       fmt.Sprintf("schema validation error: %v", err),
				DocketHint:    "output.schema_mismatch",
			}
		}

		if !result.Valid() {
			var details string
			for _, err := range result.Errors() {
				details += fmt.Sprintf("\n  - %s: %s", err.Field(), err.Description())
			}
			return Result{
				AssertionType: a.Type(),
				Passed:        false,
				Expected:      "output matching JSON schema",
				Actual:        fmt.Sprintf("validation errors:%s", details),
				Message:       fmt.Sprintf("output schema validation failed:%s", details),
				DocketHint:    "output.schema_mismatch",
			}
		}
	}

	return Result{
		AssertionType: a.Type(),
		Passed:        true,
		Message:       "output matches schema",
	}
}
