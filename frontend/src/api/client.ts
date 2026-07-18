import axios from 'axios'
import type {
  Server, ServerRequest, LogEvent, DashboardStats,
  TimelinePoint, SyncJob, IncidentSummary, ActionRun, ActionRunRequest, LoginResponse,
  SiteOpsAnswer, SiteOpsHistoryItem, SiteOpsSuggestion, AppUser, AppUserRequest,
  PlantConfig, RdsLogEntry, RdsConnectionStatus, RdsLogFilters, RdsSourceDiscovery, RdsTestResult,
  AgentJob, AgentStartRequest, AgentLogExplanation
} from '../types'

const api = axios.create({ baseURL: '/api', withCredentials: true })

api.interceptors.response.use(
  response => response,
  error => {
    const requestUrl = error.config?.url || ''
    if (error.response?.status === 401 && requestUrl !== '/auth/me') {
      if (window.location.pathname !== '/login') {
        window.location.href = '/login'
      }
    }
    return Promise.reject(error)
  }
)

// Auth
export const login = (username: string, password: string) =>
  api.post<LoginResponse>('/auth/login', { username, password }).then(r => r.data)

export const getMe = () => api.get<{ username: string; role: string; permissions: import('../types').AdminPermission[] }>('/auth/me').then(r => r.data)
export const logout = () => api.post<{ status: string }>('/auth/logout').then(r => r.data)
export const changePassword = (currentPassword: string, newPassword: string) =>
  api.post<{ status: string }>('/auth/change-password', {
    current_password: currentPassword,
    new_password: newPassword,
  }).then(r => r.data)
// Servers
export const getServers = () => api.get<Server[]>('/servers').then(r => r.data)
export const createServer = (data: ServerRequest) => api.post<Server>('/servers', data).then(r => r.data)
export const updateServer = (id: number, data: ServerRequest) => api.put<Server>(`/servers/${id}`, data).then(r => r.data)
export const deleteServer = (id: number) => api.delete(`/servers/${id}`)
export const syncServer = (id: number) => api.post<{ job_id: number }>(`/servers/${id}/sync`).then(r => r.data)
export interface SyncAllResponse {
  status: string
  server_ids: number[]
}
export const syncAll = (assetType?: 'server' | 'endpoint') =>
  api.post<SyncAllResponse>('/sync/all', null, { params: assetType ? { asset_type: assetType } : undefined }).then(r => r.data)
export const testConnection = (data: ServerRequest) =>
  api.post<{ success: boolean; error?: string; info?: string }>('/sync/test', data).then(r => r.data)

// Logs & stats
export interface LogFilters {
  server_id?: number
  event_type?: string
  event_types?: string
  severity?: string
  source?: string
  proxmox_host?: string
  vmid?: string
  q?: string
  from?: string
  to?: string
  limit?: number
  offset?: number
}

export const getLogs = (filters: LogFilters = {}) => {
  const params = Object.fromEntries(
    Object.entries(filters).filter(([, v]) => v !== undefined && v !== '')
  )
  return api.get<LogEvent[]>('/logs', { params }).then(r => r.data)
}

export const explainLogErrors = (events: LogEvent[], context: string) =>
  api.post<AgentLogExplanation>('/logs/agent-explain', {
    context,
    events: events.map(event => ({
      timestamp: event.timestamp,
      server_name: event.server_name,
      event_type: event.event_type,
      severity: event.severity,
      source: event.source,
      message: event.raw_line || event.message,
      plain_english: event.plain_english || '',
      recommended_action: event.recommended_action || '',
    })),
  }).then(response => response.data)

export const getStats = () => api.get<DashboardStats>('/stats').then(r => r.data)
export const getTimeline = () => api.get<TimelinePoint[]>('/timeline').then(r => r.data)
export const getSyncHistory = () => api.get<SyncJob[]>('/sync-history').then(r => r.data)

export const getServerStats = () => api.get('/server-stats').then(r => r.data)

export const deepSync = (id: number, since: string) => api.post(`/servers/${id}/deep-sync?since=${encodeURIComponent(since)}`).then(r => r.data)

export interface IncidentSummaryParams {
  server_id: number
  from?: string
  to?: string
}

export const getIncidentSummary = (params: IncidentSummaryParams) =>
  api.get<IncidentSummary>('/incidents/summary', { params }).then(r => r.data)

// Remote actions
export const runAction = (data: ActionRunRequest) =>
  api.post<ActionRun>('/actions/run', data).then(r => r.data)

export const getActionHistory = () =>
  api.get<ActionRun[]>('/actions/history').then(r => r.data)

// Ask SiteOps
export const askSiteOps = (question: string) =>
  api.post<SiteOpsAnswer>('/rag/query', { question }).then(r => r.data)

export const getSiteOpsHistory = () =>
  api.get<SiteOpsHistoryItem[]>('/rag/history').then(r => r.data)

export const getSiteOpsSuggestions = () =>
  api.get<SiteOpsSuggestion[]>('/rag/suggestions').then(r => r.data)

// Setup / users
export const getUsers = () => api.get<AppUser[]>('/users').then(r => r.data)
export const createUser = (data: AppUserRequest) => api.post<AppUser>('/users', data).then(r => r.data)
export const updateUser = (id: number, data: AppUserRequest) => api.put<AppUser>(`/users/${id}`, data).then(r => r.data)
export const deleteUser = (id: number) => api.delete(`/users/${id}`)

// RDS / RoboWatch
export const getRdsPlants = () => api.get<PlantConfig[]>('/rds/plants').then(r => r.data)
export const getRdsConnectionStatus = (plant: string) => api.get<RdsConnectionStatus>(`/rds/status/${encodeURIComponent(plant)}`).then(r => r.data)
export const testRdsConnection = (plant: string) => api.post<RdsTestResult>(`/rds/test/${encodeURIComponent(plant)}`).then(r => r.data)
export const saveRdsCredentials = (plant: string, username: string, password: string) => api.put<{status:string}>(`/rds/credentials/${encodeURIComponent(plant)}`, { username, password }).then(r => r.data)
export const discoverRdsSources = (plant: string) => api.post<RdsSourceDiscovery>(`/rds/discover/${encodeURIComponent(plant)}`).then(r => r.data)
export const fetchRdsLogs = (plant: string) => api.post<{ event_count?: number; message?: string }>(`/rds/fetch/${encodeURIComponent(plant)}`).then(r => r.data)
export const getRdsLogs = (filters: RdsLogFilters) => {
  const params = Object.fromEntries(
    Object.entries(filters).filter(([, v]) => v !== undefined && v !== '')
  )
  return api.get<RdsLogEntry[]>('/rds/logs', { params }).then(r => r.data)
}

export const explainRdsIncident = (entries: RdsLogEntry[], context: string) =>
  api.post<AgentLogExplanation>('/logs/agent-explain', {
    context,
    events: entries.map(entry => ({
      timestamp: entry.timestamp,
      server_name: entry.plant,
      event_type: `rds_${entry.category || 'unknown'}`,
      severity: entry.severity,
      source: entry.source_system,
      message: entry.raw_log || entry.message,
      plain_english: entry.message,
      recommended_action: '',
    })),
  }).then(response => response.data)

// Agent investigation
export const startAgentJob = (req: AgentStartRequest) =>
  api.post<{ job_id: string }>('/agent/jobs', req).then(r => r.data)
export const getAgentJob = (jobId: string) =>
  api.get<AgentJob>(`/agent/jobs/${encodeURIComponent(jobId)}`).then(r => r.data)
export const getAgentRobots = (plant: string) =>
  api.get<{ id: string; name: string }[]>('/agent/robots', { params: { plant } }).then(r => r.data)

// AMR fleet
export interface AMRStatus {
  name: string
  plant: string
  status: 'ok' | 'warning' | 'error' | 'unknown'
  disconnect_count: number
  error_count: number
  warn_count: number
  total_events: number
  last_seen: string | null
  last_issue: string
  last_issue_time: string | null
 // Connectivity stats
  last_ip: string
  last_mac: string
  reconnect_count: number
  total_offline_sec: number
  worst_drop_sec: number
  // RDS Core authoritative fields
  live_status?: 'online' | 'offline' | ''
  status_code?: number
  status_label?: string
  odo?: number
  today_odo?: number
  data_source?: string
}
export const getAMRFleet = (plant?: string) =>
  api.get<AMRStatus[]>('/amr/fleet', { params: plant ? { plant } : undefined }).then(r => r.data)

// AMR reconnect timeline — one entry per disconnect/reconnect outage.
export interface AMRDropEvent {
  plant: string
  robot: string
  state: 'offline' | 'offline_open'
  start: string
  end?: string
  duration_sec: number
  resolved: boolean
  ip: string
  message: string
  location?: string
  plain_english?: string
}
// Optional time-range filter applied to AMR event-time (ISO or YYYY-MM-DD).
export interface TimeRange { from?: string; to?: string }

export const getAMRTimeline = (plant?: string, robot?: string, range?: TimeRange) =>
  api.get<AMRDropEvent[]>('/amr/timeline', {
    params: {
      ...(plant ? { plant } : {}),
      ...(robot ? { robot } : {}),
      ...(range?.from ? { from: range.from } : {}),
      ...(range?.to ? { to: range.to } : {}),
    },
  }).then(r => r.data)

// Per-robot drill-down summary.
export interface AMRRobotSummary {
  name: string
  plant: string
  last_ip: string
  status: 'stable' | 'flapping' | 'offline' | 'high_latency' | 'unknown'
  reconnect_count: number
  disconnect_count: number
  total_offline_sec: number
  worst_drop_sec: number
  last_seen: string | null
  last_issue: string
  last_location?: string
  drops: AMRDropEvent[]
}
export const getAMRRobotSummary = (robot: string, plant?: string, range?: TimeRange) =>
  api.get<AMRRobotSummary>('/amr/robot', {
    params: { robot, ...(plant ? { plant } : {}), ...(range?.from ? { from: range.from } : {}), ...(range?.to ? { to: range.to } : {}) },
  }).then(r => r.data)

// Bad-zone aggregation: map points (LM/AP/PP) where drops cluster.
export interface AMRBadZone {
  location: string
  plant: string
  drop_count: number
  robots: string[]
  worst_drop_sec: number
  last_drop: string
}
export const getAMRBadZones = (plant?: string, range?: TimeRange) =>
  api.get<AMRBadZone[]>('/amr/badzones', { params: { ...(plant ? { plant } : {}), ...(range?.from ? { from: range.from } : {}), ...(range?.to ? { to: range.to } : {}) } }).then(r => r.data)

// Plain-English summary (LLM with rule-based fallback).
export interface AMRSummary {
  summary: string
  via: 'llm' | 'rules'
  model?: string
  llm_note?: string
}
export const getAMRSummary = (plant?: string, robot?: string, range?: TimeRange) =>
  api.get<AMRSummary>('/amr/summarize', {
    params: {
      ...(plant ? { plant } : {}),
      ...(robot ? { robot } : {}),
      ...(range?.from ? { from: range.from } : {}),
      ...(range?.to ? { to: range.to } : {}),
    },
  }).then(r => r.data)
