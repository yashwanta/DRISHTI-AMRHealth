package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"time"

	"drishti-amr-health/internal/config"
	"drishti-amr-health/internal/robowatch"
)

// Snapshotter periodically captures each robot's config/status from the RDS API
// into robot_config_snapshots, recording changes (read-only capture; no restore).
type Snapshotter struct {
	orch    *Orchestrator
	interval time.Duration
}

func NewSnapshotter(o *Orchestrator) *Snapshotter {
	return &Snapshotter{orch: o, interval: o.cfg.SnapshotInterval}
}

// Run is the background loop (called as `go snap.Run()`); it ticks every
// interval until the process exits.
func (s *Snapshotter) Run() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	// Capture once shortly after start.
	go s.captureAll()
	for range ticker.C {
		s.captureAll()
	}
}

// captureAll snapshots the status/config of every robot in every configured plant.
func (s *Snapshotter) captureAll() {
	for _, plant := range plantNames() {
		s.capturePlant(plant)
	}
}

func (s *Snapshotter) capturePlant(plantID string) {
	pc := plantLookup(plantID)
	if pc == nil {
		return
	}
	pw := plantPassword(plantID)
	if pw == "" {
		return
	}
	client := robowatch.NewClient(pc.BaseURL, pc.Port, pc.Username, pw)

	// The RDS API does not expose a dedicated config endpoint; capture the robot
	// list + AGV status JSON as a representative "config" snapshot.
	statusJSON, err := client.FetchLogs(time.Time{}, time.Time{})
	if err != nil {
		return // RDS unreachable this tick â€” try again next interval.
	}
	blob, _ := json.Marshal(statusJSON)
	checksum := sha256Hex(blob)

	// One snapshot row per (plant, capture) is enough to detect change; group
	// all robots' status under the plant for diffing.
	if !s.changedSinceLast(plantID, "_plant_", checksum) {
		return
	}
	_, err = s.orch.db.Exec(context.Background(), `
		INSERT INTO robot_config_snapshots (robot_id, plant_id, config_json, checksum)
		VALUES ($1,$2,$3,$4)`,
		"_plant_", plantID, blob, checksum)
	if err != nil {
		log.Printf("agent snapshot plant %s: %v", plantID, err)
	} else {
		log.Printf("agent snapshot: config change recorded for plant %s", plantID)
	}
}

// changedSinceLast reports whether checksum differs from the most recent stored
// snapshot for the (plant, robot) pair.
func (s *Snapshotter) changedSinceLast(plantID, robotID, checksum string) bool {
	var last string
	err := s.orch.db.QueryRow(context.Background(), `
		SELECT checksum FROM robot_config_snapshots
		WHERE plant_id=$1 AND robot_id=$2
		ORDER BY captured_at DESC LIMIT 1`, plantID, robotID).Scan(&last)
	if err != nil {
		return true // no prior snapshot -> record
	}
	return last != checksum
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func plantNames() []string {
	var names []string
	for _, p := range config.AllPlants() {
		names = append(names, p.Name)
	}
	return names
}
