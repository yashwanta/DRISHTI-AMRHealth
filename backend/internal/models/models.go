package models

import "time"

type Server struct {
	ID                   int        `json:"id"`
	Name                 string     `json:"name"`
	Host                 string     `json:"host"`
	Port                 int        `json:"port"`
	Username             string     `json:"username"`
	AuthType             string     `json:"auth_type"`
	PasswordEnc          string     `json:"-"`
	PrivateKeyEnc        string     `json:"-"`
	AssetType            string     `json:"asset_type"`
	ProxmoxHost          string     `json:"proxmox_host"`
	ProxmoxPort          int        `json:"proxmox_port"`
	ProxmoxUsername      string     `json:"proxmox_username"`
	ProxmoxAuthType      string     `json:"proxmox_auth_type"`
	ProxmoxPasswordEnc   string     `json:"-"`
	ProxmoxPrivateKeyEnc string     `json:"-"`
	VMID                 string     `json:"vmid"`
	AppLogPaths          string     `json:"app_log_paths"`
	OSType               string     `json:"os_type"`
	LastSyncAt           *time.Time `json:"last_sync_at"`
	Status               string     `json:"status"`
	CreatedAt            time.Time  `json:"created_at"`
}

type LogEvent struct {
	ID                 int64        `json:"id"`
	ServerID           int          `json:"server_id"`
	ServerName         string       `json:"server_name,omitempty"`
	Timestamp          time.Time    `json:"timestamp"`
	EventType          string       `json:"event_type"`
	Severity           string       `json:"severity"`
	Message            string       `json:"message"`
	Source             string       `json:"source"`
	RawLine            string       `json:"raw_line,omitempty"`
	PlainEnglish       string       `json:"plain_english,omitempty"`
	RecommendedAction  string       `json:"recommended_action,omitempty"`
	EvidenceClass      string       `json:"evidence_class,omitempty"`
	EvidenceConfidence string       `json:"evidence_confidence,omitempty"`
	EvidenceBadges     []string     `json:"evidence_badges,omitempty"`
	ExecutionEvidence  *bool        `json:"execution_evidence,omitempty"`
	TargetIDs          []string     `json:"target_ids,omitempty"`
	OOMAnalysis        *OOMAnalysis `json:"oom_analysis,omitempty"`
	CreatedAt          time.Time    `json:"created_at"`
}

type SyncJob struct {
	ID         int        `json:"id"`
	ServerID   int        `json:"server_id"`
	ServerName string     `json:"server_name,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Status     string     `json:"status"`
	EventCount int        `json:"event_count"`
	Error      string     `json:"error,omitempty"`
}

type DashboardStats struct {
	TotalServers      int `json:"total_servers"`
	OnlineServers     int `json:"online_servers"`
	TotalEvents       int `json:"total_events"`
	CriticalEvents    int `json:"critical_events"`
	CrashCount        int `json:"crash_count"`
	PowerOffCount     int `json:"power_off_count"`
	ErrorCount        int `json:"error_count"`
	RobotOfflineCount int `json:"robot_offline_count"`
	RobotOnlineCount  int `json:"robot_online_count"`
	DiskErrorCount    int `json:"disk_error_count"`
	UbuntuEventCount  int `json:"ubuntu_event_count"`
	ProxmoxEventCount int `json:"proxmox_event_count"`
	VMEventCount      int `json:"vm_event_count"`
	MemoryEventCount  int `json:"memory_event_count"`
	BackupEventCount  int `json:"backup_event_count"`
	RDSCoreIssueCount int `json:"rds_core_issue_count"`
	RDSMapUpdateCount int `json:"rds_map_update_count"`
	WarLinkIssueCount int `json:"warlink_issue_count"`
}

type ServerRequest struct {
	Name              string `json:"name"`
	Host              string `json:"host"`
	Port              int    `json:"port"`
	Username          string `json:"username"`
	AuthType          string `json:"auth_type"`
	Password          string `json:"password,omitempty"`
	PrivateKey        string `json:"private_key,omitempty"`
	AssetType         string `json:"asset_type,omitempty"`
	ProxmoxHost       string `json:"proxmox_host,omitempty"`
	ProxmoxPort       int    `json:"proxmox_port,omitempty"`
	ProxmoxUsername   string `json:"proxmox_username,omitempty"`
	ProxmoxAuthType   string `json:"proxmox_auth_type,omitempty"`
	ProxmoxPassword   string `json:"proxmox_password,omitempty"`
	ProxmoxPrivateKey string `json:"proxmox_private_key,omitempty"`
	VMID              string `json:"vmid,omitempty"`
	AppLogPaths       string `json:"app_log_paths,omitempty"`
	OSType            string `json:"os_type,omitempty"`
}

type IncidentEvidence struct {
	Timestamp time.Time `json:"timestamp"`
	EventType string    `json:"event_type"`
	Severity  string    `json:"severity"`
	Source    string    `json:"source"`
	Message   string    `json:"message"`
}

type IncidentSummary struct {
	ServerID       int                `json:"server_id"`
	ServerName     string             `json:"server_name"`
	ProxmoxHost    string             `json:"proxmox_host"`
	VMID           string             `json:"vmid"`
	From           time.Time          `json:"from"`
	To             time.Time          `json:"to"`
	WhatHappened   string             `json:"what_happened"`
	StartedAt      *time.Time         `json:"started_at"`
	RecoveredAt    *time.Time         `json:"recovered_at"`
	RootCause      string             `json:"root_cause"`
	RecommendedFix string             `json:"recommended_fix"`
	OOMAnalysis    *OOMAnalysis       `json:"oom_analysis,omitempty"`
	Evidence       []IncidentEvidence `json:"evidence"`
}

type OOMAnalysis struct {
	KilledVMID     string  `json:"killed_vmid,omitempty"`
	KilledVMName   string  `json:"killed_vm_name,omitempty"`
	KilledPID      string  `json:"killed_pid,omitempty"`
	KilledProcess  string  `json:"killed_process,omitempty"`
	KilledAnonGB   float64 `json:"killed_anon_gb,omitempty"`
	KilledTotalGB  float64 `json:"killed_total_gb,omitempty"`
	TopVMID        string  `json:"top_vmid,omitempty"`
	TopVMName      string  `json:"top_vm_name,omitempty"`
	TopPID         string  `json:"top_pid,omitempty"`
	TopRSSGB       float64 `json:"top_rss_gb,omitempty"`
	TopConfigMB    int     `json:"top_config_mb,omitempty"`
	ProxmoxHost    string  `json:"proxmox_host,omitempty"`
	Confidence     string  `json:"confidence"`
	Explanation    string  `json:"explanation"`
	Recommendation string  `json:"recommendation"`
}

// ---------- Playbook (batch automation) models ----------

// BatchJob represents an Ansible-style fan-out: one task executed on N hosts.
type BatchJob struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Task       string     `json:"task"`
	Params     MapJSON    `json:"params"`
	Status     string     `json:"status"`
	Total      int        `json:"total"`
	Succeeded  int        `json:"succeeded"`
	Failed     int        `json:"failed"`
	Skipped    int        `json:"skipped"`
	DryRun     bool       `json:"dry_run"`
	CreatedBy  string     `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// BatchJobResult is the per-host outcome of a single task within a batch job.
type BatchJobResult struct {
	ID         int64      `json:"id"`
	BatchID    int64      `json:"batch_id"`
	ServerID   int        `json:"server_id"`
	ServerName string     `json:"server_name"`
	Host       string     `json:"host"`
	Status     string     `json:"status"`
	Output     string     `json:"output"`
	Error      string     `json:"error,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// MapJSON is a flexible JSON object wrapper for task parameters.
type MapJSON map[string]string

// ---------- Remediation bridge (Tier 1/2) ----------

// RemediationSuggestion links a detected incident to a proposed playbook fix.
// AMR Health writes these; SiteOps reads and executes them.
type RemediationSuggestion struct {
	ID              int64                  `json:"id"`
	ServerID        int                    `json:"server_id"`
	ServerName      string                 `json:"server_name"`
	EventType       string                 `json:"event_type"`
	Severity        string                 `json:"severity"`
	Description     string                 `json:"description"`
	SuggestedTask   string                 `json:"suggested_task"`
	SuggestedParams map[string]string      `json:"suggested_params"`
	RuleName        string                 `json:"rule_name"`
	Confidence      string                 `json:"confidence"`
	AutoResolve     bool                   `json:"auto_resolve"`
	Status          string                 `json:"status"`
	ResolutionID    int64                  `json:"resolution_id"`
	ResolutionType  string                 `json:"resolution_type"`
	ResolutionOutput string                `json:"resolution_output"`
	ResolvedBy      string                 `json:"resolved_by"`
	CreatedAt       time.Time              `json:"created_at"`
	ResolvedAt      *time.Time             `json:"resolved_at,omitempty"`
}

