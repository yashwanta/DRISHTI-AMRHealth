package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return pool, nil
}

const Schema = `
CREATE TABLE IF NOT EXISTS servers (
    id            SERIAL PRIMARY KEY,
    name          TEXT NOT NULL,
    host          TEXT NOT NULL,
    port          INT  NOT NULL DEFAULT 22,
    username      TEXT NOT NULL,
    auth_type     TEXT NOT NULL DEFAULT 'password',
    password_enc  TEXT,
    private_key_enc TEXT,
    asset_type    TEXT NOT NULL DEFAULT 'server',
    proxmox_host  TEXT NOT NULL DEFAULT '',
    proxmox_port  INT  NOT NULL DEFAULT 22,
    proxmox_username TEXT NOT NULL DEFAULT '',
    proxmox_auth_type TEXT NOT NULL DEFAULT 'password',
    proxmox_password_enc TEXT,
    proxmox_private_key_enc TEXT,
    vmid          TEXT NOT NULL DEFAULT '',
    app_log_paths TEXT NOT NULL DEFAULT '',
    last_sync_at  TIMESTAMPTZ,
    status        TEXT NOT NULL DEFAULT 'unknown',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE servers ADD COLUMN IF NOT EXISTS proxmox_host TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN IF NOT EXISTS proxmox_port INT NOT NULL DEFAULT 22;
ALTER TABLE servers ADD COLUMN IF NOT EXISTS proxmox_username TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN IF NOT EXISTS proxmox_auth_type TEXT NOT NULL DEFAULT 'password';
ALTER TABLE servers ADD COLUMN IF NOT EXISTS proxmox_password_enc TEXT;
ALTER TABLE servers ADD COLUMN IF NOT EXISTS proxmox_private_key_enc TEXT;
ALTER TABLE servers ADD COLUMN IF NOT EXISTS vmid TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN IF NOT EXISTS app_log_paths TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN IF NOT EXISTS asset_type TEXT NOT NULL DEFAULT 'server';

UPDATE servers SET asset_type='server' WHERE asset_type = '';

-- Comma-separated tag list for host-group selection (e.g. linux,floor-1,workstation).
ALTER TABLE servers ADD COLUMN IF NOT EXISTS tags TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN IF NOT EXISTS os_type TEXT NOT NULL DEFAULT 'linux';

CREATE TABLE IF NOT EXISTS log_events (
    id          BIGSERIAL PRIMARY KEY,
    server_id   INT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    timestamp   TIMESTAMPTZ NOT NULL,
    event_type  TEXT NOT NULL,
    severity    TEXT NOT NULL,
    message     TEXT NOT NULL,
    source      TEXT NOT NULL,
    raw_line    TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_log_events_server_id ON log_events(server_id);
CREATE INDEX IF NOT EXISTS idx_log_events_timestamp  ON log_events(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_log_events_event_type ON log_events(event_type);

CREATE TABLE IF NOT EXISTS sync_jobs (
    id          SERIAL PRIMARY KEY,
    server_id   INT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    status      TEXT NOT NULL DEFAULT 'running',
    event_count INT NOT NULL DEFAULT 0,
    error       TEXT
);

CREATE TABLE IF NOT EXISTS action_runs (
    id          BIGSERIAL PRIMARY KEY,
    server_id   INT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    action      TEXT NOT NULL,
    command     TEXT NOT NULL,
    status      TEXT NOT NULL,
    output      TEXT NOT NULL DEFAULT '',
    error       TEXT NOT NULL DEFAULT '',
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_action_runs_server_id ON action_runs(server_id);
CREATE INDEX IF NOT EXISTS idx_action_runs_created_at ON action_runs(created_at DESC);

-- Ansible-style batch jobs: one playbook task fanned out to N hosts.
CREATE TABLE IF NOT EXISTS batch_jobs (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    task        TEXT NOT NULL,
    params      JSONB NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT 'running',
    total       INT  NOT NULL DEFAULT 0,
    succeeded   INT  NOT NULL DEFAULT 0,
    failed      INT  NOT NULL DEFAULT 0,
    skipped     INT  NOT NULL DEFAULT 0,
    dry_run     BOOLEAN NOT NULL DEFAULT FALSE,
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_batch_jobs_created_at ON batch_jobs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_batch_jobs_status ON batch_jobs(status);

-- Per-host result for a batch job.
CREATE TABLE IF NOT EXISTS batch_job_results (
    id          BIGSERIAL PRIMARY KEY,
    batch_id    BIGINT NOT NULL REFERENCES batch_jobs(id) ON DELETE CASCADE,
    server_id   INT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    server_name TEXT NOT NULL DEFAULT '',
    host        TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'pending',
    output      TEXT NOT NULL DEFAULT '',
    error       TEXT NOT NULL DEFAULT '',
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_batch_job_results_batch ON batch_job_results(batch_id);
CREATE INDEX IF NOT EXISTS idx_batch_job_results_status ON batch_job_results(status);

-- ===== Tier 1/2 Remediation Bridge =====
-- AMR Health writes suggestions when incidents are detected.
-- SiteOps reads them, executes the fix via playbooks, and writes the outcome.
-- Both apps share this table through the same PostgreSQL database.
CREATE TABLE IF NOT EXISTS remediation_suggestions (
    id              BIGSERIAL PRIMARY KEY,
    server_id       INT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    server_name     TEXT NOT NULL DEFAULT '',
    event_type      TEXT NOT NULL,
    severity        TEXT NOT NULL DEFAULT 'warning',
    description     TEXT NOT NULL,
    -- The suggested playbook task + params for SiteOps to execute.
    suggested_task  TEXT NOT NULL DEFAULT '',
    suggested_params JSONB NOT NULL DEFAULT '{}',
    -- Matched rule name that produced this suggestion.
    rule_name       TEXT NOT NULL DEFAULT '',
    confidence      TEXT NOT NULL DEFAULT 'medium',  -- high | medium | low
    auto_resolve    BOOLEAN NOT NULL DEFAULT FALSE,
    -- Lifecycle: pending -> approved -> running -> resolved | failed | escalated
    status          TEXT NOT NULL DEFAULT 'pending',
    -- Link to the action_run or batch_job that executed the fix.
    resolution_id   BIGINT NOT NULL DEFAULT 0,
    resolution_type TEXT NOT NULL DEFAULT '',  -- 'action_run' | 'batch_job'
    resolution_output TEXT NOT NULL DEFAULT '',
    resolved_by     TEXT NOT NULL DEFAULT '',
    -- The original event evidence for context.
    evidence        JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_remediation_status ON remediation_suggestions(status);
CREATE INDEX IF NOT EXISTS idx_remediation_server ON remediation_suggestions(server_id);
CREATE INDEX IF NOT EXISTS idx_remediation_created ON remediation_suggestions(created_at DESC);

-- Retention: prune resolved suggestions older than 90 days.
DELETE FROM remediation_suggestions WHERE status IN ('resolved','failed','escalated') AND created_at < NOW() - INTERVAL '90 days';

CREATE TABLE IF NOT EXISTS rag_history (
    id          BIGSERIAL PRIMARY KEY,
    username    TEXT NOT NULL DEFAULT '',
    question    TEXT NOT NULL,
    answer      TEXT NOT NULL,
    context_ids TEXT NOT NULL DEFAULT '',
    model       TEXT NOT NULL DEFAULT 'siteops-log-search',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_rag_history_created_at ON rag_history(created_at DESC);

-- Proxmox pveproxy access-log lines: keep genuine admin actions (mutating
-- methods or console/login endpoints) as ssh_login_activity, but delete the
-- routine dashboard read polling (GET /api2/.../config, /pending, /status/current)
-- which otherwise floods the table and buries real events.
-- Rows may store either the "pveproxy/access.log" filename prefix or just the
-- access-log content, so we key off the quoted "<METHOD> /api2/" request shape.
-- Read polling is exactly GET/HEAD/OPTIONS against /api2/ that is not a
-- console/login endpoint (which legitimately use GET).
DELETE FROM log_events
WHERE event_type = 'ssh_login_activity'
  AND lower(coalesce(raw_line, message)) SIMILAR TO '%"(get|head|options) /api2/%'
  AND lower(coalesce(raw_line, message)) NOT LIKE '%access/ticket%'
  AND lower(coalesce(raw_line, message)) NOT LIKE '%vncproxy%'
  AND lower(coalesce(raw_line, message)) NOT LIKE '%vncwebsocket%'
  AND lower(coalesce(raw_line, message)) NOT LIKE '%vncticket%'
  AND lower(coalesce(raw_line, message)) NOT LIKE '%termproxy%'
  AND lower(coalesce(raw_line, message)) NOT LIKE '%spiceproxy%';

UPDATE log_events
SET event_type='rds_map_update',
    severity=CASE
        WHEN COALESCE(raw_line, message) ~* '(fail|failed|failure|error|break|broken|rollback)' THEN 'high'
        ELSE 'info'
    END
WHERE (
    COALESCE(raw_line, message) ~* '(map|smap|scene)'
    AND COALESCE(raw_line, message) ~* '(push|upload|update|deploy|load|save|publish|import)'
    AND (
        source ILIKE '%rds%'
        OR source ILIKE '%roboshop%'
        OR source ILIKE '%journald_amr%'
        OR COALESCE(raw_line, message) ILIKE '%Roboshop%'
        OR COALESCE(raw_line, message) ILIKE '%RDS%'
    )
  );

UPDATE log_events
SET event_type='unknown', severity='low'
WHERE event_type='rds_map_update'
  AND NOT (
      source ILIKE '%rds%'
      OR source ILIKE '%roboshop%'
      OR source ILIKE '%journald_amr%'
      OR COALESCE(raw_line, message) ILIKE '%Roboshop%'
      OR COALESCE(raw_line, message) ILIKE '%RDS%'
  );

UPDATE log_events
SET event_type='warlink_failure',
    severity=CASE
        WHEN COALESCE(raw_line, message) ~* '(panic|segfault|core dumped|fatal)' THEN 'critical'
        WHEN COALESCE(raw_line, message) ~* '(deadman|returned 500|returned 502|returned 503|returned 504|not connected|SendUnitDataTransaction|still failing|WriteTag)' THEN 'high'
        ELSE 'medium'
    END
WHERE (
    COALESCE(raw_line, message) ~* '(WarLink|SendUnitDataTransaction|WriteTag)'
    AND COALESCE(raw_line, message) ~* '(fail|failed|failing|not connected|returned 4|returned 5|timeout|connection refused|deadman|panic|segfault|fatal|core dumped|error)'
    AND NOT (source ILIKE '%proxmox_host_memory%' AND COALESCE(raw_line, message) ~* '(/usr/bin/kvm|qemu-server| -name )')
);

UPDATE log_events
SET event_type='unknown', severity='low'
WHERE event_type='warlink_failure'
  AND source ILIKE '%proxmox_host_memory%'
  AND COALESCE(raw_line, message) ~* '(/usr/bin/kvm|qemu-server| -name )';

CREATE TABLE IF NOT EXISTS app_users (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL,
    location      TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'active',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_app_users_role ON app_users(role);
ALTER TABLE app_users ADD COLUMN IF NOT EXISTS permissions JSONB;

CREATE TABLE IF NOT EXISTS rds_log_events (
    id                 BIGSERIAL PRIMARY KEY,
    plant              TEXT NOT NULL,
    source_system      TEXT NOT NULL,
    timestamp          TIMESTAMPTZ NOT NULL,
    robot              TEXT NOT NULL DEFAULT '',
    "user"             TEXT NOT NULL DEFAULT '',
    action             TEXT NOT NULL DEFAULT '',
    category           TEXT NOT NULL DEFAULT 'unknown',
    severity           TEXT NOT NULL DEFAULT 'info',
    message            TEXT NOT NULL DEFAULT '',
    raw_log            TEXT NOT NULL DEFAULT '',
    confidence         TEXT NOT NULL DEFAULT 'low',
    execution_evidence BOOLEAN NOT NULL DEFAULT FALSE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_rds_log_events_plant ON rds_log_events(plant);
CREATE INDEX IF NOT EXISTS idx_rds_log_events_timestamp ON rds_log_events(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_rds_log_events_severity ON rds_log_events(severity);

CREATE TABLE IF NOT EXISTS rds_connection_status (
    plant               TEXT PRIMARY KEY,
    last_successful_pull TIMESTAMPTZ,
    logs_pulled          INT NOT NULL DEFAULT 0,
    last_error           TEXT,
    available_sources    TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE IF NOT EXISTS rds_credentials (
    plant TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    password_enc TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS agent_jobs (
    id                  VARCHAR(36) PRIMARY KEY,
    plant_id            VARCHAR(50) NOT NULL DEFAULT '',
    robot_id            VARCHAR(50) NOT NULL DEFAULT '',
    investigation_type  VARCHAR(100) NOT NULL DEFAULT '',
    focus               VARCHAR(500) NOT NULL DEFAULT '',
    window_start        TIMESTAMPTZ,
    window_end          TIMESTAMPTZ,
    status              VARCHAR(20) NOT NULL DEFAULT 'pending',
    log_bundle          JSONB,
    finding             JSONB,
    error               TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at        TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_agent_jobs_created_at ON agent_jobs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_jobs_plant_robot ON agent_jobs(plant_id, robot_id);

-- Add focus column to pre-existing agent_jobs tables (no-op if already present).
ALTER TABLE agent_jobs ADD COLUMN IF NOT EXISTS focus VARCHAR(500) NOT NULL DEFAULT '';

-- Add robot_ip column for storing extracted IPs at ingest time (no-op if already present).
ALTER TABLE rds_log_events ADD COLUMN IF NOT EXISTS robot_ip TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS robot_config_snapshots (
    id          SERIAL PRIMARY KEY,
    robot_id    VARCHAR(50) NOT NULL,
    plant_id    VARCHAR(50) NOT NULL DEFAULT '',
    config_json JSONB,
    checksum    VARCHAR(64) NOT NULL DEFAULT '',
    captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_robot_config_snapshots_robot ON robot_config_snapshots(robot_id, plant_id, captured_at DESC);

-- Authoritative per-robot status history from RDS Core's t_robotstatusrecord,
-- collected via SSH (source 'rds_robot_status'). Keyed by real UUID, unlike the
-- 192xx-port log lines which are protocol channels. Replaces the fragile log
-- parsing for fleet status / timeline / odometer.
CREATE TABLE IF NOT EXISTS robot_status_records (
    id           BIGSERIAL PRIMARY KEY,
    server_id    INT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    plant        TEXT NOT NULL DEFAULT '',
    uuid         TEXT NOT NULL,
    vehicle_name TEXT NOT NULL DEFAULT '',
    new_status   INT,
    old_status   INT,
    started_on   TIMESTAMPTZ,
    ended_on     TIMESTAMPTZ,
    duration_ms  BIGINT,
    odo          NUMERIC(19,2),
    today_odo    NUMERIC(19,2),
    collected_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_rsr_uuid_time ON robot_status_records(uuid, started_on DESC);
CREATE INDEX IF NOT EXISTS idx_rsr_server ON robot_status_records(server_id);
-- Dedup key: one transition per (server, uuid, started_on). Re-syncs upsert.
CREATE UNIQUE INDEX IF NOT EXISTS uq_rsr_server_uuid_started ON robot_status_records(server_id, uuid, started_on);

-- Minute-level battery telemetry sampled from the live RDS Core robot roster.
-- The unique minute bucket keeps the 30-second Fleet refresh from creating
-- duplicate samples while retaining enough detail for daily reports.
CREATE TABLE IF NOT EXISTS amr_battery_history (
    id            BIGSERIAL PRIMARY KEY,
    plant         TEXT NOT NULL,
    amr           TEXT NOT NULL,
    captured_at   TIMESTAMPTZ NOT NULL DEFAULT date_trunc('minute', NOW()),
    battery_level DOUBLE PRECISION,
    battery_temp_c DOUBLE PRECISION,
    battery_state TEXT NOT NULL DEFAULT '',
    source         TEXT NOT NULL DEFAULT 'rds_core'
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_amr_battery_history_minute
    ON amr_battery_history(plant, amr, captured_at);
CREATE INDEX IF NOT EXISTS idx_amr_battery_history_scope_time
    ON amr_battery_history(plant, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_amr_battery_history_amr_time
    ON amr_battery_history(plant, amr, captured_at DESC);

-- Durable, synchronized Wi-Fi survey data. Map identifiers intentionally remain
-- text because current RDS deployments expose MD5/version identifiers rather
-- than a shared numeric map catalogue.
CREATE TABLE IF NOT EXISTS wifi_scan_sessions (
    id BIGSERIAL PRIMARY KEY,
    plant_id TEXT NOT NULL,
    map_id TEXT NOT NULL,
    map_version TEXT NOT NULL,
    amr_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'running',
    moving_interval_seconds INT NOT NULL DEFAULT 2,
    stationary_interval_seconds INT NOT NULL DEFAULT 10,
    timestamp_tolerance_seconds INT NOT NULL DEFAULT 15,
    sample_count BIGINT NOT NULL DEFAULT 0,
    started_by TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    stopped_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_wifi_scan_sessions_scope ON wifi_scan_sessions(plant_id, map_id, map_version, started_at DESC);

-- Route positions are independent from Wi-Fi measurements so a traveled path
-- remains visible when an RSSI source is temporarily stale or unavailable.
CREATE TABLE IF NOT EXISTS wifi_survey_route_points (
    id BIGSERIAL PRIMARY KEY,
    session_id BIGINT NOT NULL REFERENCES wifi_scan_sessions(id) ON DELETE CASCADE,
    plant_id TEXT NOT NULL,
    map_id TEXT NOT NULL,
    map_version TEXT NOT NULL,
    amr_id TEXT NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL,
    x DOUBLE PRECISION NOT NULL,
    y DOUBLE PRECISION NOT NULL,
    heading DOUBLE PRECISION,
    moving BOOLEAN NOT NULL DEFAULT FALSE,
    speed DOUBLE PRECISION,
    connected BOOLEAN NOT NULL DEFAULT TRUE,
    nearest_location TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    fingerprint TEXT NOT NULL UNIQUE
);
CREATE INDEX IF NOT EXISTS idx_wifi_survey_route_scope ON wifi_survey_route_points(plant_id, map_id, map_version, timestamp);
CREATE INDEX IF NOT EXISTS idx_wifi_survey_route_session ON wifi_survey_route_points(session_id, timestamp);

CREATE TABLE IF NOT EXISTS wifi_scan_points (
    id BIGSERIAL PRIMARY KEY,
    session_id BIGINT REFERENCES wifi_scan_sessions(id) ON DELETE SET NULL,
    plant_id TEXT NOT NULL,
    map_id TEXT NOT NULL,
    map_version TEXT NOT NULL,
    amr_id TEXT NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL,
    x DOUBLE PRECISION NOT NULL,
    y DOUBLE PRECISION NOT NULL,
    heading DOUBLE PRECISION,
    moving BOOLEAN NOT NULL DEFAULT FALSE,
    speed DOUBLE PRECISION,
    rssi_dbm INT NOT NULL,
    snr_db DOUBLE PRECISION,
    noise_dbm DOUBLE PRECISION,
    ssid TEXT,
    bssid TEXT NOT NULL,
    previous_bssid TEXT,
    channel INT NOT NULL,
    frequency_mhz INT,
    band TEXT NOT NULL,
    connected BOOLEAN NOT NULL DEFAULT TRUE,
    disconnect_event BOOLEAN NOT NULL DEFAULT FALSE,
    roam_event BOOLEAN NOT NULL DEFAULT FALSE,
    latency_ms DOUBLE PRECISION,
    packet_loss_percent DOUBLE PRECISION,
    source_id TEXT NOT NULL,
    position_timestamp TIMESTAMPTZ NOT NULL,
    wifi_timestamp TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    fingerprint TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_wifi_scan_points_scope_time ON wifi_scan_points(plant_id, map_id, map_version, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_wifi_scan_points_amr_time ON wifi_scan_points(amr_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_wifi_scan_points_bssid_time ON wifi_scan_points(bssid, timestamp DESC);
CREATE UNIQUE INDEX IF NOT EXISTS uq_wifi_scan_points_fingerprint ON wifi_scan_points(fingerprint);

-- Retention: prune raw event tables older than 30 days to keep the DB bounded.
-- Runs on every backend startup (idempotent). Small metadata tables
-- (agent_jobs, sync_jobs, action_runs, rag_history) are kept indefinitely.
DELETE FROM log_events     WHERE created_at < NOW() - INTERVAL '30 days';
DELETE FROM rds_log_events WHERE created_at < NOW() - INTERVAL '30 days';
`
