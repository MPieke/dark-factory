package factoryapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	attractor "dark-factory/internal/factory"
)

type RunStatus string

const (
	RunQueued  RunStatus = "queued"
	RunRunning RunStatus = "running"
	RunSuccess RunStatus = "success"
	RunFailed  RunStatus = "failed"
)

type RunRequest struct {
	PipelinePath string `json:"pipeline_path"`
	Workdir      string `json:"workdir"`
	Runsdir      string `json:"runsdir"`
	RunID        string `json:"run_id"`
	Resume       bool   `json:"resume"`
}

type RunRecord struct {
	ID         string              `json:"id"`
	Status     RunStatus           `json:"status"`
	Error      string              `json:"error,omitempty"`
	CreatedAt  string              `json:"created_at"`
	StartedAt  string              `json:"started_at,omitempty"`
	FinishedAt string              `json:"finished_at,omitempty"`
	Request    RunRequest          `json:"request"`
	Config     attractor.RunConfig `json:"config"`
}

type Runner func(cfg attractor.RunConfig) error

type Server struct {
	runner Runner

	mu   sync.RWMutex
	runs map[string]*RunRecord
}

func NewServer(runner Runner) *Server {
	if runner == nil {
		runner = attractor.RunPipeline
	}
	return &Server{runner: runner, runs: map[string]*RunRecord{}}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/runs", s.handleRuns)
	mux.HandleFunc("/runs/", s.handleRunByID)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "factory-api"})
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.createRun(w, r)
	case http.MethodGet:
		s.listRuns(w)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleRunByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/runs/")
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "/") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid run id"})
		return
	}

	s.mu.RLock()
	rec, ok := s.runs[id]
	s.mu.RUnlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(req.PipelinePath) == "" || strings.TrimSpace(req.Workdir) == "" || strings.TrimSpace(req.Runsdir) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pipeline_path, workdir, and runsdir are required"})
		return
	}
	if req.Resume && strings.TrimSpace(req.RunID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "run_id is required when resume=true"})
		return
	}
	id := strings.TrimSpace(req.RunID)
	if id == "" {
		id = time.Now().UTC().Format("20060102_150405.000")
	}
	cfg := attractor.RunConfig{
		PipelinePath: strings.TrimSpace(req.PipelinePath),
		Workdir:      strings.TrimSpace(req.Workdir),
		Runsdir:      strings.TrimSpace(req.Runsdir),
		RunID:        id,
		Resume:       req.Resume,
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rec := &RunRecord{
		ID:        id,
		Status:    RunQueued,
		CreatedAt: now,
		Request:   req,
		Config:    cfg,
	}

	s.mu.Lock()
	if _, exists := s.runs[id]; exists {
		s.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": fmt.Sprintf("run %s already exists", id)})
		return
	}
	s.runs[id] = rec
	s.mu.Unlock()

	go s.executeRun(id, cfg)
	writeJSON(w, http.StatusAccepted, rec)
}

func (s *Server) executeRun(id string, cfg attractor.RunConfig) {
	s.mu.Lock()
	rec := s.runs[id]
	rec.Status = RunRunning
	rec.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.mu.Unlock()

	err := s.runner(cfg)

	s.mu.Lock()
	defer s.mu.Unlock()
	rec = s.runs[id]
	rec.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err != nil {
		rec.Status = RunFailed
		rec.Error = err.Error()
		return
	}
	rec.Status = RunSuccess
	rec.Error = ""
}

func (s *Server) listRuns(w http.ResponseWriter) {
	s.mu.RLock()
	items := make([]*RunRecord, 0, len(s.runs))
	for _, r := range s.runs {
		items = append(items, r)
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	writeJSON(w, http.StatusOK, map[string]any{"runs": items})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
