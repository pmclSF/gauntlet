package assertions

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pmclSF/gauntlet/internal/tut"
)

func TestSemanticMatch_SkipsOutsideNightly(t *testing.T) {
	a := &SemanticMatchAssertion{}
	ctx := Context{
		RunnerMode: "pr_ci",
		Spec: map[string]interface{}{
			"judge":     "gpt-4.1-mini",
			"prompt":    "Response confirms order is confirmed",
			"threshold": 0.8,
		},
		Output: tut.AgentOutput{
			Raw: []byte(`{"response":"Order confirmed"}`),
		},
	}
	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected skip/pass in non-nightly mode, got fail: %s", result.Message)
	}
}

func TestSemanticMatch_FailNightlyBelowThreshold(t *testing.T) {
	a := &SemanticMatchAssertion{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("judge request method = %s, want POST", r.Method)
		}
		if auth := r.Header.Get("Authorization"); !strings.HasPrefix(auth, "Bearer ") {
			t.Fatalf("missing bearer auth header: %q", auth)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if !strings.Contains(string(body), `"model":"gpt-4.1-mini"`) {
			t.Fatalf("request missing judge model: %s", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"score\":0.20,\"reason\":\"does not confirm order\"}"}}]}`))
	}))
	defer server.Close()
	t.Setenv("GAUNTLET_SEMANTIC_MATCH_ENDPOINT", server.URL)
	t.Setenv("GAUNTLET_SEMANTIC_MATCH_API_KEY", "test-key")
	ctx := Context{
		RunnerMode: "nightly",
		Spec: map[string]interface{}{
			"judge":     "gpt-4.1-mini",
			"prompt":    "Response confirms order is confirmed",
			"threshold": 0.8,
		},
		Output: tut.AgentOutput{
			Raw: []byte(`{"response":"Unable to find order"}`),
		},
	}
	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected nightly semantic mismatch failure")
	}
}

func TestSemanticMatch_PassNightly(t *testing.T) {
	a := &SemanticMatchAssertion{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"score\":0.95,\"reason\":\"clearly confirms order\"}"}}]}`))
	}))
	defer server.Close()
	t.Setenv("GAUNTLET_SEMANTIC_MATCH_ENDPOINT", server.URL)
	t.Setenv("GAUNTLET_SEMANTIC_MATCH_API_KEY", "test-key")
	ctx := Context{
		RunnerMode: "nightly",
		Spec: map[string]interface{}{
			"judge":     "gpt-4.1-mini",
			"prompt":    "Response confirms order is confirmed",
			"threshold": 0.8,
		},
		Output: tut.AgentOutput{
			Raw: []byte(`{"response":"Order is confirmed"}`),
		},
	}
	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected pass, got fail: %s", result.Message)
	}
}

func TestSemanticMatch_InvalidThreshold(t *testing.T) {
	a := &SemanticMatchAssertion{}
	ctx := Context{
		RunnerMode: "nightly",
		Spec: map[string]interface{}{
			"judge":     "gpt-4.1-mini",
			"prompt":    "confirm order",
			"threshold": 1.5,
		},
	}
	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected invalid threshold failure")
	}
}

func TestSemanticMatch_MissingJudgeFails(t *testing.T) {
	a := &SemanticMatchAssertion{}
	ctx := Context{
		RunnerMode: "nightly",
		Spec: map[string]interface{}{
			"prompt":    "confirm order",
			"threshold": 0.8,
		},
	}
	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected missing judge failure")
	}
	if !strings.Contains(result.Message, "missing required field 'judge'") {
		t.Fatalf("unexpected message: %s", result.Message)
	}
}

func TestSemanticMatch_InjectableJudgeFunc(t *testing.T) {
	a := &SemanticMatchAssertion{
		Judge: func(model, prompt, output string) (float64, string, error) {
			if model != "test-model" {
				t.Fatalf("judge received model %q, want test-model", model)
			}
			return 0.92, "looks good", nil
		},
	}
	ctx := Context{
		RunnerMode: "nightly",
		Spec: map[string]interface{}{
			"judge":     "test-model",
			"prompt":    "confirm order",
			"threshold": 0.8,
		},
		Output: tut.AgentOutput{
			Raw: []byte(`{"response":"Order confirmed"}`),
		},
	}
	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected pass with injected judge, got: %s", result.Message)
	}
	if !strings.Contains(result.Message, "0.92") {
		t.Fatalf("expected score in message, got: %s", result.Message)
	}
}

func TestSemanticMatch_InjectableJudgeError(t *testing.T) {
	a := &SemanticMatchAssertion{
		Judge: func(model, prompt, output string) (float64, string, error) {
			return 0, "", fmt.Errorf("judge unavailable")
		},
	}
	ctx := Context{
		RunnerMode: "nightly",
		Spec: map[string]interface{}{
			"judge":     "test-model",
			"prompt":    "confirm order",
			"threshold": 0.8,
		},
		Output: tut.AgentOutput{
			Raw: []byte(`{"response":"Order confirmed"}`),
		},
	}
	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected judge error failure")
	}
	if result.DocketHint != docketTagJudgeError {
		t.Fatalf("docket hint = %q, want %q", result.DocketHint, docketTagJudgeError)
	}
}

func TestSemanticMatch_MissingAPIKeyFailsNightly(t *testing.T) {
	a := &SemanticMatchAssertion{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"score\":0.95}"}}]}`))
	}))
	defer server.Close()
	t.Setenv("GAUNTLET_SEMANTIC_MATCH_ENDPOINT", server.URL)
	t.Setenv("GAUNTLET_SEMANTIC_MATCH_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	ctx := Context{
		RunnerMode: "nightly",
		Spec: map[string]interface{}{
			"judge":     "gpt-4.1-mini",
			"prompt":    "confirm order",
			"threshold": 0.8,
		},
		Output: tut.AgentOutput{
			Raw: []byte(`{"response":"Order confirmed"}`),
		},
	}
	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected judge call failure")
	}
	if !strings.Contains(result.Message, "missing GAUNTLET_SEMANTIC_MATCH_API_KEY") {
		t.Fatalf("unexpected message: %s", result.Message)
	}
}
