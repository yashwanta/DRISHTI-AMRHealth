package agent

import (
	"strings"
	"time"
)

// nowUTC is overridden in tests to make timestamps deterministic.
var nowUTC = func() time.Time { return time.Now().UTC() }

// normalizeRDSLines turns raw RDS API log lines (JSON fragments or text rows)
// into LogEntries, keeping only those mentioning the robot token.
// If a robotID was supplied but zero lines matched, a warn-level diagnostic
// entry is injected so the UI shows an actionable message rather than silence.
func normalizeRDSLines(lines []string, robotID string) []LogEntry {
	id := strings.ToLower(strings.TrimSpace(robotID))
	var out []LogEntry
	now := nowUTC()
	total := 0
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		total++
		if id != "" && !strings.Contains(strings.ToLower(l), id) {
			continue
		}
		out = append(out, LogEntry{
			Timestamp: now,
			Source:    "rds",
			Level:     classifyLevel(l),
			Message:   truncate(l, 400),
		})
	}
	// Warn when the robot ID filter produced no hits but there were lines to search.
	// This usually means the ID format differs between the UI and RDS (e.g. "AMR-05"
	// vs "amr05" vs a UUID). Surface it so the operator knows to check the ID.
	if id != "" && len(out) == 0 && total > 0 {
		out = append(out, LogEntry{
			Timestamp: now,
			Source:    "rds",
			Level:     "warn",
			Message:   "No RDS log lines matched robot ID \"" + robotID + "\" — " + itoa(total) + " lines fetched but none contained this token. Check that the robot ID matches exactly how RDS records it (name, serial, or UUID).",
		})
	}
	return out
}

// normalizeSSHLines turns remote journal/syslog output into LogEntries.
func normalizeSSHLines(out, srcTag, robotID string) []LogEntry {
	id := strings.ToLower(strings.TrimSpace(robotID))
	var entries []LogEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || isNoise(line) {
			continue
		}
		if id != "" && !strings.Contains(strings.ToLower(line), id) {
			// keep AMR-relevant lines even without the robot token
			if !isAMRRelevant(line) {
				continue
			}
		}
		ts, msg := splitJournal(line)
		entries = append(entries, LogEntry{
			Timestamp: ts,
			Source:    srcTag,
			Level:     classifyLevel(msg),
			Message:   truncate(msg, 400),
		})
	}
	return entries
}

// splitJournal extracts a leading timestamp from a journal/syslog line and
// returns it with the remainder. Recognized forms:
//   - "2026-06-18 10:30:00 ..."   (short-iso with date)
//   - "Jun 18 10:30:00 ..."        (journal short-iso / syslog)
// Falls back to (now, whole line) if no timestamp is parseable.
func splitJournal(line string) (time.Time, string) {
	parts := strings.Fields(line)
	now := nowUTC()

	// Form A: "YYYY-MM-DD HH:MM:SS ..."
	if len(parts) >= 2 && looksISODate(parts[0]) {
		t, err := time.Parse("2006-01-02 15:04:05", parts[0]+" "+parts[1])
		if err == nil {
			return t.UTC(), strings.Join(parts[2:], " ")
		}
	}

	// Form B: "Mon D HH:MM:SS ..." (day may be space-padded as two fields).
	if len(parts) >= 3 && looksMonth(parts[0]) {
		dayIdx := 1
		// journalctl -o short-iso pads the day to two columns: "Jun  8 10:00:00"
		if looksMonth(parts[0]) && !isDay(parts[1]) {
			// e.g. "Jun" then already "10:00:00" — not the common path
		}
		timeField := parts[2]
		if len(parts) >= 4 && isDay(parts[2]) {
			dayIdx = 2
			timeField = parts[3]
		}
		t, err := time.Parse("Jan 2 15:04:05", parts[0]+" "+parts[dayIdx]+" "+timeField)
		if err == nil {
			return time.Date(now.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC), strings.Join(parts[dayIdx+2:], " ")
		}
	}

	return now, line
}

func looksISODate(s string) bool { return len(s) == 10 && s[4] == '-' && s[7] == '-' }

func isDay(s string) bool {
	if len(s) == 0 || len(s) > 2 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func looksMonth(s string) bool {
	for _, mo := range []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"} {
		if s == mo {
			return true
		}
	}
	return false
}

func classifyLevel(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "error"), strings.Contains(low, "fail"), strings.Contains(low, "fatal"),
		strings.Contains(low, "critical"), strings.Contains(low, "oom"), strings.Contains(low, "killed"):
		return "error"
	case strings.Contains(low, "warn"):
		return "warn"
	default:
		return "info"
	}
}

func isNoise(line string) bool {
	low := strings.ToLower(line)
	return strings.Contains(low, "-- no entries --")
}

func isAMRRelevant(line string) bool {
	low := strings.ToLower(line)
	for _, k := range []string{"robot", "amr", "rds", "roboshop", "battery", "charge", "dock", "oom", "killed", "disconnect", "connect", "offline", "unconnected", "closingstate", "link down", "link up"} {
		if strings.Contains(low, k) {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
