package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"drishti-amr-health/internal/config"
	"drishti-amr-health/internal/models"
	"drishti-amr-health/internal/parser"
	sshclient "drishti-amr-health/internal/ssh"
)

type SyncHandler struct {
	db            *pgxpool.Pool
	encryptionKey string
	postSync      func(ctx context.Context)
}

type syncServerRow struct {
	host               string
	port               int
	username           string
	authType           string
	passwordEnc        string
	keyEnc             string
	proxmoxHost        string
	proxmoxPort        int
	proxmoxUsername    string
	proxmoxAuthType    string
	proxmoxPasswordEnc string
	proxmoxKeyEnc      string
	vmid               string
	appLogPaths        string
	lastSync           *time.Time
}

func NewSyncHandler(db *pgxpool.Pool, key string) *SyncHandler {
	return &SyncHandler{db: db, encryptionKey: key}
}

// OnSyncComplete registers a callback fired after each scheduled sync run.
// Used to trigger remediation suggestion generation.
func (h *SyncHandler) OnSyncComplete(fn func(ctx context.Context)) {
	h.postSync = fn
}

// SyncServer triggers an on-demand sync for a specific server.
func (h *SyncHandler) SyncServer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}

	jobID, err := h.runSync(context.Background(), id)
	if err != nil {
		internalError(w, err)
		return
	}
	jsonOK(w, map[string]int{"job_id": jobID})
}

// SyncAll triggers sync for every server, or a filtered asset type.
func (h *SyncHandler) SyncAll(w http.ResponseWriter, r *http.Request) {
	assetType := r.URL.Query().Get("asset_type")
	query := `SELECT id FROM servers`
	args := []any{}
	if assetType == "server" {
		query += ` WHERE asset_type <> 'endpoint'`
	} else if assetType == "endpoint" {
		query += ` WHERE asset_type = $1`
		args = append(args, assetType)
	} else if assetType != "" {
		jsonError(w, "asset_type must be server or endpoint", http.StatusBadRequest)
		return
	}

	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		internalError(w, err)
		return
	}
	var ids []int
	for rows.Next() {
		var id int
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()

	go func(serverIDs []int) {
		for _, id := range serverIDs {
			if _, err := h.runSync(context.Background(), id); err != nil {
				log.Printf("sync server %d: %v", id, err)
			}
		}
	}(ids)

	jsonOK(w, map[string]any{"status": "started", "server_ids": ids})
}

// RunScheduled is called by the scheduler â€” not exposed via HTTP.
func (h *SyncHandler) RunScheduled() {
	started := time.Now()
	log.Println("scheduler: sync run started")

	rows, err := h.db.Query(context.Background(), `SELECT id FROM servers`)
	if err != nil {
		log.Printf("scheduler: list servers: %v", err)
		return
	}
	var ids []int
	for rows.Next() {
		var id int
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()

	log.Printf("scheduler: syncing %d server(s)", len(ids))
	for _, id := range ids {
		if _, err := h.runSync(context.Background(), id); err != nil {
			log.Printf("scheduler: sync server %d: %v", id, err)
		}
	}
	log.Printf("scheduler: sync run finished in %s", time.Since(started).Round(time.Second))

	// Trigger remediation suggestion generation after sync.
	if h.postSync != nil {
		h.postSync(context.Background())
	}
}

func (h *SyncHandler) runSync(ctx context.Context, serverID int) (int, error) {
	s, err := h.loadSyncServer(ctx, serverID, true)
	if err != nil {
		return 0, fmt.Errorf("load server: %w", err)
	}

	// Create sync job record
	var jobID int
	h.db.QueryRow(ctx, `INSERT INTO sync_jobs (server_id) VALUES ($1) RETURNING id`, serverID).Scan(&jobID)

	since := time.Now().Add(-12 * time.Hour)
	if s.lastSync != nil {
		since = *s.lastSync
	}

	total, syncErr := h.collectAndStore(ctx, serverID, s, since, false)
	if syncErr != nil {
		h.finishJob(ctx, jobID, total, syncErr.Error())
		return jobID, nil
	}

	now := time.Now()

	// Auto-clean: remove boot-history entries that were incorrectly parsed as restart events.
	// These come from `last reboot` output in system_info â€” real timestamps are wrong (sync time, not boot time).
	_, cleanErr := h.db.Exec(ctx, `
		DELETE FROM log_events
		WHERE server_id=$1
		  AND source='system_info'
		  AND event_type='power_off'
		  AND (
			  message LIKE '%system boot%'
			OR message = '=last_reboot='
			OR message LIKE 'reboot %'
			OR message LIKE '%=uptime=%'
			OR message LIKE '%=df=%'
			OR message LIKE '%=free=%'
			OR message LIKE '%=services_failed=%'
			OR message LIKE '%=coredumps=%'
			OR (message LIKE '%Failed to make thread%' AND message LIKE '%realtime scheduled%')
			OR message LIKE '%RealtimeKit1%'
		  )`, serverID)
	if cleanErr != nil {
		log.Printf("cleanup server %d system_info noise: %v", serverID, cleanErr)
	}

	h.db.Exec(ctx, `UPDATE servers SET last_sync_at=$1 WHERE id=$2`, now, serverID)
	h.finishJob(ctx, jobID, total, "")
	return jobID, nil
}

func (h *SyncHandler) finishJob(ctx context.Context, jobID, count int, errMsg string) {
	status := "success"
	if errMsg != "" {
		status = "failed"
	}
	h.db.Exec(ctx, `
		UPDATE sync_jobs SET finished_at=NOW(), status=$1, event_count=$2, error=$3 WHERE id=$4`,
		status, count, errMsg, jobID)
}

// DeepSync triggers a sync from a specific date (for historical data recovery).
func (h *SyncHandler) DeepSync(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		jsonError(w, "invalid id", http.StatusBadRequest)
		return
	}
	sinceStr := r.URL.Query().Get("since")       // e.g. "2026-06-06T00:00:00Z"
	since := time.Now().Add(-7 * 24 * time.Hour) // default 7 days
	if sinceStr != "" {
		if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			since = t
		} else if t, err := time.Parse("2006-01-02", sinceStr); err == nil {
			since = t
		}
	}
	jobID, err := h.runSyncFrom(context.Background(), id, since)
	if err != nil {
		internalError(w, err)
		return
	}
	jsonOK(w, map[string]any{"job_id": jobID, "since": since.Format(time.RFC3339)})
}

func (h *SyncHandler) runSyncFrom(ctx context.Context, serverID int, since time.Time) (int, error) {
	var jobID int
	h.db.QueryRow(ctx, `INSERT INTO sync_jobs (server_id) VALUES ($1) RETURNING id`, serverID).Scan(&jobID)

	s, err := h.loadSyncServer(ctx, serverID, false)
	if err != nil {
		h.finishJob(ctx, jobID, 0, "server not found")
		return jobID, nil
	}

	total, syncErr := h.collectAndStore(ctx, serverID, s, since, true)
	if syncErr != nil {
		h.finishJob(ctx, jobID, total, syncErr.Error())
		return jobID, nil
	}
	// Run cleanup
	h.db.Exec(ctx, `DELETE FROM log_events WHERE server_id=$1 AND source='system_info' AND event_type='power_off' AND (message LIKE '%system boot%' OR message='=last_reboot=' OR message LIKE 'reboot %')`, serverID)
	h.db.Exec(ctx, `UPDATE servers SET last_sync_at=NOW() WHERE id=$1`, serverID)
	h.finishJob(ctx, jobID, total, "")
	return jobID, nil
}

func (h *SyncHandler) loadSyncServer(ctx context.Context, serverID int, includeLastSync bool) (syncServerRow, error) {
	var s syncServerRow
	err := h.db.QueryRow(ctx, `
		SELECT host, port, username, auth_type, COALESCE(password_enc,''), COALESCE(private_key_enc,''),
		       COALESCE(proxmox_host,''), proxmox_port, COALESCE(proxmox_username,''), proxmox_auth_type,
		       COALESCE(proxmox_password_enc,''), COALESCE(proxmox_private_key_enc,''),
		       COALESCE(vmid,''), COALESCE(app_log_paths,''), last_sync_at
		FROM servers WHERE id=$1`, serverID).
		Scan(&s.host, &s.port, &s.username, &s.authType, &s.passwordEnc, &s.keyEnc,
			&s.proxmoxHost, &s.proxmoxPort, &s.proxmoxUsername, &s.proxmoxAuthType,
			&s.proxmoxPasswordEnc, &s.proxmoxKeyEnc, &s.vmid, &s.appLogPaths, &s.lastSync)
	if err != nil {
		return s, err
	}
	if !includeLastSync {
		s.lastSync = nil
	}
	if s.proxmoxPort == 0 {
		s.proxmoxPort = 22
	}
	if s.proxmoxAuthType == "" {
		s.proxmoxAuthType = "password"
	}
	return s, nil
}

func (h *SyncHandler) collectAndStore(ctx context.Context, serverID int, s syncServerRow, since time.Time, includeProxmox bool) (int, error) {
	password, privateKey := h.decryptPair(s.passwordEnc, s.keyEnc)

	client, err := sshclient.Connect(sshclient.Config{
		Host:       s.host,
		Port:       s.port,
		Username:   s.username,
		AuthType:   s.authType,
		Password:   password,
		PrivateKey: privateKey,
	})
	if err != nil {
		h.db.Exec(ctx, `UPDATE servers SET status='error' WHERE id=$1`, serverID)
		return 0, err
	}
	defer client.Close()

	h.db.Exec(ctx, `UPDATE servers SET status='online' WHERE id=$1`, serverID)

	logMap, err := client.FetchLogs(since, s.appLogPaths)
	if err != nil {
		return 0, err
	}

	if includeProxmox {
		for _, prox := range h.proxmoxTargets(ctx, serverID, s) {
			proxPass, proxKey := h.decryptPair(prox.proxmoxPasswordEnc, prox.proxmoxKeyEnc)
			proxClient, err := sshclient.Connect(sshclient.Config{
				Host:       prox.proxmoxHost,
				Port:       prox.proxmoxPort,
				Username:   prox.proxmoxUsername,
				AuthType:   prox.proxmoxAuthType,
				Password:   proxPass,
				PrivateKey: proxKey,
			})
			if err == nil {
				for source, output := range proxClient.FetchProxmoxLogs(since, prox.vmid) {
					key := source
					if prox.proxmoxHost != "" {
						key = source + "@" + prox.proxmoxHost
					}
					logMap[key] = output
				}
				proxClient.Close()
			} else {
				logMap["proxmox_connection@"+prox.proxmoxHost] = fmt.Sprintf("%s proxmox ssh %s: %v", time.Now().UTC().Format(time.RFC3339), prox.proxmoxHost, err)
			}
		}
	}

	total := 0
	for source, output := range logMap {
		events := parser.ParseOutput(output, source, serverID)
		for _, ev := range events {
			h.db.Exec(ctx, `
				INSERT INTO log_events (server_id, timestamp, event_type, severity, message, source, raw_line)
				VALUES ($1,$2,$3,$4,$5,$6,$7)
				ON CONFLICT DO NOTHING`,
				ev.ServerID, ev.Timestamp, ev.EventType, ev.Severity, ev.Message, ev.Source, ev.RawLine)
			total++
		}
	}

	// Authoritative per-robot status from RDS Core (t_robotstatusrecord). The
	// 'rds_robot_status' source is TSV, not log lines â€” parse + upsert into its
	// own table. This is the source of truth for fleet status/timeline/odometer.
	if out, ok := logMap["rds_robot_status"]; ok && strings.TrimSpace(out) != "" {
		plant := config.PlantForServer("", s.host)
		total += h.ingestRobotStatus(ctx, serverID, plant, out)
	}

	return total, nil
}

// ingestRobotStatus parses the TSV output of the rds_robot_status SSH command
// (one row per status transition) and upserts into robot_status_records. Returns
// the number of rows stored. Malformed rows are skipped. Dedup is via the unique
// index on (server_id, uuid, started_on), so re-syncs are idempotent.
func (h *SyncHandler) ingestRobotStatus(ctx context.Context, serverID int, plant, output string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "uuid") {
			continue // skip header/blank
		}
		f := strings.Split(line, "\t")
		if len(f) < 9 {
			continue
		}
		uuid := strings.TrimSpace(f[0])
		vehicle := strings.TrimSpace(f[1])
		if uuid == "" {
			continue
		}
		newSt, _ := strconv.Atoi(strings.TrimSpace(f[2]))
		oldSt, _ := strconv.Atoi(strings.TrimSpace(f[3]))
		startedOn, err := parseFlexibleTimeSync(strings.TrimSpace(f[4]))
		if err != nil {
			continue
		}
		endedOn, err := parseFlexibleTimeSync(strings.TrimSpace(f[5]))
		if err != nil {
			endedOn = startedOn
		}
		duration, _ := strconv.ParseInt(strings.TrimSpace(f[6]), 10, 64)
		odo, _ := strconv.ParseFloat(strings.TrimSpace(f[7]), 64)
		todayOdo, _ := strconv.ParseFloat(strings.TrimSpace(f[8]), 64)

		_, err = h.db.Exec(ctx, `
			INSERT INTO robot_status_records
				(server_id, plant, uuid, vehicle_name, new_status, old_status,
				 started_on, ended_on, duration_ms, odo, today_odo)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (server_id, uuid, started_on) DO UPDATE SET
				new_status=EXCLUDED.new_status, old_status=EXCLUDED.old_status,
				ended_on=EXCLUDED.ended_on, duration_ms=EXCLUDED.duration_ms,
				odo=EXCLUDED.odo, today_odo=EXCLUDED.today_odo`,
			serverID, plant, uuid, vehicle, newSt, oldSt,
			startedOn, endedOn, duration, odo, todayOdo)
		if err == nil {
			count++
		}
	}
	return count
}

// rdsControllerLocation is the timezone used by RDS Core's MySQL DATETIME
// values. The database returns bare local timestamps without an offset; treating
// them as UTC makes AMR events appear several hours in the future.
var rdsControllerLocation = time.FixedZone("RDS Controller", 8*60*60)

// parseFlexibleTimeSync accepts "2006-01-02 15:04:05" (MySQL DATETIME) or
// RFC3339. Bare MySQL DATETIME values are interpreted in the RDS controller's
// UTC+8 timezone and converted to UTC for storage.
func parseFlexibleTimeSync(s string) (time.Time, error) {
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, rdsControllerLocation); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unparseable time: %q", s)
}

func (h *SyncHandler) proxmoxTargets(ctx context.Context, selectedServerID int, selected syncServerRow) []syncServerRow {
	if selected.proxmoxHost != "" && selected.proxmoxUsername != "" {
		return []syncServerRow{selected}
	}

	rows, err := h.db.Query(ctx, `
		SELECT host, port, username, auth_type, COALESCE(password_enc,''), COALESCE(private_key_enc,'')
		FROM servers
		WHERE id <> $1
		  AND (
			LOWER(name) LIKE '%pve%'
			OR LOWER(name) LIKE '%proxmox%'
			OR LOWER(host) LIKE '%pve%'
		  )
		ORDER BY name`, selectedServerID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var targets []syncServerRow
	for rows.Next() {
		var t syncServerRow
		if err := rows.Scan(&t.proxmoxHost, &t.proxmoxPort, &t.proxmoxUsername, &t.proxmoxAuthType, &t.proxmoxPasswordEnc, &t.proxmoxKeyEnc); err != nil {
			continue
		}
		if t.proxmoxPort == 0 {
			t.proxmoxPort = 22
		}
		if t.proxmoxAuthType == "" {
			t.proxmoxAuthType = "password"
		}
		t.vmid = selected.vmid
		targets = append(targets, t)
	}
	return targets
}

func (h *SyncHandler) decryptPair(passwordEnc, keyEnc string) (string, string) {
	var password, privateKey string
	if passwordEnc != "" {
		password, _ = decrypt(h.encryptionKey, passwordEnc)
	}
	if keyEnc != "" {
		privateKey, _ = decrypt(h.encryptionKey, keyEnc)
	}
	return password, privateKey
}

// TestConnection verifies SSH credentials without storing.
func (h *SyncHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	var req models.ServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Port == 0 {
		req.Port = 22
	}

	client, err := sshclient.Connect(sshclient.Config{
		Host:       req.Host,
		Port:       req.Port,
		Username:   req.Username,
		AuthType:   req.AuthType,
		Password:   req.Password,
		PrivateKey: req.PrivateKey,
	})
	if err != nil {
		jsonOK(w, map[string]any{"success": false, "error": err.Error()})
		return
	}
	defer client.Close()

	out, _ := client.Run("uname -a")
	jsonOK(w, map[string]any{"success": true, "info": out})
}
