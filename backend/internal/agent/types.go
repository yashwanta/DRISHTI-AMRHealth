// Package agent implements the DRISHTI SiteOps investigation agent: it pulls
// logs from multiple sources for a robot/time window, analyzes them with an LLM
// (Ollama) and produces a structured root-cause finding.
package agent

import "time"

// Source state constants used in SourceStatus.State and AgentJob.Status.
const (
	StatePending    = "pending"
	StateInProgress = "in_progress"
	StateDone       = "done"
	StateUnavailable = "unavailable"

	JobPending     = "pending"
	JobCollecting  = "collecting"
	JobAnalyzing   = "analyzing"
	JobComplete    = "complete"
	JobError       = "error"
)

// Source identifiers — keep stable; the frontend Panel B lists these.
const (
	SourceRDSAPI  = "RDS API logs"
	SourceDB      = "Roboshop DB events"
	SourceJournal = "System journal"
	SourceNetwork = "Network / DHCP logs"
)

// AgentJob is the full state of one investigation, returned to the frontend on
// poll. Mirrors the brief §6 AgentJob struct.
type AgentJob struct {
	ID                string          `json:"id"`
	PlantID           string          `json:"plant_id"`
	RobotID           string          `json:"robot_id"`
	InvestigationType string          `json:"investigation_type"`
	Focus             string          `json:"focus,omitempty"`
	WindowStart       time.Time       `json:"window_start"`
	WindowEnd         time.Time       `json:"window_end"`
	Status            string          `json:"status"`
	Sources           []SourceStatus  `json:"sources"`
	LogBundle         []LogEntry      `json:"log_bundle"`
	Finding           *AgentFinding   `json:"finding,omitempty"`
	Error             string          `json:"error,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	CompletedAt       *time.Time      `json:"completed_at,omitempty"`
}

// SourceStatus tracks one log source's collection progress (Panel B rows).
type SourceStatus struct {
	Source string `json:"source"`
	State  string `json:"state"` // pending | in_progress | done | unavailable
	Result string `json:"result"` // human-readable, e.g. "47 entries pulled"
	Count  int    `json:"count"`
	Error  string `json:"error,omitempty"`
}

// LogEntry is a normalized log line from any source.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"` // rds | syslog | db | network | journal
	Level     string    `json:"level"`  // info | warn | error
	Message   string    `json:"message"`
}

// TimelineEvent is an ordered key event used in the finding timeline.
type TimelineEvent struct {
	Timestamp string `json:"timestamp"`
	Source    string `json:"source"`
	Event     string `json:"event"`
}

// AgentFinding is the analyzed result (Panel C). Produced by Ollama or the
// rule-based fallback. Via records which path produced it.
type AgentFinding struct {
	RootCause   string          `json:"root_cause"`
	Confidence  string          `json:"confidence"` // high | medium | low
	Factors     []string        `json:"factors"`
	Timeline    []TimelineEvent `json:"timeline"`
	Prevention  string          `json:"prevention"`
	RawLogs     []LogEntry      `json:"raw_logs"`
	Via         string          `json:"via"` // llama3 | rules
	LLMNote     string          `json:"llm_note,omitempty"`
}

// newSources returns the four source rows in display order, all pending.
func newSources() []SourceStatus {
	return []SourceStatus{
		{Source: SourceRDSAPI, State: StatePending},
		{Source: SourceDB, State: StatePending},
		{Source: SourceJournal, State: StatePending},
		{Source: SourceNetwork, State: StatePending},
	}
}
