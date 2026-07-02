package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

type APIConnection struct {
	Plant     string `json:"plant"`
	BaseURL   string `json:"baseUrl"`
	CorePath  string `json:"corePath"`
	ScenePath string `json:"scenePath"`
}

type WifiSource struct {
	Plant     string `json:"plant"`
	Name      string `json:"name"`
	Method    string `json:"method"`
	Host      string `json:"host"`
	Username  string `json:"username"`
	SecretRef string `json:"secretRef"`
	Command   string `json:"command"`
	SavedAt   string `json:"savedAt"`
}

type WifiRobot struct {
	Plant string `json:"plant"`
	Name  string `json:"name"`
	IP    string `json:"ip"`
}

type WifiDiscoverRequest struct {
	Source WifiSource  `json:"source"`
	Robots []WifiRobot `json:"robots"`
}

type WifiDiscoverResult struct {
	OK      bool   `json:"ok"`
	Plant   string `json:"plant"`
	AMR     string `json:"amr"`
	Host    string `json:"host"`
	Command string `json:"command,omitempty"`
	Message string `json:"message"`
	Output  string `json:"output,omitempty"`
	RSSI    *int   `json:"rssi,omitempty"`
	SSID    string `json:"ssid,omitempty"`
	Quality string `json:"quality,omitempty"`
}

type WifiDiscoverResponse struct {
	OK      bool                 `json:"ok"`
	Message string               `json:"message"`
	Results []WifiDiscoverResult `json:"results"`
}
type WifiTestResult struct {
	OK      bool   `json:"ok"`
	Method  string `json:"method"`
	Host    string `json:"host"`
	Message string `json:"message"`
	Output  string `json:"output,omitempty"`
	RSSI    *int   `json:"rssi,omitempty"`
	SSID    string `json:"ssid,omitempty"`
	Quality string `json:"quality,omitempty"`
}
type ZoneEvent struct {
	Timestamp     string `json:"timestamp"`
	AMR           string `json:"amr"`
	RDSDelayMS    int    `json:"rds_delay_ms"`
	DurationMS    int    `json:"duration_ms"`
	ReconnectedAt string `json:"reconnected_at"`
}

type ZoneAcknowledgement struct {
	ID      int64  `json:"id"`
	ZoneID  string `json:"zone_id"`
	PlantID string `json:"plant_id"`
	AckBy   string `json:"ack_by"`
	AckAt   string `json:"ack_at"`
	Notes   string `json:"notes"`
}

type ReportEvent struct {
	Time     string `json:"time"`
	Plant    string `json:"plant"`
	AMR      string `json:"amr"`
	Zone     string `json:"zone"`
	Server   string `json:"server"`
	Host     string `json:"host"`
	VM       string `json:"vm"`
	Source   string `json:"source"`
	Category string `json:"category"`
	Severity string `json:"severity"`
	Topic    string `json:"topic"`
	Message  string `json:"message"`
}

type ReportEventsResponse struct {
	Events    []ReportEvent `json:"events"`
	UpdatedAt string        `json:"updated_at"`
}

type BadZoneEventsResponse struct {
	ZoneID          string               `json:"zone_id"`
	PlantID         string               `json:"plant_id"`
	Events          []ZoneEvent          `json:"events"`
	Acknowledgement *ZoneAcknowledgement `json:"acknowledgement,omitempty"`
}

type ZoneAckRequest struct {
	AckBy   string `json:"ack_by"`
	Notes   string `json:"notes"`
	PlantID string `json:"plant_id"`
}

type Server struct {
	configPath string
	staticDir  string
	client     *http.Client
	db         *sql.DB
	ackPath    string
}

func main() {
	port := env("PORT", "8090")
	server := &Server{
		configPath: env("DRISHTI_API_CONFIG", filepath.Join("data", "config", "api-connections.json")),
		staticDir:  env("DRISHTI_STATIC_DIR", filepath.Join("frontend", "dist")),
		client:     &http.Client{Timeout: 20 * time.Second},
		ackPath:    env("DRISHTI_ZONE_ACK_FILE", filepath.Join("data", "reports", "zone-acknowledgements.json")),
	}
	if err := server.initReportStore(); err != nil {
		log.Printf("report store warning: %v", err)
	}
	defer server.close()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", server.handleHealth)
	mux.HandleFunc("/api/connections", server.handleConnections)
	mux.HandleFunc("/api/wifi/test", server.handleWifiTest)
	mux.HandleFunc("/api/wifi/discover", server.handleWifiDiscover)
	mux.HandleFunc("/api/reports/search/suggest", server.handleReportSearchSuggest)
	mux.HandleFunc("/api/reports/events", server.handleReportEvents)
	mux.HandleFunc("/api/reports/bad-zones/", server.handleBadZoneReports)
	mux.HandleFunc("/api/plants/", server.handlePlantProxy)
	mux.HandleFunc("/", server.handleStatic)

	addr := ":" + port
	log.Printf("DRISHTI AMR Health listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, logRequest(mux)); err != nil {
		log.Fatal(err)
	}
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "drishti-amr-health"})
}

func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		connections, err := s.loadConnections()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, connections)
	case http.MethodPut:
		var connections []APIConnection
		if err := json.NewDecoder(r.Body).Decode(&connections); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		connections = normalizeConnections(connections)
		if err := s.saveConnections(connections); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, connections)
	case http.MethodPost:
		var connection APIConnection
		if err := json.NewDecoder(r.Body).Decode(&connection); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		connections, err := s.loadConnections()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		updated := upsertConnection(connections, connection)
		if err := s.saveConnections(updated); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	default:
		w.Header().Set("Allow", "GET, PUT, POST")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed", r.Method))
	}
}

func (s *Server) handleWifiTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed", r.Method))
		return
	}
	var source WifiSource
	if err := json.NewDecoder(r.Body).Decode(&source); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, status := s.testWifiSource(source)
	writeJSON(w, status, result)
}

func (s *Server) handleWifiDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed", r.Method))
		return
	}
	var request WifiDiscoverRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, status := s.discoverWifiRSSI(request)
	writeJSON(w, status, response)
}
func (s *Server) handlePlantProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed", r.Method))
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/plants/"), "/")
	if len(parts) != 3 || parts[1] != "rds" {
		writeError(w, http.StatusNotFound, errors.New("unknown plant API route"))
		return
	}
	plant, endpoint := parts[0], parts[2]
	connections, err := s.loadConnections()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	connection, ok := findConnection(connections, plant)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("no API connection configured for plant %q", plant))
		return
	}
	path := connection.CorePath
	if endpoint == "scene" {
		path = connection.ScenePath
	} else if endpoint != "core" {
		writeError(w, http.StatusNotFound, errors.New("unknown RDS endpoint"))
		return
	}
	target, err := joinURL(connection.BaseURL, path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	body, contentType, err := s.fetch(target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if r.URL.Query().Get("save") == "1" {
		if err := saveSnapshot(plant, endpoint, body); err != nil {
			log.Printf("snapshot save failed: %v", err)
		}
	}
	if contentType == "" {
		contentType = "application/json"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) initReportStore() error {
	databaseURL := strings.TrimSpace(os.Getenv("DRISHTI_DATABASE_URL"))
	if databaseURL == "" {
		return nil
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS zone_acknowledgements (
		id SERIAL PRIMARY KEY,
		zone_id TEXT NOT NULL,
		plant_id TEXT NOT NULL,
		ack_by TEXT NOT NULL,
		ack_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		notes TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		_ = db.Close()
		return err
	}
	s.db = db
	return nil
}

func (s *Server) close() {
	if s.db != nil {
		_ = s.db.Close()
	}
}

func (s *Server) handleReportSearchSuggest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed", r.Method))
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	connections, _ := s.loadConnections()
	suggestions, err := reportSearchSuggestions(query, connections)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, suggestions)
}

func reportSearchSuggestions(query string, connections []APIConnection) ([]string, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return []string{}, nil
	}
	seen := map[string]string{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] != "" || !strings.Contains(key, needle) {
			return
		}
		seen[key] = value
	}
	plantBySlug := map[string]string{}
	for _, connection := range connections {
		plantBySlug[slug(connection.Plant)] = connection.Plant
		add(connection.Plant)
	}
	files, err := filepath.Glob(filepath.Join("data", "rds-snapshots", "*-core-*.json"))
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		plant := plantFromSnapshotFile(file, plantBySlug)
		add(plant)
		observations, err := observationsFromSnapshot(file, plant, "")
		if err != nil {
			log.Printf("report search snapshot skipped %s: %v", file, err)
			continue
		}
		for _, observation := range observations {
			add(observation.AMR)
			add(observation.Zone)
		}
	}
	result := make([]string, 0, len(seen))
	for _, value := range seen {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		left := strings.HasPrefix(strings.ToLower(result[i]), needle)
		right := strings.HasPrefix(strings.ToLower(result[j]), needle)
		if left != right {
			return left
		}
		return result[i] < result[j]
	})
	if len(result) > 12 {
		result = result[:12]
	}
	return result, nil
}

func (s *Server) handleReportEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed", r.Method))
		return
	}
	plant := strings.TrimSpace(r.URL.Query().Get("plant"))
	if strings.EqualFold(plant, "all") {
		plant = ""
	}
	severities := reportSeverityFilter(r.URL.Query().Get("severity"))
	start, end, err := reportTimeWindow(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	connections, _ := s.loadConnections()
	events, err := reportEventsFromSnapshots(plant, severities, start, end, connections)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, ReportEventsResponse{Events: events, UpdatedAt: time.Now().Format(time.RFC3339)})
}

func reportSeverityFilter(raw string) map[string]bool {
	selected := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		value := strings.ToLower(strings.TrimSpace(part))
		if value == "" {
			continue
		}
		selected[value] = true
	}
	if len(selected) == 0 {
		selected["high"] = true
		selected["medium"] = true
		selected["low"] = true
	}
	return selected
}

func reportTimeWindow(values url.Values) (time.Time, time.Time, error) {
	now := time.Now()
	rangeValue := strings.ToLower(strings.TrimSpace(values.Get("range")))
	if rangeValue == "custom" {
		start, ok := parseReportTime(values.Get("start"))
		if !ok {
			return time.Time{}, time.Time{}, errors.New("custom start is required")
		}
		end, ok := parseReportTime(values.Get("end"))
		if !ok {
			return time.Time{}, time.Time{}, errors.New("custom end is required")
		}
		if end.Before(start) {
			return time.Time{}, time.Time{}, errors.New("custom end must be after start")
		}
		return start, end, nil
	}
	duration := 24 * time.Hour
	switch rangeValue {
	case "1h":
		duration = time.Hour
	case "6h":
		duration = 6 * time.Hour
	case "24h", "":
		duration = 24 * time.Hour
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("unsupported range %q", rangeValue)
	}
	return now.Add(-duration), now, nil
}

func reportEventsFromSnapshots(plant string, severities map[string]bool, start, end time.Time, connections []APIConnection) ([]ReportEvent, error) {
	files, err := filepath.Glob(filepath.Join("data", "rds-snapshots", "*-core-*.json"))
	if err != nil {
		return nil, err
	}
	plantBySlug := map[string]string{}
	for _, connection := range connections {
		plantBySlug[slug(connection.Plant)] = connection.Plant
	}
	events := []ReportEvent{}
	for _, file := range files {
		filePlant := plantFromSnapshotFile(file, plantBySlug)
		if plant != "" && !strings.EqualFold(filePlant, plant) && slug(filePlant) != slug(plant) {
			continue
		}
		observations, err := observationsFromSnapshot(file, filePlant, "")
		if err != nil {
			log.Printf("report event snapshot skipped %s: %v", file, err)
			continue
		}
		for _, observation := range observations {
			if observation.Timestamp.Before(start) || observation.Timestamp.After(end) {
				continue
			}
			severity, topic := reportEventSeverity(observation)
			if !severities[strings.ToLower(severity)] {
				continue
			}
			events = append(events, ReportEvent{
				Time:     observation.RawTime,
				Plant:    observation.Plant,
				AMR:      observation.AMR,
				Zone:     observation.Zone,
				Server:   "Local RDS snapshot",
				Host:     observation.Plant + " RDS",
				Source:   "RDS Core",
				Category: "AMR",
				Severity: severity,
				Topic:    topic,
				Message:  reportEventMessage(observation, severity),
			})
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Time > events[j].Time })
	if len(events) > 300 {
		events = events[:300]
	}
	return events, nil
}

func plantFromSnapshotFile(file string, plantBySlug map[string]string) string {
	name := filepath.Base(file)
	parts := strings.Split(name, "-core-")
	if len(parts) > 1 {
		if plant, ok := plantBySlug[parts[0]]; ok {
			return plant
		}
		return strings.Title(strings.ReplaceAll(parts[0], "-", " "))
	}
	return "Unknown"
}

func reportEventSeverity(observation zoneObservation) (string, string) {
	if !observation.Connected {
		return "High", "Robot offline / disconnect"
	}
	if observation.DelayMS >= 150 {
		return "Medium", "High RDS network delay"
	}
	if observation.DelayMS >= 80 {
		return "Medium", "Elevated RDS network delay"
	}
	return "Low", "RDS connectivity sample"
}

func reportEventMessage(observation zoneObservation, severity string) string {
	if severity == "High" {
		return fmt.Sprintf("%s disconnected at %s. Zone %s. RDS delay %d ms.", observation.AMR, observation.RawTime, observation.Zone, observation.DelayMS)
	}
	return fmt.Sprintf("%s reported %d ms RDS delay at %s in zone %s.", observation.AMR, observation.DelayMS, observation.RawTime, observation.Zone)
}

func (s *Server) handleBadZoneReports(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/reports/bad-zones/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 {
		writeError(w, http.StatusNotFound, errors.New("unknown bad-zone report route"))
		return
	}
	zoneID, err := url.PathUnescape(parts[0])
	if err != nil || strings.TrimSpace(zoneID) == "" {
		writeError(w, http.StatusBadRequest, errors.New("invalid zone id"))
		return
	}
	plantID, zoneName := splitZoneID(zoneID)
	switch parts[1] {
	case "events":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed", r.Method))
			return
		}
		events, err := badZoneEventsFromSnapshots(plantID, zoneName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		ack, err := s.latestZoneAcknowledgement(zoneID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, BadZoneEventsResponse{ZoneID: zoneID, PlantID: plantID, Events: events, Acknowledgement: ack})
	case "acknowledge":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed", r.Method))
			return
		}
		var request ZoneAckRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		request.AckBy = strings.TrimSpace(request.AckBy)
		if request.AckBy == "" {
			writeError(w, http.StatusBadRequest, errors.New("ack_by is required"))
			return
		}
		if request.PlantID == "" {
			request.PlantID = plantID
		}
		ack, err := s.saveZoneAcknowledgement(zoneID, request)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, ack)
	default:
		writeError(w, http.StatusNotFound, errors.New("unknown bad-zone report route"))
	}
}

func splitZoneID(zoneID string) (string, string) {
	parts := strings.SplitN(zoneID, "|", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "", strings.TrimSpace(zoneID)
}

func (s *Server) latestZoneAcknowledgement(zoneID string) (*ZoneAcknowledgement, error) {
	if s.db != nil {
		row := s.db.QueryRow(`SELECT id, zone_id, plant_id, ack_by, ack_at, notes FROM zone_acknowledgements WHERE zone_id = $1 ORDER BY ack_at DESC, id DESC LIMIT 1`, zoneID)
		var ack ZoneAcknowledgement
		var ackAt time.Time
		if err := row.Scan(&ack.ID, &ack.ZoneID, &ack.PlantID, &ack.AckBy, &ackAt, &ack.Notes); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil
			}
			return nil, err
		}
		ack.AckAt = ackAt.Format(time.RFC3339)
		return &ack, nil
	}
	acks, err := s.loadLocalAcknowledgements()
	if err != nil {
		return nil, err
	}
	var latest *ZoneAcknowledgement
	for i := range acks {
		ack := acks[i]
		if ack.ZoneID != zoneID {
			continue
		}
		if latest == nil || ack.AckAt > latest.AckAt || (ack.AckAt == latest.AckAt && ack.ID > latest.ID) {
			latest = &ack
		}
	}
	return latest, nil
}

func (s *Server) saveZoneAcknowledgement(zoneID string, request ZoneAckRequest) (ZoneAcknowledgement, error) {
	ackAt := time.Now().UTC()
	if s.db != nil {
		row := s.db.QueryRow(`INSERT INTO zone_acknowledgements(zone_id, plant_id, ack_by, ack_at, notes) VALUES($1, $2, $3, $4, $5) RETURNING id`, zoneID, request.PlantID, request.AckBy, ackAt, request.Notes)
		var id int64
		if err := row.Scan(&id); err != nil {
			return ZoneAcknowledgement{}, err
		}
		return ZoneAcknowledgement{ID: id, ZoneID: zoneID, PlantID: request.PlantID, AckBy: request.AckBy, AckAt: ackAt.Format(time.RFC3339), Notes: request.Notes}, nil
	}
	acks, err := s.loadLocalAcknowledgements()
	if err != nil {
		return ZoneAcknowledgement{}, err
	}
	var nextID int64 = 1
	for _, ack := range acks {
		if ack.ID >= nextID {
			nextID = ack.ID + 1
		}
	}
	ack := ZoneAcknowledgement{ID: nextID, ZoneID: zoneID, PlantID: request.PlantID, AckBy: request.AckBy, AckAt: ackAt.Format(time.RFC3339), Notes: request.Notes}
	acks = append(acks, ack)
	if err := os.MkdirAll(filepath.Dir(s.ackPath), 0o755); err != nil {
		return ZoneAcknowledgement{}, err
	}
	body, err := json.MarshalIndent(acks, "", "  ")
	if err != nil {
		return ZoneAcknowledgement{}, err
	}
	return ack, os.WriteFile(s.ackPath, body, 0o600)
}

func (s *Server) loadLocalAcknowledgements() ([]ZoneAcknowledgement, error) {
	body, err := os.ReadFile(s.ackPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []ZoneAcknowledgement{}, nil
		}
		return nil, err
	}
	var acks []ZoneAcknowledgement
	if err := json.Unmarshal(body, &acks); err != nil {
		return nil, err
	}
	return acks, nil
}

type zoneObservation struct {
	Zone       string
	Plant      string
	AMR        string
	Timestamp  time.Time
	RawTime    string
	DelayMS    int
	Connected  bool
	IsIncident bool
}

func badZoneEventsFromSnapshots(plantID, zoneName string) ([]ZoneEvent, error) {
	files, err := filepath.Glob(filepath.Join("data", "rds-snapshots", "*-core-*.json"))
	if err != nil {
		return nil, err
	}
	observations := []zoneObservation{}
	for _, file := range files {
		fileObservations, err := observationsFromSnapshot(file, plantID, zoneName)
		if err != nil {
			log.Printf("bad-zone snapshot skipped %s: %v", file, err)
			continue
		}
		observations = append(observations, fileObservations...)
	}
	sort.Slice(observations, func(i, j int) bool { return observations[i].Timestamp.Before(observations[j].Timestamp) })
	events := []ZoneEvent{}
	for i, observation := range observations {
		if !observation.IsIncident {
			continue
		}
		reconnectedAt := ""
		for _, later := range observations[i+1:] {
			if later.AMR == observation.AMR && later.Connected {
				reconnectedAt = later.RawTime
				break
			}
		}
		durationMS := 0
		if reconnectedAt != "" {
			if reconnectTime, ok := parseReportTime(reconnectedAt); ok {
				durationMS = int(reconnectTime.Sub(observation.Timestamp).Milliseconds())
				if durationMS < 0 {
					durationMS = 0
				}
			}
		}
		events = append(events, ZoneEvent{Timestamp: observation.RawTime, AMR: observation.AMR, RDSDelayMS: observation.DelayMS, DurationMS: durationMS, ReconnectedAt: reconnectedAt})
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Timestamp > events[j].Timestamp })
	if len(events) > 50 {
		events = events[:50]
	}
	return events, nil
}

func observationsFromSnapshot(file, plantID, zoneName string) ([]zoneObservation, error) {
	body, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	core := mapValue(payload["data"])
	if core == nil {
		return nil, errors.New("missing data object")
	}
	reports := sliceValue(core["report"])
	info, _ := os.Stat(file)
	rawTime := stringValue(core["create_on"])
	parsedTime, ok := parseReportTime(rawTime)
	if !ok && info != nil {
		parsedTime = info.ModTime()
		rawTime = parsedTime.Format(time.RFC3339)
	}
	observations := []zoneObservation{}
	for _, rawReport := range reports {
		report := mapValue(rawReport)
		if report == nil {
			continue
		}
		rbk := mapValue(report["rbk_report"])
		order := mapValue(report["current_order"])
		zone := currentZone(rbk, order)
		if strings.TrimSpace(zoneName) != "" && normalizeZone(zone) != normalizeZone(zoneName) {
			continue
		}
		name := firstString(report["uuid"], report["vehicle_id"], nestedValue(order, "vehicle"), "Unknown AMR")
		delay := intValue(report["network_delay"])
		disconnected := intValue(report["connection_status"]) == 0 || boolValue(nestedValue(mapValue(report["undispatchable_reason"]), "disconnect"))
		incident := disconnected || delay >= 80
		observations = append(observations, zoneObservation{Zone: zone, Plant: plantID, AMR: name, Timestamp: parsedTime, RawTime: rawTime, DelayMS: delay, Connected: !disconnected, IsIncident: incident})
	}
	return observations, nil
}

func currentZone(rbk, order map[string]any) string {
	if value := stringValue(nestedValue(rbk, "current_station")); value != "" {
		return value
	}
	blocks := sliceValue(nestedValue(order, "blocks"))
	if len(blocks) > 0 {
		if value := stringValue(nestedValue(mapValue(blocks[0]), "location")); value != "" {
			return value
		}
	}
	return "Unknown location"
}

func normalizeZone(value string) string {
	return strings.ToLower(regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(strings.TrimSpace(value), ""))
}

func parseReportTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02T15:04", time.RFC1123, time.RFC1123Z}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func mapValue(value any) map[string]any {
	mapped, _ := value.(map[string]any)
	return mapped
}

func sliceValue(value any) []any {
	sliced, _ := value.([]any)
	return sliced
}

func nestedValue(mapped map[string]any, key string) any {
	if mapped == nil {
		return nil
	}
	return mapped[key]
}

func firstString(values ...any) string {
	for _, value := range values {
		if text := stringValue(value); text != "" {
			return text
		}
	}
	return ""
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case json.Number:
		value, _ := typed.Int64()
		return int(value)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(s.staticDir, filepath.Clean(r.URL.Path))
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.staticDir, "index.html"))
}

func wifiDiscoverError(message string) WifiDiscoverResponse {
	return WifiDiscoverResponse{Message: message, Results: []WifiDiscoverResult{}}
}
func (s *Server) discoverWifiRSSI(request WifiDiscoverRequest) (WifiDiscoverResponse, int) {
	source := normalizeWifiSource(request.Source)
	if source.Method != "AMR SSH" {
		return wifiDiscoverError("Only AMR SSH auto-discovery is supported right now."), http.StatusBadRequest
	}
	if source.Username == "" {
		return wifiDiscoverError("Username is required for AMR RSSI auto-discovery."), http.StatusBadRequest
	}
	if looksLikePublicKey(source.SecretRef) {
		return wifiDiscoverError("Credential Reference looks like a public key. Use the private key file path available to the DRISHTI container, for example /app/data/keys/robowatch_id."), http.StatusBadRequest
	}
	if len(request.Robots) == 0 {
		return wifiDiscoverError("No AMR robot IPs were provided. Pull RDS core first so DRISHTI can read basic_info.ip."), http.StatusBadRequest
	}

	results := make([]WifiDiscoverResult, 0, len(request.Robots))
	okCount := 0
	for _, robot := range request.Robots {
		robot.IP = strings.TrimSpace(robot.IP)
		robot.Name = strings.TrimSpace(robot.Name)
		if robot.IP == "" || robot.IP == "unknown" {
			results = append(results, WifiDiscoverResult{Plant: robot.Plant, AMR: robot.Name, Host: robot.IP, Message: "No robot IP from RDS basic_info.ip."})
			continue
		}
		result := s.discoverRobotRSSI(source, robot)
		if result.OK {
			okCount++
		}
		results = append(results, result)
	}
	message := fmt.Sprintf("Found real RSSI on %d of %d AMRs.", okCount, len(results))
	return WifiDiscoverResponse{OK: okCount > 0, Message: message, Results: results}, http.StatusOK
}

func (s *Server) discoverRobotRSSI(source WifiSource, robot WifiRobot) WifiDiscoverResult {
	commands := wifiDiscoveryCommands(source.Command)
	last := WifiDiscoverResult{Plant: robot.Plant, AMR: robot.Name, Host: robot.IP, Message: "No RSSI command succeeded."}
	for _, command := range commands {
		source.Host = robot.IP
		output, err := runSSHCommand(source, command, 10*time.Second)
		cleanOutput := trimOutput(output)
		last = WifiDiscoverResult{Plant: robot.Plant, AMR: robot.Name, Host: robot.IP, Command: command, Output: cleanOutput}
		if err != nil {
			last.Message = fmt.Sprintf("SSH command failed: %v", err)
			continue
		}
		rssi := parseRSSI(cleanOutput)
		if rssi == nil {
			last.Message = "Command succeeded, but no RSSI value was found."
			last.Quality = "Unknown"
			continue
		}
		last.OK = true
		last.RSSI = rssi
		last.SSID = parseSSID(cleanOutput)
		last.Quality = rssiQuality(*rssi)
		if last.SSID != "" {
			last.Message = fmt.Sprintf("RSSI detected: %d dBm (%s) on SSID %s.", *rssi, last.Quality, last.SSID)
		} else {
			last.Message = fmt.Sprintf("RSSI detected: %d dBm (%s).", *rssi, last.Quality)
		}
		return last
	}
	return last
}

func wifiDiscoveryCommands(preferred string) []string {
	commands := []string{}
	preferred = strings.TrimSpace(preferred)
	if preferred != "" && preferred != "iw dev wlan0 link" {
		commands = append(commands, preferred)
	}
	commands = append(commands,
		"iw dev wlan0 link",
		`sh -lc "for i in $(iw dev 2>/dev/null | awk '/Interface/ {print $2}'); do iw dev \"$i\" link; done"`,
		"cat /proc/net/wireless",
		"nmcli -t -f ACTIVE,SSID,SIGNAL,BARS,CHAN,FREQ dev wifi",
	)
	seen := map[string]bool{}
	uniqueCommands := make([]string, 0, len(commands))
	for _, command := range commands {
		if command == "" || seen[command] {
			continue
		}
		seen[command] = true
		uniqueCommands = append(uniqueCommands, command)
	}
	return uniqueCommands
}

func normalizeWifiSource(source WifiSource) WifiSource {
	source.Method = strings.TrimSpace(source.Method)
	source.Host = strings.TrimSpace(source.Host)
	source.Username = strings.TrimSpace(source.Username)
	source.SecretRef = strings.TrimSpace(source.SecretRef)
	source.Command = strings.TrimSpace(source.Command)
	return source
}

func runSSHCommand(source WifiSource, command string, timeout time.Duration) (string, error) {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=8",
		"-o", "StrictHostKeyChecking=accept-new",
	}
	if source.SecretRef != "" && source.SecretRef != "CyberArk or SSH key reference" {
		args = append(args, "-i", source.SecretRef)
	}
	args = append(args, fmt.Sprintf("%s@%s", source.Username, source.Host), command)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ssh", args...).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(output), fmt.Errorf("SSH timed out after %s", timeout)
	}
	return string(output), err
}

func trimOutput(output string) string {
	cleanOutput := strings.TrimSpace(output)
	if len(cleanOutput) > 2000 {
		cleanOutput = cleanOutput[:2000]
	}
	return cleanOutput
}
func (s *Server) testWifiSource(source WifiSource) (WifiTestResult, int) {
	source.Method = strings.TrimSpace(source.Method)
	source.Host = strings.TrimSpace(source.Host)
	source.Username = strings.TrimSpace(source.Username)
	source.SecretRef = strings.TrimSpace(source.SecretRef)
	source.Command = strings.TrimSpace(source.Command)
	result := WifiTestResult{Method: source.Method, Host: source.Host}
	if source.Method != "AMR SSH" {
		result.Message = "Only AMR SSH can be tested right now. Controller API and manual import need a parser endpoint first."
		return result, http.StatusBadRequest
	}
	if source.Host == "" || source.Username == "" {
		result.Message = "Host/API and username are required for SSH RSSI testing."
		return result, http.StatusBadRequest
	}
	if source.Command == "" {
		source.Command = "iw dev wlan0 link"
	}
	if looksLikePublicKey(source.SecretRef) {
		result.Message = "Credential Reference looks like a public key. Use the private key file path available to the DRISHTI container, for example /app/data/keys/robowatch_id."
		return result, http.StatusBadRequest
	}

	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=8",
		"-o", "StrictHostKeyChecking=accept-new",
	}
	if source.SecretRef != "" && source.SecretRef != "CyberArk or SSH key reference" {
		args = append(args, "-i", source.SecretRef)
	}
	args = append(args, fmt.Sprintf("%s@%s", source.Username, source.Host), source.Command)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ssh", args...).CombinedOutput()
	cleanOutput := strings.TrimSpace(string(output))
	if len(cleanOutput) > 2000 {
		cleanOutput = cleanOutput[:2000]
	}
	result.Output = cleanOutput
	if ctx.Err() == context.DeadlineExceeded {
		result.Message = "SSH RSSI test timed out after 12 seconds."
		return result, http.StatusGatewayTimeout
	}
	if err != nil {
		result.Message = fmt.Sprintf("SSH RSSI test failed: %v", err)
		return result, http.StatusBadGateway
	}
	rssi := parseRSSI(cleanOutput)
	if rssi == nil {
		result.OK = true
		result.Message = "SSH command succeeded, but no RSSI value was found in the output."
		result.Quality = "Unknown"
		return result, http.StatusOK
	}
	result.OK = true
	result.RSSI = rssi
	result.SSID = parseSSID(cleanOutput)
	result.Quality = rssiQuality(*rssi)
	if result.SSID != "" {
		result.Message = fmt.Sprintf("SSH RSSI test succeeded. Signal %d dBm (%s) on SSID %s.", *rssi, result.Quality, result.SSID)
	} else {
		result.Message = fmt.Sprintf("SSH RSSI test succeeded. Signal %d dBm (%s).", *rssi, result.Quality)
	}
	return result, http.StatusOK
}

func looksLikePublicKey(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "ssh-rsa ") || strings.HasPrefix(value, "ssh-ed25519 ") || strings.Contains(value, "BEGIN PUBLIC KEY")
}

func parseSSID(output string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?im)^\s*SSID:\s*(.+?)\s*$`),
		regexp.MustCompile(`(?im)^\s*ssid\s*[=:]\s*(.+?)\s*$`),
	}
	for _, pattern := range patterns {
		match := pattern.FindStringSubmatch(output)
		if len(match) == 2 {
			if ssid := cleanSSID(match[1]); ssid != "" {
				return ssid
			}
		}
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "yes:") || strings.HasPrefix(line, "*:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				if ssid := cleanSSID(parts[1]); ssid != "" {
					return ssid
				}
			}
		}
	}
	return ""
}

func cleanSSID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	invalid := []string{"not found", "not reported", "not captured", "not connected", "no such", "command not found", "error"}
	for _, token := range invalid {
		if strings.Contains(lower, token) {
			return ""
		}
	}
	return value
}
func parseRSSI(output string) *int {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)signal:\s*(-?\d+)\s*dBm`),
		regexp.MustCompile(`(?i)rssi[^-\d]*(-?\d+)`),
		regexp.MustCompile(`(?m)^\s*[A-Za-z0-9_.-]+:\s+\S+\s+\S+\s+(-?\d+)\.?`),
	}
	for _, pattern := range patterns {
		match := pattern.FindStringSubmatch(output)
		if len(match) == 2 {
			value, err := strconv.Atoi(match[1])
			if err == nil {
				return &value
			}
		}
	}
	percentPattern := regexp.MustCompile(`(?m)^(?:yes|\*)[^:]*:[^:\n]*:(\d{1,3}):`)
	match := percentPattern.FindStringSubmatch(output)
	if len(match) == 2 {
		percent, err := strconv.Atoi(match[1])
		if err == nil {
			value := percent/2 - 100
			return &value
		}
	}
	return nil
}

func rssiQuality(rssi int) string {
	switch {
	case rssi >= -60:
		return "Good"
	case rssi >= -70:
		return "Weak"
	case rssi >= -80:
		return "Poor"
	default:
		return "Critical"
	}
}
func (s *Server) loadConnections() ([]APIConnection, error) {
	data, err := os.ReadFile(s.configPath)
	if errors.Is(err, os.ErrNotExist) {
		return []APIConnection{}, nil
	}
	if err != nil {
		return nil, err
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	var connections []APIConnection
	if err := json.Unmarshal(data, &connections); err != nil {
		return nil, err
	}
	return normalizeConnections(connections), nil
}

func (s *Server) saveConnections(connections []APIConnection) error {
	if err := os.MkdirAll(filepath.Dir(s.configPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(normalizeConnections(connections), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.configPath, append(data, '\n'), 0o600)
}

func normalizeConnections(connections []APIConnection) []APIConnection {
	byPlant := make(map[string]APIConnection)
	for _, connection := range connections {
		plant := strings.TrimSpace(connection.Plant)
		baseURL := strings.TrimRight(strings.TrimSpace(connection.BaseURL), "/")
		if plant == "" || baseURL == "" {
			continue
		}
		connection.Plant = plant
		connection.BaseURL = baseURL
		connection.CorePath = normalizePath(connection.CorePath, "/api/agv-report/core")
		connection.ScenePath = normalizePath(connection.ScenePath, "/api/display-scene")
		byPlant[strings.ToLower(plant)] = connection
	}
	result := make([]APIConnection, 0, len(byPlant))
	for _, connection := range byPlant {
		result = append(result, connection)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Plant < result[j].Plant })
	return result
}

func normalizePath(path, fallback string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = fallback
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func upsertConnection(connections []APIConnection, connection APIConnection) []APIConnection {
	updated := make([]APIConnection, 0, len(connections)+1)
	needle := strings.ToLower(strings.TrimSpace(connection.Plant))
	for _, existing := range connections {
		if strings.ToLower(existing.Plant) != needle {
			updated = append(updated, existing)
		}
	}
	updated = append(updated, connection)
	return normalizeConnections(updated)
}

func findConnection(connections []APIConnection, plant string) (APIConnection, bool) {
	needle := strings.ToLower(strings.TrimSpace(plant))
	for _, connection := range connections {
		if strings.ToLower(connection.Plant) == needle || slug(connection.Plant) == needle {
			return connection, true
		}
	}
	return APIConnection{}, false
}

func joinURL(base, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid base URL %q", base)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + normalizePath(path, "/")
	parsed.RawQuery = ""
	return parsed.String(), nil
}

func (s *Server) fetch(target string) ([]byte, string, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("RDS returned %s: %s", resp.Status, string(bytes.TrimSpace(body)))
	}
	return body, resp.Header.Get("Content-Type"), nil
}

func saveSnapshot(plant, endpoint string, body []byte) error {
	if err := os.MkdirAll(filepath.Join("data", "rds-snapshots"), 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%s-%s.json", slug(plant), endpoint, time.Now().Format("20060102-150405"))
	return os.WriteFile(filepath.Join("data", "rds-snapshots", name), body, 0o600)
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
