package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestNewServer(t *testing.T) {
	s := NewServer(":8080", "/tmp/evals", nil)

	if s.Addr != ":8080" {
		t.Errorf("Addr: got %q, want %q", s.Addr, ":8080")
	}
	if s.EvalsDir != "/tmp/evals" {
		t.Errorf("EvalsDir: got %q, want %q", s.EvalsDir, "/tmp/evals")
	}
	if s.StaticFS != nil {
		t.Error("StaticFS: expected nil")
	}
}

func TestNewServerFieldsInitialized(t *testing.T) {
	s := NewServer(":9090", "/evals", nil)

	if s.proposals != nil {
		t.Error("proposals: expected nil initially")
	}
	if s.libraries != nil {
		t.Error("libraries: expected nil initially")
	}
}

func TestHandleProposalsEmpty(t *testing.T) {
	s := NewServer(":8080", t.TempDir(), nil)

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
}

func TestHandleHealthNoRuns(t *testing.T) {
	evalsDir := t.TempDir()
	s := NewServer(":8080", evalsDir, nil)

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

	s := NewServer(":8080", evalsDir, nil)

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
	runsDir := filepath.Join(evalsDir, "runs", "20250101-120000-abc1234")
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatalf("failed to create runs dir: %v", err)
	}

	s := NewServer(":8080", evalsDir, nil)

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
	s := NewServer(":8080", t.TempDir(), nil)

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
	s := NewServer(":8080", t.TempDir(), nil)

	req := httptest.NewRequest("GET", "/api/baselines/diff", nil)
	w := httptest.NewRecorder()

	s.handleBaselineDiff(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status (no params): got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	req = httptest.NewRequest("GET", "/api/baselines/diff?suite=smoke", nil)
	w = httptest.NewRecorder()

	s.handleBaselineDiff(w, req)

	resp = w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status (missing scenario): got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleBaselineDiffNotFound(t *testing.T) {
	s := NewServer(":8080", t.TempDir(), nil)

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

	s := NewServer(":8080", evalsDir, nil)

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

func TestHandleBaselineDiffFlatFormat(t *testing.T) {
	evalsDir := t.TempDir()
	baselineDir := filepath.Join(evalsDir, "baselines", "smoke")
	if err := os.MkdirAll(baselineDir, 0o755); err != nil {
		t.Fatalf("failed to create baseline dir: %v", err)
	}

	// Flat format baseline
	flat := `{"scenario":"test","tool_sequence":["order_lookup"],"required_fields":["response"],"forbidden_content":[]}`
	if err := os.WriteFile(filepath.Join(baselineDir, "test.json"), []byte(flat), 0o644); err != nil {
		t.Fatalf("failed to write baseline: %v", err)
	}

	s := NewServer(":8080", evalsDir, nil)

	req := httptest.NewRequest("GET", "/api/baselines/diff?suite=smoke&scenario=test", nil)
	w := httptest.NewRecorder()
	s.handleBaselineDiff(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Verify the response has normalized (nested) fields
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ts, ok := body["tool_sequence"]
	if !ok {
		t.Fatal("expected tool_sequence in response")
	}
	tsMap, ok := ts.(map[string]interface{})
	if !ok {
		t.Fatalf("tool_sequence should be object, got %T", ts)
	}
	if _, ok := tsMap["required"]; !ok {
		t.Error("tool_sequence should have 'required' key (nested format)")
	}
}

func TestHandleApproveProposalMethodNotAllowed(t *testing.T) {
	s := NewServer(":8080", t.TempDir(), nil)

	req := httptest.NewRequest("GET", "/api/proposals/approve", nil)
	w := httptest.NewRecorder()

	s.handleApproveProposal(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestHandleRejectProposalMethodNotAllowed(t *testing.T) {
	s := NewServer(":8080", t.TempDir(), nil)

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
	s := NewServer(":8080", evalsDir, nil)

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

	if body["status"] != "no_runs" {
		t.Errorf("status: got %v, want %q", body["status"], "no_runs")
	}
}

// ---------------------------------------------------------------------------
// Phase 1 new endpoint tests
// ---------------------------------------------------------------------------

func TestHandleScenariosEmpty(t *testing.T) {
	s := NewServer(":8080", t.TempDir(), nil)

	req := httptest.NewRequest("GET", "/api/scenarios?suite=smoke", nil)
	w := httptest.NewRecorder()

	s.handleScenarios(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body []interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("expected empty array, got %d items", len(body))
	}
}

func TestHandleScenariosWithData(t *testing.T) {
	evalsDir := t.TempDir()
	suiteDir := filepath.Join(evalsDir, "smoke")
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	yamlContent := `scenario: test_scenario
description: "A test scenario"
tags:
  - smoke
input:
  messages:
    - role: user
      content: "Hello"
assertions:
  - type: tool_sequence
    required: ["lookup"]
`
	if err := os.WriteFile(filepath.Join(suiteDir, "test_scenario.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	s := NewServer(":8080", evalsDir, nil)

	req := httptest.NewRequest("GET", "/api/scenarios?suite=smoke", nil)
	w := httptest.NewRecorder()

	s.handleScenarios(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("expected 1 scenario, got %d", len(body))
	}
	if body[0]["scenario"] != "test_scenario" {
		t.Errorf("scenario name: got %v, want test_scenario", body[0]["scenario"])
	}
}

func TestHandleBaselinesEmpty(t *testing.T) {
	s := NewServer(":8080", t.TempDir(), nil)

	req := httptest.NewRequest("GET", "/api/baselines?suite=smoke", nil)
	w := httptest.NewRecorder()

	s.handleBaselines(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body []interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("expected empty array, got %d items", len(body))
	}
}

func TestHandleBaselinesWithData(t *testing.T) {
	evalsDir := t.TempDir()
	baselineDir := filepath.Join(evalsDir, "baselines", "smoke")
	if err := os.MkdirAll(baselineDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	flat := `{"scenario":"test1","tool_sequence":["order_lookup"],"required_fields":["response"],"forbidden_content":[]}`
	if err := os.WriteFile(filepath.Join(baselineDir, "test1.json"), []byte(flat), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	s := NewServer(":8080", evalsDir, nil)

	req := httptest.NewRequest("GET", "/api/baselines?suite=smoke", nil)
	w := httptest.NewRecorder()

	s.handleBaselines(w, req)

	resp := w.Result()
	var body []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("expected 1 baseline, got %d", len(body))
	}
	if body[0]["scenario"] != "test1" {
		t.Errorf("scenario: got %v, want test1", body[0]["scenario"])
	}
}

func TestHandleRunsEmpty(t *testing.T) {
	s := NewServer(":8080", t.TempDir(), nil)

	req := httptest.NewRequest("GET", "/api/runs", nil)
	w := httptest.NewRecorder()

	s.handleRuns(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body []interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("expected empty array, got %d items", len(body))
	}
}

func TestHandleRunsWithData(t *testing.T) {
	evalsDir := t.TempDir()
	for _, name := range []string{"20250101-120000-abc", "20250102-120000-def"} {
		dir := filepath.Join(evalsDir, "runs", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		data := `{"suite":"smoke","run_id":"` + name + `"}`
		if err := os.WriteFile(filepath.Join(dir, "results.json"), []byte(data), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	s := NewServer(":8080", evalsDir, nil)

	req := httptest.NewRequest("GET", "/api/runs", nil)
	w := httptest.NewRecorder()

	s.handleRuns(w, req)

	var body []map[string]interface{}
	if err := json.NewDecoder(w.Result().Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(body))
	}
	// Most recent first
	if body[0]["run_id"] != "20250102-120000-def" {
		t.Errorf("first run should be most recent, got %v", body[0]["run_id"])
	}
}

func TestCORSHeaders(t *testing.T) {
	s := NewServer(":8080", t.TempDir(), nil)

	handler := corsMiddleware(http.HandlerFunc(s.handleProposals))

	req := httptest.NewRequest("OPTIONS", "/api/proposals", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("OPTIONS status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if v := resp.Header.Get("Access-Control-Allow-Origin"); v != "*" {
		t.Errorf("CORS Allow-Origin: got %q, want %q", v, "*")
	}
	if v := resp.Header.Get("Access-Control-Allow-Methods"); v == "" {
		t.Error("CORS Allow-Methods header missing")
	}
}

func TestSPAFallback(t *testing.T) {
	staticFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>SPA</html>")},
		"assets/main.js": &fstest.MapFile{Data: []byte("console.log('hi')")},
	}

	handler := spaHandler(http.FS(staticFS))

	// Existing file should be served directly
	req := httptest.NewRequest("GET", "/assets/main.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("existing file: got %d, want %d", w.Code, http.StatusOK)
	}

	// Non-existent path should fall back to index.html
	req = httptest.NewRequest("GET", "/health", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("SPA fallback: got %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if body != "<html>SPA</html>" {
		t.Errorf("SPA fallback body: got %q, want index.html content", body)
	}

	// /api/ paths should NOT fall back
	req = httptest.NewRequest("GET", "/api/nonexistent", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("API path: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

// Ensure unused import is referenced
var _ fs.FS
