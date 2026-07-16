package playbook

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	amrssh "drishti-amr-health/internal/ssh"
)

// HostTarget is a resolved host with decrypted credentials ready for SSH.
type HostTarget struct {
	ServerID   int
	ServerName string
	Host       string
	Port       int
	Username   string
	AuthType   string
	Password   string
	PrivateKey string
}

// Runner fans a single Task out to N hosts with a bounded worker pool and
// records per-host results. It is the Ansible-style batch execution engine.
type Runner struct {
	db            *pgxpool.Pool
	encryptionKey string
	concurrency   int
}

// NewRunner creates a Runner. concurrency controls how many hosts are touched
// simultaneously (default 10).
func NewRunner(db *pgxpool.Pool, key string, concurrency int) *Runner {
	if concurrency < 1 {
		concurrency = 10
	}
	return &Runner{db: db, encryptionKey: key, concurrency: concurrency}
}

// Result is the outcome of executing a task on one host.
type Result struct {
	Status string // "success" | "failed" | "skipped"
	Output string
	Error  string
}

// RunJob executes task on every target host, updating the batch_job_results
// rows as each host finishes. It blocks until all hosts complete or ctx is
// cancelled.
func (rn *Runner) RunJob(ctx context.Context, batchID int64, task Task, targets []HostTarget, params Params, dryRun bool) {
	total := len(targets)
	var succeeded, failed, skipped int
	var mu sync.Mutex

	// Channel of work + bounded worker pool.
	work := make(chan HostTarget)
	var wg sync.WaitGroup
	for i := 0; i < rn.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for host := range work {
				res := rn.runOnHost(ctx, task, host, params, dryRun)
				rn.saveResult(ctx, batchID, host, res)
				mu.Lock()
				switch res.Status {
				case "skipped":
					skipped++
				case "failed":
					failed++
				default:
					succeeded++
				}
				mu.Unlock()
			}
		}()
	}

	for _, t := range targets {
		select {
		case work <- t:
		case <-ctx.Done():
			break
		}
	}
	close(work)
	wg.Wait()

	status := "completed"
	if ctx.Err() != nil {
		status = "aborted"
	}
	rn.finalizeJob(ctx, batchID, status, total, succeeded, failed, skipped)
}

// runOnHost connects via SSH, runs the idempotence check (if any), then the
// task command. Returns a Result with status/output/error.
func (rn *Runner) runOnHost(ctx context.Context, task Task, host HostTarget, params Params, dryRun bool) Result {
	// Build the mutation command first so validation errors surface early.
	cmd, err := task.Run(params)
	if err != nil {
		return Result{Status: "failed", Error: fmt.Sprintf("invalid task parameters: %v", err)}
	}

	client, err := amrssh.Connect(amrssh.Config{
		Host:       host.Host,
		Port:       host.Port,
		Username:   host.Username,
		AuthType:   host.AuthType,
		Password:   host.Password,
		PrivateKey: host.PrivateKey,
	})
	if err != nil {
		return Result{Status: "failed", Error: fmt.Sprintf("SSH connect %s: %v", host.Host, err)}
	}
	defer client.Close()

	// Idempotence check: if the host is already in the desired state, skip.
	if task.Check != nil {
		checkCmd := task.Check(params)
		if checkCmd != "" {
			out, checkErr := client.Run(checkCmd)
			if checkErr == nil {
				msg := "Host is already in the desired state; no change needed."
				if trim := normalizeOutput(out); trim != "" {
					msg = trim
				}
				return Result{Status: "skipped", Output: msg}
			}
		}
	}

	if dryRun {
		return Result{Status: "success", Output: "Dry run: command would be executed:\n" + cmd}
	}

	out, runErr := client.Run(cmd)
	if runErr != nil {
		return Result{Status: "failed", Output: normalizeOutput(out), Error: runErr.Error()}
	}
	res := Result{Status: "success", Output: normalizeOutput(out)}
	if res.Output == "" {
		res.Output = "Command completed successfully."
	}
	return res
}

func normalizeOutput(s string) string {
	if len(s) > 20000 {
		s = s[:20000] + "\n... (output truncated)"
	}
	return trimSpace(s)
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\r' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 {
		last := s[len(s)-1]
		if last == ' ' || last == '\n' || last == '\r' || last == '\t' {
			s = s[:len(s)-1]
		} else {
			break
		}
	}
	return s
}

// ---- DB helpers ----

func (rn *Runner) saveResult(ctx context.Context, batchID int64, host HostTarget, res Result) {
	now := time.Now()
	_, err := rn.db.Exec(ctx, `
		INSERT INTO batch_job_results (batch_id, server_id, server_name, host, status, output, error, started_at, finished_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		batchID, host.ServerID, host.ServerName, host.Host, res.Status, res.Output, res.Error, now, now)
	if err != nil {
		log.Printf("playbook: save result for host %s: %v", host.Host, err)
	}
}

func (rn *Runner) finalizeJob(ctx context.Context, batchID int64, status string, total, succeeded, failed, skipped int) {
	_, err := rn.db.Exec(ctx, `
		UPDATE batch_jobs
		SET status=$1, total=$2, succeeded=$3, failed=$4, skipped=$5, finished_at=NOW()
		WHERE id=$6`,
		status, total, succeeded, failed, skipped, batchID)
	if err != nil {
		log.Printf("playbook: finalize batch %d: %v", batchID, err)
	}
}
