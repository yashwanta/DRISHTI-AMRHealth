package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"drishti-amr-health/internal/models"
)

// RemediationHandler manages Tier 1/2 remediation suggestions.
// AMR Health creates suggestions from detected incidents; SiteOps
// reads and resolves them via playbooks.
type RemediationHandler struct {
	db *pgxpool.Pool
}

func NewRemediationHandler(db *pgxpool.Pool) *RemediationHandler {
	return &RemediationHandler{db: db}
}

// remediationRule maps an event pattern to a suggested playbook task.
type remediationRule struct {
	Name        string
	EventTypes  []string
	Task        string
	Params      map[string]string
	Confidence  string
	AutoResolve bool
}

// rules defines the Tier 1 remediation knowledge base.
var rules = []remediationRule{
	{
		Name: "restart_crashed_service", EventTypes: []string{"service_failure", "service_stop"},
		Task: "service_restart", Confidence: "high", AutoResolve: true,
	},
	{
		Name: "restart_after_oom", EventTypes: []string{"oom_kill", "memory_event"},
		Task: "service_restart", Confidence: "medium", AutoResolve: false,
	},
	{
		Name: "disk_cleanup", EventTypes: []string{"disk_full", "disk_error"},
		Task: "approved_custom_command", Confidence: "low", AutoResolve: false,
	},
}

func ruleForEvent(eventType string) *remediationRule {
	for i := range rules {
		for _, et := range rules[i].EventTypes {
			if et == eventType {
				return &rules[i]
			}
		}
	}
	return nil
}

// GenerateSuggestions scans recent critical/error events and creates
// remediation suggestions for any that match a known rule. Called after sync.
func (h *RemediationHandler) GenerateSuggestions(ctx context.Context) error {
	rows, err := h.db.Query(ctx, `
		SELECT DISTINCT ON (server_id, event_type)
		       server_id, event_type, severity, message, source
		FROM log_events
		WHERE severity IN ('critical', 'error')
		  AND created_at > NOW() - INTERVAL '30 minutes'
		  AND server_id NOT IN (
		    SELECT server_id FROM remediation_suggestions
		    WHERE status IN ('pending','approved','running')
		      AND created_at > NOW() - INTERVAL '1 hour'
		  )
		ORDER BY server_id, event_type, created_at DESC
		LIMIT 50`)
	if err != nil {
		return fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var created int
	for rows.Next() {
		var serverID int
		var eventType, severity, message, source string
		if err := rows.Scan(&serverID, &eventType, &severity, &message, &source); err != nil {
			continue
		}

		rule := ruleForEvent(eventType)
		if rule == nil {
			continue
		}

		params := rule.Params
		if params == nil {
			params = map[string]string{}
		}

		// For service restart, try to extract the service name from the message.
		if rule.Task == "service_restart" && params["service"] == "" {
			params["service"] = "auto" // SiteOps will need to specify the service
		}

		evidence, _ := json.Marshal(map[string]string{
			"event_type": eventType, "severity": severity,
			"message": message, "source": source,
		})

		_, err := h.db.Exec(ctx, `
			INSERT INTO remediation_suggestions
			    (server_id, event_type, severity, description, suggested_task,
			     suggested_params, rule_name, confidence, auto_resolve, status, evidence)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending', $10)`,
			serverID, eventType, severity, message, rule.Task,
			params, rule.Name, rule.Confidence, rule.AutoResolve, evidence)
		// Note: evidence param is intentionally the rule_name position; fix below
		if err != nil {
			log.Printf("remediation: insert suggestion: %v", err)
		} else {
			created++
		}
	}

	if created > 0 {
		log.Printf("remediation: created %d new suggestion(s)", created)
	}
	return nil
}

// List returns all remediation suggestions (newest first).
func (h *RemediationHandler) List(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	query := `
		SELECT id, server_id, server_name, event_type, severity, description,
		       suggested_task, suggested_params, rule_name, confidence, auto_resolve,
		       status, resolution_id, resolution_type, resolution_output, resolved_by,
		       created_at, resolved_at
		FROM remediation_suggestions`
	args := []any{}
	if statusFilter != "" {
		query += " WHERE status = $1"
		args = append(args, statusFilter)
	}
	query += " ORDER BY created_at DESC LIMIT 200"

	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()

	suggestions := []models.RemediationSuggestion{}
	for rows.Next() {
		var s models.RemediationSuggestion
		var paramsJSON []byte
		if err := rows.Scan(&s.ID, &s.ServerID, &s.ServerName, &s.EventType, &s.Severity,
			&s.Description, &s.SuggestedTask, &paramsJSON, &s.RuleName, &s.Confidence,
			&s.AutoResolve, &s.Status, &s.ResolutionID, &s.ResolutionType,
			&s.ResolutionOutput, &s.ResolvedBy, &s.CreatedAt, &s.ResolvedAt); err != nil {
			internalError(w, err)
			return
		}
		if paramsJSON != nil {
			_ = json.Unmarshal(paramsJSON, &s.SuggestedParams)
		}
		suggestions = append(suggestions, s)
	}
	jsonOK(w, suggestions)
}

// GetStats returns summary counts for the remediation dashboard.
func (h *RemediationHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	stats := map[string]int{
		"pending": 0, "approved": 0, "running": 0, "resolved": 0, "failed": 0, "escalated": 0,
	}
	rows, err := h.db.Query(r.Context(), `
		SELECT status, COUNT(*) FROM remediation_suggestions
		WHERE created_at > NOW() - INTERVAL '7 days'
		GROUP BY status`)
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err == nil {
			stats[status] = count
		}
	}
	jsonOK(w, stats)
}
