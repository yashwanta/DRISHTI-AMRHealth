package agent

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"drishti-amr-health/internal/config"
	"drishti-amr-health/internal/robowatch"
	amrssh "drishti-amr-health/internal/ssh"
)

// ---- collector adapter implementations ----

// rdsAdapter wraps the RoboWatch client to satisfy RDSLogSource.
type rdsAdapter struct{ client *robowatch.Client }

func (a *rdsAdapter) FetchLogs(from, to time.Time) ([]string, error) {
	if a == nil || a.client == nil {
		return nil, fmt.Errorf("no rds client")
	}
	return a.client.FetchLogs(from, to)
}

// sshAdapter wraps the SSH client to satisfy SSHRunner.
type sshAdapter struct{ client *amrssh.Client }

func (a *sshAdapter) Run(cmd string) (string, error) {
	if a == nil || a.client == nil {
		return "", fmt.Errorf("no ssh client")
	}
	return a.client.Run(cmd)
}

// dbAdapter queries the normalized log_events table to satisfy DBLogSource.
type dbAdapter struct {
	db    *pgxpool.Pool
	plant string
}

func (d *dbAdapter) RobotEvents(robotID string, from, to time.Time) ([]LogEntry, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("no db")
	}
	rows, err := d.db.Query(context.Background(), `
		SELECT le.timestamp, le.event_type, le.severity, le.source, le.message
		FROM log_events le
		JOIN servers s ON s.id = le.server_id
		WHERE le.timestamp BETWEEN $1 AND $2
		  AND (`+plantSQLCondition()+`)
		  AND ($4 = '' OR le.message ILIKE '%' || $4 || '%' OR le.source ILIKE '%' || $4 || '%')
		ORDER BY le.timestamp ASC
		LIMIT 500`,
		from, to, d.plant, robotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLogEntries(rows, "db")
}

func (d *dbAdapter) SourceEvents(kind, robotID string, from, to time.Time) ([]LogEntry, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("no db")
	}
	var sourceFilter string
	switch kind {
	case "journal":
		sourceFilter = `(le.source ILIKE 'journald%' OR le.source IN ('syslog','kern.log','system_info') OR le.event_type IN ('system_boot','system_shutdown','error','warning','rds_core_issue','robot_offline','robot_online'))`
	case "network":
		sourceFilter = `(le.source IN ('rds_network_neighbors','live_amr_tcp','syslog','kern.log') OR le.event_type IN ('network_dhcp_failure','power_network_event','robot_offline','robot_online') OR le.message ILIKE '%DHCP%' OR le.message ILIKE '%lease%' OR le.message ILIKE '%link up%' OR le.message ILIKE '%link down%' OR le.message ILIKE '%REACHABLE%' OR le.message ILIKE '%STALE%' OR le.message ILIKE '%FAILED%')`
	default:
		return nil, fmt.Errorf("unknown source kind %q", kind)
	}
	rows, err := d.db.Query(context.Background(), `
		SELECT le.timestamp, le.event_type, le.severity, le.source, le.message
		FROM log_events le
		JOIN servers s ON s.id = le.server_id
		WHERE le.timestamp BETWEEN $1 AND $2
		  AND (`+plantSQLCondition()+`)
		  AND `+sourceFilter+`
		  AND ($4 = '' OR le.message ILIKE '%' || $4 || '%' OR le.message ~* 'AMR|robot|rds|roboshop|disconnect|connect|error|fail|reset|shutdown|reboot|DHCP|lease|link|REACHABLE|STALE|FAILED')
		ORDER BY le.timestamp ASC
		LIMIT 500`,
		from, to, d.plant, robotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLogEntries(rows, kind+"-db")
}

func plantSQLCondition() string {
	return `$3 = ''
		   OR s.name ILIKE '%' || $3 || '%'
		   OR ($3 = 'Springfield' AND s.host LIKE '10.222.%')
		   OR ($3 = 'Hopkinsville' AND s.host LIKE '10.216.%')
		   OR ($3 = 'Shelbyville' AND s.host LIKE '10.205.%')`
}

type logRows interface {
	Next() bool
	Scan(dest ...any) error
}

func scanLogEntries(rows logRows, defaultSource string) ([]LogEntry, error) {
	var out []LogEntry
	for rows.Next() {
		var ts time.Time
		var etype, sev, src, msg string
		if err := rows.Scan(&ts, &etype, &sev, &src, &msg); err != nil {
			continue
		}
		if src == "" {
			src = defaultSource
		}
		out = append(out, LogEntry{
			Timestamp: ts,
			Source:    src,
			Level:     levelFromSeverity(sev, msg),
			Message:   msg,
		})
	}
	return out, nil
}

// ---- orchestrator helpers (plant -> server / collector wiring) ----

func (o *Orchestrator) buildCollector(plantID string) (*Collector, error) {
	var rds RDSLogSource
	if pc := plantLookup(plantID); pc != nil {
		if pw := plantPassword(plantID); pw != "" {
			rds = &rdsAdapter{client: robowatch.NewClient(pc.BaseURL, pc.Port, pc.Username, pw)}
		}
	}

	var ssh SSHRunner
	host, runner, err := o.sshForPlant(plantID)
	if err == nil && runner != nil {
		ssh = runner
	} else if err != nil {
		o.logf("no SSH target for plant %s: %v", plantID, err)
	}

	return NewCollector(rds, ssh, &dbAdapter{db: o.db, plant: plantID}, host), nil
}

func (o *Orchestrator) minimalCollector() *Collector {
	return NewCollector(nil, nil, &dbAdapter{db: o.db}, "")
}

func (o *Orchestrator) sshForPlant(plantID string) (string, SSHRunner, error) {
	host, port, user, authType, passEnc, keyEnc, err := o.loadServerForPlant(plantID)
	if err != nil {
		return "", nil, err
	}
	password, _ := decryptLocal(o.cfg.EncryptionKey, passEnc)
	privateKey, _ := decryptLocal(o.cfg.EncryptionKey, keyEnc)
	client, err := amrssh.Connect(amrssh.Config{
		Host: host, Port: port, Username: user, AuthType: authType,
		Password: password, PrivateKey: privateKey,
	})
	if err != nil {
		return host, nil, err
	}
	return host, &sshAdapter{client: client}, nil
}

// loadServerForPlant resolves the plant name to a server row. Matching is by the
// FleetManager host or by the plant token appearing in the server name.
func (o *Orchestrator) loadServerForPlant(plantID string) (host string, port int, user, authType, passEnc, keyEnc string, err error) {
	// Prefer the plant's own host if it has one configured.
	if pc := plantLookup(plantID); pc != nil {
		plantHost := hostFromURL(pc.BaseURL)
		row := o.db.QueryRow(context.Background(), `
			SELECT host, port, username, auth_type, COALESCE(password_enc,''), COALESCE(private_key_enc,'')
			FROM servers
			WHERE host=$1 OR name ILIKE '%' || $2 || '%'
			ORDER BY CASE WHEN host=$1 THEN 0 ELSE 1 END
			LIMIT 1`, plantHost, plantID)
		err = row.Scan(&host, &port, &user, &authType, &passEnc, &keyEnc)
		if err == nil {
			return host, port, user, authType, passEnc, keyEnc, nil
		}
	}
	// Fallback: any server whose name mentions the plant token.
	row := o.db.QueryRow(context.Background(), `
		SELECT host, port, username, auth_type, COALESCE(password_enc,''), COALESCE(private_key_enc,'')
		FROM servers WHERE name ILIKE '%' || $1 || '%' LIMIT 1`, plantID)
	err = row.Scan(&host, &port, &user, &authType, &passEnc, &keyEnc)
	return host, port, user, authType, passEnc, keyEnc, err
}

func (o *Orchestrator) logf(format string, args ...any) {
	log.Printf("agent: "+format, args...)
}

// ---- local decrypt (mirrors handlers.decrypt; isolated to avoid import cycle) ----

func decryptLocal(key, ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	k := padKeyLocal(key)
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func padKeyLocal(key string) []byte {
	b := []byte(key)
	if len(b) >= 32 {
		return b[:32]
	}
	padded := make([]byte, 32)
	copy(padded, b)
	return padded
}

func findingJSON(f *AgentFinding) []byte {
	b, err := json.Marshal(f)
	if err != nil {
		return []byte("null")
	}
	return b
}

func levelFromSeverity(sev, msg string) string {
	switch sev {
	case "critical", "high":
		return "error"
	case "medium":
		l := strings.ToLower(msg)
		for _, k := range []string{"error", "fail", "fatal"} {
			if strings.Contains(l, k) {
				return "error"
			}
		}
		return "warn"
	default:
		return "info"
	}
}

// hostFromURL extracts the bare host from a URL-like string.
func hostFromURL(u string) string {
	s := u
	for _, p := range []string{"https://", "http://"} {
		s = strings.TrimPrefix(s, p)
	}
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	return s
}

// plantLookup returns the configured plant (from config.AllPlants), matching by
// name (case-insensitive) or by the host embedded in its BaseURL.
func plantLookup(plantID string) *config.PlantConfig {
	for _, p := range config.AllPlants() {
		if strings.EqualFold(p.Name, plantID) {
			return &p
		}
		if hostFromURL(p.BaseURL) == plantID {
			return &p
		}
	}
	return nil
}

// plantPassword returns the plant's RoboWatch password from its env var.
func plantPassword(plantID string) string {
	if p := plantLookup(plantID); p != nil {
		return config.GetRobowatchPassword(p.Name)
	}
	return ""
}
