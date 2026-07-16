package handlers

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"drishti-amr-health/internal/models"
)

type LogHandler struct {
	db *pgxpool.Pool
}

func NewLogHandler(db *pgxpool.Pool) *LogHandler {
	return &LogHandler{db: db}
}

func (h *LogHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 200
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 && l <= 1000 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(q.Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	where := "WHERE 1=1"
	args := []any{}
	argN := 1

	if sID := q.Get("server_id"); sID != "" {
		where += " AND le.server_id=$" + strconv.Itoa(argN)
		args = append(args, sID)
		argN++
	}
	if et := q.Get("event_type"); et != "" {
		where += " AND le.event_type=$" + strconv.Itoa(argN)
		args = append(args, et)
		argN++
	}
	if eventTypes := q.Get("event_types"); eventTypes != "" {
		var placeholders []string
		for _, et := range strings.Split(eventTypes, ",") {
			et = strings.TrimSpace(et)
			if et == "" {
				continue
			}
			placeholders = append(placeholders, "$"+strconv.Itoa(argN))
			args = append(args, et)
			argN++
		}
		if len(placeholders) > 0 {
			where += " AND le.event_type IN (" + strings.Join(placeholders, ",") + ")"
		}
	}
	if sev := q.Get("severity"); sev != "" {
		where += " AND le.severity=$" + strconv.Itoa(argN)
		args = append(args, sev)
		argN++
	}
	if source := q.Get("source"); source != "" {
		where += " AND le.source=$" + strconv.Itoa(argN)
		args = append(args, source)
		argN++
	}
	if host := q.Get("proxmox_host"); host != "" {
		where += " AND s.proxmox_host=$" + strconv.Itoa(argN)
		args = append(args, host)
		argN++
	}
	if vmid := q.Get("vmid"); vmid != "" {
		where += " AND (s.vmid=$" + strconv.Itoa(argN) +
			" OR (',' || regexp_replace(s.vmid, '\\s+', '', 'g') || ',') LIKE $" + strconv.Itoa(argN+1) +
			" OR le.message ILIKE $" + strconv.Itoa(argN+2) +
			" OR le.message ILIKE $" + strconv.Itoa(argN+3) +
			" OR le.message ILIKE $" + strconv.Itoa(argN+4) + ")"
		args = append(args, vmid, "%,"+vmid+",%", "%VMID="+vmid+"%", "%/"+vmid+".scope%", "% "+vmid+" %")
		argN++
		argN += 4
	}
	if search := q.Get("q"); search != "" {
		terms := strings.Fields(search)
		if len(terms) == 0 {
			terms = []string{search}
		}
		var termClauses []string
		for _, term := range terms {
			termClauses = append(termClauses, "(le.message ILIKE $"+strconv.Itoa(argN)+" OR COALESCE(le.raw_line,'') ILIKE $"+strconv.Itoa(argN)+" OR le.source ILIKE $"+strconv.Itoa(argN)+" OR s.name ILIKE $"+strconv.Itoa(argN)+")")
			args = append(args, "%"+term+"%")
			argN++
		}
		where += " AND (" + strings.Join(termClauses, " OR ") + ")"
	}
	if from := q.Get("from"); from != "" {
		where += " AND le.timestamp >= $" + strconv.Itoa(argN)
		args = append(args, from)
		argN++
	}
	if to := q.Get("to"); to != "" {
		where += " AND le.timestamp <= $" + strconv.Itoa(argN)
		args = append(args, to)
		argN++
	}

	args = append(args, limit, offset)

	rows, err := h.db.Query(r.Context(), `
		SELECT le.id, le.server_id, s.name, le.timestamp, le.event_type,
		       le.severity, le.message, le.source, COALESCE(le.raw_line,''), le.created_at
		FROM log_events le
		JOIN servers s ON s.id = le.server_id
		`+where+`
		ORDER BY le.timestamp DESC
		LIMIT $`+strconv.Itoa(argN)+` OFFSET $`+strconv.Itoa(argN+1), args...)
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()

	var events []models.LogEvent
	for rows.Next() {
		var e models.LogEvent
		if err := rows.Scan(&e.ID, &e.ServerID, &e.ServerName, &e.Timestamp,
			&e.EventType, &e.Severity, &e.Message, &e.Source, &e.RawLine, &e.CreatedAt); err != nil {
			continue
		}
		if shouldAnalyzeOOMRow(e) {
			e.OOMAnalysis = h.analyzeLogEventOOM(r.Context(), e)
		}
		enrichLogEvent(&e)
		events = append(events, e)
	}
	if events == nil {
		events = []models.LogEvent{}
	}
	jsonOK(w, events)
}

func shouldAnalyzeOOMRow(ev models.LogEvent) bool {
	if ev.EventType == "admin_evidence_search" || ev.EventType == "template_code_reference" || ev.EventType == "not_execution_evidence" {
		return false
	}
	raw := strings.ToLower(ev.RawLine + " " + ev.Message)
	if strings.Contains(raw, "pveproxy/access.log") || strings.Contains(raw, "/api2/") {
		return false
	}
	if isAdminEvidenceOnly(raw) || isTemplateOrCodeOnly(raw) {
		return false
	}
	msg := strings.ToLower(ev.Message)
	return ev.EventType == "vm_killed_by_oom" ||
		ev.EventType == "host_memory_exhaustion" ||
		strings.Contains(msg, "out of memory") ||
		strings.Contains(msg, "oom-kill") ||
		strings.Contains(msg, "killed process")
}

func (h *LogHandler) analyzeLogEventOOM(ctx context.Context, ev models.LogEvent) *models.OOMAnalysis {
	hostHints := h.oomHostHints(ctx, ev)
	evidence := []models.IncidentEvidence{{
		Timestamp: ev.Timestamp,
		EventType: ev.EventType,
		Severity:  ev.Severity,
		Source:    ev.Source,
		Message:   ev.Message,
	}}

	sourceClauses, sourceArgs, nextArg := sourceLikeClause("source", sourceLikePatterns(hostHints), 4)
	windowArgs := []any{ev.Timestamp.Add(-2 * time.Second), ev.Timestamp.Add(2 * time.Second), ev.ServerID}
	windowArgs = append(windowArgs, sourceArgs...)

	windowRows, err := h.db.Query(ctx, `
		SELECT timestamp, event_type, severity, source, message
		FROM log_events
		WHERE timestamp >= $1
		  AND timestamp <= $2
		  AND (
			server_id=$3
			OR `+sourceClauses+`
		  )
		  AND (
			event_type IN ('vm_killed_by_oom','host_memory_exhaustion','swap_full','vm_stopped','vm_started')
			OR message ILIKE '%qemu.slice%'
			OR message ILIKE '%killed process%'
			OR message ILIKE '%oom%'
		  )
		ORDER BY timestamp ASC
		LIMIT $`+strconv.Itoa(nextArg), append(windowArgs, 80)...)
	if err == nil {
		defer windowRows.Close()
		for windowRows.Next() {
			var item models.IncidentEvidence
			if scanErr := windowRows.Scan(&item.Timestamp, &item.EventType, &item.Severity, &item.Source, &item.Message); scanErr == nil {
				evidence = append(evidence, item)
			}
		}
	}

	prelim := analyzeOOM(evidence)
	var killedPID, killedVMID string
	if prelim != nil {
		killedPID = prelim.KilledPID
		killedVMID = prelim.KilledVMID
	}
	if killedPID == "" {
		if m := killedProcessRe.FindStringSubmatch(ev.Message); len(m) == 3 {
			killedPID = m[1]
		}
	}
	if killedVMID == "" {
		if m := qemuScopeRe.FindStringSubmatch(ev.Message); len(m) == 2 {
			killedVMID = m[1]
		}
	}

	if killedPID != "" || killedVMID != "" {
		memoryEvidence := h.oomMemoryEvidence(ctx, hostHints, killedPID, killedVMID)
		evidence = append(evidence, memoryEvidence...)
	}

	return analyzeOOM(evidence)
}

func (h *LogHandler) oomHostHints(ctx context.Context, ev models.LogEvent) []string {
	var hosts []string
	if host := hostFromSource(ev.Source); host != "" {
		hosts = append(hosts, host)
	}

	var serverHost, proxmoxHost string
	_ = h.db.QueryRow(ctx, `SELECT COALESCE(host,''), COALESCE(proxmox_host,'') FROM servers WHERE id=$1`, ev.ServerID).Scan(&serverHost, &proxmoxHost)
	for _, host := range []string{serverHost, proxmoxHost} {
		if host != "" {
			hosts = append(hosts, host)
		}
	}
	return uniqueStrings(hosts)
}

func (h *LogHandler) oomMemoryEvidence(ctx context.Context, hostHints []string, killedPID, killedVMID string) []models.IncidentEvidence {
	var clauses []string
	var args []any
	argN := 1

	sourceClause, sourceArgs, nextArg := sourceLikeClause("source", sourceLikePatterns(hostHints), argN)
	clauses = append(clauses, sourceClause)
	args = append(args, sourceArgs...)
	argN = nextArg

	var msgClauses []string
	if killedPID != "" {
		msgClauses = append(msgClauses, "message LIKE $"+strconv.Itoa(argN))
		args = append(args, "%PID="+killedPID+"%")
		argN++
		msgClauses = append(msgClauses, "message LIKE $"+strconv.Itoa(argN))
		args = append(args, "% "+killedPID+" %")
		argN++
	}
	if killedVMID != "" {
		msgClauses = append(msgClauses, "message LIKE $"+strconv.Itoa(argN))
		args = append(args, "VMID="+killedVMID+" %")
		argN++
		msgClauses = append(msgClauses, "message LIKE $"+strconv.Itoa(argN))
		args = append(args, "%-id "+killedVMID+" %")
		argN++
	}
	if len(msgClauses) == 0 {
		return nil
	}
	clauses = append(clauses, "("+strings.Join(msgClauses, " OR ")+")")

	query := `
		SELECT timestamp, event_type, severity, source, message
		FROM log_events
		WHERE ` + strings.Join(clauses, " AND ") + `
		  AND (
			source LIKE 'proxmox_host_memory%'
			OR source LIKE 'proxmox_vm_status%'
			OR message LIKE 'VMID=%'
			OR message ILIKE '%/usr/bin/kvm%'
		  )
		ORDER BY timestamp DESC
		LIMIT 30`
	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var evidence []models.IncidentEvidence
	for rows.Next() {
		var item models.IncidentEvidence
		if err := rows.Scan(&item.Timestamp, &item.EventType, &item.Severity, &item.Source, &item.Message); err == nil {
			evidence = append(evidence, item)
		}
	}
	return evidence
}

func sourceLikePatterns(hosts []string) []string {
	if len(hosts) == 0 {
		return []string{"proxmox_%"}
	}
	patterns := make([]string, 0, len(hosts))
	for _, host := range hosts {
		patterns = append(patterns, "%@"+host)
	}
	return patterns
}

func sourceLikeClause(column string, patterns []string, startArg int) (string, []any, int) {
	var clauses []string
	args := make([]any, 0, len(patterns))
	argN := startArg
	for _, pattern := range patterns {
		clauses = append(clauses, column+" LIKE $"+strconv.Itoa(argN))
		args = append(args, pattern)
		argN++
	}
	if len(clauses) == 0 {
		return "FALSE", args, argN
	}
	return "(" + strings.Join(clauses, " OR ") + ")", args, argN
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func (h *LogHandler) Stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var stats models.DashboardStats

	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM servers`).Scan(&stats.TotalServers)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM servers WHERE status='online'`).Scan(&stats.OnlineServers)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM log_events`).Scan(&stats.TotalEvents)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM log_events WHERE severity IN ('critical','high')`).Scan(&stats.CriticalEvents)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM log_events WHERE event_type='crash'`).Scan(&stats.CrashCount)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM log_events WHERE event_type IN ('power_off','ubuntu_server_shutdown','ubuntu_server_reboot','proxmox_host_shutdown','proxmox_host_reboot','vm_stopped','vm_reboot','power_network_event')`).Scan(&stats.PowerOffCount)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM log_events WHERE event_type='error'`).Scan(&stats.ErrorCount)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM log_events WHERE event_type='robot_offline'`).Scan(&stats.RobotOfflineCount)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM log_events WHERE event_type='robot_online'`).Scan(&stats.RobotOnlineCount)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM log_events WHERE event_type='disk_error'`).Scan(&stats.DiskErrorCount)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM log_events WHERE event_type IN ('ubuntu_server_shutdown','ubuntu_server_reboot','ubuntu_log_gap','service_failure','ssh_login_activity')`).Scan(&stats.UbuntuEventCount)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM log_events WHERE event_type IN ('proxmox_host_shutdown','proxmox_host_reboot','ha_action')`).Scan(&stats.ProxmoxEventCount)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM log_events WHERE event_type IN ('vm_stopped','vm_started','vm_reboot','vm_killed_by_oom')`).Scan(&stats.VMEventCount)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM log_events WHERE event_type IN ('vm_killed_by_oom','host_memory_exhaustion','swap_full')`).Scan(&stats.MemoryEventCount)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM log_events WHERE event_type IN ('backup_job','backup_found_vm_stopped')`).Scan(&stats.BackupEventCount)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM log_events WHERE event_type='rds_core_issue'`).Scan(&stats.RDSCoreIssueCount)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM log_events WHERE event_type='rds_map_update'`).Scan(&stats.RDSMapUpdateCount)
	h.db.QueryRow(ctx, `SELECT COUNT(*) FROM log_events WHERE event_type='warlink_failure'`).Scan(&stats.WarLinkIssueCount)

	jsonOK(w, stats)
}

func (h *LogHandler) IncidentSummary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	serverID, err := strconv.Atoi(q.Get("server_id"))
	if err != nil || serverID <= 0 {
		jsonError(w, "server_id is required", http.StatusBadRequest)
		return
	}

	to := time.Now().UTC()
	from := to.Add(-24 * time.Hour)
	if raw := q.Get("from"); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			from = t
		}
	}
	if raw := q.Get("to"); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			to = t
		}
	}

	var summary models.IncidentSummary
	summary.ServerID = serverID
	summary.From = from
	summary.To = to
	if err := h.db.QueryRow(r.Context(), `
		SELECT name, COALESCE(proxmox_host,''), COALESCE(vmid,'')
		FROM servers WHERE id=$1`, serverID).Scan(&summary.ServerName, &summary.ProxmoxHost, &summary.VMID); err != nil {
		jsonError(w, "server not found", http.StatusNotFound)
		return
	}

	rows, err := h.db.Query(r.Context(), `
		SELECT timestamp, event_type, severity, source, message
		FROM log_events
		WHERE server_id=$1
		  AND timestamp >= $2
		  AND timestamp <= $3
		  AND (
			event_type <> 'unknown'
			OR source LIKE 'proxmox_host_memory%'
			OR message LIKE 'VMID=%'
		  )
		ORDER BY timestamp ASC
		LIMIT 1000`, serverID, from, to)
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()

	counts := map[string]int{}
	var first, recovered *time.Time
	var allEvents []models.IncidentEvidence
	for rows.Next() {
		var ev models.IncidentEvidence
		if err := rows.Scan(&ev.Timestamp, &ev.EventType, &ev.Severity, &ev.Source, &ev.Message); err != nil {
			continue
		}
		allEvents = append(allEvents, ev)
		counts[ev.EventType]++
		if isIncidentSignal(ev.EventType) && first == nil {
			t := ev.Timestamp
			first = &t
		}
		if ev.EventType == "robot_online" || ev.EventType == "vm_started" {
			t := ev.Timestamp
			recovered = &t
		}
	}
	evidence := selectIncidentEvidence(allEvents)
	if evidence == nil {
		evidence = []models.IncidentEvidence{}
	}

	summary.StartedAt = first
	summary.RecoveredAt = recovered
	summary.Evidence = evidence
	summary.OOMAnalysis = analyzeOOM(allEvents)
	summary.WhatHappened, summary.RootCause, summary.RecommendedFix = correlateIncident(counts, summary)
	jsonOK(w, summary)
}

func isIncidentSignal(eventType string) bool {
	switch eventType {
	case "unknown", "ssh_login_activity", "robot_online", "warning", "update":
		return false
	default:
		return true
	}
}

func selectIncidentEvidence(events []models.IncidentEvidence) []models.IncidentEvidence {
	var selected []models.IncidentEvidence
	add := func(match func(models.IncidentEvidence) bool) {
		for _, ev := range events {
			if len(selected) >= 12 {
				return
			}
			if !match(ev) || containsEvidence(selected, ev) {
				continue
			}
			if len(ev.Message) > 220 {
				ev.Message = ev.Message[:220]
			}
			selected = append(selected, ev)
		}
	}

	add(func(ev models.IncidentEvidence) bool {
		return ev.EventType == "vm_killed_by_oom" || ev.EventType == "host_memory_exhaustion" || ev.EventType == "swap_full"
	})
	add(func(ev models.IncidentEvidence) bool {
		return ev.EventType == "vm_stopped" || ev.EventType == "vm_started" || ev.EventType == "proxmox_host_reboot" || ev.EventType == "proxmox_host_shutdown"
	})
	add(func(ev models.IncidentEvidence) bool {
		return isIncidentSignal(ev.EventType)
	})
	return selected
}

func containsEvidence(events []models.IncidentEvidence, candidate models.IncidentEvidence) bool {
	for _, ev := range events {
		if ev.Timestamp.Equal(candidate.Timestamp) && ev.EventType == candidate.EventType && ev.Source == candidate.Source && ev.Message == candidate.Message {
			return true
		}
	}
	return false
}

type vmMemorySample struct {
	vmid     string
	name     string
	pid      string
	rssGB    float64
	configMB int
	host     string
}

var (
	qemuScopeRe         = regexp.MustCompile(`(?:/qemu\.slice/|[^0-9])([0-9]+)\.scope`)
	killedProcessRe     = regexp.MustCompile(`Killed process ([0-9]+) \(([^)]+)\)`)
	kernelMemoryRe      = regexp.MustCompile(`total-vm:([0-9]+)kB.*anon-rss:([0-9]+)kB`)
	vmRSSRe             = regexp.MustCompile(`VMID=([^\s]+)\s+NAME=([^\s]+)\s+PID=([^\s]+)\s+RSS_GB=([0-9.]+)\s+CONFIG_MB=([0-9]+)`)
	proxmoxHostSuffixRe = regexp.MustCompile(`@(.+)$`)
)

func analyzeOOM(events []models.IncidentEvidence) *models.OOMAnalysis {
	var analysis models.OOMAnalysis
	var top *vmMemorySample

	for _, ev := range events {
		msg := ev.Message
		lower := strings.ToLower(msg)
		if strings.HasPrefix(ev.Source, "proxmox") && analysis.ProxmoxHost == "" {
			analysis.ProxmoxHost = hostFromSource(ev.Source)
		}

		if strings.Contains(lower, "oom") || strings.Contains(lower, "out of memory") || strings.Contains(lower, "killed process") {
			if m := qemuScopeRe.FindStringSubmatch(msg); len(m) == 2 && analysis.KilledVMID == "" {
				analysis.KilledVMID = m[1]
				if host := hostFromSource(ev.Source); host != "" {
					analysis.ProxmoxHost = host
				}
			}
			if m := killedProcessRe.FindStringSubmatch(msg); len(m) == 3 {
				analysis.KilledPID = m[1]
				analysis.KilledProcess = m[2]
			}
			if m := kernelMemoryRe.FindStringSubmatch(msg); len(m) == 3 {
				if totalKB, err := strconv.ParseInt(m[1], 10, 64); err == nil {
					analysis.KilledTotalGB = kbToGB(totalKB)
				}
				if anonKB, err := strconv.ParseInt(m[2], 10, 64); err == nil {
					analysis.KilledAnonGB = kbToGB(anonKB)
				}
			}
		}

		if m := vmRSSRe.FindStringSubmatch(msg); len(m) == 6 {
			rss, _ := strconv.ParseFloat(m[4], 64)
			configMB, _ := strconv.Atoi(m[5])
			sample := vmMemorySample{
				vmid:     m[1],
				name:     m[2],
				pid:      m[3],
				rssGB:    rss,
				configMB: configMB,
				host:     hostFromSource(ev.Source),
			}
			if top == nil || sample.rssGB > top.rssGB {
				candidate := sample
				top = &candidate
			}
			if analysis.KilledVMID != "" && sample.vmid == analysis.KilledVMID {
				analysis.KilledVMName = sample.name
			}
		}
	}

	if top != nil {
		analysis.TopVMID = top.vmid
		analysis.TopVMName = top.name
		analysis.TopPID = top.pid
		analysis.TopRSSGB = top.rssGB
		analysis.TopConfigMB = top.configMB
		if analysis.ProxmoxHost == "" {
			analysis.ProxmoxHost = top.host
		}
		if analysis.KilledVMID == top.vmid && analysis.KilledVMName == "" {
			analysis.KilledVMName = top.name
		}
	}

	if analysis.KilledVMID == "" && analysis.TopVMID == "" && analysis.KilledPID == "" {
		return nil
	}

	switch {
	case analysis.KilledVMID != "" && analysis.TopVMID != "" && analysis.KilledVMID == analysis.TopVMID:
		analysis.Confidence = "high"
		analysis.Explanation = "The VM killed by the OOM event was also the highest-memory VM in the Proxmox memory snapshot."
	case analysis.KilledVMID != "":
		analysis.Confidence = "high"
		analysis.Explanation = "The Proxmox OOM log identifies the killed QEMU scope, which maps to VM " + analysis.KilledVMID + "."
	case analysis.TopVMID != "":
		analysis.Confidence = "medium"
		analysis.Explanation = "No killed VM scope was found, but the Proxmox memory snapshot shows the highest-memory VM."
	default:
		analysis.Confidence = "medium"
		analysis.Explanation = "The OOM log identifies the killed process, but no VMID memory snapshot was available."
	}

	analysis.Recommendation = "Review this VM's configured RAM, ballooning, workload memory use, and whether the Proxmox host has enough free RAM/swap before restarting or migrating workloads."
	return &analysis
}

func hostFromSource(source string) string {
	if m := proxmoxHostSuffixRe.FindStringSubmatch(source); len(m) == 2 {
		return m[1]
	}
	return ""
}

func kbToGB(kb int64) float64 {
	gb := float64(kb) / 1024 / 1024
	return float64(int(gb*100+0.5)) / 100
}

func correlateIncident(counts map[string]int, s models.IncidentSummary) (string, string, string) {
	has := func(types ...string) bool {
		for _, t := range types {
			if counts[t] > 0 {
				return true
			}
		}
		return false
	}
	label := s.ServerName
	if s.VMID != "" {
		label += " VM " + s.VMID
	}

	switch {
	case has("vm_killed_by_oom") && has("host_memory_exhaustion"):
		if s.OOMAnalysis != nil && s.OOMAnalysis.KilledVMID != "" {
			vmLabel := "VM " + s.OOMAnalysis.KilledVMID
			if s.OOMAnalysis.KilledVMName != "" {
				vmLabel += " (" + s.OOMAnalysis.KilledVMName + ")"
			}
			return label + " stopped during a Proxmox host OOM event that killed " + vmLabel + ".", vmLabel + " was killed by the Proxmox OOM killer during host memory exhaustion.", "Reduce memory pressure on " + vmLabel + ", review RAM reservation/ballooning, and add or free host RAM before restarting the VM."
		}
		if s.OOMAnalysis != nil && s.OOMAnalysis.TopVMID != "" {
			vmLabel := "VM " + s.OOMAnalysis.TopVMID
			if s.OOMAnalysis.TopVMName != "" {
				vmLabel += " (" + s.OOMAnalysis.TopVMName + ")"
			}
			return label + " stopped during a host memory pressure event.", vmLabel + " was the highest-memory VM in the Proxmox memory snapshot.", "Reduce memory pressure on " + vmLabel + ", review RAM reservation/ballooning, and add or free host RAM before restarting workloads."
		}
		return label + " stopped during a host memory pressure event.", "VM stopped due to Proxmox host memory exhaustion.", "Reduce host memory pressure, review VM reservations/ballooning, and consider moving workloads before restarting the VM."
	case has("backup_found_vm_stopped") && has("vm_stopped"):
		return label + " was already stopped when a backup job ran.", "VM was stopped before or during backup processing.", "Check Proxmox task history around the stop event, then verify backup scheduling and VM start policy."
	case has("ubuntu_log_gap") && has("proxmox_host_reboot", "proxmox_host_shutdown"):
		return label + " had an Ubuntu log gap during a Proxmox host event.", "Proxmox host shutdown or reboot likely interrupted the VM.", "Review host maintenance/power events and confirm the VM auto-start policy."
	case has("vm_stopped") && has("host_memory_exhaustion", "swap_full"):
		return label + " stopped while memory or swap was exhausted.", "VM outage likely caused by memory exhaustion.", "Free host memory, increase swap/RAM, and inspect high-memory processes."
	case has("robot_offline") && !has("vm_stopped", "ubuntu_server_reboot", "proxmox_host_reboot"):
		return label + " reported robot disconnects without matching host or VM failure.", "Robot connection or network issue.", "Check robot power, cabling/Wi-Fi, and FleetManager robot service connectivity."
	case has("crash", "error", "service_failure"):
		return label + " recorded application or service failures.", "FleetManager/application service failure.", "Restart failed services, inspect app logs, and verify dependencies such as database and storage."
	case has("disk_smart_issue", "disk_error"):
		return label + " recorded storage errors.", "Disk, filesystem, or SMART issue.", "Check disk health immediately, verify backups, and remediate failing storage."
	case has("network_dhcp_failure", "power_network_event"):
		return label + " recorded network or power events.", "Network, DHCP, link, or power interruption.", "Check switch port, DHCP lease history, UPS, and host NIC status."
	case has("ssh_login_activity") && !has("robot_offline", "vm_stopped", "crash"):
		return label + " had login activity but no clear outage signal.", "Administrative access observed; root cause not determined from available logs.", "Confirm whether an operator performed maintenance in this window."
	default:
		var names []string
		for t, c := range counts {
			if c > 0 && t != "unknown" {
				names = append(names, t)
			}
		}
		if len(names) == 0 {
			return "No categorized outage events were found in this window.", "Unknown.", "Expand the time range or run Deep Sync with Proxmox mapping configured."
		}
		return label + " had categorized events: " + strings.Join(names, ", ") + ".", "Unknown from available evidence.", "Review the evidence list and expand Deep Sync around the first event."
	}
}

func (h *LogHandler) Timeline(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(), `
		SELECT DATE_TRUNC('hour', timestamp) AS hour,
		       event_type,
		       COUNT(*) AS cnt
		FROM log_events
		WHERE timestamp >= NOW() - INTERVAL '7 days'
		GROUP BY 1, 2
		ORDER BY 1`)
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()

	type point struct {
		Hour      string `json:"hour"`
		EventType string `json:"event_type"`
		Count     int    `json:"count"`
	}
	var pts []point
	for rows.Next() {
		var p point
		rows.Scan(&p.Hour, &p.EventType, &p.Count)
		pts = append(pts, p)
	}
	if pts == nil {
		pts = []point{}
	}
	jsonOK(w, pts)
}

func (h *LogHandler) SyncHistory(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(), `
		SELECT sj.id, sj.server_id, s.name, sj.started_at, sj.finished_at, sj.status, sj.event_count, sj.error
		FROM sync_jobs sj
		JOIN servers s ON s.id = sj.server_id
		ORDER BY sj.started_at DESC
		LIMIT 50`)
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()

	var jobs []models.SyncJob
	for rows.Next() {
		var j models.SyncJob
		rows.Scan(&j.ID, &j.ServerID, &j.ServerName, &j.StartedAt,
			&j.FinishedAt, &j.Status, &j.EventCount, &j.Error)
		jobs = append(jobs, j)
	}
	if jobs == nil {
		jobs = []models.SyncJob{}
	}
	jsonOK(w, jobs)
}

// ServerStats returns per-server event breakdowns for the dashboard.
func (h *LogHandler) ServerStats(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(), `
		SELECT
			s.id,
			s.name,
			s.status,
			COALESCE(SUM(CASE WHEN le.event_type='robot_offline' THEN 1 END),0) AS robot_offline,
			COALESCE(SUM(CASE WHEN le.event_type='robot_online'  THEN 1 END),0) AS robot_online,
			COALESCE(SUM(CASE WHEN le.event_type='crash'         THEN 1 END),0) AS crashes,
			COALESCE(SUM(CASE WHEN le.event_type='disk_error'    THEN 1 END),0) AS disk_errors,
			COALESCE(SUM(CASE WHEN le.event_type='error'         THEN 1 END),0) AS errors,
			COALESCE(SUM(CASE WHEN le.event_type='warning'       THEN 1 END),0) AS warnings,
			COALESCE(SUM(CASE WHEN le.event_type='rds_core_issue' THEN 1 END),0) AS rds_core_issues,
			COALESCE(SUM(CASE WHEN le.event_type='warlink_failure' THEN 1 END),0) AS warlink_issues,
			COALESCE(SUM(CASE WHEN le.severity IN ('critical','high') THEN 1 END),0) AS critical
		FROM servers s
		LEFT JOIN log_events le ON le.server_id = s.id
		GROUP BY s.id, s.name, s.status
		ORDER BY s.name`)
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()

	type ServerStat struct {
		ID            int    `json:"id"`
		Name          string `json:"name"`
		Status        string `json:"status"`
		RobotOffline  int    `json:"robot_offline"`
		RobotOnline   int    `json:"robot_online"`
		Crashes       int    `json:"crashes"`
		DiskErrors    int    `json:"disk_errors"`
		Errors        int    `json:"errors"`
		Warnings      int    `json:"warnings"`
		RDSCoreIssues int    `json:"rds_core_issues"`
		WarLinkIssues int    `json:"warlink_issues"`
		Critical      int    `json:"critical"`
	}

	var results []ServerStat
	for rows.Next() {
		var s ServerStat
		rows.Scan(&s.ID, &s.Name, &s.Status, &s.RobotOffline, &s.RobotOnline,
			&s.Crashes, &s.DiskErrors, &s.Errors, &s.Warnings, &s.RDSCoreIssues, &s.WarLinkIssues, &s.Critical)
		results = append(results, s)
	}
	if results == nil {
		results = []ServerStat{}
	}
	jsonOK(w, results)
}
