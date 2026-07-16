package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"drishti-amr-health/internal/config"
	"drishti-amr-health/internal/robowatch"
)

type RobowatchHandler struct {
	db *pgxpool.Pool
}

func NewRobowatchHandler(db *pgxpool.Pool) *RobowatchHandler {
	return &RobowatchHandler{db: db}
}

func (h *RobowatchHandler) ListPlants(w http.ResponseWriter, r *http.Request) {
	plants := config.AllPlants()
	type resp struct {
		Name       string `json:"name"`
		SystemType string `json:"system_type"`
		BaseURL    string `json:"base_url"`
		Port       int    `json:"port"`
		Username   string `json:"username"`
	}
	out := make([]resp, len(plants))
	for i, p := range plants {
		out[i] = resp{p.Name, p.SystemType, p.BaseURL, p.Port, p.Username}
	}
	jsonOK(w, out)
}

func (h *RobowatchHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	plant := chi.URLParam(r, "plant")
	if config.GetPlant(plant) == nil {
		jsonError(w, "plant not found", http.StatusNotFound)
		return
	}

	var lastPull *time.Time
	var logsPulled int
	var lastError, sourcesJSON *string

	row := h.db.QueryRow(r.Context(),
		`SELECT last_successful_pull, logs_pulled, last_error, available_sources
			 FROM rds_connection_status WHERE plant = $1`, plant)
	_ = row.Scan(&lastPull, &logsPulled, &lastError, &sourcesJSON)

	reachable := false
	if pw := config.GetRobowatchPassword(plant); pw != "" {
		pCfg := config.GetPlant(plant)
		c := robowatch.NewClient(pCfg.BaseURL, pCfg.Port, pCfg.Username, pw)
		tr := c.TestConnection()
		reachable = tr.Reachable && tr.Authenticated
	}

	sources := make([]string, 0)
	if sourcesJSON != nil && *sourcesJSON != "" {
		_ = json.Unmarshal([]byte(*sourcesJSON), &sources)
	}

	jsonOK(w, map[string]any{
		"plant":                plant,
		"reachable":            reachable,
		"authenticated":        reachable,
		"last_successful_pull": lastPull,
		"logs_pulled":          logsPulled,
		"last_error":           lastError,
		"available_sources":    sources,
	})
}

func (h *RobowatchHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	plant := chi.URLParam(r, "plant")
	pCfg := config.GetPlant(plant)
	if pCfg == nil {
		jsonError(w, "plant not found", http.StatusNotFound)
		return
	}

	pw := config.GetRobowatchPassword(plant)
	if pw == "" {
		jsonOK(w, robowatch.TestResult{
			Reachable: false, Authenticated: false, Success: false,
			Error:     fmt.Sprintf("ROBOWATCH_%s_PASSWORD not set", strings.ToUpper(strings.ReplaceAll(plant, " ", "_"))),
			ErrorCode: "login_failed",
		})
		return
	}

	c := robowatch.NewClient(pCfg.BaseURL, pCfg.Port, pCfg.Username, pw)
	jsonOK(w, c.TestConnection())
}

func (h *RobowatchHandler) DiscoverSources(w http.ResponseWriter, r *http.Request) {
	plant := chi.URLParam(r, "plant")
	pCfg := config.GetPlant(plant)
	if pCfg == nil {
		jsonError(w, "plant not found", http.StatusNotFound)
		return
	}

	pw := config.GetRobowatchPassword(plant)
	if pw == "" {
		jsonError(w, "ROBOWATCH password not configured for this plant", http.StatusInternalServerError)
		return
	}

	c := robowatch.NewClient(pCfg.BaseURL, pCfg.Port, pCfg.Username, pw)
	sources, err := c.DiscoverSources()
	if err != nil {
		jsonError(w, fmt.Sprintf("discovery failed: %v", err), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]any{"plant": plant, "sources": sources})
}

func (h *RobowatchHandler) FetchLogs(w http.ResponseWriter, r *http.Request) {
	plant := chi.URLParam(r, "plant")
	pCfg := config.GetPlant(plant)
	if pCfg == nil {
		jsonError(w, "plant not found", http.StatusNotFound)
		return
	}

	pw := config.GetRobowatchPassword(plant)
	if pw == "" {
		jsonError(w, "ROBOWATCH password not configured", http.StatusInternalServerError)
		return
	}

	c := robowatch.NewClient(pCfg.BaseURL, pCfg.Port, pCfg.Username, pw)
	rawLines, err := c.FetchLogs(time.Time{}, time.Time{})
	if err != nil {
		h.recordFetchError(r.Context(), plant, err)
		jsonError(w, fmt.Sprintf("fetch failed: %v", err), http.StatusInternalServerError)
		return
	}

	if len(rawLines) == 0 {
		jsonOK(w, map[string]any{"event_count": 0, "message": "no logs found"})
		return
	}

	eventCount, storeErr := h.storeLogs(r.Context(), plant, pCfg.SystemType, rawLines)
	if storeErr != nil {
		h.recordFetchError(r.Context(), plant, storeErr)
		jsonError(w, fmt.Sprintf("logs fetched but storage failed: %v", storeErr), http.StatusInternalServerError)
		return
	}

	_, _ = h.db.Exec(r.Context(),
		`INSERT INTO rds_connection_status (plant, last_successful_pull, logs_pulled, last_error)
			 VALUES ($1, NOW(), $2, NULL)
			 ON CONFLICT (plant) DO UPDATE SET
			   last_successful_pull = NOW(),
			   logs_pulled = rds_connection_status.logs_pulled + $2,
			   last_error = NULL`,
		plant, eventCount)

	jsonOK(w, map[string]any{"event_count": eventCount, "message": fmt.Sprintf("stored %d events", eventCount)})
}

func (h *RobowatchHandler) ListLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	args := []any{
		nullable(q.Get("plant")),
		nullable(q.Get("from")),
		nullable(q.Get("to")),
		nullable(q.Get("robot")),
		nullable(q.Get("user")),
		nullable(q.Get("category")),
		nullable(q.Get("severity")),
		nullableBool(q.Get("execution_evidence")),
		nullable(q.Get("q")),
	}

	limit := 500
	fmt.Sscanf(q.Get("limit"), "%d", &limit)
	offset := 0
	fmt.Sscanf(q.Get("offset"), "%d", &offset)

	sql := `SELECT id, plant, source_system, timestamp, robot, "user", action, category,
			       severity, message, raw_log, confidence, execution_evidence
		        FROM rds_log_events
		        WHERE ($1::text IS NULL OR plant = $1)
		          AND ($2::timestamptz IS NULL OR timestamp >= $2)
		          AND ($3::timestamptz IS NULL OR timestamp <= $3)
		          AND ($4::text IS NULL OR robot ILIKE '%' || $4 || '%')
		          AND ($5::text IS NULL OR "user" ILIKE '%' || $5 || '%')
		          AND ($6::text IS NULL OR category = $6)
		          AND ($7::text IS NULL OR severity = $7)
		          AND ($8::boolean IS NULL OR execution_evidence = $8)
		          AND ($9::text IS NULL OR message ILIKE '%' || $9 || '%' OR raw_log ILIKE '%' || $9 || '%')
		        ORDER BY timestamp DESC
		        LIMIT $10 OFFSET $11`

	rows, err := h.db.Query(r.Context(), sql, append(args, limit, offset)...)
	if err != nil {
		jsonError(w, fmt.Sprintf("query failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var entries []map[string]any
	for rows.Next() {
		var id int
		var plant, sourceSystem, robot, user, action, category, severity, message, rawLog, confidence string
		var timestamp time.Time
		var executionEvidence bool
		_ = rows.Scan(&id, &plant, &sourceSystem, &timestamp, &robot, &user, &action,
			&category, &severity, &message, &rawLog, &confidence, &executionEvidence)
		entries = append(entries, map[string]any{
			"id": id, "plant": plant, "source_system": sourceSystem,
			"timestamp": timestamp.Format(time.RFC3339),
			"robot":     robot, "user": user, "action": action,
			"category": category, "severity": severity,
			"message": message, "raw_log": rawLog,
			"confidence": confidence, "execution_evidence": executionEvidence,
		})
	}

	if entries == nil {
		entries = []map[string]any{}
	}
	jsonOK(w, entries)
}

func (h *RobowatchHandler) storeLogs(ctx context.Context, plant, sourceSystem string, rawLines []string) (int, error) {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	count := 0
	for _, raw := range rawLines {
		entry := robowatch.NormalizeLog(raw, plant, sourceSystem)
		_, err := tx.Exec(ctx,
			`INSERT INTO rds_log_events
				   (plant, source_system, timestamp, robot, "user", action, category,
				    severity, message, raw_log, confidence, execution_evidence, robot_ip)
				 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			entry.Plant, entry.SourceSystem, entry.Timestamp,
			entry.Robot, entry.User, entry.Action, entry.Category,
			entry.Severity, entry.Message, entry.RawLog,
			entry.Confidence, entry.ExecutionEvidence, entry.RobotIP)
		if err == nil {
			count++
		}
	}

	return count, tx.Commit(ctx)
}

func (h *RobowatchHandler) recordFetchError(ctx context.Context, plant string, err error) {
	_, _ = h.db.Exec(ctx,
		`INSERT INTO rds_connection_status (plant, last_error)
			 VALUES ($1, $2)
			 ON CONFLICT (plant) DO UPDATE SET last_error = $2`,
		plant, err.Error())
}

func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullableBool(v string) any {
	if v == "" {
		return nil
	}
	return v == "true"
}
