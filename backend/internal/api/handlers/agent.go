package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"drishti-amr-health/internal/agent"
)

// AgentHandler exposes the investigation agent over HTTP.
type AgentHandler struct {
	orch *agent.Orchestrator
	snap *agent.Snapshotter
}

func NewAgentHandler(orch *agent.Orchestrator, snap *agent.Snapshotter) *AgentHandler {
	return &AgentHandler{orch: orch, snap: snap}
}

type agentStartRequest struct {
	PlantID           string `json:"plant_id"`
	RobotID           string `json:"robot_id"`
	InvestigationType string `json:"investigation_type"`
	Focus             string `json:"focus"`
	WindowStart       string `json:"window_start"` // RFC3339 or "yyyy-mm-ddTHH:mm"
	WindowEnd         string `json:"window_end"`
}

// Start creates an investigation job.
func (h *AgentHandler) Start(w http.ResponseWriter, r *http.Request) {
	var req agentStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	req.PlantID = strings.TrimSpace(req.PlantID)
	req.InvestigationType = strings.TrimSpace(req.InvestigationType)
	req.Focus = strings.TrimSpace(req.Focus)
	if req.PlantID == "" || req.InvestigationType == "" {
		jsonError(w, "plant_id and investigation_type are required", http.StatusBadRequest)
		return
	}

	start, end, err := resolveWindow(req.WindowStart, req.WindowEnd)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	jobID, err := h.orch.Start(req.PlantID, req.RobotID, req.InvestigationType, req.Focus, start, end)
	if err != nil {
		internalError(w, err)
		return
	}
	jsonOK(w, map[string]string{"job_id": jobID})
}

// Get returns the current job state for polling.
func (h *AgentHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "job_id")
	job, ok := h.orch.Store().Get(id)
	if !ok {
		jsonError(w, "job not found", http.StatusNotFound)
		return
	}
	jsonOK(w, job)
}

// Restore pushes back the last good config (after confirmation).
// Robots lists robots for a plant, used to populate the Robot ID dropdown.
// Falls back to robots inferred from log_events when the RDS API list is empty.
func (h *AgentHandler) Robots(w http.ResponseWriter, r *http.Request) {
	plant := strings.TrimSpace(r.URL.Query().Get("plant"))
	out := h.orch.Robots(r.Context(), plant)
	if out == nil {
		out = []map[string]string{}
	}
	jsonOK(w, out)
}

// Snapshots returns recent config snapshots (optional diagnostic view).
func (h *AgentHandler) Snapshots(w http.ResponseWriter, r *http.Request) {
	out := h.orch.Snapshots(r.Context(), r.URL.Query().Get("robot_id"))
	if out == nil {
		out = []any{}
	}
	jsonOK(w, out)
}

// resolveWindow parses the two window timestamps; defaults to the last 3 hours.
func resolveWindow(startStr, endStr string) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	end := now
	start := now.Add(-3 * time.Hour)

	parse := func(s string) (time.Time, error) {
		s = strings.TrimSpace(s)
		if s == "" {
			return time.Time{}, nil
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UTC(), nil
		}
		// datetime-local form: "yyyy-mm-ddTHH:mm"
		if t, err := time.Parse("2006-01-02T15:04", s); err == nil {
			return t.UTC(), nil
		}
		if t, err := time.Parse("2006-01-02 15:04", s); err == nil {
			return t.UTC(), nil
		}
		return time.Time{}, &windowErr{s}
	}

	if t, err := parse(startStr); err != nil {
		return start, end, err
	} else if !t.IsZero() {
		start = t
	}
	if t, err := parse(endStr); err != nil {
		return start, end, err
	} else if !t.IsZero() {
		end = t
	}
	if !end.After(start) {
		return start, end, &windowErr{"window_end must be after window_start"}
	}
	return start, end, nil
}

type windowErr struct{ msg string }

func (e *windowErr) Error() string { return "invalid window: " + e.msg }

// (strconv used for future count formatting; kept to avoid churn)
var _ = strconv.Itoa
