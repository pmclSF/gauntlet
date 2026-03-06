package determinism

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/pmclSF/gauntlet/internal/tut"
)

// ---------------------------------------------------------------------------
// Harness.Env()
// ---------------------------------------------------------------------------

func TestHarnessEnv_DefaultValues(t *testing.T) {
	h := NewHarness()
	env := h.Env()

	expected := map[string]string{
		"GAUNTLET_FREEZE_TIME": "2025-01-15T10:00:00Z",
		"GAUNTLET_RNG_SEED":   "42",
		"GAUNTLET_LOCALE":     "en_US.UTF-8",
		"GAUNTLET_TIMEZONE":   "UTC",
		"GAUNTLET_ENABLED":    "1",
		"PYTHONDONTWRITEBYTECODE": "1",
		"PYTHONHASHSEED":      "0",
		"PYTHONUNBUFFERED":    "1",
	}

	envMap := envSliceToMap(env)
	for key, want := range expected {
		got, ok := envMap[key]
		if !ok {
			t.Errorf("missing env var %s", key)
			continue
		}
		if got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestHarnessEnv_CustomValues(t *testing.T) {
	h := &Harness{
		FreezeTime: time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC),
		RNGSeed:    99,
		Locale:     "de_DE.UTF-8",
		Timezone:   "Europe/Berlin",
	}
	env := h.Env()
	envMap := envSliceToMap(env)

	if got := envMap["GAUNTLET_FREEZE_TIME"]; got != "2024-06-15T14:30:00Z" {
		t.Errorf("GAUNTLET_FREEZE_TIME = %q", got)
	}
	if got := envMap["GAUNTLET_RNG_SEED"]; got != "99" {
		t.Errorf("GAUNTLET_RNG_SEED = %q", got)
	}
	if got := envMap["GAUNTLET_LOCALE"]; got != "de_DE.UTF-8" {
		t.Errorf("GAUNTLET_LOCALE = %q", got)
	}
	if got := envMap["GAUNTLET_TIMEZONE"]; got != "Europe/Berlin" {
		t.Errorf("GAUNTLET_TIMEZONE = %q", got)
	}
}

func TestHarnessEnv_ContainsGauntletEnabled(t *testing.T) {
	h := NewHarness()
	env := h.Env()
	found := false
	for _, e := range env {
		if e == "GAUNTLET_ENABLED=1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("GAUNTLET_ENABLED=1 not found in Env()")
	}
}

func TestHarnessEnv_PythonVars(t *testing.T) {
	h := NewHarness()
	envMap := envSliceToMap(h.Env())

	pythonVars := map[string]string{
		"PYTHONDONTWRITEBYTECODE": "1",
		"PYTHONHASHSEED":         "0",
		"PYTHONUNBUFFERED":       "1",
	}
	for key, want := range pythonVars {
		if got, ok := envMap[key]; !ok {
			t.Errorf("missing %s", key)
		} else if got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestHarnessEnv_CountEntries(t *testing.T) {
	h := NewHarness()
	env := h.Env()
	// Should have 8 entries based on the implementation
	if len(env) != 8 {
		t.Errorf("expected 8 env vars, got %d: %v", len(env), env)
	}
}

// ---------------------------------------------------------------------------
// FreezeTimeEnv
// ---------------------------------------------------------------------------

func TestFreezeTimeEnv(t *testing.T) {
	ts := time.Date(2025, 3, 20, 8, 0, 0, 0, time.UTC)
	got := FreezeTimeEnv(ts)
	want := "GAUNTLET_FREEZE_TIME=2025-03-20T08:00:00Z"
	if got != want {
		t.Errorf("FreezeTimeEnv = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// CanonicalizeOutput — key sorting
// ---------------------------------------------------------------------------

func TestCanonicalizeOutput_SortsKeys(t *testing.T) {
	input := map[string]interface{}{
		"zebra":    1,
		"apple":    2,
		"mango":    3,
	}
	data, err := CanonicalizeOutput(input)
	if err != nil {
		t.Fatalf("CanonicalizeOutput failed: %v", err)
	}
	got := string(data)
	// Keys must appear in alphabetical order
	appleIdx := strings.Index(got, "apple")
	mangoIdx := strings.Index(got, "mango")
	zebraIdx := strings.Index(got, "zebra")
	if appleIdx >= mangoIdx || mangoIdx >= zebraIdx {
		t.Errorf("keys not sorted: %s", got)
	}
}

func TestCanonicalizeOutput_NestedKeysSorted(t *testing.T) {
	input := map[string]interface{}{
		"outer_b": map[string]interface{}{
			"z_key": "z",
			"a_key": "a",
		},
		"outer_a": "val",
	}
	data, err := CanonicalizeOutput(input)
	if err != nil {
		t.Fatalf("CanonicalizeOutput failed: %v", err)
	}
	got := string(data)
	// outer_a must come before outer_b
	if strings.Index(got, "outer_a") >= strings.Index(got, "outer_b") {
		t.Errorf("outer keys not sorted: %s", got)
	}
	// a_key must come before z_key
	if strings.Index(got, "a_key") >= strings.Index(got, "z_key") {
		t.Errorf("nested keys not sorted: %s", got)
	}
}

// ---------------------------------------------------------------------------
// CanonicalizeOutput — array order preservation
// ---------------------------------------------------------------------------

func TestCanonicalizeOutput_PreservesArrayOrder(t *testing.T) {
	input := map[string]interface{}{
		"items": []interface{}{"cherry", "banana", "apple"},
	}
	data, err := CanonicalizeOutput(input)
	if err != nil {
		t.Fatalf("CanonicalizeOutput failed: %v", err)
	}
	got := string(data)
	cherryIdx := strings.Index(got, "cherry")
	bananaIdx := strings.Index(got, "banana")
	appleIdx := strings.Index(got, "apple")
	if cherryIdx >= bananaIdx || bananaIdx >= appleIdx {
		t.Errorf("array order not preserved: %s", got)
	}
}

func TestCanonicalizeOutput_ArrayOfObjects(t *testing.T) {
	input := map[string]interface{}{
		"results": []interface{}{
			map[string]interface{}{"z": 1, "a": 2},
			map[string]interface{}{"b": 3, "a": 4},
		},
	}
	data, err := CanonicalizeOutput(input)
	if err != nil {
		t.Fatalf("CanonicalizeOutput failed: %v", err)
	}
	// Verify it can be parsed back and order maintained
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse canonical output: %v", err)
	}
	results := parsed["results"].([]interface{})
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// First object should have z and a keys
	first := results[0].(map[string]interface{})
	if first["z"] == nil || first["a"] == nil {
		t.Errorf("first object missing keys: %v", first)
	}
}

// ---------------------------------------------------------------------------
// CanonicalizeOutput — float normalization
// ---------------------------------------------------------------------------

func TestCanonicalizeOutput_IntegerFloatsTruncated(t *testing.T) {
	input := map[string]interface{}{
		"count": float64(42),
	}
	data, err := CanonicalizeOutput(input)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	got := string(data)
	// 42.0 should become 42 (integer)
	if strings.Contains(got, "42.0") {
		t.Errorf("float64(42) should be serialized as integer, got %s", got)
	}
	if !strings.Contains(got, "42") {
		t.Errorf("expected 42 in output, got %s", got)
	}
}

func TestCanonicalizeOutput_TrueFloatPreserved(t *testing.T) {
	input := map[string]interface{}{
		"price": float64(19.99),
	}
	data, err := CanonicalizeOutput(input)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "19.99") {
		t.Errorf("expected 19.99 in output, got %s", got)
	}
}

// ---------------------------------------------------------------------------
// CanonicalizeOutput — whitespace trimming
// ---------------------------------------------------------------------------

func TestCanonicalizeOutput_TrimsTrailingWhitespace(t *testing.T) {
	input := map[string]interface{}{
		"name": "Alice   ",
	}
	data, err := CanonicalizeOutput(input)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	got := string(data)
	if strings.Contains(got, "Alice   ") {
		t.Errorf("trailing whitespace should be trimmed: %s", got)
	}
	if !strings.Contains(got, "Alice") {
		t.Errorf("expected Alice in output: %s", got)
	}
}

// ---------------------------------------------------------------------------
// CanonicalizeOutput — idempotence
// ---------------------------------------------------------------------------

func TestCanonicalizeOutput_Idempotent(t *testing.T) {
	input := map[string]interface{}{
		"b": []interface{}{3, 2, 1},
		"a": map[string]interface{}{"z": "last", "a": "first"},
	}
	data1, err := CanonicalizeOutput(input)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	// Parse the result back and canonicalize again
	var parsed interface{}
	if err := json.Unmarshal(data1, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	data2, err := CanonicalizeOutput(parsed)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if string(data1) != string(data2) {
		t.Errorf("canonicalize is not idempotent:\n  pass1: %s\n  pass2: %s", data1, data2)
	}
}

func TestCanonicalizeOutput_PreservesNullValues(t *testing.T) {
	input := map[string]interface{}{
		"present": nil,
		"nested": map[string]interface{}{
			"child": nil,
		},
	}
	data, err := CanonicalizeOutput(input)
	if err != nil {
		t.Fatalf("CanonicalizeOutput failed: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `"present":null`) {
		t.Fatalf("expected top-level null field, got %s", got)
	}
	if !strings.Contains(got, `"child":null`) {
		t.Fatalf("expected nested null field, got %s", got)
	}
}

func TestCanonicalizeOutput_NormalizesUnicodeToNFC(t *testing.T) {
	input := map[string]interface{}{
		// "cafe\u0301" (e + combining acute accent)
		"label": "cafe\u0301",
	}
	data, err := CanonicalizeOutput(input)
	if err != nil {
		t.Fatalf("CanonicalizeOutput failed: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "café") {
		t.Fatalf("expected NFC-normalized unicode in output, got %s", got)
	}
	if strings.Contains(got, "cafe\u0301") {
		t.Fatalf("expected combining form to be normalized, got %s", got)
	}
}

func TestCanonicalizeJSON_DifferentKeyOrderingMatches(t *testing.T) {
	left := []byte(`{"b":{"z":1,"a":2},"a":[{"y":2,"x":1}]}`)
	right := []byte(`{"a":[{"x":1,"y":2}],"b":{"a":2,"z":1}}`)
	leftCanonical, err := CanonicalizeJSON(left)
	if err != nil {
		t.Fatalf("CanonicalizeJSON(left) failed: %v", err)
	}
	rightCanonical, err := CanonicalizeJSON(right)
	if err != nil {
		t.Fatalf("CanonicalizeJSON(right) failed: %v", err)
	}
	if string(leftCanonical) != string(rightCanonical) {
		t.Fatalf("canonical JSON mismatch:\nleft:  %s\nright: %s", leftCanonical, rightCanonical)
	}
}

// ---------------------------------------------------------------------------
// Violation detection: clock skew
// ---------------------------------------------------------------------------

func TestDetectClockSkew_MatchingTimestamp(t *testing.T) {
	h := NewHarness()
	// Output contains the freeze time exactly
	output := `The current time is 2025-01-15T10:00:00 and the weather is nice.`
	w := h.detectClockSkew(output)
	if w != nil {
		t.Errorf("expected no warning for matching timestamp, got: %v", w)
	}
}

func TestDetectClockSkew_DifferentTimestamp(t *testing.T) {
	h := NewHarness()
	// Output contains a timestamp far from freeze time
	output := `Generated at 2025-06-20T15:30:00`
	w := h.detectClockSkew(output)
	if w == nil {
		t.Fatal("expected clock skew warning")
	}
	if w.Type != "nondeterminism.time" {
		t.Errorf("Type = %q, want %q", w.Type, "nondeterminism.time")
	}
	if !strings.Contains(w.Message, "differs from freeze time") {
		t.Errorf("Message should mention freeze time: %s", w.Message)
	}
}

func TestDetectClockSkew_NoTimestampInOutput(t *testing.T) {
	h := NewHarness()
	output := `Hello, world! The answer is 42.`
	w := h.detectClockSkew(output)
	if w != nil {
		t.Errorf("expected no warning for output without timestamps, got: %v", w)
	}
}

func TestDetectClockSkew_WithinOneSecondTolerance(t *testing.T) {
	h := &Harness{
		FreezeTime: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
	}
	// Exactly 1 second off — should NOT trigger (tolerance is > 1 second)
	output := `Time: 2025-01-15T10:00:01`
	w := h.detectClockSkew(output)
	if w != nil {
		t.Errorf("1-second difference should be within tolerance, got: %v", w)
	}
}

func TestDetectClockSkew_JustOverTolerance(t *testing.T) {
	h := &Harness{
		FreezeTime: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
	}
	// 2 seconds off — should trigger
	output := `Time: 2025-01-15T10:00:02`
	w := h.detectClockSkew(output)
	if w == nil {
		t.Fatal("expected clock skew warning for 2-second difference")
	}
}

// ---------------------------------------------------------------------------
// Violation detection: entropy
// ---------------------------------------------------------------------------

func TestDetectEntropy_HighEntropyString(t *testing.T) {
	h := NewHarness()
	parsed := map[string]interface{}{
		"token": "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6",
	}
	// No traces, so the token is not in known strings
	w := h.detectEntropy(parsed, nil)
	if w == nil {
		t.Fatal("expected entropy warning for high-entropy string")
	}
	if w.Type != "nondeterminism.rng" {
		t.Errorf("Type = %q, want %q", w.Type, "nondeterminism.rng")
	}
}

func TestDetectEntropy_KnownStringFromTrace(t *testing.T) {
	h := NewHarness()
	token := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6"
	parsed := map[string]interface{}{
		"token": token,
	}
	// The same token appears in a trace response
	traceResponse, _ := json.Marshal(map[string]interface{}{
		"token": token,
	})
	traces := []tut.TraceEvent{
		{
			EventType: "tool_call",
			Response:  json.RawMessage(traceResponse),
		},
	}
	w := h.detectEntropy(parsed, traces)
	if w != nil {
		t.Errorf("expected no warning when high-entropy string is in fixtures, got: %v", w)
	}
}

func TestDetectEntropy_ShortStringsIgnored(t *testing.T) {
	h := NewHarness()
	parsed := map[string]interface{}{
		"code": "xYz12345", // 8 chars, should be ignored
	}
	w := h.detectEntropy(parsed, nil)
	if w != nil {
		t.Errorf("expected no warning for short string (<=8 chars), got: %v", w)
	}
}

func TestDetectEntropy_LowEntropyLongString(t *testing.T) {
	h := NewHarness()
	parsed := map[string]interface{}{
		"message": "aaaaaaaaaaaaaaaaaa", // very low entropy
	}
	w := h.detectEntropy(parsed, nil)
	if w != nil {
		t.Errorf("expected no warning for low-entropy string, got: %v", w)
	}
}

// ---------------------------------------------------------------------------
// Violation detection: locale
// ---------------------------------------------------------------------------

func TestDetectLocaleLeaks_DecimalComma(t *testing.T) {
	// European-style decimal: "3,5" (3.5 in US format)
	output := `The price is 3,5 euros.`
	w := detectLocaleLeaks(output)
	if w == nil {
		t.Fatal("expected locale warning for decimal comma")
	}
	if w.Type != "nondeterminism.locale" {
		t.Errorf("Type = %q, want %q", w.Type, "nondeterminism.locale")
	}
	if !strings.Contains(w.Message, "locale-specific") {
		t.Errorf("Message = %q", w.Message)
	}
}

func TestDetectLocaleLeaks_NoLocaleIssue(t *testing.T) {
	output := `The price is 3.50 dollars and the count is 1000 items.`
	w := detectLocaleLeaks(output)
	if w != nil {
		t.Errorf("expected no locale warning for US-formatted numbers, got: %v", w)
	}
}

func TestDetectLocaleLeaks_ThousandsSeparatorFalsePositive(t *testing.T) {
	// "1,000" is a US thousands separator, NOT a locale issue.
	// The regex checks for digit,digit{1,2} so "1,000" has 3 digits after comma.
	output := `Total: 1,000 items shipped.`
	w := detectLocaleLeaks(output)
	if w != nil {
		t.Errorf("US thousands separator should not trigger locale warning, got: %v", w)
	}
}

func TestDetectLocaleLeaks_EmptyOutput(t *testing.T) {
	w := detectLocaleLeaks("")
	if w != nil {
		t.Errorf("empty output should not trigger locale warning, got: %v", w)
	}
}

// ---------------------------------------------------------------------------
// Validate (integration of all violation checks)
// ---------------------------------------------------------------------------

func TestValidate_NoViolations(t *testing.T) {
	h := NewHarness()
	output := tut.AgentOutput{
		Raw:    []byte(`{"status":"ok","message":"All good"}`),
		Parsed: map[string]interface{}{"status": "ok", "message": "All good"},
	}
	warnings := h.Validate(output, nil)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
}

func TestValidate_ClockSkewDetected(t *testing.T) {
	h := NewHarness()
	output := tut.AgentOutput{
		Raw:    []byte(`{"time":"2025-06-20T15:30:00"}`),
		Parsed: map[string]interface{}{"time": "2025-06-20T15:30:00"},
	}
	warnings := h.Validate(output, nil)
	found := false
	for _, w := range warnings {
		if w.Type == "nondeterminism.time" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected clock skew warning, got %v", warnings)
	}
}

func TestValidate_LocaleViolationDetected(t *testing.T) {
	h := NewHarness()
	output := tut.AgentOutput{
		Raw:    []byte(`The temperature is 23,5 degrees.`),
		Parsed: nil, // no parsed output
	}
	warnings := h.Validate(output, nil)
	found := false
	for _, w := range warnings {
		if w.Type == "nondeterminism.locale" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected locale warning, got %v", warnings)
	}
}

// ---------------------------------------------------------------------------
// Shannon entropy helper
// ---------------------------------------------------------------------------

func TestShannonEntropy_EmptyString(t *testing.T) {
	e := shannonEntropy("")
	if e != 0 {
		t.Errorf("entropy of empty string = %f, want 0", e)
	}
}

func TestShannonEntropy_SingleCharRepeated(t *testing.T) {
	e := shannonEntropy("aaaaaaaaaa")
	if e != 0 {
		t.Errorf("entropy of single repeated char = %f, want 0", e)
	}
}

func TestShannonEntropy_HighEntropy(t *testing.T) {
	// A UUID-like string has high entropy
	e := shannonEntropy("550e8400-e29b-41d4-a716-446655440000")
	if e < 3.0 {
		t.Errorf("expected high entropy for UUID-like string, got %f", e)
	}
}

// ---------------------------------------------------------------------------
// Warning struct
// ---------------------------------------------------------------------------

func TestWarningJSON(t *testing.T) {
	w := Warning{
		Type:    "nondeterminism.time",
		Message: "clock skew detected",
	}
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed Warning
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Type != w.Type || parsed.Message != w.Message {
		t.Errorf("round-trip mismatch: %+v vs %+v", parsed, w)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func envSliceToMap(env []string) map[string]string {
	m := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}
