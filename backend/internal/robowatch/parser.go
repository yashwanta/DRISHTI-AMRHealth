package robowatch

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type NormalizedEntry struct {
	Plant             string    `json:"plant"`
	SourceSystem      string    `json:"source_system"`
	Timestamp         time.Time `json:"timestamp"`
	Robot             string    `json:"robot"`
	RobotIP           string    `json:"robot_ip"` // best-effort IP extracted at parse time
	User              string    `json:"user"`
	Action            string    `json:"action"`
	Category          string    `json:"category"`
	Severity          string    `json:"severity"`
	Message           string    `json:"message"`
	RawLog            string    `json:"raw_log"`
	Confidence        string    `json:"confidence"`
	ExecutionEvidence bool      `json:"execution_evidence"`
}

// anyIPRe matches any bare IPv4 address.
var anyIPRe = regexp.MustCompile(`\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})\b`)

// extractIP tries several patterns in order and returns the first match.
func extractIP(candidates ...string) string {
	// Priority 1: [Server:IP:port] pattern (Roboshop specific)
	serverRe := regexp.MustCompile(`\[Server:(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}):`)
	for _, s := range candidates {
		if m := serverRe.FindStringSubmatch(s); len(m) == 2 {
			return m[1]
		}
	}
	// Priority 2: any IP address
	for _, s := range candidates {
		if m := anyIPRe.FindStringSubmatch(s); len(m) == 2 {
			// Skip loopback / broadcast
			if m[1] != "127.0.0.1" && m[1] != "0.0.0.0" && m[1] != "255.255.255.255" {
				return m[1]
			}
		}
	}
	return ""
}

func NormalizeLog(raw string, plant, sourceSystem string) NormalizedEntry {
	entry := NormalizedEntry{
		Plant:        plant,
		SourceSystem: sourceSystem,
		RawLog:       raw,
		Timestamp:    time.Now(),
		Severity:     "info",
		Category:     "unknown",
		Confidence:   "low",
	}

	if j, err := parseJSONLog(raw); err == nil {
		applyJSONFields(&entry, j)
		return entry
	}

	applyRegexPatterns(&entry, raw)
	return entry
}

type jsonLogFields struct {
	Time      string `json:"time"`
	Timestamp string `json:"timestamp"`
	Robot     string `json:"robot"`
	RobotID   string `json:"robot_id"`
	RobotName string `json:"robot_name"`
	User      string `json:"user"`
	Username  string `json:"username"`
	Action    string `json:"action"`
	Type      string `json:"type"`
	Event     string `json:"event"`
	Category  string `json:"category"`
	Severity  string `json:"severity"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Msg       string `json:"msg"`
	// IP fields — various RDS/Roboshop API formats
	IP         string `json:"ip"`
	ServerIP   string `json:"server_ip"`
	RobotIP    string `json:"robot_ip"`
	ClientIP   string `json:"client_ip"`
	RemoteAddr string `json:"remote_addr"`
	Host       string `json:"host"`
}

func parseJSONLog(raw string) (jsonLogFields, error) {
	var j jsonLogFields
	err := json.Unmarshal([]byte(raw), &j)
	return j, err
}

func applyJSONFields(entry *NormalizedEntry, j jsonLogFields) {
	for _, src := range []string{j.Time, j.Timestamp} {
		if src == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, src); err == nil {
			entry.Timestamp = t
			break
		}
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", src, time.Local); err == nil {
			entry.Timestamp = t
			break
		}
		var unix int64
		if _, err := fmt.Sscanf(src, "%d", &unix); err == nil {
			entry.Timestamp = time.Unix(unix, 0)
			break
		}
	}

	if j.Robot != "" {
		entry.Robot = j.Robot
	} else if j.RobotID != "" {
		entry.Robot = j.RobotID
	} else if j.RobotName != "" {
		entry.Robot = j.RobotName
	}

	if j.User != "" {
		entry.User = j.User
	} else if j.Username != "" {
		entry.User = j.Username
	}

	if j.Action != "" {
		entry.Action = j.Action
	} else if j.Event != "" {
		entry.Action = j.Event
	} else if j.Type != "" {
		entry.Action = j.Type
	}

	if j.Category != "" {
		entry.Category = classifyCategory(j.Category)
	} else if entry.Action != "" {
		entry.Category = classifyCategory(entry.Action)
	}

	if j.Severity != "" {
		entry.Severity = normalizeSeverity(j.Severity)
	} else if j.Level != "" {
		entry.Severity = normalizeSeverity(j.Level)
	}

	if j.Message != "" {
		entry.Message = j.Message
	} else if j.Msg != "" {
		entry.Message = j.Msg
	}

	// Extract IP from JSON fields first, then fall back to message text
	for _, ipField := range []string{j.IP, j.ServerIP, j.RobotIP, j.ClientIP, j.RemoteAddr, j.Host} {
		if ipField != "" {
			if m := anyIPRe.FindStringSubmatch(ipField); len(m) == 2 {
				entry.RobotIP = m[1]
				break
			}
		}
	}
	if entry.RobotIP == "" {
		entry.RobotIP = extractIP(entry.Message, entry.RawLog)
	}

	entry.Confidence = "medium"
	entry.ExecutionEvidence = detectExecutionEvidence(entry.Action, entry.Message)
}

var (
	timeRe     = regexp.MustCompile(`(?i)\[?(20\d\d-\d\d-\d\d[ T]\d\d:\d\d:\d\d)`)
	robotRe    = regexp.MustCompile(`(?i)(?:robot[_\s]?[:#]?\s*)(\S+)`)
	userRe     = regexp.MustCompile(`(?i)(?:user[_\s]?[:#]?\s*)(\S+)`)
	actionRe   = regexp.MustCompile(`(?i)(?:action[_\s]?[:#]?\s*)(\S+)`)
	levelRe    = regexp.MustCompile(`(?i)\b(CRITICAL|FATAL|ERROR|WARN(?:ING)?|INFO|DEBUG|TRACE)\b`)
	categoryRe = regexp.MustCompile(`(?i)\b(charge|dock|goto|nav|move|error|fault|warn|status|health|battery|update|settings|reset|sync)\b`)
)

func applyRegexPatterns(entry *NormalizedEntry, raw string) {
	if m := timeRe.FindStringSubmatch(raw); len(m) > 1 {
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", m[1], time.Local); err == nil {
			entry.Timestamp = t
		}
	}
	if entry.Robot == "" {
		if m := robotRe.FindStringSubmatch(raw); len(m) > 1 {
			entry.Robot = m[1]
		}
	}
	if entry.User == "" {
		if m := userRe.FindStringSubmatch(raw); len(m) > 1 {
			entry.User = m[1]
		}
	}
	if entry.Action == "" {
		if m := actionRe.FindStringSubmatch(raw); len(m) > 1 {
			entry.Action = m[1]
		}
	}
	if entry.Severity == "info" {
		if m := levelRe.FindStringSubmatch(raw); len(m) > 1 {
			entry.Severity = normalizeSeverity(m[1])
		}
	}
	if entry.Action == "" {
		if m := categoryRe.FindStringSubmatch(raw); len(m) > 1 {
			entry.Action = m[1]
		}
	}
	if entry.Category == "unknown" {
		entry.Category = classifyCategory(entry.Action)
	}
	if entry.Message == "" {
		msg := strings.TrimSpace(raw)
		if len(msg) > 200 {
			msg = msg[:200] + "..."
		}
		entry.Message = msg
	}
	if entry.RobotIP == "" {
		entry.RobotIP = extractIP(entry.Message, raw)
	}
	entry.ExecutionEvidence = detectExecutionEvidence(entry.Action, entry.Message)
}

func classifyCategory(s string) string {
	s = strings.ToLower(s)
	switch {
	case containsAny(s, "charge", "charging", "battery", " soc", "voltage", "power"): return "charge"
	case containsAny(s, "dock", "docking", "undock"): return "dock"
	case containsAny(s, "goto", "nav", "move", "path", "route", "target"): return "navigation"
	case containsAny(s, "status", "heartbeat", "alive", "ping"): return "status"
	case containsAny(s, "error", "fault", "fail", "exception", "panic", "critical"): return "error"
	case containsAny(s, "update", "upgrade", "upload", "deploy", "sync"): return "update"
	case containsAny(s, "settings", "config", "reset", "default", "restore"): return "settings"
	case containsAny(s, "health", "diag", "sensor", "motor", "wheel", "lidar", "camera"): return "health"
	default: return "unknown"
	}
}

func normalizeSeverity(level string) string {
	switch strings.ToUpper(level) {
	case "CRITICAL", "FATAL": return "critical"
	case "ERROR": return "high"
	case "WARN", "WARNING": return "medium"
	case "DEBUG", "TRACE": return "low"
	default: return "info"
	}
}

func detectExecutionEvidence(action, message string) bool {
	s := strings.ToLower(action + " " + message)
	evidenceKw := []string{"charge_cmd", "dock_cmd", "goto_target", "set_charging",
		"navigation", "manual", "operator", "command", "task", "mission",
		"executing", "started", "completed", "failed", "reached"}
	nonEvidenceKw := []string{"heartbeat", "keepalive", "ping", "poll", "syslog", "cron", "system health"}
	return containsAny(s, evidenceKw...) && !containsAny(s, nonEvidenceKw...)
}

func containsAny(s string, parts ...string) bool {
	for _, p := range parts {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
