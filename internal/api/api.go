// Package api implements the Gauntlet API server.
// It serves the React UI and provides REST endpoints for
// proposals, IO pairs, suite health, and baseline diffs.
package api

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pmclSF/gauntlet/internal/baseline"
	"github.com/pmclSF/gauntlet/internal/discovery"
	"github.com/pmclSF/gauntlet/internal/iopairs"
	"github.com/pmclSF/gauntlet/internal/scenario"
)

// Server is the Gauntlet API server.
type Server struct {
	Addr     string
	EvalsDir string
	StaticFS fs.FS // embedded or on-disk filesystem for UI assets

	proposals []discovery.Proposal
	libraries []*iopairs.Library
	mu        sync.RWMutex
}

// NewServer creates a new API server.
func NewServer(addr, evalsDir string, staticFS fs.FS) *Server {
	return &Server{
		Addr:     addr,
		EvalsDir: evalsDir,
		StaticFS: staticFS,
	}
}

// Start begins serving the API.
func (s *Server) Start() error {
	if err := s.loadData(); err != nil {
		log.Printf("WARN: failed to load data: %v", err)
	}

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := s.loadData(); err != nil {
				log.Printf("WARN: refresh failed: %v", err)
			}
		}
	}()

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/proposals", s.handleProposals)
	mux.HandleFunc("/api/proposals/approve", s.handleApproveProposal)
	mux.HandleFunc("/api/proposals/reject", s.handleRejectProposal)
	mux.HandleFunc("/api/pairs", s.handlePairs)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/results", s.handleResults)
	mux.HandleFunc("/api/baselines/diff", s.handleBaselineDiff)
	mux.HandleFunc("/api/scenarios", s.handleScenarios)
	mux.HandleFunc("/api/baselines", s.handleBaselines)
	mux.HandleFunc("/api/runs", s.handleRuns)

	// Static file serving with SPA fallback
	if s.StaticFS != nil {
		mux.Handle("/", spaHandler(http.FS(s.StaticFS)))
	}

	handler := corsMiddleware(mux)

	log.Printf("Gauntlet API server starting on %s", s.Addr)
	return http.ListenAndServe(s.Addr, handler)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func spaHandler(fsys http.FileSystem) http.Handler {
	fileServer := http.FileServer(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		f, err := fsys.Open(r.URL.Path)
		if err != nil {
			// File not found — serve index.html for SPA routing
			r.URL.Path = "/"
		} else {
			f.Close()
		}
		fileServer.ServeHTTP(w, r)
	})
}

func (s *Server) loadData() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Load proposals
	proposalPath := filepath.Join(s.EvalsDir, "proposals.yaml")
	if proposals, err := discovery.LoadProposals(proposalPath); err == nil {
		s.proposals = proposals
	}

	// Load IO pair libraries
	pairsDir := filepath.Join(s.EvalsDir, "pairs")
	if libs, err := iopairs.LoadLibrariesFromDir(pairsDir); err == nil {
		s.libraries = libs
	}

	return nil
}

func (s *Server) handleProposals(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.proposals); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode proposals response: %v", err), http.StatusInternalServerError)
	}
}

func (s *Server) handleApproveProposal(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.proposals {
		if s.proposals[i].ID == req.ID {
			s.proposals[i].Status = "approved"
			s.saveProposals()
			w.WriteHeader(http.StatusOK)
			if err := json.NewEncoder(w).Encode(s.proposals[i]); err != nil {
				http.Error(w, fmt.Sprintf("failed to encode approved proposal response: %v", err), http.StatusInternalServerError)
			}
			return
		}
	}

	http.Error(w, "proposal not found", http.StatusNotFound)
}

func (s *Server) handleRejectProposal(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.proposals {
		if s.proposals[i].ID == req.ID {
			s.proposals[i].Status = "rejected"
			s.saveProposals()
			w.WriteHeader(http.StatusOK)
			if err := json.NewEncoder(w).Encode(s.proposals[i]); err != nil {
				http.Error(w, fmt.Sprintf("failed to encode rejected proposal response: %v", err), http.StatusInternalServerError)
			}
			return
		}
	}

	http.Error(w, "proposal not found", http.StatusNotFound)
}

func (s *Server) handlePairs(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.libraries); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode pairs response: %v", err), http.StatusInternalServerError)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resultsDir := filepath.Join(s.EvalsDir, "runs")
	entries, err := os.ReadDir(resultsDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if encodeErr := json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "no_runs",
		}); encodeErr != nil {
			http.Error(w, fmt.Sprintf("failed to encode health response: %v", encodeErr), http.StatusInternalServerError)
		}
		return
	}

	var latestResults []byte
	for i := len(entries) - 1; i >= 0; i-- {
		p := filepath.Join(resultsDir, entries[i].Name(), "results.json")
		if data, err := os.ReadFile(p); err == nil {
			latestResults = data
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if latestResults != nil {
		if _, err := w.Write(latestResults); err != nil {
			http.Error(w, fmt.Sprintf("failed to write health response: %v", err), http.StatusInternalServerError)
		}
	} else {
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "no_results",
		}); err != nil {
			http.Error(w, fmt.Sprintf("failed to encode health response: %v", err), http.StatusInternalServerError)
		}
	}
}

func (s *Server) handleResults(w http.ResponseWriter, r *http.Request) {
	s.handleHealth(w, r)
}

func (s *Server) handleBaselineDiff(w http.ResponseWriter, r *http.Request) {
	suite := r.URL.Query().Get("suite")
	scenario := r.URL.Query().Get("scenario")

	if suite == "" || scenario == "" {
		http.Error(w, "suite and scenario required", http.StatusBadRequest)
		return
	}

	baselineDir := filepath.Join(s.EvalsDir, "baselines")
	contract, err := baseline.Load(baselineDir, suite, scenario)
	if err != nil {
		http.Error(w, fmt.Sprintf("baseline error: %v", err), http.StatusInternalServerError)
		return
	}
	if contract == nil {
		http.Error(w, "baseline not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(contract); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode baseline diff response: %v", err), http.StatusInternalServerError)
	}
}

func (s *Server) handleScenarios(w http.ResponseWriter, r *http.Request) {
	suite := r.URL.Query().Get("suite")
	if suite == "" {
		suite = "smoke"
	}

	suiteDir := filepath.Join(s.EvalsDir, suite)
	scenarios, err := scenario.LoadSuite(suiteDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if encodeErr := json.NewEncoder(w).Encode([]interface{}{}); encodeErr != nil {
			http.Error(w, fmt.Sprintf("failed to encode scenarios response: %v", encodeErr), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(scenarios); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode scenarios response: %v", err), http.StatusInternalServerError)
	}
}

func (s *Server) handleBaselines(w http.ResponseWriter, r *http.Request) {
	suite := r.URL.Query().Get("suite")
	if suite == "" {
		suite = "smoke"
	}

	baselineDir := filepath.Join(s.EvalsDir, "baselines", suite)
	entries, err := os.ReadDir(baselineDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if encodeErr := json.NewEncoder(w).Encode([]interface{}{}); encodeErr != nil {
			http.Error(w, fmt.Sprintf("failed to encode baselines response: %v", encodeErr), http.StatusInternalServerError)
		}
		return
	}

	var baselines []*baseline.Contract
	parentDir := filepath.Join(s.EvalsDir, "baselines")
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		c, err := baseline.Load(parentDir, suite, name)
		if err != nil || c == nil {
			continue
		}
		baselines = append(baselines, c)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(baselines); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode baselines response: %v", err), http.StatusInternalServerError)
	}
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	resultsDir := filepath.Join(s.EvalsDir, "runs")
	entries, err := os.ReadDir(resultsDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if encodeErr := json.NewEncoder(w).Encode([]interface{}{}); encodeErr != nil {
			http.Error(w, fmt.Sprintf("failed to encode runs response: %v", encodeErr), http.StatusInternalServerError)
		}
		return
	}

	// Sort entries by name descending (most recent first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() > entries[j].Name()
	})

	const maxRuns = 20
	var runs []json.RawMessage
	for _, entry := range entries {
		if len(runs) >= maxRuns {
			break
		}
		p := filepath.Join(resultsDir, entry.Name(), "results.json")
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		runs = append(runs, json.RawMessage(data))
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(runs); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode runs response: %v", err), http.StatusInternalServerError)
	}
}

func (s *Server) saveProposals() {
	// TODO(stage3): Return this error to the caller so mutation endpoints report failures.
	path := filepath.Join(s.EvalsDir, "proposals.yaml")
	if err := discovery.SaveProposals(s.proposals, path); err != nil {
		log.Printf("WARN: failed to save proposals: %v", err)
	}
}
