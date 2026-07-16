package agent

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Collector dependencies are expressed as small interfaces so the collector
// can be unit-tested with fakes and is decoupled from the concrete SSH / RDS /
// pgx clients.

// RDSLogSource fetches raw log lines from the RDS/RoboWatch API for a plant,
// window-filtered to [from, to].
type RDSLogSource interface {
	FetchLogs(from, to time.Time) ([]string, error)
}

// SSHRunner runs a command on a remote host and returns combined output.
type SSHRunner interface {
	Run(cmd string) (string, error)
}

// DBLogSource queries normalized log_events for a robot/time window.
type DBLogSource interface {
	RobotEvents(robotID string, from, to time.Time) ([]LogEntry, error)
	SourceEvents(kind, robotID string, from, to time.Time) ([]LogEntry, error)
}

// Collector fans out log collection across the four sources concurrently.
type Collector struct {
	rds RDSLogSource
	ssh SSHRunner
	db  DBLogSource
	// sshHost is used to label network/journal entries.
	sshHost string
}

func NewCollector(rds RDSLogSource, ssh SSHRunner, db DBLogSource, sshHost string) *Collector {
	return &Collector{rds: rds, ssh: ssh, db: db, sshHost: sshHost}
}

// Collect runs all sources concurrently, updating the job's source statuses via
// the store as each completes. It returns the merged, window-filtered log bundle.
// A source error marks that source unavailable but never aborts the run.
func (c *Collector) Collect(ctx context.Context, store *Store, jobID, robotID string, windowStart, windowEnd time.Time) []LogEntry {
	var (
		mu  sync.Mutex
		all []LogEntry
		wg  sync.WaitGroup
	)

	add := func(entries []LogEntry) {
		mu.Lock()
		all = append(all, entries...)
		mu.Unlock()
	}

	// --- Source 1: RDS API ---
	wg.Add(1)
	go func() {
		defer wg.Done()
		store.UpdateSource(jobID, SourceRDSAPI, StateInProgress, "Fetching...", 0, "")
		if c.rds == nil {
			store.UpdateSource(jobID, SourceRDSAPI, StateUnavailable, "Source not configured", 0, "no rds client")
			return
		}
		lines, err := c.rds.FetchLogs(windowStart, windowEnd)
		if err != nil {
			store.UpdateSource(jobID, SourceRDSAPI, StateUnavailable, "Source unreachable", 0, err.Error())
			return
		}
		entries := normalizeRDSLines(lines, robotID)
		store.UpdateSource(jobID, SourceRDSAPI, StateDone, resultText(len(entries), "entries pulled"), len(entries), "")
		add(entries)
	}()

	// --- Source 2: Roboshop / log_events DB (robot-scoped) ---
	wg.Add(1)
	go func() {
		defer wg.Done()
		store.UpdateSource(jobID, SourceDB, StateInProgress, "Querying...", 0, "")
		if c.db == nil {
			store.UpdateSource(jobID, SourceDB, StateUnavailable, "Source not configured", 0, "no db client")
			return
		}
		entries, err := c.db.RobotEvents(robotID, windowStart, windowEnd)
		if err != nil {
			store.UpdateSource(jobID, SourceDB, StateUnavailable, "Query failed", 0, err.Error())
			return
		}
		store.UpdateSource(jobID, SourceDB, StateDone, resultText(len(entries), "entries pulled"), len(entries), "")
		add(entries)
	}()

	// --- Source 3: remote system journal ---
	wg.Add(1)
	go func() {
		defer wg.Done()
		store.UpdateSource(jobID, SourceJournal, StateInProgress, "Fetching...", 0, "")
		entries := c.collectOverSSH(SourceJournal, journalCmd(windowStart, windowEnd, robotID), "journal", robotID)
		entries = c.withDBFallback(entries, "journal", robotID, windowStart, windowEnd)
		store.UpdateSource(jobID, SourceJournal, statusFor(entries), resultText(len(entries), "entries pulled"), len(entries), "")
		add(entries)
	}()

	// --- Source 4: DHCP / network logs ---
	wg.Add(1)
	go func() {
		defer wg.Done()
		store.UpdateSource(jobID, SourceNetwork, StateInProgress, "Fetching...", 0, "")
		entries := c.collectOverSSH(SourceNetwork, dhcpCmd(windowStart, windowEnd, robotID), "network", robotID)
		entries = c.withDBFallback(entries, "network", robotID, windowStart, windowEnd)
		store.UpdateSource(jobID, SourceNetwork, statusFor(entries), resultText(len(entries), "entries pulled"), len(entries), "")
		add(entries)
	}()

	wg.Wait()

	// Sort merged bundle by timestamp (stable enough; sources already ordered).
	mu.Lock()
	defer mu.Unlock()
	sortByTime(all)
	return all
}

// collectOverSSH runs one command on the remote host and normalizes the output.
// An SSH error is treated as "unavailable" (empty result).
func (c *Collector) collectOverSSH(label, cmd, srcTag, robotID string) []LogEntry {
	if c.ssh == nil {
		return nil
	}
	out, err := c.ssh.Run(cmd)
	if err != nil || strings.TrimSpace(out) == "" {
		return nil
	}
	return normalizeSSHLines(out, srcTag, robotID)
}

func (c *Collector) withDBFallback(entries []LogEntry, kind, robotID string, from, to time.Time) []LogEntry {
	if len(entries) > 0 || c.db == nil {
		return entries
	}
	dbEntries, err := c.db.SourceEvents(kind, robotID, from, to)
	if err != nil {
		return entries
	}
	if len(dbEntries) == 0 {
		return []LogEntry{}
	}
	return dbEntries
}

func statusFor(entries []LogEntry) string {
	if entries == nil {
		return StateUnavailable
	}
	return StateDone
}

func resultText(count int, suffix string) string {
	if count == 0 {
		return "No matching entries"
	}
	return itoa(count) + " " + suffix
}

// journalCmd builds a remote journalctl command scoped to the window and the
// robot token. Output is short-iso for reliable timestamp parsing.
func journalCmd(from, to time.Time, robotID string) string {
	return "journalctl --since " + shellQuote(from.Format("2006-01-02 15:04:05")) +
		" --until " + shellQuote(to.Format("2006-01-02 15:04:05")) +
		" --no-pager -o short-iso 2>/dev/null | grep -Ei " + shellQuote(robotGrep(robotID)) + " | tail -n 5000 || true"
}

// dhcpCmd greps syslog for DHCP/lease/connectivity activity plus the robot
// token. When a window is provided the output is narrowed to lines whose
// leading syslog timestamp falls within [from, to] using awk. Falls back to
// tail-3000 when no window is set.
func dhcpCmd(from, to time.Time, robotID string) string {
	pattern := "DHCP|lease|disconnect|connect|link down|link up"
	if id := strings.TrimSpace(robotID); id != "" {
		pattern += "|" + id
	}
	grep := "zgrep -hEi " + shellQuote(pattern) + " /var/log/syslog* 2>/dev/null"

	if from.IsZero() && to.IsZero() {
		return grep + " | tail -n 3000 || true"
	}

	// grep -P filter: extract lines whose leading timestamp falls within [from,to].
	// Uses perl-compatible regex so it works on modern Linux (Ubuntu/Debian).
	// Handles ISO "YYYY-MM-DD HH:MM:SS" (rsyslog) and syslog "Mon DD HH:MM:SS".
	// Falls back to passing all lines through if grep -P is unavailable.
	fromStr := from.UTC().Format("2006-01-02 15:04:05")
	toStr := to.UTC().Format("2006-01-02 15:04:05")
	// Use awk with POSIX-safe regex (no {n} quantifiers — use literal repetition).
	isoRe := `[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] [0-9][0-9]:[0-9][0-9]:[0-9][0-9]`
	awkProg := `BEGIN{from="` + fromStr + `";to="` + toStr + `"}` +
		`{if(match($0,/` + isoRe + `/)){ts=substr($0,RSTART,RLENGTH);if(ts>=from&&ts<=to)print;next}` +
		`if(NF>=3){ts=$1" "$2" "$3;if(ts>=from&&ts<=to)print}}`
	return grep + " | awk " + shellQuote(awkProg) + " | tail -n 3000 || true"
}

// robotGrep narrows journal output to lines likely about the given robot and
// DRISHTI-relevant keywords. Falls back to a broad AMR filter if no robot id.
func robotGrep(robotID string) string {
	base := "Roboshop|rds|AMR|robot|charge|battery|dock|disconnect|connect|error|fail|reset|default|shutdown|reboot|oom|killed"
	id := strings.TrimSpace(robotID)
	if id == "" {
		return base
	}
	if !strings.Contains(base, id) {
		base = id + "|" + base
	}
	return base
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func itoa(n int) string {
	// avoid strconv import churn in this file; n is small.
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func sortByTime(entries []LogEntry) {
	// Simple insertion sort: bundles are small (capped per source) and mostly
	// pre-sorted; this keeps the dependency surface minimal.
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j-1].Timestamp.After(entries[j].Timestamp); j-- {
			entries[j-1], entries[j] = entries[j], entries[j-1]
		}
	}
}
