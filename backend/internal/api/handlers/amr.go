package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"drishti-amr-health/internal/config"
	"drishti-amr-health/internal/models"
	"drishti-amr-health/internal/robowatch"
)

// AMRHandler serves fleet-level AMR health summaries.
type AMRHandler struct {
	db          *pgxpool.Pool
	ollamaURL   string
	ollamaModel string
	llmAPIKey   string
}

func NewAMRHandler(db *pgxpool.Pool, ollamaURL, ollamaModel, llmAPIKey string) *AMRHandler {
	return &AMRHandler{db: db, ollamaURL: ollamaURL, ollamaModel: ollamaModel, llmAPIKey: llmAPIKey}
}

// fleetFromStatusRecords builds the fleet from the authoritative
// robot_status_records table (RDS Core's t_robotstatusrecord). Returns nil if
// the table has no data for the requested plant (so the caller falls back to the
// legacy log-based fleet). plant=="" â†’ all plants.
//
// For each real AMR (uuid ~ '^AMR-') it takes the newest status record and:
//   - status_code / status_label from new_status
//   - online when the newest record ended within `onlineWindow` ago (the robot is
//     actively reporting); older or status 0 â†’ offline
//   - odometer from odo / today_odo
//   - last_seen = ended_on of the newest record
//
// Test/sim robots (sim_*, SWW*) are excluded. IP is populated later from
// RDS Core /robotsStatus report[].basic_info.ip when that live API is reachable.
//
// Connectivity stats (disconnects, reconnects, offline duration, worst drop) are
// computed from the SAME table's status transitions: a record with new_status=0 is
// a disconnect; a transition old_status=0 â†’ new_statusâ‰ 0 is a reconnect; the
// duration_ms of the new_status=0 records sums to total offline. This is REAL
// per-UUID data (unlike the legacy 192xx-port path, where the port is a protocol
// channel, not a robot ID). The window is all-time (every record in the table) so
// every column reflects full history.
func (h *AMRHandler) fleetFromStatusRecords(ctx context.Context, plant string, onlineWindow time.Duration) []*AMRStatus {
	// Latest record per plant+uuid via DISTINCT ON. Filter to real AMR robots.
	q := `
		SELECT DISTINCT ON (rsr.plant, rsr.uuid)
			rsr.uuid, COALESCE(rsr.vehicle_name, rsr.uuid), rsr.plant,
			rsr.new_status, rsr.started_on, rsr.ended_on, rsr.odo, rsr.today_odo
		FROM robot_status_records rsr
		WHERE rsr.uuid ~ '^AMR'
		  AND ($1::text = '' OR rsr.plant = $1)
		ORDER BY rsr.plant, rsr.uuid, rsr.started_on DESC`
	rows, err := h.db.Query(ctx, q, plant)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []*AMRStatus
	now := time.Now()
	for rows.Next() {
		var uuid, vehicle, rplant string
		var newStatus int
		var startedOn, endedOn time.Time
		var odo, todayOdo float64
		if err := rows.Scan(&uuid, &vehicle, &rplant, &newStatus, &startedOn, &endedOn, &odo, &todayOdo); err != nil {
			continue
		}
		// Some RDS tables store site-local timestamps without timezone; clamp future
		// values so the UI does not show robots as last seen hours from now.
		if endedOn.After(now.Add(5 * time.Minute)) {
			endedOn = now
		}
		online := newStatus != 0 && endedOn.After(now.Add(-onlineWindow))
		status := "ok"
		if !online {
			status = "error"
		}
		s := &AMRStatus{
			Name:        normaliseAMRName(uuid),
			Plant:       rplant,
			Status:      status,
			StatusCode:  newStatus,
			StatusLabel: robotStatusCodeLabel(newStatus),
			Odo:         odo,
			TodayOdo:    todayOdo,
			LastSeen:    &endedOn,
			LastIssue:   fmt.Sprintf("%s (status %d)", robotStatusCodeLabel(newStatus), newStatus),
			LiveStatus:  map[bool]string{true: "online", false: "offline"}[online],
			DataSource:  "rds_core",
		}
		out = append(out, s)
	}

	// Connectivity stats from status transitions (real, UUID-keyed, all-time).
	conn := h.connectivityFromStatusRecords(ctx, plant)
	for _, s := range out {
		key := fleetKey(s.Plant, s.Name)
		if c, ok := conn[key]; ok {
			s.DisconnectCount = c.disconnects
			s.ReconnectCount = c.reconnects
			s.TotalOfflineSec = c.totalOff
			s.WorstDropSec = c.worstDrop
		}
	}

	fleetMap := outByKey(out)
	if live := h.fetchLiveStatus(plant); len(live) > 0 {
		for key, ls := range live {
			if _, ok := fleetMap[key]; ok {
				continue
			}
			parts := strings.SplitN(key, "|", 2)
			if len(parts) != 2 {
				continue
			}
			status := "error"
			liveStatus := "offline"
			if ls.Online {
				status = "ok"
				liveStatus = "online"
			}
			s := &AMRStatus{
				Name:        parts[1],
				Plant:       parts[0],
				Status:      status,
				StatusCode:  ls.Code,
				StatusLabel: robotStatusCodeLabel(ls.Code),
				LastIssue:   ls.Reason,
				LiveStatus:  liveStatus,
				DataSource:  "rds_core_live",
			}
			out = append(out, s)
			fleetMap[key] = s
		}
		applyLiveStatus(fleetMap, live, plant)
	}
	out = append(out, mergeCoreRobotStatus(fleetMap, h.fetchCoreRobotStatuses(plant))...)

	return out
}

// connStats is the per-robot connectivity profile computed from status transitions.
type connStats struct {
	disconnects int
	reconnects  int
	totalOff    int
	worstDrop   int
}

// connectivityFromStatusRecords computes per-UUID connectivity stats from the
// robot_status_records transitions. Disconnects = new_status=0 records; reconnects =
// transitions where old_status=0 and new_statusâ‰ 0; offline duration = sum/max of
// duration_ms over the new_status=0 records. All-time. Keyed by fleetKey(plant,name).
func (h *AMRHandler) connectivityFromStatusRecords(ctx context.Context, plant string) map[string]connStats {
	q := `
		SELECT uuid, COALESCE(plant,''),
			COUNT(*) FILTER (WHERE new_status = 0)                                   AS disconnects,
			COUNT(*) FILTER (WHERE old_status = 0 AND new_status <> 0)               AS reconnects,
			(COALESCE(SUM(duration_ms) FILTER (WHERE new_status = 0), 0) / 1000)::bigint AS total_off_sec,
			(COALESCE(MAX(duration_ms) FILTER (WHERE new_status = 0), 0) / 1000)::bigint AS worst_off_sec
		FROM robot_status_records
		WHERE uuid ~ '^AMR'
		  AND ($1::text = '' OR plant = $1)
		GROUP BY uuid, plant`
	rows, err := h.db.Query(ctx, q, plant)
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := map[string]connStats{}
	for rows.Next() {
		var uuid, rplant string
		var disc, recon int64
		var totalOff, worst int64
		if err := rows.Scan(&uuid, &rplant, &disc, &recon, &totalOff, &worst); err != nil {
			continue
		}
		out[fleetKey(rplant, normaliseAMRName(uuid))] = connStats{
			disconnects: int(disc),
			reconnects:  int(recon),
			totalOff:    int(totalOff),
			worstDrop:   int(worst),
		}
	}
	return out
}

// outByKey indexes a fleet slice into a fleetKeyâ†’*AMRStatus map for applyAMRIPs.
func outByKey(fleet []*AMRStatus) map[string]*AMRStatus {
	m := make(map[string]*AMRStatus, len(fleet))
	for _, s := range fleet {
		m[fleetKey(s.Plant, s.Name)] = s
	}
	return m
}

// AMRStatus summarises one AMR's health and connectivity across all log sources.
type AMRStatus struct {
	Name            string     `json:"name"`
	Plant           string     `json:"plant"`
	Status          string     `json:"status"` // "ok" | "warning" | "error" | "unknown"
	DisconnectCount int        `json:"disconnect_count"`
	ErrorCount      int        `json:"error_count"`
	WarnCount       int        `json:"warn_count"`
	TotalEvents     int        `json:"total_events"`
	LastSeen        *time.Time `json:"last_seen"`
	LastIssue       string     `json:"last_issue"`
	LastIssueTime   *time.Time `json:"last_issue_time"`
	// Connectivity details (populated from disconnect/connect event pairing)
	LastIP          string `json:"last_ip"`           // e.g. "10.222.42.19"
	LastMAC         string `json:"last_mac"`          // e.g. "B8:27:EB:1A:2B:3C"
	ReconnectCount  int    `json:"reconnect_count"`   // TCP reconnects observed
	TotalOfflineSec int    `json:"total_offline_sec"` // approx cumulative offline seconds
	WorstDropSec    int    `json:"worst_drop_sec"`    // longest single drop in seconds
	// Live state from RDS Core (authoritative "connected right now"). When
	// available it OVERRIDES the log-derived status: a robot that dropped
	// overnight but is connected again now shows "ok", not "error". Empty when the
	// RDS call failed (we then fall back to the log-derived status).
	LiveStatus string `json:"live_status"` // "online" | "offline" | "" (unknown)
	StatusCode int    `json:"status_code"` // raw RDS newStatus code (0 if unknown)
	// Authoritative per-robot data from t_robotstatusrecord (RDS Core MySQL).
	// Populated when the 'rds_robot_status' collection has run. Human-readable
	// status label, odometer, and whether the robot is currently connected.
	StatusLabel  string   `json:"status_label"`            // e.g. "Idle", "Moving", "Offline"
	Odo          float64  `json:"odo"`                     // cumulative meters
	TodayOdo     float64  `json:"today_odo"`               // meters today
	BatteryLevel *float64 `json:"battery_level,omitempty"` // percent, 0-100
	BatteryTempC *float64 `json:"battery_temp_c,omitempty"`
	BatteryState string   `json:"battery_state,omitempty"`
	DataSource   string   `json:"data_source"` // "rds_core" | "logs"
}

// robotStatusCodeLabel maps a Seer/SRC new_status code to a short human label.
// Based on the SRC controller enum + operator confirmation that codes 1â€“5 are all
// connected/running states. Adjust here if your site uses a different mapping.
func robotStatusCodeLabel(code int) string {
	switch code {
	case 0:
		return "Offline"
	case 1:
		return "Idle"
	case 2:
		return "Moving"
	case 3:
		return "Paused"
	case 4:
		return "Charging"
	case 5:
		return "Standby"
	case 6:
		return "Error"
	case 7:
		return "Manual"
	default:
		return fmt.Sprintf("State %d", code)
	}
}

// amrNameRe matches strings like AMR-01, AMR-02, amr01, AMR_03, etc.
var amrNameRe = regexp.MustCompile(`(?i)\bAMR[-_]?\d+\b`)

// serverIPRe extracts IP from Roboshop messages like "[Server:10.222.42.19:9305]"
var serverIPRe = regexp.MustCompile(`\[Server:(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}):`)

// roboshopTagRe pulls the AMR slot tag from Roboshop log lines: a "[19204]" run
// immediately followed by a log-level bracket like "[info]"/"[warning]". The tag
// minus 19200 is the AMR number (19204 â†’ AMR-04). Matching is position-independent
// so it works for both "[Roboshop][19204]" (Hopkinsville roboshop_app) and the
// bare "Roboshop.desktop[pid]: [19204]" (Springfield journald_amr). The trailing
// level bracket avoids matching unrelated 19xxx numbers in messages.
var roboshopTagRe = regexp.MustCompile(`\[(19\d{3})\]\[(?:info|warning|error)\]`)

// roboshopEventTSRe parses the event timestamp from "[20260616 171531.023]". The
// DB timestamp column is ingest-time (whole batches share one value), so this
// bracket form is the only reliable event clock.
var roboshopEventTSRe = regexp.MustCompile(`\[(\d{8}) (\d{6})`)

// localIPRe extracts the IP from "[Local:10.216.4.59:45540]". NOTE: in Roboshop
// slotTcp logs, [Local] is the FleetManager's OWN socket IP (not the AMR's), and
// [Server:IP:port] is where the AMR connected from â€” but that IP also roams
// (DHCP/AP changes), so it's an unreliable per-AMR identifier. We prefer the
// RDS Core /robotsStatus IPs and only fall back to [Server] in legacy drop evidence.
var localIPRe = regexp.MustCompile(`\[Local:(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}):\d+\]`)

// tcpDestRe parses an OUTBOUND "ss -tnp" row where the FleetManager connected TO
// the AMR:
//
//	"ESTAB 0 0 10.222.10.76:35160 10.222.42.22:19206"
//
// AMR number = dest port's 192xx suffix; robot IP = dest IP. CAVEAT: at plants
// that talk to robots via Wi-Fi APs (Springfield), this dest IP is an AP/proxy
// address shared across many robots â€” an unreliable identifier. We prefer the
// inbound form (tcpSrcRe) when available.
var tcpDestRe = regexp.MustCompile(`\s+\d+\s+\d+\s+[0-9.]+:\d+\s+([0-9.]+):192(\d{2})\b`)

// tcpSrcRe parses an INBOUND "ss -tnp" row where the AMR connected INTO the
// FleetManager's listener:
//
//	"ESTAB 0 0 10.222.10.76:19204 10.228.0.57:62946"
//
// AMR number = source port's 192xx suffix (the FM listener for that slot); robot
// IP = dest IP (the far end that dialed in). This was the best socket-level
// signal before /robotsStatus was available, but it is not used for the fleet IP.
var tcpSrcRe = regexp.MustCompile(`\s+\d+\s+\d+\s+[0-9.]+:192(\d{2})\s+([0-9.]+):\d+\b`)

// parseRoboshopEventTS turns a "[YYYYMMDD HHMMSS]" bracket timestamp into a UTC
// time. Returns ok=false (and the caller keeps the ingest timestamp) if absent.
func parseRoboshopEventTS(msg string) (time.Time, bool) {
	m := roboshopEventTSRe.FindStringSubmatch(msg)
	if len(m) != 3 {
		return time.Time{}, false
	}
	t, err := time.Parse("20060102 150405", m[1]+" "+m[2])
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// fetchLiveStatus pulls the live per-robot connection state from RDS Core. For a
// specific plant it queries that plant; for plant=="" (All Plants) it fans out
// across every configured plant and merges. The returned map is keyed by
// fleetKey(plant, robotName) so the same robot number at different plants (e.g.
// AMR-06 @ Springfield online vs AMR-06 @ Hopkinsville offline) stays distinct.
// Returns nil only if NO plant yielded live data. Best-effort: a failing plant is
// skipped, not fatal.
func (h *AMRHandler) fetchLiveStatus(plant string) map[string]robowatch.RobotLiveStatus {
	plants := []string{plant}
	if plant == "" {
		plants = nil
		for _, p := range config.AllPlants() {
			plants = append(plants, p.Name)
		}
	}
	merged := map[string]robowatch.RobotLiveStatus{}
	for _, pname := range plants {
		pc := config.GetPlant(pname)
		if pc == nil {
			continue
		}
		pw := config.GetRobowatchPassword(pname)
		if pw == "" {
			continue
		}
		c := robowatch.NewClient(pc.BaseURL, pc.Port, pc.Username, pw)
		live, err := c.LiveStatus()
		if err != nil || len(live) == 0 {
			continue
		}
		for name, st := range live {
			merged[fleetKey(pname, normaliseAMRName(name))] = st
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

type coreRobotFleetStatus struct {
	robowatch.RobotCoreStatus
	Name  string
	Plant string
}

// fetchCoreRobotStatuses pulls each robot's current roster/status from RDS Core
// /robotsStatus on port 8088. This is the live source used to populate new AMRs
// as soon as they are added to a plant's RDS Core, even before historical logs or
// robot_status_records have been collected.
func (h *AMRHandler) fetchCoreRobotStatuses(plant string) map[string]coreRobotFleetStatus {
	plants := []string{plant}
	if plant == "" {
		plants = nil
		for _, p := range config.AllPlants() {
			plants = append(plants, p.Name)
		}
	}
	merged := map[string]coreRobotFleetStatus{}
	for _, pname := range plants {
		pc := config.GetPlant(pname)
		if pc == nil {
			continue
		}
		c := robowatch.NewClient(pc.BaseURL, pc.Port, pc.Username, "")
		robots, err := c.CoreRobotStatus()
		if err != nil || len(robots) == 0 {
			continue
		}
		for rawName, st := range robots {
			name := normaliseAMRName(rawName)
			if name == "" || !strings.HasPrefix(name, "AMR-") {
				continue
			}
			merged[fleetKey(pname, name)] = coreRobotFleetStatus{
				RobotCoreStatus: st,
				Name:            name,
				Plant:           pname,
			}
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

// fetchCoreRobotIPs returns only the RDS Core robot IP map for callers that do
// not need the full /robotsStatus payload.
func (h *AMRHandler) fetchCoreRobotIPs(plant string) map[string]string {
	core := h.fetchCoreRobotStatuses(plant)
	if len(core) == 0 {
		return nil
	}
	ips := map[string]string{}
	for key, st := range core {
		if st.IP != "" {
			ips[key] = st.IP
		}
	}
	if len(ips) == 0 {
		return nil
	}
	return ips
}

func mergeCoreRobotStatus(fleet map[string]*AMRStatus, core map[string]coreRobotFleetStatus) []*AMRStatus {
	if len(core) == 0 {
		return nil
	}
	added := []*AMRStatus{}
	for key, st := range core {
		if existing, ok := fleet[key]; ok {
			if st.IP != "" {
				existing.LastIP = st.IP
			}
			if st.MAC != "" {
				existing.LastMAC = st.MAC
			}
			if st.Odo > 0 {
				existing.Odo = st.Odo
			}
			existing.TodayOdo = st.TodayOdo
			existing.BatteryLevel = st.BatteryLevel
			existing.BatteryTempC = st.BatteryTempC
			existing.BatteryState = st.BatteryState
			seenAt := coreRobotLastSeenAt(st)
			if !seenAt.IsZero() && (existing.LastSeen == nil || seenAt.After(*existing.LastSeen)) {
				existing.LastSeen = &seenAt
			}
			if existing.Plant == "" {
				existing.Plant = st.Plant
			}
			// /robotsStatus is the complete live roster. Its connection_status is
			// therefore a stronger connectivity signal than absence from the
			// agvStatusCurrent response, which can contain only changed rows.
			if st.ConnectionStatus == 1 {
				existing.LiveStatus = "online"
				existing.Status = "ok"
				if existing.StatusLabel == "" || existing.StatusLabel == "Offline" {
					existing.StatusLabel = "Online"
					existing.LastIssue = "RDS Core reports robot online"
				}
			} else {
				existing.LiveStatus = "offline"
				existing.Status = "error"
				existing.StatusLabel = "Offline"
				existing.LastIssue = "RDS Core reports robot offline"
			}
			applyCoreOfflineFallback(existing, st)
			continue
		}

		status := "error"
		liveStatus := "offline"
		statusLabel := "Offline"
		lastIssue := "RDS Core reports robot offline"
		if st.ConnectionStatus == 1 {
			status = "ok"
			liveStatus = "online"
			statusLabel = "Online"
			lastIssue = "RDS Core reports robot online"
		}
		seenAt := coreRobotLastSeenAt(st)
		s := &AMRStatus{
			Name:         st.Name,
			Plant:        st.Plant,
			Status:       status,
			LastIP:       st.IP,
			LastMAC:      st.MAC,
			LiveStatus:   liveStatus,
			StatusCode:   st.ConnectionStatus,
			StatusLabel:  statusLabel,
			Odo:          st.Odo,
			TodayOdo:     st.TodayOdo,
			BatteryLevel: st.BatteryLevel,
			BatteryTempC: st.BatteryTempC,
			BatteryState: st.BatteryState,
			LastSeen:     &seenAt,
			LastIssue:    lastIssue,
			DataSource:   "rds_core_live",
		}
		applyCoreOfflineFallback(s, st)
		fleet[key] = s
		added = append(added, s)
	}
	return added
}

func coreRobotLastSeenAt(st coreRobotFleetStatus) time.Time {
	if st.ConnectionStatus == 0 && !st.LastReceivedAt.IsZero() {
		if st.SeenAt.IsZero() || !st.LastReceivedAt.After(st.SeenAt.Add(5*time.Minute)) {
			return st.LastReceivedAt
		}
	}
	return st.SeenAt
}

func applyCoreOfflineFallback(s *AMRStatus, st coreRobotFleetStatus) {
	if st.ConnectionStatus != 0 || st.SeenAt.IsZero() || st.LastReceivedAt.IsZero() {
		return
	}
	if s.DisconnectCount > 0 || s.ReconnectCount > 0 || s.TotalOfflineSec > 0 || s.WorstDropSec > 0 {
		return
	}
	if !st.SeenAt.After(st.LastReceivedAt) {
		return
	}
	offlineSec := int(st.SeenAt.Sub(st.LastReceivedAt).Seconds())
	if offlineSec <= 0 {
		return
	}
	s.DisconnectCount = 1
	s.TotalOfflineSec = offlineSec
	s.WorstDropSec = offlineSec
	s.LastIssue = fmt.Sprintf("RDS Core reports robot offline; last telemetry %d seconds before the latest poll", offlineSec)
}

// liveLookup fetches live status for a single robot at a plant (or all plants
// when plant=="") and returns that robot's status, or ok=false if not found.
func (h *AMRHandler) liveLookup(plant, robot string) (robowatch.RobotLiveStatus, bool) {
	live := h.fetchLiveStatus(plant)
	if live == nil {
		return robowatch.RobotLiveStatus{}, false
	}
	// If a specific plant, look up directly. If all plants, the robot may exist at
	// several â€” prefer the one matching `plant`, else the first match.
	if plant != "" {
		ls, ok := live[fleetKey(plant, robot)]
		return ls, ok
	}
	for pname := range plantIndex {
		if ls, ok := live[fleetKey(pname, robot)]; ok {
			return ls, true
		}
	}
	return robowatch.RobotLiveStatus{}, false
}

// plantIndex is the set of configured plant names, used by liveLookup for the
// all-plants case. Built once lazily.
var plantIndex = func() map[string]bool {
	m := map[string]bool{}
	for _, p := range config.AllPlants() {
		m[p.Name] = true
	}
	return m
}()

// applyLiveStatus overrides each fleet row's status with the live RDS Core state
// when available. A robot reported online (newStatus 5/4) is "ok" even if it had
// historical disconnects. A robot in the logs but ABSENT from the live roster is
// "offline" (RDS Core cannot see it) â€” but only when the live map covers that
// robot's plant (so an All-Plants map from a subset of plants doesn't false-flag).
func applyLiveStatus(fleet map[string]*AMRStatus, live map[string]robowatch.RobotLiveStatus, plantScope string) {
	if len(live) == 0 {
		return
	}
	for _, s := range fleet {
		ls, present := live[fleetKey(s.Plant, s.Name)]
		if !present {
			// agvStatusCurrent is not a guaranteed full-fleet roster on every RDS
			// version. Absence is unknown; /robotsStatus supplies the authoritative
			// connection_status later in the merge pipeline.
			continue
		}
		if ls.Online {
			s.LiveStatus = "online"
			s.StatusCode = ls.Code
			// Live online wins: a robot connected right now is "ok", even if it had
			// historical disconnects overnight.
			if s.Status == "error" || s.Status == "unknown" {
				s.Status = "ok"
			}
		} else {
			s.LiveStatus = "offline"
			s.StatusCode = ls.Code
			s.Status = "error"
		}
	}
}

// loadAMRIPs reads the legacy live_amr_tcp snapshot (the FleetManager's `ss -tnp`
// output) and maps each AMR slot to a socket-level peer IP. The fleet page now
// uses RDS Core /robotsStatus instead; this legacy parser remains for older
// drop/timeline evidence and fallback investigations. plant=="" means all plants.
//
// There are TWO connection directions and they have very different reliability:
//   - INBOUND  (FM:192xx â† robotIP): the robot dialed into the FM's listener.
//     The remote IP is the robot itself â€” AUTHORITATIVE.
//   - OUTBOUND (FM:ephemeral â†’ IP:192xx): the FM reached out to the robot. At
//     plants behind Wi-Fi APs (Springfield) this dest IP is a shared AP/proxy
//     address that blurs across robots â€” APPROXIMATE only.
//
// We prefer inbound when present; otherwise fall back to the most-frequent
// outbound IP. Each AMR can appear under multiple IPs (roaming), so we count
// frequency and keep the most common. Robots absent from the snapshot (e.g.
// AMR-01/02/03 at Springfield, which generate no socket traffic) get no IP â€”
// there is genuinely no data for them.
//
// Returns a map keyed by fleetKey(plant, "AMR-NN"). FM-own-host IPs are excluded.
func (h *AMRHandler) loadAMRIPs(ctx context.Context, plant string) map[string]string {
	rows, err := h.db.Query(ctx, `
		SELECT le.message, COALESCE(s.host,'') AS server_host
		FROM log_events le
		LEFT JOIN servers s ON s.id = le.server_id
		WHERE le.source = 'live_amr_tcp'
		  AND ($1::text = '' OR s.host = $1 OR COALESCE(s.name,'') ILIKE '%' || $1 || '%')
		ORDER BY le.timestamp DESC
		LIMIT 1500`, plant)
	if err != nil {
		return nil
	}
	defer rows.Close()

	type ipCount struct {
		ip string
		n  int
	}
	// Separate frequency tables so inbound can win outright when present.
	inbound := map[string]map[string]int{}  // authoritative
	outbound := map[string]map[string]int{} // approximate (AP blur)
	tally := func(store map[string]map[string]int, key, ip string) {
		if store[key] == nil {
			store[key] = map[string]int{}
		}
		store[key][ip]++
	}

	for rows.Next() {
		var msg, srvHost string
		if err := rows.Scan(&msg, &srvHost); err != nil {
			continue
		}
		inferred := config.PlantForServer("", srvHost)
		if inferred == "" {
			continue
		}
		// Inbound: FM:192xx â† robotIP:port  (robot dialed in â€” authoritative).
		if mi := tcpSrcRe.FindStringSubmatch(msg); len(mi) == 3 {
			amrNum, ip := mi[1], mi[2]
			// Reject the FM's own host IP (self-loop connections), and any
			// malformed/garbage IP the socket table emits (e.g. Springfield
			// sometimes shows "10.2.222.5" â€” not valid IPv4). Only real,
			// non-self remotes count toward the frequency tally.
			if validRobotIP(ip, srvHost) {
				tally(inbound, fleetKey(inferred, "AMR-"+amrNum), ip)
			}
		}
		// Outbound: FM:port â†’ IP:192xx  (AP-proxied â€” approximate).
		if mo := tcpDestRe.FindStringSubmatch(msg); len(mo) == 3 {
			ip, amrNum := mo[1], mo[2]
			if validRobotIP(ip, srvHost) {
				tally(outbound, fleetKey(inferred, "AMR-"+amrNum), ip)
			}
		}
	}

	// pickMode returns the most frequent IP and the confidence = winner's share
	// of that direction's total (0â€“1). A confident winner (e.g. one AP that
	// dominates an AMR's connections) is preferred over a sparse tie across many
	// addresses, where the "winner" is just noise.
	pickMode := func(freq map[string]int) (ipCount, float64) {
		best := ipCount{}
		total := 0
		for ip, n := range freq {
			total += n
			if n > best.n {
				best = ipCount{ip, n}
			}
		}
		conf := 0.0
		if total > 0 {
			conf = float64(best.n) / float64(total)
		}
		return best, conf
	}

	out := map[string]string{}
	// At Wi-Fi/AP plants each AMR slot roams across several AP addresses, so no
	// single IP is "the robot". Inbound is authoritative in principle, but its
	// winner is often a sparse tie (self-loop dominates, remainder is noise â€”
	// including garbled remotes like "10.2.222.5"). Outbound's cluster is more
	// stable and always a real plant AP. Strategy:
	//   - take inbound only when it's a confident leader (>=30%); that means a
	//     real direct connection, not noise;
	//   - otherwise use outbound's mode (a real AP the FM reaches the robot via);
	//   - if neither has any data, leave blank.
	for key := range inbound {
		ib, iconf := pickMode(inbound[key])
		if ib.ip != "" && iconf >= 0.30 {
			out[key] = ib.ip
			continue
		}
		if ob, _ := pickMode(outbound[key]); ob.ip != "" {
			out[key] = ob.ip
		}
	}
	for key := range outbound {
		if _, have := out[key]; have {
			continue
		}
		if ob, _ := pickMode(outbound[key]); ob.ip != "" {
			out[key] = ob.ip
		}
	}
	return out
}

// validRobotIP reports whether ip is a usable per-AMR address from the socket
// snapshot: it must be a syntactically valid IPv4 (4 dotted octets 0â€“255) and
// must not equal the FleetManager's own host IP (self-loop connections show the
// FM talking to itself). This filters both garbage like "10.2.222.5" (sometimes
// emitted by the Springfield box) and the FM's self-address.
func validRobotIP(ip, srvHost string) bool {
	if ip == "" || ip == srvHost {
		return false
	}
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if len(p) == 0 || len(p) > 3 {
			return false
		}
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
			n = n*10 + int(c-'0')
		}
		if n > 255 {
			return false
		}
	}
	return true
}

// available. Rows without a live IP keep their log-derived IP.
func applyAMRIPs(fleet map[string]*AMRStatus, ips map[string]string) {
	if len(ips) == 0 {
		return
	}
	for key, s := range fleet {
		if ip, ok := ips[key]; ok && ip != "" {
			s.LastIP = ip
		}
	}
}

// including connectivity stats (IP, reconnects, offline duration, worst drop).
func (h *AMRHandler) FleetStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	plant := r.URL.Query().Get("plant") // optional plant filter

	// Authoritative source: RDS Core's t_robotstatusrecord (collected via SSH).
	// Prefer it when data exists; fall back to the legacy log-based fleet below
	// only when the table is empty (e.g. before the first 'rds_robot_status' sync,
	// or for plants the collector hasn't run on).
	if fleet := h.fleetFromStatusRecords(ctx, plant, 10*time.Minute); len(fleet) > 0 {
		// Worst status first (error/offline before ok), then by name.
		sort.Slice(fleet, func(i, j int) bool {
			ri, rj := statusRank(fleet[i].Status), statusRank(fleet[j].Status)
			if ri != rj {
				return ri > rj
			}
			return fleet[i].Name < fleet[j].Name
		})
		jsonOK(w, fleet)
		return
	}

	// --- Source 1: rds_log_events grouped by robot ---
	rdsRows, err := h.db.Query(ctx, `
		SELECT
			robot,
			plant,
			MAX(timestamp)                                              AS last_seen,
			COUNT(*)                                                    AS total,
			COUNT(*) FILTER (WHERE severity = 'error'
			                    OR category ILIKE '%disconnect%'
			                    OR message  ILIKE '%disconnect%'
			                    OR message  ILIKE '%offline%'
			                    OR message  ILIKE '%unconnected%'
			                    OR message  ILIKE '%connection refused%'
			                    OR message  ILIKE '%remote host closed%')  AS disconnects,
			COUNT(*) FILTER (WHERE severity = 'error')                  AS errors,
			COUNT(*) FILTER (WHERE severity = 'warning' OR severity = 'warn') AS warns,
			(ARRAY_AGG(message ORDER BY timestamp DESC))[1]             AS last_message,
			(ARRAY_AGG(timestamp ORDER BY timestamp DESC)
			  FILTER (WHERE severity = 'error'
			             OR category ILIKE '%disconnect%'
			             OR message  ILIKE '%disconnect%'
			             OR message  ILIKE '%offline%'))[1]             AS last_issue_time,
			(ARRAY_AGG(message ORDER BY timestamp DESC)
			  FILTER (WHERE severity = 'error'
			             OR category ILIKE '%disconnect%'
			             OR message  ILIKE '%disconnect%'
			             OR message  ILIKE '%offline%'))[1]             AS last_issue_msg,
			-- Last known IP stored at ingest time
			(ARRAY_AGG(robot_ip ORDER BY timestamp DESC)
			  FILTER (WHERE robot_ip <> ''))[1] AS last_ip
		FROM rds_log_events
		WHERE robot <> ''
		  AND ($1::text = '' OR plant = $1)
		GROUP BY robot, plant
		ORDER BY last_seen DESC`, plant)
	// If the table doesn't exist yet (first run before RDS logs are fetched),
	// skip gracefully and return an empty fleet rather than a 500.
	if err != nil {
		jsonOK(w, []*AMRStatus{})
		return
	}
	defer rdsRows.Close()

	fleet := map[string]*AMRStatus{}

	for rdsRows.Next() {
		var robot, rdsPlant, lastMsg string
		var lastIssueMsg, lastIP *string
		var lastSeen time.Time
		var lastIssueTime *time.Time
		var total, disconnects, errors, warns int64

		if err := rdsRows.Scan(&robot, &rdsPlant, &lastSeen, &total, &disconnects, &errors, &warns,
			&lastMsg, &lastIssueTime, &lastIssueMsg, &lastIP); err != nil {
			continue
		}
		key := normaliseAMRName(robot)
		fk := fleetKey(rdsPlant, key)
		ip := ""
		_ = lastIP
		s := &AMRStatus{
			Name:            key,
			Plant:           rdsPlant,
			DisconnectCount: int(disconnects),
			ErrorCount:      int(errors),
			WarnCount:       int(warns),
			TotalEvents:     int(total),
			LastSeen:        &lastSeen,
			LastIssue:       truncateStr(firstOf(lastIssueMsg, &lastMsg), 120),
			LastIssueTime:   lastIssueTime,
			LastIP:          ip,
		}
		s.Status = deriveStatus(s)
		fleet[fk] = s
	}

	// --- Source 2: log_events robot events (SSH journal + Roboshop app logs) ---
	// No SQL plant filter here â€” "Hop-Fleetmanager" won't match ILIKE '%Hopkinsville%'.
	// We JOIN servers to get name/host, derive plant in Go via PlantForServer(), then filter.
	// Use substring() to extract AMR number from message for grouping â€”
	// log_events has no robot column; the AMR name lives inside the message text.
	sysRows, err := h.db.Query(ctx, `
		SELECT
			upper('AMR-' || substring(le.message FROM '(?i)AMR[-_]?(\d+)')) AS amr_name,
			COALESCE(s.name, '')  AS server_name,
			COALESCE(s.host, '')  AS server_host,
			MAX(le.timestamp)     AS last_seen,
			COUNT(*)              AS total,
			COUNT(*) FILTER (WHERE le.event_type = 'robot_offline')             AS disconnects,
			COUNT(*) FILTER (WHERE le.severity IN ('error','critical'))          AS errors,
			COUNT(*) FILTER (WHERE le.severity = 'warning')                     AS warns,
			(ARRAY_AGG(le.message ORDER BY le.timestamp DESC))[1]               AS last_msg,
			(ARRAY_AGG(le.timestamp ORDER BY le.timestamp DESC)
			  FILTER (WHERE le.event_type = 'robot_offline'
			             OR le.severity IN ('error','critical')))[1]            AS last_issue_time
		FROM log_events le
		LEFT JOIN servers s ON s.id = le.server_id
		WHERE le.message ~* 'AMR[-_]?\d+'
		  AND le.event_type IN ('robot_offline','robot_online','rds_core_issue','error','warning',
		                        'battery_error','battery_status','roboshop_charge_command',
		                        'roboshop_chargedi_change','warlink_failure')
		GROUP BY amr_name, s.name, s.host
		LIMIT 200`)
	if err == nil {
		defer sysRows.Close()
		for sysRows.Next() {
			var amrName, serverName, serverHost, lastMsg string
			var lastSeen time.Time
			var lastIssueTime *time.Time
			var total, disconnects, errors, warns int64
			if err := sysRows.Scan(&amrName, &serverName, &serverHost, &lastSeen, &total, &disconnects, &errors, &warns,
				&lastMsg, &lastIssueTime); err != nil {
				continue
			}
			if amrName == "" || amrName == "AMR-" {
				continue
			}
			key := normaliseAMRName(amrName)

			// Derive plant from server name/host â€” handles "Hop-Fleetmanager" â†’ "Hopkinsville"
			inferredPlant := config.PlantForServer(serverName, serverHost)

			// Apply plant filter in Go (not SQL) to correctly handle name aliases
			if plant != "" && inferredPlant != "" && inferredPlant != plant {
				continue
			}
			fk := fleetKey(inferredPlant, key)

			ip := ""

			if existing, ok := fleet[fk]; ok {
				existing.DisconnectCount += int(disconnects)
				existing.ErrorCount += int(errors)
				existing.WarnCount += int(warns)
				existing.TotalEvents += int(total)
				if existing.LastSeen != nil && lastSeen.After(*existing.LastSeen) {
					existing.LastSeen = &lastSeen
				} else if existing.LastSeen == nil {
					existing.LastSeen = &lastSeen
				}
				if existing.LastIssueTime == nil && lastIssueTime != nil {
					existing.LastIssueTime = lastIssueTime
					existing.LastIssue = truncateStr(lastMsg, 120)
				}
				if existing.Plant == "" && inferredPlant != "" {
					existing.Plant = inferredPlant
				}
				existing.Status = deriveStatus(existing)
			} else {
				s := &AMRStatus{
					Name:            key,
					Plant:           inferredPlant,
					DisconnectCount: int(disconnects),
					ErrorCount:      int(errors),
					WarnCount:       int(warns),
					TotalEvents:     int(total),
					LastSeen:        &lastSeen,
					LastIssue:       truncateStr(lastMsg, 120),
					LastIssueTime:   lastIssueTime,
					LastIP:          ip,
				}
				s.Status = deriveStatus(s)
				fleet[fk] = s
			}
		}
	}

	// --- Source 3b: connectivity from log_events Roboshop file logs ---
	// Roboshop logs embed port numbers in [Server:IP:port] â€” port 19200+N maps to AMR-N.
	// This gives us disconnect/reconnect data even without rds_log_events. We pair
	// offâ†’on transitions per AMR to compute reconnect count, total offline, worst drop,
	// and (unlike before) we CREATE fleet entries here so AMRs that only appear in
	// Roboshop logs still show up on the dashboard.
	events := h.loadPortEvents(ctx, plant, amrWindow{})
	connMap := pairPortEvents(events)

	for fk, c := range connMap {
		if s, ok := fleet[fk]; ok {
			if s.ReconnectCount == 0 {
				s.ReconnectCount = c.reconnects
			}
			if s.TotalOfflineSec == 0 {
				s.TotalOfflineSec = c.totalOff
			}
			if s.WorstDropSec == 0 {
				s.WorstDropSec = c.worstDrop
			}
			if s.Plant == "" && c.plant != "" {
				s.Plant = c.plant
			}
		} else if c.lastSeen != nil {
			// First time we see this AMR at this plant: create it from Roboshop
			// connectivity data. Each reconnect pair starts with a disconnect, so
			// reconnect count is also the disconnect count (used by deriveStatus).
			s := &AMRStatus{
				Name:            c.amrName,
				Plant:           c.plant,
				ReconnectCount:  c.reconnects,
				DisconnectCount: c.reconnects,
				TotalOfflineSec: c.totalOff,
				WorstDropSec:    c.worstDrop,
				LastSeen:        c.lastSeen,
				LastIssue:       truncateStr(c.lastMsg, 120),
			}
			s.Status = deriveStatus(s)
			fleet[fk] = s
		}
	}

	// --- Source 3: connectivity stats via disconnect/reconnect event pairing ---
	// Uses LEAD() window function to pair each disconnect with the next connect event,
	// computing offline duration per gap. Grouped by robot for totals + worst drop.
	connRows, err := h.db.Query(ctx, `
		WITH classified AS (
			SELECT
				robot,
				plant,
				timestamp,
				CASE
					WHEN message ILIKE '%SocketState:UnconnectedState%'
					  OR message ILIKE '%disconnect%'
					  OR message ILIKE '%ClosingState%'
					  OR message ILIKE '%not connected%'
					  OR message ILIKE '%connection refused%'
					  OR message ILIKE '%remote host closed%'
					  OR message ILIKE '%add device failed%'
					THEN 'off'
					WHEN message ILIKE '%SocketState:ConnectedState%'
					  OR (message ILIKE '%connect%'
					      AND NOT message ILIKE '%disconnect%'
					      AND NOT message ILIKE '%unconnect%')
					THEN 'on'
					ELSE NULL
				END AS state
			FROM rds_log_events
			WHERE robot <> ''
			  AND ($1::text = '' OR plant = $1)
		),
		transitions AS (
			SELECT
				robot,
				plant,
				timestamp  AS off_time,
				state,
				LEAD(timestamp) OVER (PARTITION BY plant, robot ORDER BY timestamp) AS next_time,
				LEAD(state)     OVER (PARTITION BY plant, robot ORDER BY timestamp) AS next_state
			FROM classified
			WHERE state IS NOT NULL
		)
		SELECT
			robot,
			plant,
			COUNT(*)      FILTER (WHERE state = 'off')                           AS reconnect_count,
			COALESCE(SUM(
				CASE WHEN state = 'off' AND next_state = 'on'
				     THEN GREATEST(0, EXTRACT(EPOCH FROM (next_time - off_time))::int)
				     ELSE 0 END
			), 0)                                                                AS total_offline_sec,
			COALESCE(MAX(
				CASE WHEN state = 'off' AND next_state = 'on'
				     THEN GREATEST(0, EXTRACT(EPOCH FROM (next_time - off_time))::int)
				     ELSE 0 END
			), 0)                                                                AS worst_drop_sec
		FROM transitions
		GROUP BY robot, plant`, plant)
	if err == nil {
		defer connRows.Close()
		for connRows.Next() {
			var robot, rdsPlant string
			var reconnects, totalOffline, worstDrop int64
			if err := connRows.Scan(&robot, &rdsPlant, &reconnects, &totalOffline, &worstDrop); err != nil {
				continue
			}
			key := normaliseAMRName(robot)
			if s, ok := fleet[fleetKey(rdsPlant, key)]; ok {
				s.ReconnectCount = int(reconnects)
				s.TotalOfflineSec = int(totalOffline)
				s.WorstDropSec = int(worstDrop)
			}
		}
	}

	// Live RDS Core status is the authoritative "connected right now" signal. It
	// both seeds robots that exist in RDS but have no log events yet (e.g. AMR-01
	// that never dropped) and overrides the log-derived status (a robot that
	// dropped overnight but is online again now shows "ok"). Fetched per-plant
	// (single view) or merged across all plants (All-Plants view).
	live := h.fetchLiveStatus(plant)
	// Seed robots from the live roster not already present (no log history). Only
	// meaningful for a single-plant view â€” in All-Plants we can't attribute a
	// roster robot to a specific plant from the union alone.
	if plant != "" {
		for name := range live {
			key := normaliseAMRName(name)
			if _, ok := fleet[fleetKey(plant, key)]; ok {
				continue
			}
			s := &AMRStatus{Name: key, Plant: plant}
			s.Status = deriveStatus(s)
			fleet[fleetKey(plant, key)] = s
		}
	}
	applyLiveStatus(fleet, live, plant)
	mergeCoreRobotStatus(fleet, h.fetchCoreRobotStatuses(plant))

	// Sort: worst affected first, then by name
	result := make([]*AMRStatus, 0, len(fleet))
	for _, v := range fleet {
		result = append(result, v)
	}
	sort.Slice(result, func(i, j int) bool {
		si, sj := statusRank(result[i].Status), statusRank(result[j].Status)
		if si != sj {
			return si > sj
		}
		if result[i].TotalOfflineSec != result[j].TotalOfflineSec {
			return result[i].TotalOfflineSec > result[j].TotalOfflineSec
		}
		return result[i].Name < result[j].Name
	})

	jsonOK(w, result)
}

// Timeline returns a flat, per-robot list of disconnect/reconnect outages built
// from the Roboshop app logs (portâ†’AMR offâ†’on pairing). It supports plant==""
// (All Plants) and an optional robot filter. This is the data behind the AMR
// reconnect-timeline view: each drop has a start, end (if recovered), duration,
// and the IP/plant at the time of the disconnect.
func (h *AMRHandler) Timeline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	plant := r.URL.Query().Get("plant")
	robot := r.URL.Query().Get("robot")
	win := parseWindow(r)

	// Prefer the authoritative status-transition timeline from RDS Core when data
	// exists; fall back to the legacy socket-port drop pairing otherwise.
	if drops := h.statusTransitionDrops(ctx, plant, robot, win); drops != nil {
		attachLocations(drops, h.loadLocationEvents(ctx, plant, win))
		if drops == nil {
			drops = []dropEvent{}
		}
		jsonOK(w, drops)
		return
	}

	events := h.loadPortEvents(ctx, plant, win)
	drops := buildDropTimeline(events)

	// Attach the nearest preceding map-point (LM/AP/PP) to each drop so the UI
	// can show where the robot likely was when it lost connection.
	attachLocations(drops, h.loadLocationEvents(ctx, plant, win))

	if robot != "" {
		want := normaliseAMRName(robot)
		filtered := drops[:0]
		for _, d := range drops {
			if normaliseAMRName(d.Robot) == want {
				filtered = append(filtered, d)
			}
		}
		drops = filtered
	}

	if drops == nil {
		drops = []dropEvent{}
	}
	jsonOK(w, drops)
}

// statusTransitionDrops builds a timeline from RDS Core's authoritative status
// records. Each row where new_status = 0 (Offline) is treated as a disconnect;
// the next row with new_status != 0 is the reconnect. Returns nil when the
// robot_status_records table has no data for the scope (caller falls back to the
// socket-port pairing). plant/robot filter as elsewhere; win is the time window.
func (h *AMRHandler) statusTransitionDrops(ctx context.Context, plant, robot string, win amrWindow) []dropEvent {
	q := `
		SELECT uuid, COALESCE(plant,''), new_status, old_status, started_on, ended_on, duration_ms
		FROM robot_status_records
		WHERE uuid ~ '^AMR'
		  AND ($1::text = '' OR plant = $1)
		  AND ($2::text = '' OR uuid ~* ('^AMR[-_]?0*' || regexp_replace($2::text, '\D', '', 'g') || '$'))
		ORDER BY started_on`
	args := []any{plant, robot}
	if !win.from.IsZero() || !win.to.IsZero() {
		q = `
			SELECT uuid, COALESCE(plant,''), new_status, old_status, started_on, ended_on, duration_ms
			FROM robot_status_records
			WHERE uuid ~ '^AMR'
			  AND ($1::text = '' OR plant = $1)
			  AND ($2::text = '' OR uuid ~* ('^AMR[-_]?0*' || regexp_replace($2::text, '\D', '', 'g') || '$'))
			  AND ($3::timestamptz IS NULL OR started_on >= $3)
			  AND ($4::timestamptz IS NULL OR started_on <= $4)
			ORDER BY started_on`
		args = append(args, nullableTime(win.from), nullableTime(win.to))
	}
	rows, err := h.db.Query(ctx, q, args...)
	if err != nil {
		log.Printf("statusTransitionDrops query error: %v", err)
		return nil
	}
	defer rows.Close()

	type rec struct {
		uuid, plant  string
		newSt, oldSt int
		start, end   time.Time
		duration     int64
	}
	byRobot := map[string][]rec{}
	hasData := false
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.uuid, &r.plant, &r.newSt, &r.oldSt, &r.start, &r.end, &r.duration); err != nil {
			continue
		}
		hasData = true
		key := fleetKey(r.plant, r.uuid)
		byRobot[key] = append(byRobot[key], r)
	}
	if !hasData {
		return nil
	}

	var out []dropEvent
	for _, recs := range byRobot {
		if len(recs) == 0 {
			continue
		}
		uuid := recs[0].uuid
		var openStart *time.Time
		var openIP, openMsg string
		for _, r := range recs {
			if r.newSt == 0 && openStart == nil {
				// Disconnect: status went to 0.
				t := r.start
				openStart = &t
				openMsg = fmt.Sprintf("%s went offline (status %dâ†’%d)", normaliseAMRName(uuid), r.oldSt, r.newSt)
			} else if r.newSt != 0 && openStart != nil {
				// Reconnect: recovered from offline.
				end := r.start
				dur := int(end.Sub(*openStart).Seconds())
				if dur < 0 {
					dur = 0
				}
				out = append(out, dropEvent{
					Plant: r.plant, Robot: normaliseAMRName(uuid), State: "offline",
					Start: *openStart, End: &end, DurationSec: dur, Resolved: true,
					IP: openIP, Message: openMsg, Location: "",
					PlainEnglish: dropPlainEnglish(openMsg),
				})
				openStart = nil
			}
		}
		// Unresolved offline (no recovery seen in window).
		if openStart != nil {
			out = append(out, dropEvent{
				Plant: recs[0].plant, Robot: normaliseAMRName(uuid), State: "offline_open",
				Start: *openStart, DurationSec: 0, Resolved: false,
				IP: openIP, Message: openMsg, PlainEnglish: dropPlainEnglish(openMsg),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.After(out[j].Start) })
	return out
}

// nullableTime returns a *time.Time usable as a nullable pgx arg.
func nullableTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// RobotSummary is the per-robot drill-down block returned by /api/amr/robot.
// It bundles the connectivity stats, the robot's status classification, its last
// known location, and a recent drop timeline â€” everything the per-robot view needs
// in one call.
type RobotSummary struct {
	Name            string     `json:"name"`
	Plant           string     `json:"plant"`
	LastIP          string     `json:"last_ip"`
	LastMAC         string     `json:"last_mac"`
	Status          string     `json:"status"` // stable | flapping | offline | high_latency | unknown
	ReconnectCount  int        `json:"reconnect_count"`
	DisconnectCount int        `json:"disconnect_count"`
	TotalOfflineSec int        `json:"total_offline_sec"`
	WorstDropSec    int        `json:"worst_drop_sec"`
	LastSeen        *time.Time `json:"last_seen"`
	LastIssue       string     `json:"last_issue"`
	LastLocation    string     `json:"last_location,omitempty"`
	LiveStatus      string     `json:"live_status"` // "online" | "offline" | "" (unknown)
	StatusCode      int        `json:"status_code"` // raw RDS newStatus (0 if unknown)
	// Authoritative data from RDS Core (t_robotstatusrecord).
	StatusLabel string      `json:"status_label"` // "Idle", "Moving", "Offline"...
	Odo         float64     `json:"odo"`          // cumulative meters
	TodayOdo    float64     `json:"today_odo"`    // meters today
	DataSource  string      `json:"data_source"`  // "rds_core" | "logs"
	Drops       []dropEvent `json:"drops"`
}

// classifyRobotStatus turns the connectivity numbers into the human status the
// task asks for (stable/flapping/high-latency/offline). Flapping = many short
// reconnects in a short window; offline = an unresolved drop in the window.
func classifyRobotStatus(drops []dropEvent, lastSeen *time.Time) string {
	if len(drops) == 0 {
		if lastSeen == nil {
			return "unknown"
		}
		return "stable"
	}
	// Unresolved (still offline)?
	for _, d := range drops {
		if !d.Resolved {
			return "offline"
		}
	}
	// Flapping: >=3 drops within any 8-minute window.
	if b := detectBurstGo(drops); b {
		return "flapping"
	}
	return "stable"
}

// detectBurstGo mirrors the frontend detectBurst: >=3 drops within an 8-min window.
func detectBurstGo(drops []dropEvent) bool {
	if len(drops) < 3 {
		return false
	}
	starts := make([]int64, 0, len(drops))
	for _, d := range drops {
		starts = append(starts, d.Start.Unix())
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i] < starts[j] })
	const windowSec = 8 * 60
	best := 1
	for i := 0; i < len(starts); i++ {
		n := 1
		for j := i + 1; j < len(starts) && starts[j]-starts[i] <= windowSec; j++ {
			n++
		}
		if n > best {
			best = n
		}
	}
	return best >= 3
}

// RobotSummary returns the per-robot drill-down: connectivity stats, classified
// status, last known location, and the recent drop timeline (with locations).
// Query: plant (optional, "" = match by name only), robot (required).
func (h *AMRHandler) RobotSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	plant := r.URL.Query().Get("plant")
	robot := normaliseAMRName(r.URL.Query().Get("robot"))
	if robot == "" {
		jsonError(w, "robot parameter required", http.StatusBadRequest)
		return
	}
	win := parseWindow(r)

	// Fleet status gives us the connectivity stats for this robot (and confirms
	// the plant if the caller didn't pass one).
	fleet := h.fleetSnapshot(ctx, plant, win)

	locs := h.loadLocationEvents(ctx, plant, win)
	drops := h.dropsFor(ctx, plant, robot, win)
	attachLocations(drops, locs)

	// Find this robot's row in the fleet snapshot. If plant wasn't specified,
	// pick the plant with the most activity for this robot.
	var s *AMRStatus
	for _, a := range fleet {
		if a.Name == robot {
			if s == nil || a.TotalOfflineSec > s.TotalOfflineSec {
				s = a
			}
		}
	}

	lastSeen := time.Now()
	summaryPlant := plant
	lastIP := ""
	lastMAC := ""
	reconnects, disconnects, totalOff, worst := 0, 0, 0, 0
	lastIssue := ""
	if s != nil {
		lastSeen = derefTime(s.LastSeen, time.Now())
		summaryPlant = s.Plant
		lastIP = s.LastIP
		lastMAC = s.LastMAC
		reconnects = s.ReconnectCount
		disconnects = s.DisconnectCount
		totalOff = s.TotalOfflineSec
		worst = s.WorstDropSec
		lastIssue = s.LastIssue
	}

	lastIP = ""

	// Status: live RDS Core state wins when available (online robots are "stable"
	// even with historical drops); otherwise classify from the drop timeline.
	status := classifyRobotStatus(drops, &lastSeen)
	liveStr := ""
	statusCode := 0
	if ls, ok := h.liveLookup(summaryPlant, robot); ok {
		if ls.Online {
			liveStr = "online"
			statusCode = ls.Code
			status = "stable"
		} else {
			liveStr = "offline"
			statusCode = ls.Code
			status = "offline"
		}
	} else if h.fetchLiveStatus(summaryPlant) != nil {
		// Live data was fetched for this plant but this robot isn't in the roster â†’ offline.
		liveStr = "offline"
		status = "offline"
	}

	// Authoritative override: if RDS Core status records exist for this robot,
	// use the real status code/label/odometer and clear the unreliable IP. The
	// legacy log-derived fields above are kept only as a fallback when no records
	// exist (e.g. before the rds_robot_status collection has run).
	statusLabel := ""
	odo, todayOdo := 0.0, 0.0
	dataSource := "logs"
	if auth := h.authoritativeRobotStatus(ctx, summaryPlant, robot); auth != nil {
		status = auth.Status
		statusCode = auth.StatusCode
		statusLabel = auth.StatusLabel
		liveStr = auth.LiveStatus
		odo = auth.Odo
		todayOdo = auth.TodayOdo
		lastSeen = derefTime(auth.LastSeen, lastSeen)
		if auth.LastIssue != "" {
			lastIssue = auth.LastIssue
		}
		dataSource = "rds_core"
	}
	if ips := h.fetchCoreRobotIPs(summaryPlant); len(ips) > 0 {
		if ip := ips[fleetKey(summaryPlant, robot)]; ip != "" {
			lastIP = ip
		}
	}
	if core := h.fetchCoreRobotStatuses(summaryPlant); len(core) > 0 {
		if st, ok := core[fleetKey(summaryPlant, robot)]; ok && st.MAC != "" {
			lastMAC = st.MAC
		}
	}

	jsonOK(w, RobotSummary{
		Name:            robot,
		Plant:           summaryPlant,
		LastIP:          lastIP,
		LastMAC:         lastMAC,
		Status:          status,
		ReconnectCount:  reconnects,
		DisconnectCount: disconnects,
		TotalOfflineSec: totalOff,
		WorstDropSec:    worst,
		LastSeen:        &lastSeen,
		LastIssue:       truncateStr(lastIssue, 160),
		LastLocation:    lastLocationBefore(locs, summaryPlant, robot, lastSeen),
		Drops:           drops,
		LiveStatus:      liveStr,
		StatusCode:      statusCode,
		StatusLabel:     statusLabel,
		Odo:             odo,
		TodayOdo:        todayOdo,
		DataSource:      dataSource,
	})
}

// authoritativeRobotStatus returns the authoritative per-robot status from RDS
// Core (robot_status_records), or nil if none exists. Used to override the
// log-derived fields in RobotSummary with real data.
func (h *AMRHandler) authoritativeRobotStatus(ctx context.Context, plant, robot string) *AMRStatus {
	fleet := h.fleetFromStatusRecords(ctx, plant, 10*time.Minute)
	for _, a := range fleet {
		if a.Name == robot {
			return a
		}
	}
	return nil
}

func derefTime(t *time.Time, fallback time.Time) time.Time {
	if t == nil {
		return fallback
	}
	return *t
}

// dropsFor returns the drop timeline scoped to a plant and robot. Exposed so the
// summary handler reuses the same authoritative source as /amr/timeline.
func (h *AMRHandler) dropsFor(ctx context.Context, plant, robot string, win amrWindow) []dropEvent {
	if drops := h.statusTransitionDrops(ctx, plant, robot, win); drops != nil {
		return drops
	}

	events := h.loadPortEvents(ctx, plant, win)
	drops := buildDropTimeline(events)
	if robot == "" {
		return drops
	}
	want := normaliseAMRName(robot)
	out := drops[:0]
	for _, d := range drops {
		if normaliseAMRName(d.Robot) == want {
			out = append(out, d)
		}
	}
	return out
}

// BadZone is one map-point (LM/AP/PP) with the drops + robots attributed to it.
// When several distinct AMRs drop near the same point, that points to a coverage
// or Wi-Fi issue at that location rather than a single-robot fault.
type BadZone struct {
	Location     string    `json:"location"`
	Plant        string    `json:"plant"`
	DropCount    int       `json:"drop_count"`
	Robots       []string  `json:"robots"`
	WorstDropSec int       `json:"worst_drop_sec"`
	LastDrop     time.Time `json:"last_drop"`
}

// BadZones aggregates drops by their attributed map-point. Drops with no location
// are excluded. Result is sorted worst-first (most drops, then worst single drop).
// A location with multiple distinct robots is the "multiple AMRs dropping in the
// same area â†’ likely Wi-Fi/coverage" signal the task wants.
func (h *AMRHandler) BadZones(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	plant := r.URL.Query().Get("plant")
	win := parseWindow(r)

	events := h.loadPortEvents(ctx, plant, win)
	drops := buildDropTimeline(events)
	attachLocations(drops, h.loadLocationEvents(ctx, plant, win))

	type agg struct {
		loc    string
		plant  string
		drops  int
		worst  int
		last   time.Time
		robots map[string]bool
	}
	byLoc := map[string]*agg{}
	for _, d := range drops {
		if d.Location == "" {
			continue
		}
		key := d.Plant + "|" + d.Location
		a, ok := byLoc[key]
		if !ok {
			a = &agg{loc: d.Location, plant: d.Plant, robots: map[string]bool{}, last: d.Start}
			byLoc[key] = a
		}
		a.drops++
		if d.DurationSec > a.worst {
			a.worst = d.DurationSec
		}
		if d.Start.After(a.last) {
			a.last = d.Start
		}
		a.robots[d.Robot] = true
	}

	out := make([]BadZone, 0, len(byLoc))
	for _, a := range byLoc {
		robots := make([]string, 0, len(a.robots))
		for rk := range a.robots {
			robots = append(robots, rk)
		}
		sort.Strings(robots)
		out = append(out, BadZone{
			Location: a.loc, Plant: a.plant, DropCount: a.drops,
			Robots: robots, WorstDropSec: a.worst, LastDrop: a.last,
		})
	}
	// Worst first: most drops, then worst single drop, then most distinct robots.
	sort.Slice(out, func(i, j int) bool {
		if out[i].DropCount != out[j].DropCount {
			return out[i].DropCount > out[j].DropCount
		}
		if out[i].WorstDropSec != out[j].WorstDropSec {
			return out[i].WorstDropSec > out[j].WorstDropSec
		}
		return len(out[i].Robots) > len(out[j].Robots)
	})
	jsonOK(w, out)
}

// so RobotSummary can reuse it without going through HTTP. It runs the full
// FleetStatus pipeline with the given plant filter.
func (h *AMRHandler) fleetSnapshot(ctx context.Context, plant string, win amrWindow) []*AMRStatus {
	// Defer to the real handler logic by calling it into a recorder is fragile;
	// instead we re-query the same sources the handler uses. To avoid duplicating
	// FleetStatus's body, we accept a small duplication: rebuild just the
	// connectivity view (Source 3b), which is what carries reconnect/offline.
	events := h.loadPortEvents(ctx, plant, win)
	connMap := pairPortEvents(events)
	out := make([]*AMRStatus, 0, len(connMap))
	for _, c := range connMap {
		if c.lastSeen == nil {
			continue
		}
		s := &AMRStatus{
			Name:            c.amrName,
			Plant:           c.plant,
			ReconnectCount:  c.reconnects,
			DisconnectCount: c.reconnects,
			TotalOfflineSec: c.totalOff,
			WorstDropSec:    c.worstDrop,
			LastSeen:        c.lastSeen,
			LastIssue:       truncateStr(c.lastMsg, 120),
		}
		s.Status = deriveStatus(s)
		out = append(out, s)
	}
	return out
}

// fleetKey is the dedup key for the fleet map: plant + AMR name. The same AMR
// number exists at multiple plants (AMR-04 @ Springfield vs AMR-04 @ Hopkinsville),
// so keying on name alone would make one silently overwrite the other in the
// All-Plants view. Both are distinct robots and must be kept separate.
func fleetKey(plant, amrName string) string {
	return plant + "|" + amrName
}

// parseWindow reads optional from/to query params into an amrWindow. Accepts RFC3339
// or "YYYY-MM-DD" forms; "to" is treated as inclusive end-of-day when given as a
// date. Empty/invalid â†’ zero (unbounded), so existing callers behave as before.
func parseWindow(r *http.Request) amrWindow {
	var w amrWindow
	if v := r.URL.Query().Get("from"); v != "" {
		if t, ok := parseFlexibleTime(v); ok {
			w.from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, ok := parseFlexibleTime(v); ok {
			w.to = t
		}
	}
	return w
}

// parseFlexibleTime accepts RFC3339 or a bare "YYYY-MM-DD" date. A bare date for
// "to" is extended to end-of-day so the day is inclusive.
func parseFlexibleTime(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			if layout == "2006-01-02" {
				return t.Add(24*time.Hour - time.Second), true
			}
			return t, true
		}
	}
	return time.Time{}, false
}

// normaliseAMRName uppercases and standardises to "AMR-XX" form.
func normaliseAMRName(raw string) string {
	upper := strings.ToUpper(strings.TrimSpace(raw))
	re := regexp.MustCompile(`(?i)AMR[-_]?(\d+)`)
	if m := re.FindStringSubmatch(upper); len(m) == 2 {
		digits := strings.TrimLeft(m[1], "0")
		if digits == "" {
			digits = "0"
		}
		if len(digits) == 1 {
			digits = "0" + digits
		}
		return "AMR-" + digits
	}
	return upper
}

func deriveStatus(s *AMRStatus) string {
	if s.DisconnectCount > 0 || s.ErrorCount > 0 {
		return "error"
	}
	if s.WarnCount > 0 {
		return "warning"
	}
	if s.TotalEvents > 0 {
		return "ok"
	}
	return "unknown"
}

func statusRank(s string) int {
	switch s {
	case "error":
		return 3
	case "warning":
		return 2
	case "ok":
		return 1
	default:
		return 0
	}
}

func firstOf(a, b *string) string {
	if a != nil && *a != "" {
		return *a
	}
	if b != nil {
		return *b
	}
	return ""
}

func truncateStr(s string, n int) string {
	if s == "" {
		return s
	}
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "â€¦"
}

// portEvent is one parsed Roboshop log line: a connect ("on") or disconnect
// ("off") for a specific AMR, with the IP and plant derived from its server row.
type portEvent struct {
	ts     time.Time
	state  string // "off" (disconnect) or "on" (connect)
	amrKey string // normalized "AMR-NN"
	plant  string
	ip     string
	msg    string // original message, kept for last-issue display
}

// amrWindow is an optional event-time filter [from, to] applied to the bracket
// event timestamp (NOT the ingest timestamp). Zero values mean unbounded on that
// side. Used by the AMR endpoints when the UI asks for a time range.
type amrWindow struct {
	from time.Time
	to   time.Time
}

// inWindow reports whether t falls within w (unbounded sides pass through).
func (w amrWindow) inWindow(t time.Time) bool {
	if !w.from.IsZero() && t.Before(w.from) {
		return false
	}
	if !w.to.IsZero() && t.After(w.to) {
		return false
	}
	return true
}

// loadPortEvents reads Roboshop slotTcp events from log_events (both the
// 'roboshop_app' file logs and the 'journald_amr' journal lines â€” Springfield
// only emits the latter), maps the embedded slot tag (19200+N â†’ AMR-N) to an AMR
// name, and classifies each row as a connect/disconnect. plant=="" means all
// plants. win filters by event time (zero = unbounded). Capped to bound the scan.
func (h *AMRHandler) loadPortEvents(ctx context.Context, plant string, win amrWindow) []portEvent {
	rows, err := h.db.Query(ctx, `
		SELECT
			le.message,
			le.timestamp,
			le.event_type,
			COALESCE(s.name,'')   AS server_name,
			COALESCE(s.host,'')   AS server_host
		FROM log_events le
		LEFT JOIN servers s ON s.id = le.server_id
		WHERE le.source IN ('roboshop_app', 'journald_amr')
		  AND le.message ~ '\[19[0-9]{3}\]\[(info|warning|error)\]'
		  AND (le.message ~* '(SocketState|slotTcpStateChange|ConnectedState|UnconnectedState|ClosingState)'
		       OR le.event_type IN ('robot_offline','robot_online'))
		ORDER BY le.timestamp
		LIMIT 10000`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var events []portEvent
	for rows.Next() {
		var msg, evType, srvName, srvHost string
		var ts time.Time
		if err := rows.Scan(&msg, &ts, &evType, &srvName, &srvHost); err != nil {
			continue
		}
		inferredPlant := config.PlantForServer(srvName, srvHost)
		if plant != "" && inferredPlant != "" && inferredPlant != plant {
			continue
		}
		// AMR identity: the "[Roboshop][NNNN]" slot tag. 19200+N â†’ AMR-N. We no
		// longer key off the [Server:...:port] port, which is the Roboshop
		// listener port (fixed), not a per-AMR port.
		tm := roboshopTagRe.FindStringSubmatch(msg)
		if len(tm) != 2 {
			continue
		}
		tag := 0
		fmt.Sscanf(tm[1], "%d", &tag)
		if tag < 19200 || tag > 19299 {
			continue
		}
		key := fmt.Sprintf("AMR-%02d", tag-19200)

		// Event clock: prefer the bracket timestamp in the message (the DB
		// timestamp is ingest-time and shared by whole batches); fall back to it.
		evts, ok := parseRoboshopEventTS(msg)
		if !ok {
			evts = ts
		}
		// Apply the optional event-time window (zero = unbounded).
		if !win.inWindow(evts) {
			continue
		}

		// AMR IP (best-effort from this log line): the AMR connects from the
		// [Server:IP:port] side; [Local:...] is the FleetManager itself, so we
		// explicitly prefer Server and reject the FleetManager's own host IP.
		// This is legacy drop evidence only; fleet IP comes from /robotsStatus.
		ip := ""
		if sm := serverIPRe.FindStringSubmatch(msg); len(sm) == 2 && sm[1] != srvHost {
			ip = sm[1]
		} else if lm := localIPRe.FindStringSubmatch(msg); len(lm) == 2 && lm[1] != srvHost {
			ip = lm[1]
		}

		state := ""
		lower := strings.ToLower(msg)
		switch {
		case strings.Contains(lower, "unconnectedstate"),
			strings.Contains(lower, "closingstate"),
			strings.Contains(lower, "not connected"),
			strings.Contains(lower, "remote host closed"),
			evType == "robot_offline":
			state = "off"
		case strings.Contains(lower, "connectedstate"),
			evType == "robot_online":
			state = "on"
		}
		if state == "" {
			continue
		}
		events = append(events, portEvent{ts: evts, state: state, amrKey: key, plant: inferredPlant, ip: ip, msg: msg})
	}
	return dedupePortEvents(events)
}

// dedupePortEvents sorts events by event time and collapses the duplicated
// transitions that Roboshop emits (the same socket change is logged from several
// log files / repeated lines within the same second). Per AMR it keeps only
// actual state CHANGES: a run of identical states becomes one event, so
// reconnect counts and outage durations aren't inflated by the duplication.
func dedupePortEvents(events []portEvent) []portEvent {
	if len(events) == 0 {
		return events
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].ts.Before(events[j].ts) })

	out := make([]portEvent, 0, len(events))
	// Last emitted state per AMR-per-plant (same robot number at different plants
	// is a separate transition stream and must not collapse into each other).
	last := map[string]string{}
	for _, ev := range events {
		fk := fleetKey(ev.plant, ev.amrKey)
		if prev, ok := last[fk]; ok && prev == ev.state {
			continue // same state as the previous emitted event for this AMR
		}
		last[fk] = ev.state
		out = append(out, ev)
	}
	return out
}

// amrConn is the aggregated connectivity profile for one AMR (at one plant), built
// by pairing disconnectâ†’connect transitions.
type amrConn struct {
	amrName     string
	reconnects  int
	totalOff    int
	worstDrop   int
	lastOffTime *time.Time // nil means currently connected (or never seen off)
	lastIP      string
	plant       string
	lastSeen    *time.Time
	lastMsg     string
}

// pairPortEvents walks the chronological connect/disconnect stream and computes,
// per AMR-per-plant, reconnect count + cumulative offline seconds + worst single
// drop. The map is keyed by fleetKey (plant + AMR name) so the same robot number
// at different plants stays distinct.
func pairPortEvents(events []portEvent) map[string]*amrConn {
	connMap := map[string]*amrConn{}
	for _, ev := range events {
		fk := fleetKey(ev.plant, ev.amrKey)
		c, ok := connMap[fk]
		if !ok {
			c = &amrConn{amrName: ev.amrKey}
			connMap[fk] = c
		}
		if ev.ip != "" {
			c.lastIP = ev.ip
		}
		if ev.plant != "" {
			c.plant = ev.plant
		}
		c.lastSeen = &ev.ts
		c.lastMsg = ev.msg
		if ev.state == "off" {
			c.reconnects++
			t := ev.ts
			c.lastOffTime = &t
		} else if ev.state == "on" && c.lastOffTime != nil {
			secs := int(ev.ts.Sub(*c.lastOffTime).Seconds())
			if secs > 0 {
				c.totalOff += secs
				if secs > c.worstDrop {
					c.worstDrop = secs
				}
			}
			c.lastOffTime = nil
		}
	}
	return connMap
}

// dropEvent is one completed outage: a disconnect followed (eventually) by a
// reconnect, with the outage duration. Unresolved drops (disconnect with no
// matching reconnect in the window) are returned with durationSec=0 and
// resolved=false so the UI can flag them as "still/last seen offline".
type dropEvent struct {
	Plant       string     `json:"plant"`
	Robot       string     `json:"robot"`
	State       string     `json:"state"` // "offline" (recovered) | "offline_open" (unresolved)
	Start       time.Time  `json:"start"`
	End         *time.Time `json:"end,omitempty"`
	DurationSec int        `json:"duration_sec"`
	Resolved    bool       `json:"resolved"`
	IP          string     `json:"ip"`
	Message     string     `json:"message"`
	// Location is the map point (LM/AP/PP token) the robot was navigating to most
	// recently before this drop â€” "where it likely was when it lost connection".
	// Empty when no gotarget/navigation event precedes the drop for that robot.
	Location string `json:"location,omitempty"`
	// PlainEnglish is a one-line human explanation of the drop, generated by the
	// same engine that explains /logs rows. Lets the timeline show raw + explanation.
	PlainEnglish string `json:"plain_english,omitempty"`
}

// buildDropTimeline turns the chronological event stream into a flat, per-robot
// list of outages. plant=="" â†’ all plants. Returns newest-first.
func buildDropTimeline(events []portEvent) []dropEvent {
	// Track the open "off" per robot as we walk chronologically.
	type openDrop struct {
		start time.Time
		ip    string
		msg   string
		plant string
	}
	open := map[string]*openDrop{}
	var out []dropEvent
	for _, ev := range events {
		if ev.state == "off" {
			open[ev.amrKey] = &openDrop{start: ev.ts, ip: ev.ip, msg: ev.msg, plant: ev.plant}
		} else if ev.state == "on" {
			if d, ok := open[ev.amrKey]; ok {
				end := ev.ts
				out = append(out, dropEvent{
					Plant: d.plant, Robot: ev.amrKey, State: "offline",
					Start: d.start, End: &end,
					DurationSec: maxSecs(0, int(end.Sub(d.start).Seconds())),
					Resolved:    true, IP: d.ip, Message: d.msg,
					PlainEnglish: dropPlainEnglish(d.msg),
				})
				delete(open, ev.amrKey)
			}
		}
	}
	// Unresolved disconnects (no reconnect seen) â€” surface as still offline.
	for robot, d := range open {
		out = append(out, dropEvent{
			Plant: d.plant, Robot: robot, State: "offline_open",
			Start: d.start, DurationSec: 0, Resolved: false, IP: d.ip, Message: d.msg,
			PlainEnglish: dropPlainEnglish(d.msg),
		})
	}
	// Newest first.
	sort.Slice(out, func(i, j int) bool { return out[i].Start.After(out[j].Start) })
	return out
}

// maxSecs returns b when a<b, else a â€” a tiny max() that avoids importing the
// new builtins everywhere (kept consistent with the file's minimal-dependency style).
func maxSecs(a, b int) int {
	if a < b {
		return b
	}
	return a
}

// dropPlainEnglish renders a one-line human explanation for a drop's raw message
// by routing it through the shared /logs explainer as a robot_offline event. Keeps
// AMR-specific wording ("Robot X closed the connection unexpectedly", etc.)
// consistent with the rest of the dashboard.
func dropPlainEnglish(msg string) string {
	if strings.TrimSpace(msg) == "" {
		return ""
	}
	ev := models.LogEvent{EventType: "robot_offline", Severity: "high", Message: msg, RawLine: msg, Source: "roboshop_app"}
	return PlainEnglishLog(ev)
}

// locEvent is a robot navigation target: the map point (LM/AP/PP) a robot was
// sent to, and when. Used to answer "where was the robot when it dropped?".
type locEvent struct {
	ts       time.Time
	plant    string
	amrKey   string
	location string // e.g. "LM113", "AP154", "PP180"
}

// gotargetIDRe extracts the target id from a robot_task_gotarget_req line's
// JSON payload, e.g. ...Send:[3051]robot_task_gotarget_req ... {"id":"LM113"}
var gotargetIDRe = regexp.MustCompile(`"id"\s*:\s*"([A-Za-z]{1,4}[0-9]{1,5})"`)

// loadLocationEvents reads robot navigation (gotarget) events from log_events
// for both roboshop_app and journald_amr. Each carries the AMR tag (so we know
// which robot) and a target id (LM/AP/PP â€” where it was heading). plant==""
// means all plants. Capped to bound the scan.
func (h *AMRHandler) loadLocationEvents(ctx context.Context, plant string, win amrWindow) []locEvent {
	rows, err := h.db.Query(ctx, `
		SELECT
			le.message,
			le.timestamp,
			COALESCE(s.name,'')   AS server_name,
			COALESCE(s.host,'')   AS server_host
		FROM log_events le
		LEFT JOIN servers s ON s.id = le.server_id
		WHERE le.source IN ('roboshop_app', 'journald_amr', 'syslog')
		  AND le.message ~* 'gotarget'
		ORDER BY le.timestamp
		LIMIT 8000`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []locEvent
	for rows.Next() {
		var msg, srvName, srvHost string
		var ts time.Time
		if err := rows.Scan(&msg, &ts, &srvName, &srvHost); err != nil {
			continue
		}
		inferredPlant := config.PlantForServer(srvName, srvHost)
		if plant != "" && inferredPlant != "" && inferredPlant != plant {
			continue
		}
		// AMR identity via the slot tag (same flexible matcher as for drops).
		tm := roboshopTagRe.FindStringSubmatch(msg)
		if len(tm) != 2 {
			continue
		}
		tag := 0
		fmt.Sscanf(tm[1], "%d", &tag)
		if tag < 19200 || tag > 19299 {
			continue
		}
		key := fmt.Sprintf("AMR-%02d", tag-19200)

		// Target id from the JSON payload.
		idm := gotargetIDRe.FindStringSubmatch(msg)
		if len(idm) != 2 {
			continue
		}
		loc := strings.ToUpper(idm[1])

		// Prefer the bracket event timestamp over ingest-time.
		evts, ok := parseRoboshopEventTS(msg)
		if !ok {
			evts = ts
		}
		if !win.inWindow(evts) {
			continue
		}
		out = append(out, locEvent{ts: evts, plant: inferredPlant, amrKey: key, location: loc})
	}
	return out
}

// attachLocations annotates each drop with the map point its robot was navigating
// to most recently before the disconnect. We walk the per-robot location stream
// and, for every drop, take the latest location at-or-before the drop start.
// Drops with no preceding location keep Location="". Sorted by location time.
func attachLocations(drops []dropEvent, locs []locEvent) {
	if len(locs) == 0 || len(drops) == 0 {
		return
	}
	// Index locations by robot, sorted ascending by time, for binary search.
	byRobot := map[string][]locEvent{}
	for _, l := range locs {
		byRobot[l.amrKey] = append(byRobot[l.amrKey], l)
	}
	for r := range byRobot {
		sort.Slice(byRobot[r], func(i, j int) bool { return byRobot[r][i].ts.Before(byRobot[r][j].ts) })
	}
	for i := range drops {
		ls := byRobot[drops[i].Robot]
		if len(ls) == 0 {
			continue
		}
		// Largest index whose ts <= drop start.
		lo, hi := 0, len(ls)-1
		best := -1
		for lo <= hi {
			mid := (lo + hi) / 2
			if ls[mid].ts.Before(drops[i].Start) || ls[mid].ts.Equal(drops[i].Start) {
				best = mid
				lo = mid + 1
			} else {
				hi = mid - 1
			}
		}
		if best >= 0 {
			drops[i].Location = ls[best].location
		}
	}
}

// lastLocationBefore returns the most recent navigation location for a robot at
// or before `t`, or "" if none. Used for the per-robot summary.
func lastLocationBefore(locs []locEvent, plant, robot string, t time.Time) string {
	var match locEvent
	found := false
	for _, l := range locs {
		if robot != "" && l.amrKey != robot {
			continue
		}
		if plant != "" && l.plant != "" && l.plant != plant {
			continue
		}
		if (l.ts.Before(t) || l.ts.Equal(t)) && (!found || l.ts.After(match.ts)) {
			match = l
			found = true
		}
	}
	if !found {
		return ""
	}
	return match.location
}
