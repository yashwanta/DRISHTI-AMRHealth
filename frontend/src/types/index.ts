export interface Server {
  id: number
  name: string
  host: string
  port: number
  username: string
  auth_type: 'password' | 'key'
  asset_type: 'server' | 'endpoint'
  proxmox_host: string
  proxmox_port: number
  proxmox_username: string
  proxmox_auth_type: 'password' | 'key'
  vmid: string
  app_log_paths: string
  last_sync_at: string | null
  status: 'online' | 'offline' | 'error' | 'unknown'
  created_at: string
}

export interface ServerRequest {
  name: string
  host: string
  port: number
  username: string
  auth_type: 'password' | 'key'
  asset_type?: 'server' | 'endpoint'
  password?: string
  private_key?: string
  proxmox_host?: string
  proxmox_port?: number
  proxmox_username?: string
  proxmox_auth_type?: 'password' | 'key'
  proxmox_password?: string
  proxmox_private_key?: string
  vmid?: string
  app_log_paths?: string
}

export interface LogEvent {
  id: number
  server_id: number
  server_name: string
  timestamp: string
  event_type:
    | 'robot_offline'
    | 'ubuntu_server_shutdown'
    | 'ubuntu_server_reboot'
    | 'proxmox_host_shutdown'
    | 'proxmox_host_reboot'
    | 'vm_stopped'
    | 'vm_started'
    | 'vm_reboot'
    | 'vm_killed_by_oom'
    | 'host_memory_exhaustion'
    | 'swap_full'
    | 'backup_job'
    | 'backup_found_vm_stopped'
    | 'ha_action'
    | 'disk_smart_issue'
    | 'network_dhcp_failure'
    | 'ssh_login_activity'
    | 'rds_core_issue'
    | 'rds_map_update'
    | 'rds_model_update'
    | 'battery_error'
    | 'battery_status'
    | 'amr_charge_command'
    | 'amr_dock_command'
    | 'amr_gotarget_station'
    | 'rds_settings_reset'
    | 'rds_settings_defaulted'
    | 'rds_upgrade_reset'
    | 'rds_core_activation_issue'
    | 'rds_scene_map_error'
    | 'admin_evidence_search'
    | 'template_code_reference'
    | 'not_execution_evidence'
    | 'roboshop_charge_command'
    | 'roboshop_chargedi_change'
    | 'warlink_failure'
    | 'service_failure'
    | 'ubuntu_log_gap'
    | 'power_network_event'
    | 'unknown'
    | 'crash'
    | 'power_off'
    | 'error'
    | 'warning'
    | 'info'
    | 'robot_online'
    | 'disk_error'
    | 'update'
  severity: 'critical' | 'high' | 'medium' | 'low' | 'info'
  message: string
  source: string
  raw_line?: string
  plain_english?: string
  recommended_action?: string
  evidence_class?: string
  evidence_confidence?: 'high' | 'medium' | 'low' | string
  evidence_badges?: string[]
  execution_evidence?: boolean
  target_ids?: string[]
  oom_analysis?: OOMAnalysis
  created_at: string
}

export interface DashboardStats {
  total_servers: number
  online_servers: number
  total_events: number
  critical_events: number
  crash_count: number
  power_off_count: number
  error_count: number
  robot_offline_count: number
  robot_online_count: number
  disk_error_count: number
  ubuntu_event_count: number
  proxmox_event_count: number
  vm_event_count: number
  memory_event_count: number
  backup_event_count: number
  rds_core_issue_count: number
  rds_map_update_count: number
  warlink_issue_count: number
}

export interface TimelinePoint {
  hour: string
  event_type: string
  count: number
}

export interface SyncJob {
  id: number
  server_id: number
  server_name: string
  started_at: string
  finished_at: string | null
  status: 'running' | 'success' | 'failed'
  event_count: number
  error: string
}

export type EventType = LogEvent['event_type']
export type Severity = LogEvent['severity']

export interface IncidentEvidence {
  timestamp: string
  event_type: string
  severity: string
  source: string
  message: string
}

export interface IncidentSummary {
  server_id: number
  server_name: string
  proxmox_host: string
  vmid: string
  from: string
  to: string
  what_happened: string
  started_at: string | null
  recovered_at: string | null
  root_cause: string
  recommended_fix: string
  oom_analysis?: OOMAnalysis
  evidence: IncidentEvidence[]
}

export interface OOMAnalysis {
  killed_vmid?: string
  killed_vm_name?: string
  killed_pid?: string
  killed_process?: string
  killed_anon_gb?: number
  killed_total_gb?: number
  top_vmid?: string
  top_vm_name?: string
  top_pid?: string
  top_rss_gb?: number
  top_config_mb?: number
  proxmox_host?: string
  confidence: string
  explanation: string
  recommendation: string
}

export interface LoginResponse {
  token: string
  username: string
  role: UserRole
  expires_at: string
}

export type UserRole =
  | 'Super Admin'
  | 'Global Admin'
  | 'Global Admin Read Only'
  | 'Location Admin'
  | 'IT User'

export interface AppUser {
  id: number
  username: string
  role: UserRole
  location: string
  status: 'active' | 'disabled'
  created_at: string
  updated_at: string
}

export interface AppUserRequest {
  username?: string
  password?: string
  role: UserRole
  location?: string
  status?: 'active' | 'disabled'
}

export type AutomationAction =
  | 'privilege_check'
  | 'service_status'
  | 'service_restart'
  | 'service_start'
  | 'service_stop'
  | 'service_enable'
  | 'service_disable'
  | 'package_update_cache'
  | 'package_list_upgrades'
  | 'package_upgrade_dry_run'
  | 'package_upgrade'
  | 'package_install'
  | 'remediate_cve_2026_31431_linux_signed'
  | 'remediate_cve_2026_43494_linux_signed_upgrade'
  | 'remediate_cve_2026_43494_ubuntu_generic_kernel'
  | 'system_reboot'
  | 'approved_custom_command'

export interface ActionRunRequest {
  server_id: number
  action: AutomationAction
  service_name?: string
  package_name?: string
  command?: string
}

export interface ActionRun {
  id: number
  server_id: number
  action: AutomationAction
  command: string
  status: 'running' | 'success' | 'failed'
  output: string
  error?: string
  created_at: string
}

export interface SiteOpsSourceEvent {
  id: number
  server_name: string
  timestamp: string
  event_type: string
  severity: string
  message: string
  source: string
  raw_line?: string
  plain_english?: string
  recommended_action?: string
}

export interface SiteOpsAnswer {
  answer: string
  model: string
  source_events: SiteOpsSourceEvent[]
}

export interface SiteOpsHistoryItem {
  id: number
  question: string
  answer: string
  model: string
  created_at: string
}

export interface SiteOpsSuggestion {
  question: string
  category: string
  description: string
  event_type?: string
  count?: number
}

// ── RDS / RoboWatch ──────────────────────────────────────────────

export interface PlantConfig {
  name: string
  system_type: string
  base_url: string
  port: number
  username: string
}

export interface RdsLogEntry {
  id: number
  plant: string
  source_system: string
  timestamp: string
  robot: string
  user: string
  action: string
  category: string
  severity: string
  message: string
  raw_log: string
  confidence: string
  execution_evidence: boolean
}

export interface RdsConnectionStatus {
  plant: string
  reachable: boolean
  authenticated: boolean
  last_successful_pull: string | null
  logs_pulled: number
  last_error: string | null
  available_sources: string[]
}

export interface RdsLogFilters {
  plant?: string
  from?: string
  to?: string
  robot?: string
  user?: string
  category?: string
  severity?: string
  execution_evidence?: boolean
  q?: string
  limit?: number
  offset?: number
}

export interface RdsSourceDiscovery {
  sources: RdsLogSource[]
}

export interface RdsLogSource {
  name: string
  type: 'api' | 'html_table'
  url: string
  description: string
}

export interface RdsTestResult {
  reachable: boolean
  authenticated: boolean
  success: boolean
  error?: string
  error_code?: string
}

// ===== Agent investigation =====

export type AgentJobStatus = 'pending' | 'collecting' | 'analyzing' | 'complete' | 'error'
export type SourceState = 'pending' | 'in_progress' | 'done' | 'unavailable'

export interface AgentSourceStatus {
  source: string
  state: SourceState
  result: string
  count: number
  error?: string
}

export interface AgentLogEntry {
  timestamp: string
  source: string
  level: 'info' | 'warn' | 'error'
  message: string
}

export interface AgentTimelineEvent {
  timestamp: string
  source: string
  event: string
}

export interface AgentFinding {
  root_cause: string
  confidence: 'high' | 'medium' | 'low'
  factors: string[]
  timeline: AgentTimelineEvent[]
  prevention: string
  raw_logs: AgentLogEntry[]
  via: string
  llm_note?: string
}

export interface AgentJob {
  id: string
  plant_id: string
  robot_id: string
  investigation_type: string
  window_start: string
  window_end: string
  status: AgentJobStatus
  sources: AgentSourceStatus[]
  log_bundle: AgentLogEntry[]
  finding?: AgentFinding
  error?: string
  created_at: string
  completed_at?: string
}

export interface AgentStartRequest {
  plant_id: string
  robot_id: string
  investigation_type: string
  focus?: string
  window_start: string
  window_end: string
}
