// Package assertions implements the assertion engine for Gauntlet.
// Each assertion type evaluates a specific aspect of the TUT's behavior
// and returns a pass/fail result with structured context.
package assertions

import (
	"encoding/json"
	"sort"

	"github.com/pmclSF/gauntlet/internal/scenario"
	"github.com/pmclSF/gauntlet/internal/tut"
)

// Assertion evaluates a single check against a scenario run.
type Assertion interface {
	// Type returns the assertion type name (matches YAML "type" field).
	Type() string
	// Evaluate runs the assertion against the run context.
	Evaluate(ctx Context) Result
	// IsSoft returns true if this is a soft signal (never blocks PRs).
	IsSoft() bool
}

// Context provides all data needed for assertion evaluation.
type Context struct {
	ScenarioName string
	Input        scenario.Input
	Output       tut.AgentOutput
	ToolTrace    []tut.TraceEvent
	WorldState   WorldState
	Baseline     *ContractBaseline
	FixtureUsed  map[string]string
	Spec         map[string]interface{}
}

// WorldState represents the assembled world state for assertion context.
type WorldState struct {
	Tools     map[string]ToolState
	Databases map[string]DBState
}

// ToolState is the state of a tool in the world.
type ToolState struct {
	Name     string
	State    string
	Response json.RawMessage
}

// DBState is the state of a database in the world.
type DBState struct {
	Name     string
	SeedSets []string
	Data     map[string]interface{}
}

// ContractBaseline is a contract-type baseline for comparison.
type ContractBaseline struct {
	ToolSequence     []string
	OutputSchema     map[string]interface{}
	ForbiddenContent []string
	RequiredFields   []string
}

// Result is the outcome of a single assertion evaluation.
type Result struct {
	AssertionType string `json:"type"`
	Passed        bool   `json:"passed"`
	Expected      string `json:"expected,omitempty"`
	Actual        string `json:"actual,omitempty"`
	Message       string `json:"message"`
	Soft          bool   `json:"soft"`
	DocketHint    string `json:"docket_hint,omitempty"`
}

// registry maps assertion type names to their implementations.
var registry = map[string]Assertion{}

// Register adds an assertion type to the registry.
func Register(a Assertion) {
	registry[a.Type()] = a
}

// Get returns the assertion implementation for the given type name.
func Get(typeName string) (Assertion, bool) {
	a, ok := registry[typeName]
	return a, ok
}

// RegisteredTypes returns all registered assertion type names sorted
// lexicographically for deterministic policy validation and messaging.
func RegisteredTypes() []string {
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// EvaluateAll runs all assertions from a scenario spec against the context.
func EvaluateAll(specs []scenario.AssertionSpec, ctx Context) []Result {
	var results []Result
	for _, spec := range specs {
		a, ok := Get(spec.Type)
		if !ok {
			results = append(results, Result{
				AssertionType: spec.Type,
				Passed:        false,
				Message:       "unknown assertion type: " + spec.Type,
				DocketHint:    "unknown",
			})
			continue
		}
		specCtx := ctx
		specCtx.Spec = spec.Raw
		result := a.Evaluate(specCtx)
		if result.AssertionType == "" {
			result.AssertionType = spec.Type
		}
		// Enforce registry soft/hard contract even if assertion impl forgets.
		result.Soft = a.IsSoft()
		results = append(results, result)
	}
	return results
}

func init() {
	Register(&OutputSchemaAssertion{})
	Register(&ToolSequenceAssertion{})
	Register(&ToolArgsAssertion{})
	Register(&RetryCapAssertion{})
	Register(&ForbiddenToolAssertion{})
	Register(&ForbiddenContentAssertion{})
	Register(&OutputDerivableAssertion{})
	Register(&SensitiveLeakAssertion{})
	Register(&TokenBudgetAssertion{})
}
