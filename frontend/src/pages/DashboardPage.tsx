import { useCallback, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { format, isValid, parseISO } from 'date-fns'
import { Activity, AlertTriangle, Bell, CheckCircle, Database, Network, Radio, RefreshCw, Server, Shield } from 'lucide-react'
import { getLogs, getServerStats, getStats, syncAll } from '../api/client'

const CARD_BG = 'bg-gray-800 border border-gray-700'

interface RdsInfo {
  serverIP?: string
  serverPort?: string
  tcpReason?: string
  socketState?: string
}

function safeTime(ts: string) {
  try {
    const d = parseISO(ts)
    return isValid(d) ? format(d, 'h:mm a') : '-'
  } catch {
    return '-'
  }
}

function parseRds(msg: string): RdsInfo {
  return {
    serverIP: msg.match(/\[Server:([0-9.]+):/)?.[1],
    serverPort: msg.match(/\[Server:[^:]+:(\d+)\]/)?.[1],
    tcpReason: msg.match(/\[Tcp:([^\]]+)\]/)?.[1],
    socketState: msg.match(/SocketState:(\S+)/)?.[1],
  }
}

function disconnectLabel(msg: string): string {
  const m = msg.toLowerCase()
  if (m.includes('connection refused')) return 'TCP connection refused'
  if (m.includes('remote host closed')) return 'Remote host closed'
  if (m.includes('timeout')) return 'Connection timeout'
  if (m.includes('unconnected')) return 'Unconnected state'
  if (m.includes('add device failed')) return 'Add device failed'
  if (m.includes('not connected')) return 'Not connected'
  return 'Disconnected'
}

function disconnectAction(rds: RdsInfo): string {
  const tcp = (rds.tcpReason ?? '').toLowerCase()
  const ip = rds.serverIP ? `robot ${rds.serverIP}` : 'the robot'
  if (tcp.includes('connection refused')) return `Check if ${ip} is powered on and its network service is running.`
  if (tcp.includes('remote host closed')) return `${ip} closed the connection. It may have restarted or dropped network.`
  if (tcp.includes('timeout')) return `Cannot reach ${ip}. Check network cables, Wi-Fi, and routing from FleetManager.`
  return `Verify ${ip} is powered on, reachable, and running its robot-side service.`
}

function eventSummary(message: string) {
  return message.replace(/\s+/g, ' ').slice(0, 90)
}

export default function DashboardPage() {
  const nav = useNavigate()
  const qc = useQueryClient()
  const [selectedDisconnect, setSelectedDisconnect] = useState<number | null>(null)
  const [syncing, setSyncing] = useState(false)
  const [syncMessage, setSyncMessage] = useState('')
  const [syncError, setSyncError] = useState('')

  const { data: stats } = useQuery({ queryKey: ['stats'], queryFn: getStats, refetchInterval: 30_000 })
  const { data: serverStats = [] } = useQuery({ queryKey: ['server-stats'], queryFn: getServerStats, refetchInterval: 30_000 })
  const { data: disconnects = [] } = useQuery({ queryKey: ['logs', 'robot_offline'], queryFn: () => getLogs({ event_type: 'robot_offline', limit: 30 }), refetchInterval: 30_000 })
  const { data: rdsIssues = [] } = useQuery({ queryKey: ['logs', 'rds_core_issue'], queryFn: () => getLogs({ event_type: 'rds_core_issue', limit: 5 }), refetchInterval: 30_000 })
  const { data: recent = [] } = useQuery({ queryKey: ['logs', 'recent'], queryFn: () => getLogs({ limit: 8 }), refetchInterval: 30_000 })

  const refreshSyncData = useCallback(() => {
    qc.invalidateQueries({ queryKey: ['servers'] })
    qc.invalidateQueries({ queryKey: ['server-stats'] })
    qc.invalidateQueries({ queryKey: ['stats'] })
    qc.invalidateQueries({ queryKey: ['logs'] })
    qc.invalidateQueries({ queryKey: ['sync-history'] })
  }, [qc])

  const handleSync = useCallback(async () => {
    setSyncing(true)
    setSyncMessage('')
    setSyncError('')
    try {
      const result = await syncAll()
      setSyncMessage(`Queued sync for ${result.server_ids.length} target(s). Log pulls continue in the background.`)
      refreshSyncData()
      window.setTimeout(refreshSyncData, 12_000)
    } catch (err) {
      setSyncError(err instanceof Error ? err.message : 'Sync request failed.')
    } finally {
      window.setTimeout(() => setSyncing(false), 8000)
    }
  }, [refreshSyncData])

  const eventLabels: Record<string, string> = {
    robot_offline: 'Robot offline',
    robot_online: 'Robot online',
    crash: 'App crash',
    rds_core_issue: 'RDS core issue',
    rds_map_update: 'RDS map update',
    warlink_failure: 'WarLink / PLC',
    vm_killed_by_oom: 'VM killed by OOM',
    host_memory_exhaustion: 'Host memory exhaustion',
    disk_error: 'Disk error',
    disk_smart_issue: 'Disk/SMART issue',
    network_dhcp_failure: 'Network failure',
    service_failure: 'Service failure',
    ssh_login_activity: 'SSH/login',
    warning: 'Warning',
    error: 'Error',
  }

  const metricCards = [
    { Icon: Server, val: `${stats?.online_servers ?? 0}/${stats?.total_servers ?? 0}`, label: 'SERVERS ONLINE', sub: 'Server inventory and SSH health', bar: 'bg-green-500', href: '/servers' },
    { Icon: Database, val: String(stats?.rds_core_issue_count ?? 0), label: 'RDS CORE', sub: `${stats?.rds_map_update_count ?? 0} map updates`, bar: 'bg-emerald-500', valColor: (stats?.rds_core_issue_count ?? 0) > 0 ? 'text-rose-300' : 'text-white', href: '/logs?event_type=rds_core_issue' },
    { Icon: Radio, val: String(stats?.robot_offline_count ?? 0), label: 'ROBOT DISCONNECTS', sub: `${stats?.robot_online_count ?? 0} reconnects`, bar: 'bg-red-500', valColor: 'text-red-400', href: '/logs?event_type=robot_offline' },
    { Icon: Network, val: String(stats?.warlink_issue_count ?? 0), label: 'WARLINK / PLC', sub: 'PLC tag and heartbeat issues', bar: 'bg-cyan-500', href: '/logs?event_type=warlink_failure' },
    { Icon: AlertTriangle, val: String(stats?.crash_count ?? 0), label: 'APP CRASHES', sub: 'Click logs to group by application', bar: 'bg-amber-500', valColor: 'text-amber-400', href: '/logs?event_type=crash' },
    { Icon: Shield, val: String(stats?.proxmox_event_count ?? 0), label: 'PROXMOX', sub: 'Host reboot, shutdown, HA', bar: 'bg-purple-500', href: '/logs?event_types=proxmox_host_shutdown,proxmox_host_reboot,ha_action' },
    { Icon: AlertTriangle, val: String(stats?.memory_event_count ?? 0), label: 'OOM / MEMORY', sub: 'Host memory, swap, VM killed', bar: 'bg-red-600', valColor: 'text-red-400', href: '/logs?event_types=vm_killed_by_oom,host_memory_exhaustion,swap_full' },
    { Icon: Activity, val: (stats?.total_events ?? 0).toLocaleString(), label: 'TOTAL EVENTS', sub: `${stats?.critical_events ?? 0} high/critical`, bar: 'bg-gray-500', href: '/logs?severity=critical' },
  ]

  return (
    <div className="flex flex-col h-full bg-gray-900 text-gray-100">
      <div className="flex items-center justify-between px-6 py-4 bg-gray-900 border-b border-gray-700">
        <div>
          <h1 className="text-base font-semibold text-white">DRISHTI SiteOps</h1>
          <p className="text-xs text-gray-400 mt-0.5">Operations overview across servers, robots, RDS, WarLink, Proxmox, logs, and automation</p>
        </div>
        <button onClick={handleSync} disabled={syncing} className="flex items-center gap-2 text-sm font-medium px-4 py-2 rounded-lg bg-gray-700 hover:bg-gray-600 text-white border border-gray-600 transition-colors disabled:opacity-50">
          <RefreshCw size={14} className={syncing ? 'animate-spin' : ''} />
          {syncing ? 'Sync queued...' : 'Sync all'}
        </button>
      </div>

      <div className="flex-1 overflow-y-auto p-5 space-y-4">
        {(syncMessage || syncError) && (
          <div className={`flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 rounded-lg border px-4 py-3 text-sm ${syncError ? 'bg-red-950/40 border-red-800 text-red-100' : 'bg-blue-950/40 border-blue-800 text-blue-100'}`}>
            <span>{syncError || syncMessage}</span>
            {!syncError && (
              <button onClick={() => nav('/sync')} className="self-start sm:self-auto text-xs font-semibold px-3 py-1.5 rounded-md bg-blue-700 hover:bg-blue-600 text-white">
                View Sync Jobs
              </button>
            )}
          </div>
        )}

        <div className="grid grid-cols-1 md:grid-cols-4 xl:grid-cols-8 gap-3">
          {metricCards.map(c => (
            <button key={c.label} onClick={() => nav(c.href)} className={`${CARD_BG} rounded-lg p-3 relative overflow-hidden text-left transition-colors hover:bg-gray-700/70 hover:border-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500/60`}>
              <c.Icon size={16} className="text-gray-400 mb-2" />
              <div className={`text-xl font-semibold ${c.valColor ?? 'text-white'}`}>{c.val}</div>
              <div className="text-[11px] font-medium text-gray-400 mt-1 tracking-wide">{c.label}</div>
              <div className="text-[11px] mt-1 text-gray-500 leading-4">{c.sub}</div>
              <div className={`absolute bottom-0 left-0 right-0 h-0.5 ${c.bar}`} />
            </button>
          ))}
        </div>

        <div className="grid grid-cols-1 xl:grid-cols-5 gap-3">
          <div className={`xl:col-span-2 ${CARD_BG} rounded-lg p-4`}>
            <div className="flex items-center justify-between mb-3">
              <h2 className="text-sm font-semibold text-white flex items-center gap-2"><Radio size={15} className="text-gray-400" /> Robot disconnections</h2>
              <button onClick={() => nav('/logs?event_type=robot_offline')} className="text-xs text-indigo-400 hover:text-indigo-300">{disconnects.length} events</button>
            </div>
            <div className="space-y-1 max-h-52 overflow-y-auto">
              {disconnects.length === 0 && <p className="text-sm text-gray-500 text-center py-6">No disconnections recorded</p>}
              {disconnects.map(ev => {
                const rds = parseRds(ev.message)
                const label = disconnectLabel(ev.message)
                const isOpen = selectedDisconnect === ev.id
                const isRed = label.includes('refused') || label.includes('closed') || label.includes('timeout')
                return (
                  <div key={ev.id}>
                    <button onClick={() => setSelectedDisconnect(isOpen ? null : ev.id)} className={`w-full text-left flex items-center gap-2 px-2.5 py-1.5 rounded-md border transition-all ${isOpen ? 'border-indigo-500 bg-indigo-900/30' : 'border-gray-700 hover:border-gray-600 hover:bg-gray-750'}`}>
                      <span className={`w-2 h-2 rounded-full flex-shrink-0 ${isRed ? 'bg-red-500' : 'bg-amber-400'}`} />
                      <span className="font-mono text-xs font-semibold text-gray-200 w-24 flex-shrink-0">{rds.serverIP ?? '-'}</span>
                      <span className={`text-[11px] px-2 py-0.5 rounded-md border font-medium flex-1 text-left truncate ${isRed ? 'text-red-300 bg-red-900/40 border-red-700' : 'text-amber-300 bg-amber-900/40 border-amber-700'}`}>{label}</span>
                      <span className="text-xs text-gray-500 flex-shrink-0">{safeTime(ev.timestamp)}</span>
                    </button>
                    {isOpen && (
                      <div className="mx-2 mb-1 rounded-lg border border-indigo-700 bg-gray-900 p-2.5 space-y-2">
                        <p className="text-xs text-gray-200 font-medium">{disconnectAction(rds)}</p>
                        <div className="grid grid-cols-4 gap-2">
                          {[
                            { label: 'Robot IP', val: rds.serverIP ?? '-' },
                            { label: 'Port', val: rds.serverPort ?? '-' },
                            { label: 'TCP reason', val: rds.tcpReason ?? '-' },
                            { label: 'State', val: rds.socketState?.replace('State', '') ?? '-' },
                          ].map(f => (
                            <div key={f.label} className="bg-gray-800 border border-gray-700 rounded-md p-2 text-center">
                              <div className="text-[10px] text-gray-500 mb-0.5">{f.label}</div>
                              <div className="text-xs font-semibold text-gray-200 font-mono truncate">{f.val}</div>
                            </div>
                          ))}
                        </div>
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          </div>

          <div className={`xl:col-span-2 ${CARD_BG} rounded-lg p-4`}>
            <div className="flex items-center justify-between mb-3">
              <h2 className="text-sm font-semibold text-white flex items-center gap-2"><Server size={15} className="text-gray-400" /> Server health</h2>
              <button onClick={() => nav('/servers')} className="text-xs text-indigo-400 hover:text-indigo-300">Manage servers</button>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-2 max-h-72 overflow-y-auto">
              {serverStats.map((s: any) => (
                <button key={s.id} onClick={() => nav(`/logs?server_id=${s.id}`)} className="text-left bg-gray-900 border border-gray-700 hover:border-gray-600 rounded-lg p-3 transition-colors">
                  <div className="flex items-center gap-2 mb-2">
                    <span className={`w-2 h-2 rounded-full ${s.status === 'online' ? 'bg-green-500' : 'bg-gray-500'}`} />
                    <span className="text-sm font-medium text-gray-200 truncate">{s.name}</span>
                    <span className={`ml-auto text-[10px] px-2 py-0.5 rounded-full font-medium ${s.status === 'online' ? 'bg-green-900/50 text-green-400 border border-green-700' : 'bg-gray-700 text-gray-400'}`}>{s.status === 'online' ? 'Online' : 'Offline'}</span>
                  </div>
                  <div className="grid grid-cols-3 gap-1.5">
                    {[
                      { label: 'Robot', value: s.robot_offline, tone: s.robot_offline > 0 ? 'text-red-400' : 'text-gray-500' },
                      { label: 'RDS', value: s.rds_core_issues ?? 0, tone: (s.rds_core_issues ?? 0) > 0 ? 'text-rose-300' : 'text-gray-500' },
                      { label: 'Crash', value: s.crashes, tone: s.crashes > 0 ? 'text-amber-400' : 'text-gray-500' },
                      { label: 'WarLink', value: s.warlink_issues ?? 0, tone: (s.warlink_issues ?? 0) > 0 ? 'text-cyan-300' : 'text-gray-500' },
                      { label: 'Errors', value: s.errors, tone: s.errors > 0 ? 'text-orange-400' : 'text-gray-500' },
                      { label: 'Disk', value: s.disk_errors > 0 ? s.disk_errors : 'ok', tone: s.disk_errors > 0 ? 'text-yellow-400' : 'text-green-400' },
                    ].map(item => (
                      <div key={item.label} className="bg-gray-950/60 border border-gray-800 rounded-md px-2 py-1.5 text-center">
                        <div className={`text-xs font-bold ${item.tone}`}>{item.value}</div>
                        <div className="text-[10px] text-gray-600">{item.label}</div>
                      </div>
                    ))}
                  </div>
                </button>
              ))}
            </div>
          </div>

          <div className={`${CARD_BG} rounded-lg p-4`}>
            <div className="flex items-center justify-between mb-3">
              <h2 className="text-sm font-semibold text-white flex items-center gap-2"><Database size={15} className="text-gray-400" /> RDS issues</h2>
              <button onClick={() => nav('/logs?event_type=rds_core_issue')} className="text-xs text-indigo-400 hover:text-indigo-300">Open</button>
            </div>
            <div className="space-y-2">
              {rdsIssues.length === 0 && <p className="text-xs text-gray-500">No RDS core issues found.</p>}
              {rdsIssues.map(ev => (
                <button key={ev.id} onClick={() => nav(`/logs?event_type=rds_core_issue&server_id=${ev.server_id}`)} className="w-full text-left border-l-2 border-l-rose-400 pl-3 py-1.5 hover:bg-gray-700/50 rounded-r transition-colors">
                  <div className="flex items-center gap-2">
                    <span className="text-xs font-semibold text-gray-300 truncate">{ev.server_name}</span>
                    <span className="text-xs text-gray-500 ml-auto">{safeTime(ev.timestamp)}</span>
                  </div>
                  <p className="text-xs text-gray-500 mt-0.5 truncate">{eventSummary(ev.plain_english || ev.message)}</p>
                </button>
              ))}
            </div>
          </div>
        </div>

        <div className="grid grid-cols-1 xl:grid-cols-3 gap-3">
          <div className={`xl:col-span-2 ${CARD_BG} rounded-lg p-4`}>
            <div className="flex items-center justify-between mb-3">
              <h2 className="text-sm font-semibold text-white flex items-center gap-2"><Bell size={14} className="text-gray-400" /> Recent events</h2>
              <button onClick={() => nav('/logs')} className="text-xs text-indigo-400 hover:text-indigo-300">Open logs</button>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
              {recent.slice(0, 8).map(ev => (
                <button key={ev.id} onClick={() => nav(`/logs?event_type=${ev.event_type}`)} className="w-full text-left bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 hover:border-gray-600 hover:bg-gray-750 transition-colors">
                  <div className="flex items-center gap-2">
                    <span className="text-xs font-semibold text-gray-300 truncate">{ev.server_name}</span>
                    <span className="text-xs text-gray-500 ml-auto flex-shrink-0">{safeTime(ev.timestamp)}</span>
                  </div>
                  <p className="text-xs text-gray-500 mt-1 truncate">{eventLabels[ev.event_type] ?? ev.event_type} - {eventSummary(ev.plain_english || ev.message)}</p>
                </button>
              ))}
              {recent.length === 0 && <p className="text-sm text-gray-500 py-6">No recent events found.</p>}
            </div>
          </div>

          <div className={`${CARD_BG} rounded-lg p-4`}>
            <h2 className="text-sm font-semibold text-white flex items-center gap-2 mb-3"><Shield size={14} className="text-gray-400" /> System health</h2>
            <div className="space-y-2">
              {[
                { icon: CheckCircle, label: 'Disk - all servers', detail: `${stats?.disk_error_count ?? 0} disk errors`, ok: (stats?.disk_error_count ?? 0) === 0 },
                { icon: Database, label: 'RDS core', detail: `${stats?.rds_core_issue_count ?? 0} RDS issues`, ok: (stats?.rds_core_issue_count ?? 0) === 0 },
                { icon: Radio, label: 'Robot connectivity', detail: `${stats?.robot_offline_count ?? 0} disconnects`, ok: (stats?.robot_offline_count ?? 0) === 0 },
                { icon: AlertTriangle, label: 'App crashes', detail: `${stats?.crash_count ?? 0} crashes`, ok: (stats?.crash_count ?? 0) === 0 },
                { icon: Activity, label: 'Critical events', detail: `${stats?.critical_events ?? 0} high/critical`, ok: (stats?.critical_events ?? 0) === 0 },
              ].map(item => (
                <button key={item.label} onClick={() => nav('/logs')} className="w-full flex items-start gap-2 rounded-lg border border-gray-700 bg-gray-900 px-3 py-2 text-left hover:border-gray-600 hover:bg-gray-750 transition-colors">
                  <item.icon size={14} className={`mt-0.5 flex-shrink-0 ${item.ok ? 'text-green-500' : 'text-amber-400'}`} />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center justify-between gap-2">
                      <span className="text-xs font-medium text-gray-300">{item.label}</span>
                      <span className={`text-xs font-medium ${item.ok ? 'text-green-400' : 'text-amber-400'}`}>{item.ok ? 'OK' : 'Check'}</span>
                    </div>
                    <p className="text-xs text-gray-500">{item.detail}</p>
                  </div>
                </button>
              ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
