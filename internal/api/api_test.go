package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewServer(t *testing.T) {
	s := NewServer(":8080", "/tmp/evals", "/tmp/static")

	if s.Addr != ":8080" {
		t.Errorf("Addr: got %q, want %q", s.Addr, ":8080")
	}
	if s.EvalsDir != "/tmp/evals" {
		t.Errorf("EvalsDir: got %q, want %q", s.EvalsDir, "/tmp/evals")
	}
	if s.StaticDir != "/tmp/static" {
		t.Errorf("StaticDir: got %q, want %q", s.StaticDir, "/tmp/static")
	}
}

func TestNewServerFieldsInitialized(t *testing.T) {
	s := NewServer(":9090", "/evals", "")

	if s.proposals != nil {
		t.Error("proposals: expected nil initially")
	}
	if s.libraries != nil {
		t.Error("libraries: expected nil initially")
	}
}

func TestHandleProposalsEmpty(t *testing.T) {
	s := NewServer(":8080", t.TempDir(), "")

	req := httptest.NewRequest("GET", "/api/proposals", nil)
	w := httptest.NewRecorder()

	s.handleProposals(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", contentType, "application/json")
	}

	var body interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// null JSON is valid for nil slice
}

func TestHandleHealthNoRuns(t *testing.T) {
	evalsDir := t.TempDir()
	s := NewServer(":8080", evalsDir, "")

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()

	s.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	status, ok := body["status"]
	if !ok {
		t.Fatal("expected 'status' key in response")
	}
	if status != "no_runs" {
		t.Errorf("status: got %q, want %q", status, "no_runs")
	}
}

func TestHandleHealthWithResults(t *testing.T) {
	evalsDir := t.TempDir()
	runsDir := filepath.Join(evalsDir, "runs", "20250101-120000-abc1234")
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatalf("failed to create runs dir: %v", err)
	}

	resultsJSON := `{"version":"1","suite":"smoke","summary":{"total":2,"passed":2}}`
	if err := os.WriteFile(filepath.Join(runsDir, "results.json"), []byte(resultsJSON), 0o644); err != nil {
		t.Fatalf("failed to write results.json: %v", err)
	}

	s := NewServer(":8080", evalsDir, "")

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()

	s.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["version"] != "1" {
		t.Errorf("version: got %v, want %q", body["version"], "1")
	}
	if body["suite"] != "smoke" {
		t.Errorf("suite: got %v, want %q", body["suite"], "smoke")
	}
}

func TestHandleHealthNoResults(t *testing.T) {
	evalsDir := t.TempDir()
	// Create runs dir but no results.json files
	runsDir := filepath.Join(evalsDir, "runs", "20250101-120000-abc1234")
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatalf("failed to create runs dir: %v", err)
	}

	s := NewServer(":8080", evalsDir, "")

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()

	s.handleHealth(w, req)

	resp := w.Result()
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "no_results" {
		t.Errorf("status: got %v, want %q", body["status"], "no_results")
	}
}

func TestHandlePairsEmpty(t *testing.T) {
	s := NewServer(":8080", t.TempDir(), "")

	req := httptest.NewRequest("GET", "/api/pairs", nil)
	w := httptest.NewRecorder()

	s.handlePairs(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", contentType, "application/json")
	}
}

func TestHandleBaselineDiffMissingParams(t *testing.T) {
	s := NewServer(":8080", t.TempDir(), "")

	// Missing both params
	req := httptest.NewRequest("GET", "/api/baselines/diff", nil)
	w := httptest.NewRecorder()

	s.handleBaselineDiff(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status (no params): got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	// Missing scenario
	req = httptest.NewRequest("GET", "/api/baselines/diff?suite=smoke", nil)
	w = httptest.NewRecorder()

	s.handleBaselineDiff(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status (missing scenario): got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleBaselineDiffNotFound(t *testing.T) {
	s := NewServer(":8080", t.TempDir(), "")

	req := httptest.NewRequest("GET", "/api/baselines/diff?suite=smoke&scenario=test1", nil)
	w := httptest.NewRecorder()

	s.handleBaselineDiff(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestHandleBaselineDiffFound(t *testing.T) {
	evalsDir := t.TempDir()
	baselineDir := filepath.Join(evalsDir, "baselines", "smoke")
	if err := os.MkdirAll(baselineDir, 0o755); err != nil {
		t.Fatalf("failed to create baseline dir: %v", err)
	}

	baselineJSON := `{"tool_sequence": {"required": ["search"]}}`
	if err := os.WriteFile(filepath.Join(baselineDir, "test1.json"), []byte(baselineJSON), 0o644); err != nil {
		t.Fatalf("failed to write baseline: %v", err)
	}

	s := NewServer(":8080", evalsDir, "")

	req := httptest.NewRequest("GET", "/api/baselines/diff?suite=smoke&scenario=test1", nil)
	w := httptest.NewRecorder()

	s.handleBaselineDiff(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", contentType, "application/json")
	}
}

func TestHandleApproveProposalMethodNotAllowed(t *testing.T) {
	s := NewServer(":8080", t.TempDir(), "")

	req := httptest.NewRequest("GET", "/api/proposals/approve", nil)
	w := httptest.NewRecorder()

	s.handleApproveProposal(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestHandleRejectProposalMethodNotAllowed(t *testing.T) {
	s := NewServer(":8080", t.TempDir(), "")

	req := httptest.NewRequest("GET", "/api/proposals/reject", nil)
	w := httptest.NewRecorder()

	s.handleRejectProposal(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestHandleResultsDelegatesToHealth(t *testing.T) {
	evalsDir := t.TempDir()
	s := NewServer(":8080", evalsDir, "")

	req := httptest.NewRequest("GET", "/api/results", nil)
	w := httptest.NewRecorder()

	s.handleResults(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should return no_runs since there's no runs directory
	if body["status"] != "no_runs" {
		t.Errorf("status: got %v, want %q", body["status"], "no_runs")
	}
}
