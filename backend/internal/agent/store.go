package agent

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Store is a concurrency-safe in-memory registry of active agent jobs. It is
// the source of truth for live progress (Panel B polling); the agent_jobs DB
// table is the durable record.
type Store struct {
	mu    sync.RWMutex
	jobs  map[string]*AgentJob
}

func NewStore() *Store {
	return &Store{jobs: make(map[string]*AgentJob)}
}

// New creates a job with a fresh ID and pending sources, stores it, and returns it.
func (s *Store) New(plantID, robotID, investigationType, focus string, windowStart, windowEnd time.Time) *AgentJob {
	job := &AgentJob{
		ID:                newID(),
		PlantID:           plantID,
		RobotID:           robotID,
		InvestigationType: investigationType,
		Focus:             focus,
		WindowStart:       windowStart,
		WindowEnd:         windowEnd,
		Status:            JobPending,
		Sources:           newSources(),
		LogBundle:         []LogEntry{},
		CreatedAt:         time.Now().UTC(),
	}
	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()
	return job
}

// Get returns a shallow copy of a job so callers can't mutate store state
// outside the orchestrator.
func (s *Store) Get(id string) (*AgentJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, false
	}
	cp := *j
	return &cp, true
}

// Snapshot returns the live job pointer for in-place mutation by the
// orchestrator. Intended for internal package use only.
func (s *Store) Snapshot(id string) (*AgentJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	return j, ok
}

// SnapshotSourceStatus returns a copy of the job's current source statuses.
func (s *Store) SnapshotSourceStatus(id string) []SourceStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil
	}
	out := make([]SourceStatus, len(j.Sources))
	copy(out, j.Sources)
	return out
}

// UpdateSource sets the state/result of one source row by source name.
func (s *Store) UpdateSource(id, source, state, result string, count int, errText string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return
	}
	for i := range j.Sources {
		if j.Sources[i].Source == source {
			j.Sources[i].State = state
			j.Sources[i].Result = result
			j.Sources[i].Count = count
			j.Sources[i].Error = errText
			return
		}
	}
}

// AppendLogs appends collected entries to the job's log bundle (mutex-guarded).
func (s *Store) AppendLogs(id string, entries []LogEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return
	}
	j.LogBundle = append(j.LogBundle, entries...)
}

// SetStatus updates the job status (and optional error).
func (s *Store) SetStatus(id, status, errText string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		j.Status = status
		if errText != "" {
			j.Error = errText
		}
		if status == JobComplete || status == JobError {
			now := time.Now().UTC()
			j.CompletedAt = &now
		}
	}
}

// SetFinding attaches the analyzed finding to the job.
func (s *Store) SetFinding(id string, finding *AgentFinding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		j.Finding = finding
	}
}

// newID returns a 36-char hex ID (fits VARCHAR(36)) from crypto/rand.
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read essentially never fails on modern platforms; fall back to a
		// time-derived value to stay functional in the degenerate case.
		return time.Now().UTC().Format("20060102150405000000000000000000")
	}
	return hex.EncodeToString(b[:])
}
