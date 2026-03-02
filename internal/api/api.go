// Package api implements the Gauntlet API server.
// It serves the React UI and provides REST endpoints for
// proposals, IO pairs, suite health, and baseline diffs.
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/gauntlet-dev/gauntlet/internal/discovery"
	"github.com/gauntlet-dev/gauntlet/internal/iopairs"
)

// Server is the Gauntlet API server.
type Server struct {
	Addr       string
	EvalsDir   string
	StaticDir  string

	proposals  []discovery.Proposal
	libraries  []*iopairs.Library
	mu         sync.RWMutex
}

// NewServer creates a new API server.
func NewServer(addr, evalsDir, staticDir string) *Server {
	return &Server{
		Addr:      addr,
		EvalsDir:  evalsDir,
		StaticDir: staticDir,
	}
}

// Start begins serving the API.
func (s *Server) Start() error {
	if err := s.loadData(); err != nil {
		log.Printf("WARN: failed to load data: %v", err)
	}

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/proposals", s.handleProposals)
	mux.HandleFunc("/api/proposals/approve", s.handleApproveProposal)
	mux.HandleFunc("/api/proposals/reject", s.handleRejectProposal)
	mux.HandleFunc("/api/pairs", s.handlePairs)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/results", s.handleResults)
	mux.HandleFunc("/api/baselines/diff", s.handleBaselineDiff)

	// Static file serving for React UI
	if s.StaticDir != "" {
		fs := http.FileServer(http.Dir(s.StaticDir))
		mux.Handle("/", fs)
	}

	log.Printf("Gauntlet API server starting on %s", s.Addr)
	return http.ListenAndServe(s.Addr, mux)
}

func (s *Server) loadData() error {
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
	json.NewEncoder(w).Encode(s.proposals)
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
			json.NewEncoder(w).Encode(s.proposals[i])
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
			json.NewEncoder(w).Encode(s.proposals[i])
			return
		}
	}

	http.Error(w, "proposal not found", http.StatusNotFound)
}

func (s *Server) handlePairs(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.libraries)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Return latest suite health from results
	resultsDir := filepath.Join(s.EvalsDir, "runs")
	entries, err := os.ReadDir(resultsDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "no_runs",
		})
		return
	}

	// Find most recent results.json
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
		w.Write(latestResults)
	} else {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "no_results",
		})
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

	baselinePath := filepath.Join(s.EvalsDir, "baselines", suite, scenario+".json")
	data, err := os.ReadFile(baselinePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("baseline not found: %v", err), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (s *Server) saveProposals() {
	path := filepath.Join(s.EvalsDir, "proposals.yaml")
	if err := discovery.SaveProposals(s.proposals, path); err != nil {
		log.Printf("WARN: failed to save proposals: %v", err)
	}
}
