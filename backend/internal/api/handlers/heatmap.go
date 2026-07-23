package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HeatmapHandler struct{ db *pgxpool.Pool }

func NewHeatmapHandler(db *pgxpool.Pool) *HeatmapHandler { return &HeatmapHandler{db: db} }

type SurveyRoutePoint struct {
	ID              int64     `json:"id,omitempty"`
	SessionID       int64     `json:"session_id"`
	PlantID         string    `json:"plant_id"`
	MapID           string    `json:"map_id"`
	MapVersion      string    `json:"map_version"`
	AMRID           string    `json:"amr_id"`
	Timestamp       time.Time `json:"timestamp"`
	X               float64   `json:"x"`
	Y               float64   `json:"y"`
	Heading         *float64  `json:"heading,omitempty"`
	Moving          bool      `json:"moving"`
	Speed           *float64  `json:"speed,omitempty"`
	Connected       bool      `json:"connected"`
	NearestLocation string    `json:"nearest_location,omitempty"`
}

func (h *HeatmapHandler) SaveRoutePoint(w http.ResponseWriter, r *http.Request) {
	var p SurveyRoutePoint
	if json.NewDecoder(r.Body).Decode(&p) != nil {
		jsonError(w, "invalid route point", 400)
		return
	}
	p.PlantID, p.MapID, p.MapVersion, p.AMRID, p.NearestLocation = strings.TrimSpace(p.PlantID), strings.TrimSpace(p.MapID), strings.TrimSpace(p.MapVersion), strings.TrimSpace(p.AMRID), strings.TrimSpace(p.NearestLocation)
	if p.SessionID <= 0 || p.PlantID == "" || p.MapID == "" || p.MapVersion == "" || p.AMRID == "" || p.Timestamp.IsZero() {
		jsonError(w, "session, plant, map, map version, AMR and timestamp are required", 422)
		return
	}
	if !isFinite(p.X) || !isFinite(p.Y) {
		jsonError(w, "finite X/Y coordinates are required", 422)
		return
	}
	var sessionPlant, sessionMap, sessionVersion, sessionAMRs, status string
	if err := h.db.QueryRow(r.Context(), `SELECT plant_id,map_id,map_version,amr_id,status FROM wifi_scan_sessions WHERE id=$1`, p.SessionID).Scan(&sessionPlant, &sessionMap, &sessionVersion, &sessionAMRs, &status); err != nil {
		jsonError(w, "recording session not found", 404)
		return
	}
	if status != "running" || !strings.EqualFold(sessionPlant, p.PlantID) || sessionMap != p.MapID || sessionVersion != p.MapVersion || !containsCSVValue(sessionAMRs, p.AMRID) {
		jsonError(w, "route point does not match the active recording session", 422)
		return
	}
	raw := fmt.Sprintf("%d|%s|%d|%.4f|%.4f", p.SessionID, strings.ToLower(p.AMRID), p.Timestamp.Unix(), p.X, p.Y)
	sum := sha256.Sum256([]byte(raw))
	fp := hex.EncodeToString(sum[:])
	err := h.db.QueryRow(r.Context(), `INSERT INTO wifi_survey_route_points(session_id,plant_id,map_id,map_version,amr_id,timestamp,x,y,heading,moving,speed,connected,nearest_location,fingerprint) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT(fingerprint) DO NOTHING RETURNING id`, p.SessionID, p.PlantID, p.MapID, p.MapVersion, p.AMRID, p.Timestamp, p.X, p.Y, p.Heading, p.Moving, p.Speed, p.Connected, p.NearestLocation, fp).Scan(&p.ID)
	if err == pgx.ErrNoRows {
		jsonOK(w, map[string]any{"saved": false, "duplicate": true})
		return
	}
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, map[string]any{"saved": true, "point": p})
}

func containsCSVValue(values, target string) bool {
	for _, value := range strings.Split(values, ",") {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

type WifiScanPoint struct {
	ID                int64     `json:"id,omitempty"`
	SessionID         *int64    `json:"session_id,omitempty"`
	PlantID           string    `json:"plant_id"`
	SourcePlant       string    `json:"source_plant,omitempty"`
	MapID             string    `json:"map_id"`
	MapVersion        string    `json:"map_version"`
	AMRID             string    `json:"amr_id"`
	WifiAMRID         string    `json:"wifi_amr_id,omitempty"`
	Timestamp         time.Time `json:"timestamp"`
	X                 float64   `json:"x"`
	Y                 float64   `json:"y"`
	Heading           *float64  `json:"heading"`
	Moving            bool      `json:"moving"`
	Speed             *float64  `json:"speed"`
	RSSIDBM           int       `json:"rssi_dbm"`
	SNRDB             *float64  `json:"snr_db"`
	NoiseDBM          *float64  `json:"noise_dbm"`
	SSID              *string   `json:"ssid"`
	BSSID             string    `json:"bssid"`
	PreviousBSSID     *string   `json:"previous_bssid"`
	Channel           int       `json:"channel"`
	FrequencyMHz      *int      `json:"frequency_mhz"`
	Band              string    `json:"band"`
	Connected         bool      `json:"connected"`
	DisconnectEvent   bool      `json:"disconnect_event"`
	RoamEvent         bool      `json:"roam_event"`
	LatencyMS         *float64  `json:"latency_ms"`
	PacketLossPercent *float64  `json:"packet_loss_percent"`
	SourceID          string    `json:"source_id"`
	PositionTimestamp time.Time `json:"position_timestamp"`
	WifiTimestamp     time.Time `json:"wifi_timestamp"`
	CreatedAt         time.Time `json:"created_at,omitempty"`
}

func validateScanPoint(p *WifiScanPoint, tolerance time.Duration) error {
	p.PlantID, p.SourcePlant, p.MapID, p.MapVersion, p.AMRID, p.WifiAMRID = strings.TrimSpace(p.PlantID), strings.TrimSpace(p.SourcePlant), strings.TrimSpace(p.MapID), strings.TrimSpace(p.MapVersion), strings.TrimSpace(p.AMRID), strings.TrimSpace(p.WifiAMRID)
	if p.SourcePlant == "" {
		p.SourcePlant = p.PlantID
	}
	if p.WifiAMRID == "" {
		p.WifiAMRID = p.AMRID
	}
	if p.PlantID == "" || p.MapID == "" || p.MapVersion == "" || p.AMRID == "" {
		return fmt.Errorf("plant, map, map version and AMR are required")
	}
	if !strings.EqualFold(p.PlantID, p.SourcePlant) {
		return fmt.Errorf("plant mismatch: map is %s but Wi-Fi source is %s", p.PlantID, p.SourcePlant)
	}
	if !strings.EqualFold(p.AMRID, p.WifiAMRID) {
		return fmt.Errorf("AMR mismatch: position is %s but Wi-Fi reading is %s", p.AMRID, p.WifiAMRID)
	}
	if !p.Timestamp.IsZero() && p.Timestamp.Before(p.PositionTimestamp) { /* accepted; timestamps need not order */
	}
	if p.PositionTimestamp.IsZero() || p.WifiTimestamp.IsZero() {
		return fmt.Errorf("position and Wi-Fi timestamps are required")
	}
	delta := p.PositionTimestamp.Sub(p.WifiTimestamp)
	if delta < 0 {
		delta = -delta
	}
	if delta > tolerance {
		return fmt.Errorf("timestamp mismatch is %s (maximum %s)", delta.Round(time.Second), tolerance)
	}
	if !isFinite(p.X) || !isFinite(p.Y) {
		return fmt.Errorf("finite X/Y coordinates are required")
	}
	if p.RSSIDBM > 0 || p.RSSIDBM < -150 {
		return fmt.Errorf("RSSI must be between -150 and 0 dBm")
	}
	if strings.TrimSpace(p.BSSID) == "" {
		return fmt.Errorf("BSSID/AP is required")
	}
	if p.Channel < 0 {
		return fmt.Errorf("channel cannot be negative")
	}
	if strings.TrimSpace(p.Band) == "" {
		return fmt.Errorf("band is required")
	}
	if strings.TrimSpace(p.SourceID) == "" {
		return fmt.Errorf("source ID is required")
	}
	if p.Timestamp.IsZero() {
		if p.PositionTimestamp.After(p.WifiTimestamp) {
			p.Timestamp = p.PositionTimestamp
		} else {
			p.Timestamp = p.WifiTimestamp
		}
	}
	return nil
}
func isFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func fingerprint(p WifiScanPoint) string {
	// Deduplicate an exact retry of one capture, not every future observation at
	// the same position. Including session and capture time lets a new survey
	// record a stationary AMR while still rejecting a repeated request.
	sessionID := int64(0)
	if p.SessionID != nil {
		sessionID = *p.SessionID
	}
	raw := fmt.Sprintf("%d|%d|%s|%s|%s|%s|%.4f|%.4f|%d|%s|%t", sessionID, p.Timestamp.UnixNano(), strings.ToLower(p.PlantID), p.MapID, p.MapVersion, strings.ToLower(p.AMRID), p.X, p.Y, p.RSSIDBM, strings.ToLower(p.BSSID), p.Connected)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (h *HeatmapHandler) SavePoint(w http.ResponseWriter, r *http.Request) {
	var p WifiScanPoint
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		jsonError(w, "invalid scan point", 400)
		return
	}
	tolerance := 15 * time.Second
	if v := r.URL.Query().Get("tolerance_seconds"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n >= 1 && n <= 300 {
			tolerance = time.Duration(n) * time.Second
		}
	}
	if err := validateScanPoint(&p, tolerance); err != nil {
		jsonError(w, err.Error(), 422)
		return
	}
	var prevBSSID string
	var prevConnected bool
	_ = h.db.QueryRow(r.Context(), `SELECT bssid,connected FROM wifi_scan_points WHERE plant_id=$1 AND amr_id=$2 ORDER BY timestamp DESC LIMIT 1`, p.PlantID, p.AMRID).Scan(&prevBSSID, &prevConnected)
	if p.PreviousBSSID == nil && prevBSSID != "" {
		p.PreviousBSSID = &prevBSSID
	}
	p.RoamEvent = prevBSSID != "" && p.Connected && !strings.EqualFold(prevBSSID, p.BSSID)
	p.DisconnectEvent = prevBSSID != "" && prevConnected && !p.Connected
	fp := fingerprint(p)
	err := h.db.QueryRow(r.Context(), `INSERT INTO wifi_scan_points(session_id,plant_id,map_id,map_version,amr_id,timestamp,x,y,heading,moving,speed,rssi_dbm,snr_db,noise_dbm,ssid,bssid,previous_bssid,channel,frequency_mhz,band,connected,disconnect_event,roam_event,latency_ms,packet_loss_percent,source_id,position_timestamp,wifi_timestamp,fingerprint) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29) ON CONFLICT(fingerprint) DO NOTHING RETURNING id,created_at`, p.SessionID, p.PlantID, p.MapID, p.MapVersion, p.AMRID, p.Timestamp, p.X, p.Y, p.Heading, p.Moving, p.Speed, p.RSSIDBM, p.SNRDB, p.NoiseDBM, p.SSID, p.BSSID, p.PreviousBSSID, p.Channel, p.FrequencyMHz, p.Band, p.Connected, p.DisconnectEvent, p.RoamEvent, p.LatencyMS, p.PacketLossPercent, p.SourceID, p.PositionTimestamp, p.WifiTimestamp, fp).Scan(&p.ID, &p.CreatedAt)
	if err == pgx.ErrNoRows {
		jsonOK(w, map[string]any{"saved": false, "duplicate": true})
		return
	}
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	if p.SessionID != nil {
		_, _ = h.db.Exec(r.Context(), `UPDATE wifi_scan_sessions SET sample_count=sample_count+1 WHERE id=$1`, *p.SessionID)
	}
	jsonOK(w, map[string]any{"saved": true, "point": p})
}

type sessionRequest struct {
	PlantID            string `json:"plant_id"`
	MapID              string `json:"map_id"`
	MapVersion         string `json:"map_version"`
	AMRID              string `json:"amr_id"`
	MovingInterval     int    `json:"moving_interval_seconds"`
	StationaryInterval int    `json:"stationary_interval_seconds"`
	Tolerance          int    `json:"timestamp_tolerance_seconds"`
}

func (h *HeatmapHandler) StartSession(w http.ResponseWriter, r *http.Request) {
	var q sessionRequest
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		jsonError(w, "invalid session", 400)
		return
	}
	if q.MovingInterval == 0 {
		q.MovingInterval = 2
	}
	if q.StationaryInterval == 0 {
		q.StationaryInterval = 10
	}
	if q.Tolerance == 0 {
		q.Tolerance = 15
	}
	if strings.TrimSpace(q.PlantID) == "" || strings.TrimSpace(q.MapID) == "" || strings.TrimSpace(q.MapVersion) == "" {
		jsonError(w, "plant, map and map version are required", 422)
		return
	}
	user, _ := usernameFromRequest(r)
	// A browser refresh or service restart can lose the in-memory recorder before
	// it calls StopSession. A new recorder for the plant supersedes those orphaned
	// sessions, so close them before creating the new one.
	if _, err := h.db.Exec(r.Context(), `UPDATE wifi_scan_sessions SET status='stopped',stopped_at=NOW() WHERE plant_id=$1 AND status='running'`, q.PlantID); err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	var id int64
	var started time.Time
	err := h.db.QueryRow(r.Context(), `INSERT INTO wifi_scan_sessions(plant_id,map_id,map_version,amr_id,moving_interval_seconds,stationary_interval_seconds,timestamp_tolerance_seconds,started_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id,started_at`, q.PlantID, q.MapID, q.MapVersion, q.AMRID, q.MovingInterval, q.StationaryInterval, q.Tolerance, user).Scan(&id, &started)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, map[string]any{"id": id, "status": "running", "started_at": started})
}
func (h *HeatmapHandler) StopSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	tag, err := h.db.Exec(r.Context(), `UPDATE wifi_scan_sessions SET status='stopped',stopped_at=NOW() WHERE id=$1 AND status='running'`, id)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	jsonOK(w, map[string]any{"stopped": tag.RowsAffected() > 0})
}
func (h *HeatmapHandler) Sessions(w http.ResponseWriter, r *http.Request) {
	// Survey samples arrive every 2 seconds while moving and every 10 seconds
	// while stationary. If a session has produced no route or Wi-Fi activity for
	// two minutes, no recorder is alive and it must not be reported as running.
	_, err := h.db.Exec(r.Context(), `
		UPDATE wifi_scan_sessions s
		SET status='stopped', stopped_at=GREATEST(
			s.started_at,
			COALESCE((SELECT MAX(rp.timestamp) FROM wifi_survey_route_points rp WHERE rp.session_id=s.id), s.started_at),
			COALESCE((SELECT MAX(sp.timestamp) FROM wifi_scan_points sp WHERE sp.session_id=s.id), s.started_at)
		)
		WHERE s.status='running'
		  AND GREATEST(
			s.started_at,
			COALESCE((SELECT MAX(rp.timestamp) FROM wifi_survey_route_points rp WHERE rp.session_id=s.id), s.started_at),
			COALESCE((SELECT MAX(sp.timestamp) FROM wifi_scan_points sp WHERE sp.session_id=s.id), s.started_at)
		  ) < NOW() - INTERVAL '2 minutes'`)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	rows, err := h.db.Query(r.Context(), `SELECT s.id,s.plant_id,s.map_id,s.map_version,s.amr_id,s.status,s.moving_interval_seconds,s.stationary_interval_seconds,s.timestamp_tolerance_seconds,s.sample_count,(SELECT COUNT(*) FROM wifi_survey_route_points rp WHERE rp.session_id=s.id),s.started_by,s.started_at,s.stopped_at FROM wifi_scan_sessions s ORDER BY s.started_at DESC LIMIT 50`)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, count, routeCount int64
		var plant, mapID, version, amr, status, user string
		var moving, stationary, tolerance int
		var started time.Time
		var stopped *time.Time
		if rows.Scan(&id, &plant, &mapID, &version, &amr, &status, &moving, &stationary, &tolerance, &count, &routeCount, &user, &started, &stopped) == nil {
			out = append(out, map[string]any{"id": id, "plant_id": plant, "map_id": mapID, "map_version": version, "amr_id": amr, "status": status, "moving_interval_seconds": moving, "stationary_interval_seconds": stationary, "timestamp_tolerance_seconds": tolerance, "sample_count": count, "route_count": routeCount, "started_by": user, "started_at": started, "stopped_at": stopped})
		}
	}
	jsonOK(w, out)
}

type cell struct {
	X               float64   `json:"x"`
	Y               float64   `json:"y"`
	Count           int       `json:"measurement_count"`
	Average         float64   `json:"average"`
	Minimum         float64   `json:"minimum"`
	Maximum         float64   `json:"maximum"`
	Worst           float64   `json:"worst"`
	AMRCount        int       `json:"amr_count"`
	MostCommonBSSID string    `json:"most_common_bssid"`
	First           time.Time `json:"first_timestamp"`
	Last            time.Time `json:"last_timestamp"`
	Confidence      string    `json:"confidence_level"`
	AverageSNR      *float64  `json:"average_snr"`
	DisconnectCount int       `json:"disconnect_count"`
	RoamCount       int       `json:"roam_count"`
	AMRs            []string  `json:"contributing_amrs"`
}
type rawPoint struct {
	X          float64   `json:"x"`
	Y          float64   `json:"y"`
	RSSI       float64   `json:"rssi_dbm"`
	SNR        *float64  `json:"snr_db"`
	BSSID      string    `json:"bssid"`
	AMR        string    `json:"amr_id"`
	Timestamp  time.Time `json:"timestamp"`
	Disconnect bool      `json:"disconnect_event"`
	Roam       bool      `json:"roam_event"`
}
type routePoint struct {
	SessionID       int64     `json:"session_id"`
	X               float64   `json:"x"`
	Y               float64   `json:"y"`
	AMR             string    `json:"amr_id"`
	Timestamp       time.Time `json:"timestamp"`
	Moving          bool      `json:"moving"`
	Connected       bool      `json:"connected"`
	NearestLocation string    `json:"nearest_location"`
}
type accumulator struct {
	cell
	sum, snrSum  float64
	snrN         int
	amrs, bssids map[string]int
}

func (h *HeatmapHandler) Query(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	plant, mapID, version := q.Get("plant"), q.Get("map"), q.Get("map_version")
	if plant == "" || mapID == "" || version == "" {
		jsonError(w, "plant, map and map_version are required", 400)
		return
	}
	grid := 3.0
	if v, e := strconv.ParseFloat(q.Get("grid_size"), 64); e == nil && v > 0 {
		grid = v
	}
	metric := strings.ToLower(q.Get("metric"))
	if metric == "" {
		metric = "rssi"
	}
	if metric != "rssi" && metric != "snr" && metric != "disconnect" && metric != "roaming" {
		jsonError(w, "unsupported metric", 400)
		return
	}
	args := []any{plant, mapID, version}
	where := `plant_id=$1 AND map_id=$2 AND map_version=$3`
	add := func(clause string, val any) {
		args = append(args, val)
		where += fmt.Sprintf(" AND "+clause, len(args))
	}
	if v := q.Get("amr"); v != "" {
		amrs := strings.Split(v, ",")
		filtered := amrs[:0]
		for _, amr := range amrs {
			if value := strings.TrimSpace(amr); value != "" {
				filtered = append(filtered, value)
			}
		}
		if len(filtered) > 0 {
			add("amr_id=ANY($%d)", filtered)
		}
	}
	if v := q.Get("session"); v != "" {
		if sessionID, e := strconv.ParseInt(v, 10, 64); e == nil && sessionID > 0 {
			add("session_id=$%d", sessionID)
		} else {
			jsonError(w, "session must be a positive integer", 400)
			return
		}
	}
	if v := q.Get("bssid"); v != "" {
		add("bssid=$%d", v)
	}
	if v := q.Get("band"); v != "" {
		add("band=$%d", v)
	}
	if v := q.Get("channel"); v != "" {
		if n, e := strconv.Atoi(v); e == nil {
			add("channel=$%d", n)
		}
	}
	if v := q.Get("start"); v != "" {
		if t, e := time.Parse(time.RFC3339, v); e == nil {
			add("timestamp >= $%d", t)
		}
	}
	if v := q.Get("end"); v != "" {
		if t, e := time.Parse(time.RFC3339, v); e == nil {
			add("timestamp <= $%d", t)
		}
	}
	rows, err := h.db.Query(r.Context(), `SELECT x,y,rssi_dbm,snr_db,bssid,amr_id,timestamp,disconnect_event,roam_event FROM wifi_scan_points WHERE `+where+` ORDER BY timestamp`, args...)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	cells := map[string]*accumulator{}
	points := []rawPoint{}
	for rows.Next() {
		var x, y float64
		var rssi int
		var snr *float64
		var bssid, amr string
		var ts time.Time
		var disc, roam bool
		if rows.Scan(&x, &y, &rssi, &snr, &bssid, &amr, &ts, &disc, &roam) != nil {
			continue
		}
		points = append(points, rawPoint{X: x, Y: y, RSSI: float64(rssi), SNR: snr, BSSID: bssid, AMR: amr, Timestamp: ts, Disconnect: disc, Roam: roam})
		value := float64(rssi)
		if metric == "snr" {
			if snr == nil {
				continue
			}
			value = *snr
		} else if metric == "disconnect" {
			if disc {
				value = 1
			} else {
				value = 0
			}
		} else if metric == "roaming" {
			if roam {
				value = 1
			} else {
				value = 0
			}
		}
		cx := math.Floor(x/grid) * grid
		cy := math.Floor(y/grid) * grid
		key := fmt.Sprintf("%.6f:%.6f", cx, cy)
		a := cells[key]
		if a == nil {
			a = &accumulator{cell: cell{X: cx, Y: cy, Minimum: value, Maximum: value, Worst: value, First: ts, Last: ts}, amrs: map[string]int{}, bssids: map[string]int{}}
			cells[key] = a
		}
		a.Count++
		a.sum += value
		if value < a.Minimum {
			a.Minimum = value
		}
		if value > a.Maximum {
			a.Maximum = value
		}
		if metric == "rssi" || metric == "snr" {
			a.Worst = a.Minimum
		} else {
			a.Worst = a.Maximum
		}
		a.Last = ts
		a.amrs[amr]++
		a.bssids[bssid]++
		if snr != nil {
			a.snrSum += *snr
			a.snrN++
		}
		if disc {
			a.DisconnectCount++
		}
		if roam {
			a.RoamCount++
		}
	}
	out := make([]cell, 0, len(cells))
	for _, a := range cells {
		a.Average = a.sum / float64(a.Count)
		a.AMRCount = len(a.amrs)
		if a.snrN > 0 {
			v := a.snrSum / float64(a.snrN)
			a.AverageSNR = &v
		}
		for k, v := range a.amrs {
			a.AMRs = append(a.AMRs, k)
			_ = v
		}
		sort.Strings(a.AMRs)
		max := 0
		for k, v := range a.bssids {
			if v > max {
				max = v
				a.MostCommonBSSID = k
			}
		}
		if a.Count < 3 {
			a.Confidence = "unknown"
		} else if a.Count < 10 {
			a.Confidence = "low"
		} else if a.Count < 30 {
			a.Confidence = "medium"
		} else {
			a.Confidence = "high"
		}
		out = append(out, a.cell)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Y == out[j].Y {
			return out[i].X < out[j].X
		}
		return out[i].Y < out[j].Y
	})
	routeArgs := []any{plant, mapID, version}
	routeWhere := `plant_id=$1 AND map_id=$2 AND map_version=$3`
	if v := q.Get("session"); v != "" {
		if sessionID, e := strconv.ParseInt(v, 10, 64); e == nil && sessionID > 0 {
			routeArgs = append(routeArgs, sessionID)
			routeWhere += fmt.Sprintf(" AND session_id=$%d", len(routeArgs))
		}
	}
	if v := q.Get("amr"); v != "" {
		filtered := []string{}
		for _, amr := range strings.Split(v, ",") {
			if value := strings.TrimSpace(amr); value != "" {
				filtered = append(filtered, value)
			}
		}
		if len(filtered) > 0 {
			routeArgs = append(routeArgs, filtered)
			routeWhere += fmt.Sprintf(" AND amr_id=ANY($%d)", len(routeArgs))
		}
	}
	if v := q.Get("start"); v != "" {
		if t, e := time.Parse(time.RFC3339, v); e == nil {
			routeArgs = append(routeArgs, t)
			routeWhere += fmt.Sprintf(" AND timestamp >= $%d", len(routeArgs))
		}
	}
	if v := q.Get("end"); v != "" {
		if t, e := time.Parse(time.RFC3339, v); e == nil {
			routeArgs = append(routeArgs, t)
			routeWhere += fmt.Sprintf(" AND timestamp <= $%d", len(routeArgs))
		}
	}
	route := []routePoint{}
	if routeRows, routeErr := h.db.Query(r.Context(), `SELECT session_id,x,y,amr_id,timestamp,moving,connected,nearest_location FROM wifi_survey_route_points WHERE `+routeWhere+` ORDER BY timestamp`, routeArgs...); routeErr == nil {
		defer routeRows.Close()
		for routeRows.Next() {
			var point routePoint
			if routeRows.Scan(&point.SessionID, &point.X, &point.Y, &point.AMR, &point.Timestamp, &point.Moving, &point.Connected, &point.NearestLocation) == nil {
				route = append(route, point)
			}
		}
	}
	jsonOK(w, map[string]any{"cells": out, "points": points, "route_points": route, "grid_size": grid, "metric": metric})
}
