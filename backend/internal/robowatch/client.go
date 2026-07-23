package robowatch

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string
	Port       int
	Username   string
	Password   string
	httpClient *http.Client
	token      string
}

type LogSource struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // "api" or "html_table"
	URL         string `json:"url"`
	Description string `json:"description"`
}

type TestResult struct {
	Reachable     bool   `json:"reachable"`
	Authenticated bool   `json:"authenticated"`
	Success       bool   `json:"success"`
	Error         string `json:"error,omitempty"`
	ErrorCode     string `json:"error_code,omitempty"`
}

type RobotCoreStatus struct {
	UUID             string
	VehicleID        string
	IP               string
	MAC              string
	Odo              float64
	TodayOdo         float64
	BatteryLevel     *float64
	BatteryTempC     *float64
	BatteryState     string
	ConnectionStatus int
	SeenAt           time.Time
	LastReceivedAt   time.Time
}

func NewClient(baseURL string, port int, username, password string) *Client {
	return &Client{
		BaseURL:  baseURL,
		Port:     port,
		Username: username,
		Password: password,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (c *Client) TestConnection() TestResult {
	target := fmt.Sprintf("%s:%d", c.host(), c.Port)
	conn, err := dialTimeout(target, 5*time.Second)
	if err != nil {
		return TestResult{
			Reachable: false, Authenticated: false, Success: false,
			Error:     fmt.Sprintf("cannot reach %s: %v", target, err),
			ErrorCode: "cannot_reach_host",
		}
	}
	conn.Close()

	_, loginErr := c.login()
	if loginErr != nil {
		errMsg := loginErr.Error()
		errCode := "unknown"
		switch {
		case strings.Contains(errMsg, "401"), strings.Contains(errMsg, "unauthorized"),
			strings.Contains(errMsg, "password"):
			errCode = "login_failed"
		case strings.Contains(errMsg, "403"), strings.Contains(errMsg, "forbidden"):
			errCode = "permission_denied"
		case strings.Contains(errMsg, "timeout"):
			errCode = "timeout"
		}
		return TestResult{
			Reachable: true, Authenticated: false, Success: false,
			Error:     fmt.Sprintf("login failed: %v", loginErr),
			ErrorCode: errCode,
		}
	}
	return TestResult{Reachable: true, Authenticated: true, Success: true}
}

func (c *Client) DiscoverSources() ([]LogSource, error) {
	if err := c.ensureAuthenticated(); err != nil {
		return nil, fmt.Errorf("authentication required: %w", err)
	}

	var sources []LogSource
	base := strings.TrimSuffix(c.BaseURL, "/")

	// Probe known RoboWatch API endpoints
	apiEndpoints := []struct{ path, name, desc string }{
		{"/api/robot/list", "Robot List", "API: list of robots with status"},
		{"/api/log/entries", "Log Entries", "API: structured log entry feed"},
		{"/api/log/export", "Log Export", "API: downloadable log export"},
		{"/api/fleet/status", "Fleet Status", "API: fleet-wide status"},
		{"/api/stat/agvStatusCurrent", "AGV Status", "API: AGV current status"},
		{"/api/stat/vehicleBatteryLevel", "Battery Levels", "API: battery level data"},
		{"/api/getCoreRobotOrders", "Robot Orders", "API: robot order queue"},
		{"/api/stat/findUUid", "Robot UUIDs", "API: robot UUID list"},
	}
	for _, ep := range apiEndpoints {
		if c.probeEndpoint(ep.path) {
			sources = append(sources, LogSource{Name: ep.name, Type: "api", URL: base + ep.path, Description: ep.desc})
		}
	}

	// Probe HTML pages as fallback
	htmlPages := []struct{ path, name, desc string }{
		{"/logs", "Log Table", "HTML: human-readable log table"},
		{"/log", "Log Table (alt)", "HTML: alternative log path"},
		{"/history", "History", "HTML: robot operation history"},
		{"/robots", "Robot Table", "HTML: robot status table"},
		{"/dashboard", "Dashboard", "HTML: fleet dashboard"},
	}
	for _, pg := range htmlPages {
		if c.probeHTMLTable(base + pg.path) {
			sources = append(sources, LogSource{Name: pg.name, Type: "html_table", URL: base + pg.path, Description: pg.desc})
		}
	}
	return sources, nil
}

// FetchLogs pulls RDS log lines and keeps only those whose embedded timestamp
// falls within [from, to]. Lines without a parseable timestamp are kept only when
// from/to are both zero (the caller asked for everything).
func (c *Client) FetchLogs(from, to time.Time) ([]string, error) {
	if err := c.ensureAuthenticated(); err != nil {
		return nil, fmt.Errorf("authentication required: %w", err)
	}

	var rawLines []string
	base := strings.TrimSuffix(c.BaseURL, "/")

	// Prefer API log sources
	for _, path := range []string{
		"/api/log/entries", "/api/log/export", "/api/stat/agvStatusCurrent",
		"/api/stat/vehicleBatteryLevel", "/api/getCoreRobotOrders",
	} {
		lines, err := c.fetchLines(base + path)
		if err == nil && len(lines) > 0 {
			rawLines = append(rawLines, lines...)
		}
	}

	// Fallback: HTML table scrape
	for _, path := range []string{"/logs", "/history", "/log"} {
		lines, err := c.scrapeLogTable(base + path)
		if err == nil && len(lines) > 0 {
			rawLines = append(rawLines, lines...)
		}
	}

	// Window-filter: keep only lines whose timestamp is within [from, to]. If the
	// window is open (both zero) return everything. Lines with no parseable
	// timestamp are kept (pass-through) so they aren't silently dropped — the
	// downstream normalizer will assign them a fallback timestamp.
	if from.IsZero() && to.IsZero() {
		return rawLines, nil
	}
	filtered := rawLines[:0]
	for _, l := range rawLines {
		ts, ok := parseRoboshopTimestamp(l)
		if !ok {
			// No recognisable timestamp: keep the line so it reaches the normalizer.
			filtered = append(filtered, l)
			continue
		}
		if !from.IsZero() && ts.Before(from) {
			continue
		}
		if !to.IsZero() && ts.After(to) {
			continue
		}
		filtered = append(filtered, l)
	}
	return filtered, nil
}

// parseRoboshopTimestamp extracts an event time from a log line. Recognizes:
//   - "[YYYYMMDD HHMMSS.fff]" — Roboshop bracket form
//   - "YYYY-MM-DD HH:MM:SS"   — space-separated ISO (common in RDS exports)
//   - "YYYY-MM-DDTHH:MM:SSZ"  — RFC3339 / journal short-iso
//
// All forms are treated as UTC (RDS clocks are site-local; best-effort filter).
// Returns ok=false only when none of the above match.
var roboBracketsRe = regexp.MustCompile(`\[(\d{4})(\d{2})(\d{2}) (\d{2})(\d{2})(\d{2})`)
var spaceISOre = regexp.MustCompile(`(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2}:\d{2})`)

func parseRoboshopTimestamp(line string) (time.Time, bool) {
	// Form 1: Roboshop bracket "[YYYYMMDD HHMMSS]"
	if m := roboBracketsRe.FindStringSubmatch(line); len(m) == 7 {
		s := fmt.Sprintf("%s-%s-%s %s:%s:%s", m[1], m[2], m[3], m[4], m[5], m[6])
		if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
			return t, true
		}
	}
	// Form 2: space-separated ISO "YYYY-MM-DD HH:MM:SS"
	if m := spaceISOre.FindStringSubmatch(line); len(m) == 3 {
		if t, err := time.Parse("2006-01-02 15:04:05", m[1]+" "+m[2]); err == nil {
			return t.UTC(), true
		}
	}
	// Form 3: RFC3339-ish "YYYY-MM-DDTHH:MM:SS"
	if s := firstISOSubstring(line); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// firstISOSubstring returns the first "YYYY-MM-DDTHH:MM:SS" run in line (with a
// trailing Z), or "" if none. Used as a permissive fallback for journal/syslog
// lines that embed ISO timestamps.
func firstISOSubstring(line string) string {
	for i := 0; i+19 <= len(line); i++ {
		if line[i] == '2' && line[i+4] == '-' && line[i+7] == '-' && line[i+10] == 'T' &&
			line[i+13] == ':' && line[i+16] == ':' {
			return line[i:i+19] + "Z"
		}
	}
	return ""
}

// ?? Internal helpers ?????????????????????????????????????????????????????????

func (c *Client) host() string {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return strings.TrimPrefix(strings.TrimPrefix(c.BaseURL, "http://"), "https://")
	}
	host := u.Host
	if colon := strings.LastIndex(host, ":"); colon >= 0 {
		host = host[:colon]
	}
	return host
}

func (c *Client) rdsCoreURL(path string) string {
	return fmt.Sprintf("http://%s:8088%s", c.host(), path)
}

// login performs the RoboWatch multi-step auth:
// 1. GET /admin/encrypt to check if password encryption is required
// 2. POST /admin/login with md5(password) and sha256(md5(password)+"Rds123!")
func (c *Client) login() (string, error) {
	// Step 1: check encryption requirement
	encResp, err := c.requestRaw("GET", c.BaseURL+"admin/encrypt", nil)
	if err != nil {
		return "", fmt.Errorf("encrypt check: %w", err)
	}

	needEncrypt := false
	if encMap, ok := encResp.(map[string]any); ok {
		if v, ok := encMap["data"].(bool); ok {
			needEncrypt = v
		}
	}

	// Step 2: compute password variants
	md5pwd := md5Hash(c.Password)
	sha2pwd := sha256Hash(md5pwd + "Rds123!")

	loginPayload := map[string]string{
		"username":     c.Username,
		"sha2Password": sha2pwd,
	}
	if needEncrypt {
		loginPayload["password"] = md5pwd
	} else {
		loginPayload["password"] = c.Password
	}

	resp, err := c.requestRaw("POST", c.BaseURL+"admin/login", loginPayload)
	if err != nil {
		return "", fmt.Errorf("login request: %w", err)
	}

	if respMap, ok := resp.(map[string]any); ok {
		if code, ok := respMap["code"].(float64); ok && int(code) != 200 {
			return "", fmt.Errorf("login failed: code=%v msg=%v", code, respMap["msg"])
		}
		if data, ok := respMap["data"].(map[string]any); ok {
			if token, ok := data["token"].(string); ok && token != "" {
				c.token = token
				return token, nil
			}
		}
	}

	return "", fmt.Errorf("login response unexpected: %v", resp)
}

func (c *Client) ensureAuthenticated() error {
	if c.token != "" {
		return nil
	}
	_, err := c.login()
	return err
}

func (c *Client) requestRaw(method, url string, payload any) (any, error) {
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("token", c.token)
		req.Header.Set("Authorization", c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(raw))
	}

	var result any
	if err := json.Unmarshal(raw, &result); err == nil {
		return result, nil
	}
	return string(raw), nil
}

func (c *Client) probeEndpoint(path string) bool {
	req, err := http.NewRequest("GET", c.BaseURL+path, nil)
	if err != nil {
		return false
	}
	if c.token != "" {
		req.Header.Set("token", c.token)
		req.Header.Set("Authorization", c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func (c *Client) probeHTMLTable(urlStr string) bool {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return false
	}
	if c.token != "" {
		req.Header.Set("token", c.token)
		req.Header.Set("Authorization", c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if err != nil {
		return false
	}
	return bytes.Contains(body, []byte("<table")) && bytes.Contains(body, []byte("<tr"))
}

func (c *Client) fetchLines(path string) ([]string, error) {
	req, err := http.NewRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("token", c.token)
		req.Header.Set("Authorization", c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<21))
	if err != nil {
		return nil, err
	}

	// Try bare JSON array: [{...}, {...}]
	var rawArr []json.RawMessage
	if err := json.Unmarshal(body, &rawArr); err == nil {
		lines := make([]string, len(rawArr))
		for i, e := range rawArr {
			lines[i] = string(e)
		}
		return lines, nil
	}

	// Try wrapped JSON object: {"code":200,"data":[...]} or {"rows":[...]} etc.
	// RDS / Roboshop APIs typically wrap responses in an outer object.
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err == nil {
		for _, key := range []string{"data", "rows", "list", "logs", "records", "items", "result"} {
			if raw, ok := envelope[key]; ok {
				var arr []json.RawMessage
				if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
					lines := make([]string, len(arr))
					for i, e := range arr {
						lines[i] = string(e)
					}
					return lines, nil
				}
				// data field might be a single object; treat as one line
				if len(raw) > 2 {
					return []string{string(raw)}, nil
				}
			}
		}
	}

	// Fallback: line-based
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			result = append(result, l)
		}
	}
	return result, nil
}

// RobotLiveStatus is one robot's current connection state from RDS Core.
type RobotLiveStatus struct {
	UUID   string
	Code   int    // raw newStatus from RDS
	Online bool   // derived: true for the "connected" codes
	Reason string // human label for the state, e.g. "Online", "Offline / issue"
}

// LiveStatus fetches the live per-robot connection state via POST
// /api/stat/agvStatusCurrent (this endpoint rejects GET with 9006). Returns a
// uuid→status map. Used to answer "is this AMR connected right now?" — the
// authoritative online/offline signal, as opposed to historical drop counts.
//
// Seer/Roboshop newStatus codes observed in the field:
//
//	5 = Online/connected (the common good state — all healthy robots report this)
//	4 = Online with a task/charging state (robot is reachable and working)
//	1,2,3,6,... = offline / error states (robot is NOT currently connected)
//
// A robot present in the fleet but absent from this response is also offline
// (RDS Core cannot see it).
func (c *Client) LiveStatus() (map[string]RobotLiveStatus, error) {
	if err := c.ensureAuthenticated(); err != nil {
		return nil, fmt.Errorf("authentication required: %w", err)
	}
	out := map[string]RobotLiveStatus{}
	rows, err := c.postJSON(c.BaseURL+"api/stat/agvStatusCurrent", nil)
	if err != nil {
		return nil, err
	}
	// The response is {"code":200,"msg":"Success","data":[{"uuid":"AMR-01","newStatus":5},...]}.
	// rows here is the unwrapped "data" array (or each entry as a JSON line).
	for _, row := range rows {
		var s struct {
			UUID      string `json:"uuid"`
			NewStatus int    `json:"newStatus"`
		}
		if err := json.Unmarshal([]byte(row), &s); err != nil || s.UUID == "" {
			continue
		}
		// Per operator confirmation: a robot present in the RDS Core live roster is
		// ONLINE regardless of newStatus (codes 1–5 all represent connected/working
		// states — idle, moving, charging, task-running, etc.). Only a robot ABSENT
		// from the roster is offline (RDS Core cannot see it). The raw code is kept
		// for display; it's not the online/offline discriminator.
		online := s.NewStatus >= 1 && s.NewStatus <= 5
		reason := "Offline"
		if online {
			reason = "Online"
		}
		out[strings.TrimSpace(s.UUID)] = RobotLiveStatus{
			UUID: s.UUID, Code: s.NewStatus, Online: online, Reason: reason,
		}
	}
	return out, nil
}

// CoreRobotStatus fetches the vendor RDS Core /robotsStatus payload from port
// 8088 and returns uuid->status. Unlike the RoboWatch/socket logs, this endpoint
// reports the robot's own IP at report[].basic_info.ip.
func (c *Client) CoreRobotStatus() (map[string]RobotCoreStatus, error) {
	resp, err := c.requestRaw("GET", c.rdsCoreURL("/robotsStatus"), nil)
	if err != nil {
		return nil, err
	}
	env, ok := resp.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("robotsStatus response unexpected: %T", resp)
	}
	// Springfield (and some other RDS builds) wraps the actual payload in the
	// standard {code,msg,data:{...}} envelope. Older builds return the payload
	// directly. Accept both forms so the full robot roster remains authoritative.
	if data, wrapped := env["data"].(map[string]any); wrapped {
		env = data
	}
	seenAt := time.Now().UTC()
	if rawSeen, ok := env["create_on"].(string); ok && rawSeen != "" {
		if parsed, err := time.Parse(time.RFC3339, rawSeen); err == nil {
			seenAt = parsed
		}
	}
	report, ok := env["report"]
	if !ok {
		return nil, fmt.Errorf("robotsStatus missing report")
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		UUID             string `json:"uuid"`
		VehicleID        string `json:"vehicle_id"`
		ConnectionStatus int    `json:"connection_status"`
		BasicInfo        struct {
			IP string `json:"ip"`
			// RDS Core /robotsStatus exposes the robot's network adapter MAC in
			// basic_info.mac (some firmware variants use mac_address).
			MAC        string `json:"mac"`
			MACAddress string `json:"mac_address"`
		} `json:"basic_info"`
		RBKReport struct {
			Odo                  float64 `json:"odo"`
			TodayOdo             float64 `json:"today_odo"`
			BatteryLevel         any     `json:"battery_level"`
			BatteryTemperature   any     `json:"battery_temperature"`
			BatteryTemp          any     `json:"battery_temp"`
			BatteryStatus        any     `json:"battery_status"`
			Charging             any     `json:"charging"`
			BatteryChargeCurrent any     `json:"battery_charge_current"`
			Current              any     `json:"current"`
			Battery              struct {
				Level       any `json:"level"`
				Temperature any `json:"temperature"`
				Status      any `json:"status"`
			} `json:"battery"`
			ReceivedOn struct {
				DataNsec any `json:"data_nsec"`
				PubNsec  any `json:"pub_nsec"`
			} `json:"received_on"`
		} `json:"rbk_report"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}

	out := map[string]RobotCoreStatus{}
	for _, row := range rows {
		name := strings.TrimSpace(row.UUID)
		if name == "" {
			name = strings.TrimSpace(row.VehicleID)
		}
		if name == "" {
			continue
		}
		ip := strings.TrimSpace(row.BasicInfo.IP)
		if !validIPv4(ip) {
			ip = ""
		}
		mac := normaliseMAC(row.BasicInfo.MAC, row.BasicInfo.MACAddress)
		lastReceivedAt := unixRobotStatusTime(row.RBKReport.ReceivedOn.DataNsec)
		if lastReceivedAt.IsZero() {
			lastReceivedAt = unixRobotStatusTime(row.RBKReport.ReceivedOn.PubNsec)
		}
		batteryLevel := firstNumeric(row.RBKReport.BatteryLevel, row.RBKReport.Battery.Level)
		if batteryLevel != nil && *batteryLevel >= 0 && *batteryLevel <= 1 {
			value := *batteryLevel * 100
			batteryLevel = &value
		}
		batteryTemp := firstNumeric(row.RBKReport.BatteryTemperature, row.RBKReport.BatteryTemp, row.RBKReport.Battery.Temperature)
		batteryState := firstText(row.RBKReport.BatteryStatus, row.RBKReport.Battery.Status)
		if batteryState == "" {
			chargeCurrent := firstNumeric(row.RBKReport.BatteryChargeCurrent, row.RBKReport.Current)
			if anyBool(row.RBKReport.Charging) || (chargeCurrent != nil && *chargeCurrent > 0.1) {
				batteryState = "Charging"
			} else if chargeCurrent != nil && *chargeCurrent < -0.1 {
				batteryState = "Discharging"
			} else if row.RBKReport.Charging != nil {
				batteryState = "Not charging"
			} else if chargeCurrent != nil {
				batteryState = "Idle"
			}
		}
		out[name] = RobotCoreStatus{
			UUID:             row.UUID,
			VehicleID:        row.VehicleID,
			IP:               ip,
			MAC:              mac,
			Odo:              row.RBKReport.Odo,
			TodayOdo:         row.RBKReport.TodayOdo,
			BatteryLevel:     batteryLevel,
			BatteryTempC:     batteryTemp,
			BatteryState:     batteryState,
			ConnectionStatus: row.ConnectionStatus,
			SeenAt:           seenAt,
			LastReceivedAt:   lastReceivedAt,
		}
	}
	return out, nil
}

func firstNumeric(values ...any) *float64 {
	for _, raw := range values {
		var value float64
		var ok bool
		switch typed := raw.(type) {
		case float64:
			value, ok = typed, true
		case float32:
			value, ok = float64(typed), true
		case int:
			value, ok = float64(typed), true
		case json.Number:
			value, _ = typed.Float64()
			ok = true
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(typed, "%")), 64)
			if err == nil {
				value, ok = parsed, true
			}
		}
		if ok {
			return &value
		}
	}
	return nil
}

func firstText(values ...any) string {
	for _, raw := range values {
		if value := strings.TrimSpace(fmt.Sprint(raw)); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func anyBool(raw any) bool {
	switch typed := raw.(type) {
	case bool:
		return typed
	case string:
		value, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return value
	case float64:
		return typed != 0
	}
	return false
}

func unixRobotStatusTime(raw any) time.Time {
	var n int64
	switch v := raw.(type) {
	case float64:
		n = int64(v)
	case int64:
		n = v
	case json.Number:
		parsed, err := v.Int64()
		if err != nil {
			return time.Time{}
		}
		n = parsed
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return time.Time{}
		}
		n = parsed
	default:
		return time.Time{}
	}
	if n <= 0 {
		return time.Time{}
	}
	// RDS Core labels these fields *_nsec, but field data observed in plants is
	// millisecond epoch (13 digits). Keep support for seconds and nanoseconds too.
	switch {
	case n > 1_000_000_000_000_000:
		return time.Unix(0, n).UTC()
	case n > 1_000_000_000_000:
		return time.UnixMilli(n).UTC()
	case n > 1_000_000_000:
		return time.Unix(n, 0).UTC()
	default:
		return time.Time{}
	}
}
func validIPv4(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.To4() != nil
}

// hexRe matches a 12-hex-digit MAC after its separators are stripped.
// Accepts colon (B8:27:EB:...), dash, and period separators, plus bare hex.
var hexRe = regexp.MustCompile(`(?i)^[0-9a-f]{12}$`)

// normaliseMAC returns the first well-formed candidate MAC, upper-cased and
// separated by colons (IEEE 802 form). Empty when no valid MAC is provided.
func normaliseMAC(candidates ...string) string {
	for _, raw := range candidates {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		// Strip any separator so we can validate the bare hex digits.
		compact := strings.NewReplacer(":", "", "-", "", ".", "", " ", "").Replace(s)
		if !hexRe.MatchString(compact) {
			continue
		}
		// Reformat as XX:XX:XX:XX:XX:XX.
		out := make([]byte, 0, 17)
		for i := 0; i < len(compact); i += 2 {
			if i > 0 {
				out = append(out, ':')
			}
			out = append(out, compact[i], compact[i+1])
		}
		return strings.ToUpper(string(out))
	}
	return ""
}

// postJSON POSTs a JSON body and returns the response as a slice of JSON lines,
// unwrapping a {"data":[...]}/{code,...data} envelope the same way fetchLines
// does. Exists because several RDS status endpoints (agvStatusCurrent,
// vehicleBatteryLevel) reject GET with 9006 "Request method 'GET' not supported".
func (c *Client) postJSON(urlStr string, payload any) ([]string, error) {
	var body []byte
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = b
	}
	req, err := http.NewRequest("POST", urlStr, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("token", c.token)
		req.Header.Set("Authorization", c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<21))
	if err != nil {
		return nil, err
	}
	// Unwrap envelope {code,msg,data:[...]} → array of raw JSON lines.
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err == nil && len(env.Data) > 0 {
		var arr []json.RawMessage
		if err := json.Unmarshal(env.Data, &arr); err == nil {
			out := make([]string, len(arr))
			for i, e := range arr {
				out[i] = string(e)
			}
			return out, nil
		}
		// data was a single object, not an array
		return []string{string(env.Data)}, nil
	}
	// Bare array or line-based fallback.
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		out := make([]string, len(arr))
		for i, e := range arr {
			out[i] = string(e)
		}
		return out, nil
	}
	return []string{string(raw)}, nil
}

func (c *Client) scrapeLogTable(urlStr string) ([]string, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("token", c.token)
		req.Header.Set("Authorization", c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<21))
	if err != nil {
		return nil, err
	}
	return scrapeTableRows(body), nil
}

func scrapeTableRows(html []byte) []string {
	htmlL := strings.ToLower(string(html))
	rows := strings.Split(htmlL, "</tr>")
	result := make([]string, 0)
	for _, row := range rows {
		if !strings.Contains(row, "<td") && !strings.Contains(row, "<th") {
			continue
		}
		text := stripTags(row)
		text = strings.Join(strings.Fields(text), " ")
		text = strings.TrimSpace(text)
		if text != "" {
			result = append(result, text)
		}
	}
	return result
}

func stripTags(s string) string {
	var result []byte
	inTag := false
	for i := 0; i < len(s); i++ {
		if s[i] == '<' {
			inTag = true
		} else if s[i] == '>' {
			inTag = false
			result = append(result, ' ')
		} else if !inTag {
			result = append(result, s[i])
		}
	}
	return string(result)
}

func dialTimeout(addr string, timeout time.Duration) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.Dial("tcp", addr)
}

func md5Hash(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return hexEncode(h.Sum(nil))
}

func sha256Hash(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hexEncode(h.Sum(nil))
}

func hexEncode(b []byte) string {
	const hex = "0123456789abcdef"
	res := make([]byte, len(b)*2)
	for i, c := range b {
		res[i*2] = hex[c>>4]
		res[i*2+1] = hex[c&0xf]
	}
	return string(res)
}
