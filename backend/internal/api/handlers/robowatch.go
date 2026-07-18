package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"drishti-amr-health/internal/config"
	"drishti-amr-health/internal/robowatch"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RobowatchHandler struct {
	db            *pgxpool.Pool
	encryptionKey string
}

func NewRobowatchHandler(db *pgxpool.Pool, encryptionKey string) *RobowatchHandler {
	return &RobowatchHandler{db: db, encryptionKey: encryptionKey}
}

func (h *RobowatchHandler) credentials(ctx context.Context, plant string) (string, string) {
	pCfg := config.GetPlant(plant)
	if pCfg == nil {
		return "", ""
	}
	var username, passwordEnc string
	if err := h.db.QueryRow(ctx, `SELECT username,password_enc FROM rds_credentials WHERE plant=$1`, plant).Scan(&username, &passwordEnc); err == nil {
		if password, err := decrypt(h.encryptionKey, passwordEnc); err == nil && password != "" {
			return username, password
		}
	}
	return pCfg.Username, config.GetRobowatchPassword(plant)
}

func (h *RobowatchHandler) SaveCredentials(w http.ResponseWriter, r *http.Request) {
	plant := chi.URLParam(r, "plant")
	pCfg := config.GetPlant(plant)
	if pCfg == nil {
		jsonError(w, "plant not found", http.StatusNotFound)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		req.Username = pCfg.Username
	}
	if req.Password == "" {
		jsonError(w, "password is required", http.StatusBadRequest)
		return
	}
	passwordEnc, err := encrypt(h.encryptionKey, req.Password)
	if err != nil {
		jsonError(w, "could not secure password", http.StatusInternalServerError)
		return
	}
	_, err = h.db.Exec(r.Context(), `INSERT INTO rds_credentials(plant,username,password_enc,updated_at) VALUES($1,$2,$3,NOW()) ON CONFLICT(plant) DO UPDATE SET username=EXCLUDED.username,password_enc=EXCLUDED.password_enc,updated_at=NOW()`, plant, req.Username, passwordEnc)
	if err != nil {
		jsonError(w, "could not save credentials", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]string{"status": "saved", "plant": plant})
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
	var serverStatus string
	var sourcesJSON []byte
	host := plantRDSHost(plant)
	_ = h.db.QueryRow(r.Context(), `SELECT s.last_sync_at, s.status, COUNT(le.id), COALESCE(json_agg(DISTINCT le.source) FILTER (WHERE le.source IS NOT NULL),'[]'::json) FROM servers s LEFT JOIN log_events le ON le.server_id=s.id WHERE s.host=$1 GROUP BY s.id,s.last_sync_at,s.status`, host).Scan(&lastPull, &serverStatus, &logsPulled, &sourcesJSON)

	reachable := false
	if username, pw := h.credentials(r.Context(), plant); pw != "" {
		pCfg := config.GetPlant(plant)
		c := robowatch.NewClient(pCfg.BaseURL, pCfg.Port, username, pw)
		tr := c.TestConnection()
		reachable = tr.Reachable && tr.Authenticated
	}

	sources := make([]string, 0)
	if len(sourcesJSON) > 0 {
		_ = json.Unmarshal(sourcesJSON, &sources)
	}
	var lastError any
	if serverStatus == "error" {
		lastError = "FleetManager SSH sync is in error state"
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

	username, pw := h.credentials(r.Context(), plant)
	if pw == "" {
		jsonOK(w, robowatch.TestResult{
			Reachable: false, Authenticated: false, Success: false,
			Error:     fmt.Sprintf("ROBOWATCH_%s_PASSWORD not set", strings.ToUpper(strings.ReplaceAll(plant, " ", "_"))),
			ErrorCode: "login_failed",
		})
		return
	}

	c := robowatch.NewClient(pCfg.BaseURL, pCfg.Port, username, pw)
	jsonOK(w, c.TestConnection())
}

func (h *RobowatchHandler) DiscoverSources(w http.ResponseWriter, r *http.Request) {
	plant := chi.URLParam(r, "plant")
	pCfg := config.GetPlant(plant)
	if pCfg == nil {
		jsonError(w, "plant not found", http.StatusNotFound)
		return
	}

	username, pw := h.credentials(r.Context(), plant)
	if pw == "" {
		jsonError(w, "ROBOWATCH password not configured for this plant", http.StatusInternalServerError)
		return
	}

	c := robowatch.NewClient(pCfg.BaseURL, pCfg.Port, username, pw)
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

	username, pw := h.credentials(r.Context(), plant)
	if pw == "" {
		jsonError(w, "ROBOWATCH password not configured", http.StatusInternalServerError)
		return
	}

	c := robowatch.NewClient(pCfg.BaseURL, pCfg.Port, username, pw)
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
	host := plantRDSHost(q.Get("plant"))
	args := []any{
		nullable(host),
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

	sql := `SELECT le.id, COALESCE($12::text,''), le.source, le.timestamp,
			       COALESCE((regexp_match(le.message,'(?i)(AMR[-_ ]?[0-9]+|RBK[-_ ]?[0-9]+)'))[1],''),
			       '', le.event_type,
			       CASE WHEN le.event_type ILIKE '%charge%' OR le.message ILIKE '%charge%' THEN 'charge' WHEN le.event_type ILIKE '%dock%' OR le.message ILIKE '%dock%' THEN 'dock' WHEN le.event_type ILIKE '%nav%' OR le.message ILIKE '%gotarget%' THEN 'navigation' WHEN le.severity IN ('critical','high') THEN 'error' ELSE 'status' END,
			       le.severity, le.message, COALESCE(le.raw_line,''), 'medium', false
		        FROM log_events le JOIN servers s ON s.id=le.server_id
		        WHERE ($1::text IS NULL OR s.host = $1)
		          AND ($2::timestamptz IS NULL OR le.timestamp >= $2)
		          AND ($3::timestamptz IS NULL OR le.timestamp <= $3)
		          AND ($4::text IS NULL OR le.message ILIKE '%' || $4 || '%' OR COALESCE(le.raw_line,'') ILIKE '%' || $4 || '%')
		          AND ($5::text IS NULL OR le.message ILIKE '%' || $5 || '%')
		          AND ($6::text IS NULL OR le.event_type ILIKE '%' || $6 || '%' OR le.message ILIKE '%' || $6 || '%')
		          AND ($7::text IS NULL OR le.severity = $7)
		          AND ($8::boolean IS NULL OR $8 = false)
		          AND ($9::text IS NULL OR le.message ILIKE '%' || $9 || '%' OR COALESCE(le.raw_line,'') ILIKE '%' || $9 || '%' OR le.source ILIKE '%' || $9 || '%')
		        ORDER BY le.timestamp DESC
		        LIMIT $10 OFFSET $11`

	rows, err := h.db.Query(r.Context(), sql, append(args, limit, offset, q.Get("plant"))...)
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

func plantRDSHost(plant string) string {
	pCfg := config.GetPlant(plant)
	if pCfg == nil {
		return ""
	}
	parsed, err := url.Parse(pCfg.BaseURL)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
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
