package agent

import (
	"context"
	"encoding/json"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"drishti-amr-health/internal/robowatch"
)

// Config tunes the agent runtime (LLM endpoint + snapshot interval).
type Config struct {
	OllamaURL        string
	OllamaModel      string
	LLMAPIKey        string
	SnapshotInterval time.Duration
	EncryptionKey    string
}

// amrRe matches AMR-01, AMR_01, AMR01 and captures the numeric suffix.
var amrRe = regexp.MustCompile(`(?i)AMR[-_]?(\d+)`)

// Orchestrator owns the job store and runs investigations end-to-end.
type Orchestrator struct {
	store *Store
	db    *pgxpool.Pool
	cfg   Config
}

func NewOrchestrator(db *pgxpool.Pool, cfg Config) *Orchestrator {
	if cfg.SnapshotInterval == 0 {
		cfg.SnapshotInterval = 15 * time.Minute
	}
	return &Orchestrator{store: NewStore(), db: db, cfg: cfg}
}

func (o *Orchestrator) Store() *Store  { return o.store }
func (o *Orchestrator) Config() Config { return o.cfg }

// Robots lists known robots for a plant: first from the RDS API, then from
// log_events-derived robot tokens. Returns [{id, name}] maps for the dropdown.
func (o *Orchestrator) Robots(ctx context.Context, plantID string) []map[string]string {
	if plantID == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []map[string]string

	// Live RDS Core roster. This is the same source used by the AMR Fleet page
	// and avoids stale log-inferred IDs such as AMR-0 or AMR-250005.
	if pc := plantLookup(plantID); pc != nil {
		c := robowatch.NewClient(pc.BaseURL, pc.Port, pc.Username, "")
		if robots, err := c.CoreRobotStatus(); err == nil && len(robots) > 0 {
			ids := make([]string, 0, len(robots))
			for raw := range robots {
				id := normaliseRobotID(raw)
				if !validDropdownRobotID(id) || seen[id] {
					continue
				}
				seen[id] = true
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				out = append(out, map[string]string{"id": id, "name": id})
			}
			if len(out) > 0 {
				return out
			}
		}
	}

	// Source 2: rds_log_events.robot column â€” these are AMR names parsed directly
	// from RDS API responses (e.g. "AMR-02", "AMR-03").
	rdsRows, err := o.db.Query(ctx, `
		SELECT DISTINCT robot
		FROM rds_log_events
		WHERE plant = $1
		  AND robot <> ''
		  AND robot ~* 'AMR'
		ORDER BY robot
		LIMIT 50`, plantID)
	if err == nil {
		defer rdsRows.Close()
		for rdsRows.Next() {
			var robot string
			if err := rdsRows.Scan(&robot); err != nil || robot == "" {
				continue
			}
			id := normaliseRobotID(robot)
			if !validDropdownRobotID(id) || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, map[string]string{"id": id, "name": id})
		}
	}

	// Source 3: infer AMR-XX tokens from log_events message text.
	logRows, err := o.db.Query(ctx, `
		SELECT DISTINCT (regexp_matches(le.message, 'AMR[-_]?\d+', 'gi'))[1] AS rid
		FROM log_events le
		JOIN servers s ON s.id = le.server_id
		WHERE s.name ILIKE '%' || $1 || '%'
		  AND le.message ~* 'AMR'
		LIMIT 50`, plantID)
	if err == nil {
		defer logRows.Close()
		for logRows.Next() {
			var rid *string
			if err := logRows.Scan(&rid); err != nil || rid == nil || *rid == "" {
				continue
			}
			id := normaliseRobotID(*rid)
			if !validDropdownRobotID(id) || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, map[string]string{"id": id, "name": id})
		}
	}
	return out
}

// normaliseRobotID uppercases and standardises AMR01 / AMR_01 â†’ AMR-01.
func normaliseRobotID(raw string) string {
	upper := strings.ToUpper(strings.TrimSpace(raw))
	if m := amrRe.FindStringSubmatch(upper); len(m) == 2 {
		n, err := strconv.Atoi(m[1])
		if err == nil && n > 0 && n < 100 {
			return "AMR-" + strconv.Itoa(n/10) + strconv.Itoa(n%10)
		}
		return "AMR-" + m[1]
	}
	return upper
}

var dropdownAMRRe = regexp.MustCompile(`^AMR-(\d+)$`)

func validDropdownRobotID(id string) bool {
	m := dropdownAMRRe.FindStringSubmatch(id)
	if len(m) != 2 {
		return false
	}
	n, err := strconv.Atoi(m[1])
	return err == nil && n > 0 && n <= 999
}

// Snapshots returns recent config snapshots (newest first).
func (o *Orchestrator) Snapshots(ctx context.Context, robotID string) []any {
	q := `SELECT robot_id, plant_id, checksum, captured_at FROM robot_config_snapshots`
	args := []any{}
	if robotID != "" {
		q += ` WHERE robot_id=$1`
		args = append(args, robotID)
	}
	q += ` ORDER BY captured_at DESC LIMIT 20`
	rows, err := o.db.Query(ctx, q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []any
	for rows.Next() {
		var rid, pid, sum string
		var at time.Time
		if err := rows.Scan(&rid, &pid, &sum, &at); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"robot_id": rid, "plant_id": pid, "checksum": sum, "captured_at": at,
		})
	}
	return out
}

// Start creates and persists a job, then runs it asynchronously. Returns the
// job id immediately so the HTTP handler can respond without blocking.
func (o *Orchestrator) Start(plantID, robotID, investigationType, focus string, windowStart, windowEnd time.Time) (string, error) {
	job := o.store.New(plantID, robotID, investigationType, focus, windowStart, windowEnd)
	o.persistJob(job)
	go o.run(context.Background(), job.ID)
	return job.ID, nil
}

// run is the job lifecycle: pending -> collecting -> analyzing -> complete/error.
func (o *Orchestrator) run(ctx context.Context, jobID string) {
	job, ok := o.store.Snapshot(jobID)
	if !ok {
		return
	}

	o.store.SetStatus(jobID, JobCollecting, "")

	collector, err := o.buildCollector(job.PlantID)
	if err != nil {
		log.Printf("agent job %s: collector build: %v", jobID, err)
		collector = o.minimalCollector()
	}

	bundle := collector.Collect(ctx, o.store, jobID, job.RobotID, job.WindowStart, job.WindowEnd)
	o.store.AppendLogs(jobID, bundle)

	// Decide outcome from source results.
	totalDone, allUnavailable := summarizeSources(o.store.SnapshotSourceStatus(jobID))
	if totalDone == 0 && len(bundle) == 0 {
		msg := "No events found in this time range for this robot"
		if allUnavailable {
			msg = "All log sources were unavailable; check RDS API and SSH connectivity"
		}
		o.store.SetStatus(jobID, JobError, msg)
		o.persistFinal(jobID, JobError, msg, nil)
		return
	}

	o.store.SetStatus(jobID, JobAnalyzing, "")

	// Try the LLM; fall back to deterministic rules on any failure.
	live, _ := o.store.Get(jobID)
	finding, llmErr := AnalyzeWithLLM(o.cfg.OllamaURL, o.cfg.OllamaModel, o.cfg.LLMAPIKey, live)
	if llmErr != nil {
		log.Printf("agent job %s: LLM unavailable (%v), using rule-based fallback", jobID, llmErr)
		finding = FallbackFinding(live)
		finding.LLMNote = "LLM unavailable â€” raw logs collected, manual analysis recommended. Showing rule-based analysis."
	}

	o.store.SetFinding(jobID, finding)
	o.store.SetStatus(jobID, JobComplete, "")
	o.persistFinal(jobID, JobComplete, "", finding)
}

// summarizeSources reports total collected entries and whether every source was
// unavailable. Declared here to keep store.go free of cross-file assumptions.
func summarizeSources(sources []SourceStatus) (totalDone int, allUnavailable bool) {
	if len(sources) == 0 {
		return 0, true
	}
	allUnavailable = true
	for _, s := range sources {
		if s.State == StateDone {
			totalDone += s.Count
			allUnavailable = false
		} else if s.State == StatePending || s.State == StateInProgress {
			allUnavailable = false
		}
	}
	return totalDone, allUnavailable
}

// --- DB persistence helpers ---

func (o *Orchestrator) persistJob(job *AgentJob) {
	_, err := o.db.Exec(context.Background(), `
		INSERT INTO agent_jobs (id, plant_id, robot_id, investigation_type, focus, window_start, window_end, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		job.ID, job.PlantID, job.RobotID, job.InvestigationType, job.Focus, job.WindowStart, job.WindowEnd, job.Status)
	if err != nil {
		log.Printf("agent: persist job %s: %v", job.ID, err)
	}
}

func (o *Orchestrator) persistFinal(jobID, status, errMsg string, finding *AgentFinding) {
	// Cap the raw logs stored in agent_jobs.finding (JSONB) so one large job
	// cannot bloat the table. The in-memory job keeps the full bundle for the
	// live session; only the persisted copy is trimmed.
	if finding != nil {
		stored := finding
		if len(stored.RawLogs) > 200 {
			stored = &AgentFinding{
				RootCause: stored.RootCause, Confidence: stored.Confidence,
				Factors: stored.Factors, Timeline: stored.Timeline, Prevention: stored.Prevention,
				RawLogs: stored.RawLogs[:200], Via: stored.Via, LLMNote: stored.LLMNote,
			}
		}
		_, _ = o.db.Exec(context.Background(), `
			UPDATE agent_jobs SET status=$2, error=$3, completed_at=NOW(), finding=$4 WHERE id=$1`,
			jobID, status, errMsg, findingJSON(stored))
	} else {
		_, _ = o.db.Exec(context.Background(), `
			UPDATE agent_jobs SET status=$2, error=$3, completed_at=NOW() WHERE id=$1`,
			jobID, status, errMsg)
	}
}

// robotsFromRDS probes the RDS API for a robot list. The RDS API shape varies,
// so this tries common fields (id/robotId/robot_id/name) on any JSON it gets.
func robotsFromRDS(c *robowatch.Client) ([]string, error) {
	lines, err := c.FetchLogs(time.Time{}, time.Time{})
	if err != nil {
		return nil, err
	}
	var ids []string
	seen := map[string]bool{}
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || (!strings.HasPrefix(l, "{") && !strings.HasPrefix(l, "[")) {
			continue
		}
		// Try to find id-like fields anywhere in the JSON text.
		for _, m := range idRe.FindAllStringSubmatch(l, -1) {
			id := strings.Trim(m[1], `"' `)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, errNoRobots
	}
	return ids, nil
}

var idRe = regexp.MustCompile(`"(?:id|robotId|robot_id|robotSN|robotSn|name|robotCode)"\s*:\s*"([^"]+)"`)

var errNoRobots = &simpleErr{"no robot ids in RDS response"}

type simpleErr struct{ msg string }

func (e *simpleErr) Error() string { return e.msg }

// keep encoding/json referenced (used by snapshots/adapters via Marshal in
// other files of the package; declared here defensively).
var _ = json.Marshal
