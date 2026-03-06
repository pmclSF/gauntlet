package assertions

import (
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
