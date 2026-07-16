import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { formatDistanceToNow, parseISO, isValid, format } from 'date-fns'
import {
  RefreshCw, Search, ChevronDown, ChevronUp, AlertTriangle,
  Radio, FileText, ExternalLink, Network,
} from 'lucide-react'
import { getLogs, getRdsPlants } from '../api/client'
import type { LogEvent } from '../types'

// ── Plant inference from server_name ─────────────────────────────────────────

function plantFromServer(serverName: string): string {
  const low = serverName.toLowerCase()
  if (low.includes('springfield'))  return 'Springfield'
  if (low.includes('hop') || low.includes('hopkinsville')) return 'Hopkinsville'
  if (low.includes('shelby') || low.includes('shelbyville')) return 'Shelbyville'
  return serverName || '—'
}

// ── Robot name extraction ─────────────────────────────────────────────────────

const amrRe = /AMR[-_]?(\d+)/i

function robotFromMessage(msg: string, source?: string): string {
  // live_amr_tcp: "ESTAB 0 0 server:port amr_ip:amr_port" — port maps unreliably, skip for name
  // roboshop_app (Hopkinsville): [Server:IP:PORT] where PORT-19200 = AMR slot number
  if (source === 'roboshop_app') {
    const m = /\[Server:[\d.]+:(\d+)\]/.exec(msg)
    if (m) {
      const port = parseInt(m[1], 10)
      const n = port - 19200
      if (n >= 1 && n <= 99) return `AMR-${String(n).padStart(2, '0')}`
    }
  }
  const m = amrRe.exec(msg) ?? amrRe.exec(source ?? '')
  return m ? `AMR-${m[1]}` : '—'
}

// ── IP extraction ─────────────────────────────────────────────────────────────

const serverIPRe = /\[Server:(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}):/
const anyIPRe = /\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})\b/g
// Matches ss/netstat output: ESTAB  0  0  server:port  amr_ip:amr_port
const tcpRowRe = /\S+\s+\d+\s+\d+\s+[\d.]+:\d+\s+([\d.]+):(\d+)/

function ipFromMessage(msg: string, source?: string): string {
  // live_amr_tcp: "ESTAB 0 0 <fleet-manager-ip>:60606 <amr-ip>:19206"
  if (source === 'live_amr_tcp') {
    const m = tcpRowRe.exec(msg)
    if (m) return m[1] // destination IP = AMR IP
  }
  const m = serverIPRe.exec(msg)
  if (m) return m[1]
  const ips = Array.from(msg.matchAll(anyIPRe), x => x[1])
    .filter(ip => ip !== '127.0.0.1' && ip !== '0.0.0.0')
  return ips[0] ?? ''
}

// ── Helpers ────────────────────────────────────────────────────────────────────

function relTime(iso: string): string {
  try {
    const d = parseISO(iso)
    return isValid(d) ? formatDistanceToNow(d, { addSuffix: true }) : '—'
  } catch { return '—' }
}

function absTime(iso: string): string {
  try {
    const d = parseISO(iso)
    return isValid(d) ? format(d, 'MMM d, HH:mm:ss') : '—'
  } catch { return '—' }
}

const SEV_CLS: Record<string, string> = {
  critical: 'bg-red-900/70 text-red-300 border border-red-700',
  high:     'bg-red-900/50 text-red-400 border border-red-800',
  medium:   'bg-amber-900/50 text-amber-300 border border-amber-700',
  low:      'bg-yellow-900/40 text-yellow-400 border border-yellow-800',
  info:     'bg-gray-800 text-gray-400 border border-gray-700',
}

// ── Log row ────────────────────────────────────────────────────────────────────

function LogRow({ event, ipByRobot }: { event: LogEvent; ipByRobot: Record<string, string> }) {
  const [expanded, setExpanded] = useState(false)
  const nav = useNavigate()
  const robot = robotFromMessage(event.message, event.source)
  const plant = plantFromServer(event.server_name)
  const ip    = ipFromMessage(event.message, event.source) || (robot !== '—' ? ipByRobot[robot] ?? '' : '')
  const hasRaw = !!(event.raw_line?.trim())
  const hasPE  = !!(event.plain_english?.trim())

  return (
    <>
      <tr className="border-b border-gray-800 hover:bg-gray-800/60 transition-colors group">
        {/* Timestamp */}
        <td className="px-4 py-3 whitespace-nowrap align-top">
          <div className="text-xs text-gray-300 font-mono">{absTime(event.timestamp)}</div>
          <div className="text-[11px] text-gray-600 mt-0.5">{relTime(event.timestamp)}</div>
        </td>

        {/* Plant */}
        <td className="px-3 py-3 whitespace-nowrap align-top">
          <span className="text-xs font-medium text-indigo-300">{plant}</span>
        </td>

        {/* Robot */}
        <td className="px-3 py-3 whitespace-nowrap align-top">
          {robot !== '—'
            ? <button
                onClick={() => nav(`/agent?robot=${encodeURIComponent(robot)}&plant=${encodeURIComponent(plant !== '—' ? plant : '')}`)}
                className="text-xs font-bold text-blue-400 hover:text-blue-300 hover:underline flex items-center gap-1"
              >
                {robot} <ExternalLink size={10} />
              </button>
            : <span className="text-gray-600 text-xs">—</span>}
        </td>

        {/* IP */}
        <td className="px-3 py-3 whitespace-nowrap align-top">
          <span className="font-mono text-[11px] text-gray-400">{ip || '—'}</span>
        </td>

        {/* Severity */}
        <td className="px-3 py-3 whitespace-nowrap align-top">
          <span className={`text-[10px] font-semibold px-2 py-0.5 rounded-full uppercase ${SEV_CLS[event.severity] ?? SEV_CLS.info}`}>
            {event.severity}
          </span>
        </td>

        {/* Plain English / message */}
        <td className="px-3 py-3 align-top max-w-md">
          {hasPE
            ? <p className="text-sm text-gray-200 leading-snug">{event.plain_english}</p>
            : <p className="text-sm text-gray-400 leading-snug line-clamp-2">{event.message}</p>}
          {hasRaw && (
            <button
              onClick={() => setExpanded(v => !v)}
              className="mt-1 flex items-center gap-1 text-[11px] text-gray-600 hover:text-gray-400 transition-colors"
            >
              {expanded ? <><ChevronUp size={11} /> Hide raw</> : <><ChevronDown size={11} /> Show raw</>}
            </button>
          )}
        </td>
      </tr>

      {/* Raw log expansion */}
      {expanded && hasRaw && (
        <tr className="border-b border-gray-800 bg-gray-950">
          <td colSpan={6} className="px-4 py-3">
            <pre className="text-[11px] text-green-400 font-mono bg-black/60 rounded-lg px-4 py-3 overflow-x-auto whitespace-pre-wrap break-all leading-relaxed">
              {event.raw_line}
            </pre>
          </td>
        </tr>
      )}
    </>
  )
}

// ── Springfield TCP Connection Panel ─────────────────────────────────────────
// Springfield FleetManager has no Roboshop app logs (unlike Hopkinsville), so
// per-AMR event data isn't available. We instead surface the live_amr_tcp
// snapshot which shows each AMR's current TCP connection state and IP.

const sfTcpRe = /^(\S+)\s+\d+\s+\d+\s+[\d.]+:\d+\s+(10\.222\.(?!10\.)\d+\.\d+):(\d+)/

function SfTcpPanel({ tcpEvents }: { tcpEvents: LogEvent[] }) {
  const amrMap: Record<string, { states: Record<string, number>; ports: Set<number> }> = {}

  for (const ev of tcpEvents) {
    const m = sfTcpRe.exec(ev.message || '')
    if (!m) continue
    const [, state, ip, port] = m
    if (!amrMap[ip]) amrMap[ip] = { states: {}, ports: new Set() }
    amrMap[ip].states[state] = (amrMap[ip].states[state] || 0) + 1
    amrMap[ip].ports.add(parseInt(port))
  }

  const entries = Object.entries(amrMap).sort(([a], [b]) => a.localeCompare(b))
  if (entries.length === 0) return null

  return (
    <div className="mx-6 mt-4 mb-2 rounded-lg border border-gray-700 bg-gray-800/40 overflow-hidden">
      <div className="px-4 py-2 border-b border-gray-700 flex items-center gap-2">
        <Network size={13} className="text-blue-400" />
        <span className="text-xs font-semibold text-gray-300">Springfield — Live AMR TCP Connections</span>
        <span className="ml-auto text-[10px] text-gray-500">{entries.length} AMRs · live_amr_tcp snapshot · IP is the only identifier available (no Roboshop logs on this server)</span>
      </div>
      <table className="w-full">
        <thead>
          <tr className="text-[10px] font-semibold text-gray-500 uppercase tracking-wide">
            <th className="px-4 py-2 text-left">AMR IP</th>
            <th className="px-4 py-2 text-left">Connections</th>
            <th className="px-4 py-2 text-left">State</th>
          </tr>
        </thead>
        <tbody>
          {entries.map(([ip, data]) => {
            const hasProblems = 'FIN-WAIT-1' in data.states || 'SYN-SENT' in data.states || 'CLOSE-WAIT' in data.states
            const total = Object.values(data.states).reduce((a, b) => a + b, 0)
            const stateStr = Object.entries(data.states)
              .sort(([a], [b]) => a.localeCompare(b))
              .map(([s, n]) => `${s}×${n}`)
              .join('  ')
            return (
              <tr key={ip} className="border-t border-gray-700/40 hover:bg-gray-700/30 transition-colors">
                <td className="px-4 py-2.5 font-mono text-sm text-gray-200">{ip}</td>
                <td className="px-4 py-2.5 text-sm text-gray-400">{total}</td>
                <td className={`px-4 py-2.5 text-sm font-medium ${hasProblems ? 'text-amber-400' : 'text-green-500'}`}>
                  {hasProblems ? '⚠ ' : '✓ '}{stateStr}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

// ── Page ───────────────────────────────────────────────────────────────────────

const AMR_EVENT_TYPES = [
  'robot_offline', 'robot_online', 'rds_core_issue', 'rds_core_activation_issue',
  'rds_scene_map_error', 'rds_settings_reset', 'rds_settings_defaulted',
  'rds_upgrade_reset', 'battery_error', 'battery_status',
  'roboshop_charge_command', 'roboshop_chargedi_change', 'warlink_failure',
  'amr_charge_command', 'amr_dock_command', 'amr_gotarget_station',
  'error', 'warning',
]

export default function AMRLogsPage() {
  const [search, setSearch]       = useState('')
  const [plant, setPlant]         = useState('')
  const [severity, setSeverity]   = useState('')
  const [limit, setLimit]         = useState(200)

  const { data: plants = [] } = useQuery({
    queryKey: ['rds-plants'],
    queryFn: getRdsPlants,
  })


  const { data: logs = [], isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ['amr-logs', limit],
    queryFn: () => getLogs({
      event_types: AMR_EVENT_TYPES.join(','),
      limit,
    }),
    refetchInterval: 60_000,
    staleTime: 30_000,
  })

  // Fetch live_amr_tcp events to build robot→IP lookup
  const { data: tcpEvents = [] } = useQuery({
    queryKey: ['amr-tcp-events'],
    queryFn: () => getLogs({ source: 'live_amr_tcp', limit: 200 }),
    staleTime: 60_000,
  })

  // Build robot name → latest IP map from TCP connection events
  const ipByRobot = useMemo(() => {
    const map: Record<string, string> = {}
    for (const ev of tcpEvents) {
      const robot = robotFromMessage(ev.message, ev.source)
      const ip    = ipFromMessage(ev.message, ev.source)
      if (robot !== '—' && ip) map[robot] = ip
    }
    return map
  }, [tcpEvents])

  // Client-side filter — plant and search applied locally to avoid extra API params
  const filtered = useMemo(() => {
    return logs.filter(ev => {
      if (plant) {
        const evPlant = plantFromServer(ev.server_name)
        if (evPlant !== plant) return false
      }
      if (severity && ev.severity !== severity) return false
      if (search) {
        const low = search.toLowerCase()
        const robot = robotFromMessage(ev.message, ev.source)
        if (
          !ev.message.toLowerCase().includes(low) &&
          !ev.server_name.toLowerCase().includes(low) &&
          !robot.toLowerCase().includes(low) &&
          !(ev.plain_english ?? '').toLowerCase().includes(low)
        ) return false
      }
      return true
    })
  }, [logs, plant, severity, search])

  const robotSet = useMemo(() => {
    const s = new Set<string>()
    filtered.forEach(ev => {
      const r = robotFromMessage(ev.message, ev.source)
      if (r !== '—') s.add(r)
    })
    return s
  }, [filtered])

  return (
    <div className="flex flex-col h-full bg-gray-900 text-gray-100">
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-4 border-b border-gray-700 gap-4 flex-wrap">
        <div>
          <h1 className="text-base font-semibold text-white flex items-center gap-2">
            <FileText size={16} className="text-indigo-400" /> AMR Logs
          </h1>
          <p className="text-xs text-gray-400 mt-0.5">
            AMR-specific events · {filtered.length} shown{robotSet.size > 0 ? ` · ${robotSet.size} robots` : ''}
          </p>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          {/* Plant filter */}
          <select
            value={plant}
            onChange={e => setPlant(e.target.value)}
            className="bg-gray-800 border border-gray-700 rounded-lg px-3 py-1.5 text-sm text-gray-200 focus:outline-none focus:ring-1 focus:ring-indigo-500"
          >
            <option value="">All Plants</option>
            {plants.map(p => <option key={p.name} value={p.name}>{p.name}</option>)}
          </select>

          {/* Severity filter */}
          <select
            value={severity}
            onChange={e => setSeverity(e.target.value)}
            className="bg-gray-800 border border-gray-700 rounded-lg px-3 py-1.5 text-sm text-gray-200 focus:outline-none focus:ring-1 focus:ring-indigo-500"
          >
            <option value="">All Severity</option>
            {['critical','high','medium','low','info'].map(s => (
              <option key={s} value={s}>{s.charAt(0).toUpperCase() + s.slice(1)}</option>
            ))}
          </select>

          {/* Load more */}
          <select
            value={limit}
            onChange={e => setLimit(Number(e.target.value))}
            className="bg-gray-800 border border-gray-700 rounded-lg px-3 py-1.5 text-sm text-gray-200 focus:outline-none focus:ring-1 focus:ring-indigo-500"
          >
            <option value={100}>Last 100</option>
            <option value={200}>Last 200</option>
            <option value={500}>Last 500</option>
            <option value={1000}>Last 1000</option>
          </select>

          <button
            onClick={() => refetch()}
            disabled={isFetching}
            className="flex items-center gap-2 text-xs px-3 py-1.5 rounded-lg bg-gray-700 hover:bg-gray-600 border border-gray-600 text-white transition-colors disabled:opacity-50"
          >
            <RefreshCw size={12} className={isFetching ? 'animate-spin' : ''} /> Refresh
          </button>
        </div>
      </div>

      {/* Search */}
      <div className="px-6 py-3 border-b border-gray-800 flex items-center gap-3">
        <div className="relative flex-1 max-w-sm">
          <Search size={13} className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500" />
          <input
            value={search}
            onChange={e => setSearch(e.target.value)}
            placeholder="Search robot, message, plain English…"
            className="w-full bg-gray-800 border border-gray-700 rounded-lg pl-8 pr-3 py-2 text-sm text-gray-200 placeholder-gray-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
          />
        </div>
        <span className="text-xs text-gray-600">
          {robotSet.size > 0 && (
            <span className="text-gray-500">
              Robots seen: <span className="text-gray-300">{[...robotSet].sort().join(', ')}</span>
            </span>
          )}
        </span>
      </div>

      {/* Springfield TCP panel — shown when Springfield is selected or no plant filter */}
      {(plant === '' || plant === 'Springfield') && (
        <SfTcpPanel tcpEvents={tcpEvents} />
      )}

      {/* Table */}
      <div className="flex-1 overflow-y-auto">
        {isLoading && (
          <div className="flex items-center justify-center py-20 text-gray-500">
            <RefreshCw size={18} className="animate-spin mr-2" /> Loading AMR logs…
          </div>
        )}
        {error && !isLoading && (
          <div className="m-6 rounded-lg border border-red-800 bg-red-950/40 px-4 py-3 text-sm text-red-200">
            <AlertTriangle size={14} className="inline mr-2" />
            Failed to load logs. Is the backend running?
          </div>
        )}
        {!isLoading && !error && filtered.length === 0 && (
          <div className="flex flex-col items-center justify-center py-20 text-gray-500 gap-3">
            <Radio size={32} className="text-gray-700" />
            <p className="text-sm">No AMR log events found.</p>
            <p className="text-xs text-gray-600">
              {logs.length === 0
                ? 'Sync a server first (SSH journal sync picks up AMR events).'
                : 'No events match the current filter.'}
            </p>
          </div>
        )}
        {!isLoading && !error && filtered.length > 0 && (
          <table className="w-full text-sm">
            <thead className="bg-gray-800/80 border-b border-gray-700 sticky top-0 z-10">
              <tr>
                <th className="px-4 py-2.5 text-left text-[11px] font-semibold text-gray-400 uppercase tracking-wide whitespace-nowrap">Timestamp</th>
                <th className="px-3 py-2.5 text-left text-[11px] font-semibold text-gray-400 uppercase tracking-wide">Plant</th>
                <th className="px-3 py-2.5 text-left text-[11px] font-semibold text-gray-400 uppercase tracking-wide">Robot</th>
                <th className="px-3 py-2.5 text-left text-[11px] font-semibold text-gray-400 uppercase tracking-wide">IP</th>
                <th className="px-3 py-2.5 text-left text-[11px] font-semibold text-gray-400 uppercase tracking-wide">Severity</th>
                <th className="px-3 py-2.5 text-left text-[11px] font-semibold text-gray-400 uppercase tracking-wide">Plain English / Message</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map(ev => (
                <LogRow key={ev.id} event={ev} ipByRobot={ipByRobot} />
              ))}
            </tbody>
          </table>
        )}
        {!isLoading && !error && filtered.length > 0 && (
          <div className="px-4 py-3 text-[11px] text-gray-600 border-t border-gray-800">
            Showing {filtered.length} events · Click a robot name to open Agent investigation · "Show raw" expands the original log line
          </div>
        )}
      </div>
    </div>
  )
}
