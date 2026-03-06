// Package docket implements failure classification for Gauntlet.
// Every failing scenario is assigned a primary docket tag indicating
// the most likely failure category.
package docket

// Tags in precedence order (highest to lowest).
// When multiple tags match, the highest-precedence one is primary.
const (
	TagFixtureMiss         = "fixture.miss"
	TagFixtureIntegrity    = "fixture.integrity"
	TagTUTExitNonzero      = "tut.exit_nonzero"
	TagInputMalformed       = "input.malformed"
	TagInputSchemaDrift     = "input.schema_drift"
	TagToolArgsInvalid      = "tool.args_invalid"
	TagToolTimeoutRetryCap  = "tool.timeout_retrycap"
	TagToolForbidden        = "tool.forbidden"
	TagToolNetworkEscape    = "tool.network_escape"
	TagRAGInjection         = "rag.injection"
	TagPlannerRetryStorm    = "planner.retry_storm"
	TagPlannerPrematureEnd  = "planner.premature_finalize"
	TagModelNetworkEscape   = "model.network_escape"
	TagOutputInvalidJSON    = "output.invalid_json"
	TagOutputSchemaMismatch = "output.schema_mismatch"
	TagOutputSensitiveLeak  = "output.sensitive_leak"
	TagOutputUngrounded     = "output.ungrounded"
	TagUnknown              = "unknown"
)

// TagPrecedence defines the priority order for docket tags.
// Lower index = higher precedence.
var TagPrecedence = []string{
	TagFixtureMiss,
	TagFixtureIntegrity,
	TagTUTExitNonzero,
	TagInputMalformed,
	TagInputSchemaDrift,
	TagToolArgsInvalid,
	TagToolTimeoutRetryCap,
	TagToolForbidden,
	TagToolNetworkEscape,
	TagRAGInjection,
	TagPlannerRetryStorm,
	TagPlannerPrematureEnd,
	TagModelNetworkEscape,
	TagOutputInvalidJSON,
	TagOutputSchemaMismatch,
	TagOutputSensitiveLeak,
	TagOutputUngrounded,
	TagUnknown,
}

// IsFirstClassTag reports whether a tag is assigned directly by the runner and
// should bypass heuristic culprit analysis.
func IsFirstClassTag(tag string) bool {
	switch tag {
	case TagFixtureMiss, TagFixtureIntegrity, TagTUTExitNonzero:
		return true
	default:
		return false
	}
}

// precedenceIndex maps tag to its precedence index.
var precedenceIndex map[string]int

func init() {
	precedenceIndex = make(map[string]int)
	for i, tag := range TagPrecedence {
		precedenceIndex[tag] = i
	}
}

// Precedence returns the precedence index for a tag (lower = higher priority).
func Precedence(tag string) int {
	if idx, ok := precedenceIndex[tag]; ok {
		return idx
	}
	return len(TagPrecedence) // unknown tags have lowest precedence
}
