package main

import (
	"bytes"
	"context"
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

type WifiTestResult struct {
	OK      bool   `json:"ok"`
	Method  string `json:"method"`
	Host    string `json:"host"`
	Message string `json:"message"`
	Output  string `json:"output,omitempty"`
	RSSI    *int   `json:"rssi,omitempty"`
	Quality string `json:"quality,omitempty"`
}
type Server struct {
	configPath string
	staticDir  string
	client     *http.Client
}

func main() {
	port := env("PORT", "8090")
	server := &Server{
		configPath: env("DRISHTI_API_CONFIG", filepath.Join("data", "config", "api-connections.json")),
		staticDir:  env("DRISHTI_STATIC_DIR", filepath.Join("frontend", "dist")),
		client:     &http.Client{Timeout: 20 * time.Second},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", server.handleHealth)
	mux.HandleFunc("/api/connections", server.handleConnections)
	mux.HandleFunc("/api/wifi/test", server.handleWifiTest)
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

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(s.staticDir, filepath.Clean(r.URL.Path))
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.staticDir, "index.html"))
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
	result.Quality = rssiQuality(*rssi)
	result.Message = fmt.Sprintf("SSH RSSI test succeeded. Signal %d dBm (%s).", *rssi, result.Quality)
	return result, http.StatusOK
}

func looksLikePublicKey(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "ssh-rsa ") || strings.HasPrefix(value, "ssh-ed25519 ") || strings.Contains(value, "BEGIN PUBLIC KEY")
}

func parseRSSI(output string) *int {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)signal:\s*(-?\d+)\s*dBm`),
		regexp.MustCompile(`(?i)rssi[^-\d]*(-?\d+)`),
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
