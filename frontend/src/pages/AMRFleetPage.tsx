import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { formatDistanceToNow, parseISO, isValid, format } from 'date-fns'
import {
  AlertTriangle, CheckCircle, RefreshCw, Radio, WifiOff, Wifi,
  Activity, Search, ChevronDown, ChevronUp, LayoutGrid, Table2,
  ChevronRight, X, Clock, TrendingDown, MapPin, Sparkles, Loader2,
  Download, FileText, Calendar
} from 'lucide-react'
import { getAMRFleet, getRdsPlants, getAMRTimeline, getAMRSummary, getAMRBadZones, getAMRBatteryHistory } from '../api/client'
import type { AMRStatus, AMRDropEvent, AMRSummary, TimeRange } from '../api/client'
import { exportCSV, exportPrintPDF, esc, humanDuration } from '../utils/export'

// ── helpers ────────────────────────────────────────────────────────────────────

function relTime(iso: string | null): string {
  if (!iso) return '—'
  try {
    const d = parseISO(iso)
    return isValid(d) ? formatDistanceToNow(d, { addSuffix: true }) : '—'
  } catch { return '—' }
}

function fmtDuration(secs: number): string {
  if (!secs || secs <= 0) return '0 s'
  if (secs < 60) return `${secs} s`
  if (secs < 3600) return `~${Math.round(secs / 60)} min`
  const h = Math.floor(secs / 3600)
  const m = Math.round((secs % 3600) / 60)
  return m > 0 ? `~${h}h ${m}m` : `~${h}h`
}

function fmtOffline(secs: number): string {
  if (!secs || secs <= 0) return '0 s'
  return fmtDuration(secs)
}

const STATUS_BADGE: Record<string, string> = {
  error:   'bg-red-900/60 text-red-300 border border-red-700',
  warning: 'bg-amber-900/60 text-amber-300 border border-amber-700',
  ok:      'bg-green-900/50 text-green-400 border border-green-700',
  unknown: 'bg-gray-800 text-gray-400 border border-gray-600',
}
const STATUS_DOT: Record<string, string> = {
  error:   'bg-red-500 animate-pulse',
  warning: 'bg-amber-400 animate-pulse',
  ok:      'bg-green-500',
  unknown: 'bg-gray-600',
}
const STATUS_LABEL: Record<string, string> = {
  error: 'Issues', warning: 'Warning', ok: 'OK', unknown: 'No data',
}

function StatusIcon({ status }: { status: string }) {
  if (status === 'error')   return <WifiOff size={15} className="text-red-400" />
  if (status === 'warning') return <AlertTriangle size={15} className="text-amber-400" />
  if (status === 'ok')      return <Wifi size={15} className="text-green-400" />
  return <Radio size={15} className="text-gray-500" />
}

// ── Connectivity Table ─────────────────────────────────────────────────────────

type SortKey = 'name' | 'status' | 'reconnect_count' | 'total_offline_sec' | 'worst_drop_sec' | 'disconnect_count' | 'last_seen' | 'today_odo' | 'battery_level' | 'battery_temp_c'

function statusRank(s: string) {
  return s === 'error' ? 3 : s === 'warning' ? 2 : s === 'ok' ? 1 : 0
}

function amrKey(amr: Pick<AMRStatus, 'plant' | 'name'>): string {
  return `${amr.plant || ''}|${amr.name}`
}


function fmtMeters(meters?: number | null): string {
  if (meters === undefined || meters === null || Number.isNaN(meters)) return '-'
  if (meters <= 0) return '0 m'
  if (meters < 1000) return `${Math.round(meters)} m`
  return `${(meters / 1000).toFixed(1)} km`
}

function batteryLevelTone(level?: number): string {
  if (level === undefined) return 'text-gray-600'
  if (level < 20) return 'text-red-300'
  if (level < 35) return 'text-amber-300'
  return 'text-green-300'
}

function batteryTempTone(temp?: number): string {
  if (temp === undefined) return 'text-gray-600'
  if (temp >= 55) return 'text-red-300'
  if (temp >= 45) return 'text-amber-300'
  return 'text-cyan-200'
}

function rdsState(amr: AMRStatus): string {
  if (amr.status_label) return amr.status_label
  if (amr.live_status === 'online') return 'Online'
  if (amr.live_status === 'offline') return 'Offline'
  return STATUS_LABEL[amr.status] || 'Unknown'
}

function ConnectivityTable({
  amrs, selected, onToggle, onToggleAll, onInvestigate, activePlant,
}: {
  amrs: AMRStatus[]
  selected: Set<string>
  onToggle: (amr: AMRStatus) => void
  onToggleAll: (amrs: AMRStatus[]) => void
  onInvestigate: (amr: AMRStatus) => void
  activePlant: string
}) {
  const [sortKey, setSortKey] = useState<SortKey>('total_offline_sec')
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('desc')

  function toggleSort(key: SortKey) {
    if (sortKey === key) setSortDir(d => d === 'asc' ? 'desc' : 'asc')
    else { setSortKey(key); setSortDir('desc') }
  }

  const sorted = [...amrs].sort((a, b) => {
    let av: string | number = a[sortKey as keyof AMRStatus] as number ?? 0
    let bv: string | number = b[sortKey as keyof AMRStatus] as number ?? 0
    if (sortKey === 'status') { av = statusRank(a.status); bv = statusRank(b.status) }
    if (sortKey === 'name')   { av = a.name; bv = b.name }
    if (sortKey === 'last_seen') { av = a.last_seen ? Date.parse(a.last_seen) : 0; bv = b.last_seen ? Date.parse(b.last_seen) : 0 }
    if (sortKey === 'today_odo') { av = a.today_odo ?? 0; bv = b.today_odo ?? 0 }
    if (typeof av === 'string' && typeof bv === 'string') {
      return sortDir === 'asc' ? av.localeCompare(bv) : bv.localeCompare(av)
    }
    return sortDir === 'asc' ? (av as number) - (bv as number) : (bv as number) - (av as number)
  })

  function Th({ label, col }: { label: string; col: SortKey }) {
    const active = sortKey === col
    return (
      <th
        className="px-4 py-2.5 text-left text-[11px] font-semibold text-gray-400 uppercase tracking-wide cursor-pointer select-none hover:text-gray-200 whitespace-nowrap"
        onClick={() => toggleSort(col)}
      >
        <span className="inline-flex items-center gap-1">
          {label}
          {active ? sortDir === 'desc' ? <ChevronDown size={11} /> : <ChevronUp size={11} /> : <span className="w-2.5" />}
        </span>
      </th>
    )
  }

  const allChecked = sorted.length > 0 && sorted.every(a => selected.has(amrKey(a)))

  return (
    <div className="overflow-x-auto rounded-xl border border-gray-700">
      <table className="w-full text-sm">
        <thead className="bg-gray-800/80 border-b border-gray-700">
          <tr>
            <th className="px-3 py-2.5">
              <input
                type="checkbox"
                checked={allChecked}
                onChange={() => onToggleAll(sorted)}
                className="rounded border-gray-600 bg-gray-700 text-indigo-500 cursor-pointer"
              />
            </th>
            <Th label="Robot"          col="name" />
            <th className="px-4 py-2.5 text-left text-[11px] font-semibold text-gray-400 uppercase tracking-wide">Plant</th>
            <th className="px-4 py-2.5 text-left text-[11px] font-semibold text-gray-400 uppercase tracking-wide">IP</th>
            <th className="px-4 py-2.5 text-left text-[11px] font-semibold text-gray-400 uppercase tracking-wide">MAC</th>
            <Th label="Status"         col="status" />
            <th className="px-4 py-2.5 text-left text-[11px] font-semibold text-gray-400 uppercase tracking-wide">RDS State</th>
            <Th label="Battery" col="battery_level" />
            <Th label="Battery Temp" col="battery_temp_c" />
            <th className="px-4 py-2.5 text-left text-[11px] font-semibold text-gray-400 uppercase tracking-wide">Battery State</th>
            <Th label="Last Seen"      col="last_seen" />
            <Th label="Today Odo"      col="today_odo" />
            <Th label="TCP Reconnects" col="reconnect_count" />
            <Th label="Disconnects"    col="disconnect_count" />
            <Th label="~Time Offline"  col="total_offline_sec" />
            <Th label="Worst Drop"     col="worst_drop_sec" />
            <th className="px-4 py-2.5" />
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-800">
          {sorted.map((amr, i) => (
            <tr
              key={amrKey(amr)}
              className={`${i % 2 === 0 ? 'bg-gray-900' : 'bg-gray-850'} hover:bg-gray-800 transition-colors ${selected.has(amrKey(amr)) ? 'ring-inset ring-1 ring-indigo-600' : ''}`}
            >
              <td className="px-3 py-3">
                <input
                  type="checkbox"
                  checked={selected.has(amrKey(amr))}
                  onChange={() => onToggle(amr)}
                  className="rounded border-gray-600 bg-gray-700 text-indigo-500 cursor-pointer"
                />
              </td>
              <td className="px-4 py-3 whitespace-nowrap">
                <div className="flex items-center gap-2.5">
                  <span className={`w-2 h-2 rounded-full flex-shrink-0 ${STATUS_DOT[amr.status]}`} />
                  <StatusIcon status={amr.status} />
                  <span className="font-bold text-white tracking-wide">{amr.name}</span>
                </div>
              </td>
              <td className="px-4 py-3 text-xs text-gray-400 whitespace-nowrap">
                {amr.plant || activePlant || <span className="text-gray-600">—</span>}
              </td>
              <td className="px-4 py-3 font-mono text-gray-300 text-xs whitespace-nowrap">
                {amr.last_ip || <span className="text-gray-600">-</span>}
              </td>
              <td className="px-4 py-3 font-mono text-gray-400 text-xs whitespace-nowrap">
                {amr.last_mac || <span className="text-gray-600">-</span>}
              </td>
              <td className="px-4 py-3">
                <span className={`text-[11px] font-semibold px-2 py-0.5 rounded-full ${STATUS_BADGE[amr.status]}`}>
                  {STATUS_LABEL[amr.status]}
                </span>
              </td>
              <td className="px-4 py-3 text-xs text-gray-300 whitespace-nowrap">
                <span title={amr.status_code !== undefined ? `RDS code ${amr.status_code}` : undefined}>{rdsState(amr)}</span>
              </td>
              <td className={`px-4 py-3 text-xs font-semibold whitespace-nowrap ${batteryLevelTone(amr.battery_level)}`}>
                {amr.battery_level === undefined ? '—' : `${Math.round(amr.battery_level)}%`}
              </td>
              <td className={`px-4 py-3 text-xs font-semibold whitespace-nowrap ${batteryTempTone(amr.battery_temp_c)}`}>
                {amr.battery_temp_c === undefined ? '—' : `${amr.battery_temp_c.toFixed(1)} °C`}
              </td>
              <td className="px-4 py-3 text-xs text-gray-300 whitespace-nowrap">
                {amr.battery_state || '—'}
              </td>
              <td className="px-4 py-3 text-xs text-gray-400 whitespace-nowrap">
                {relTime(amr.last_seen)}
              </td>
              <td className="px-4 py-3 text-xs text-gray-300 whitespace-nowrap">
                {fmtMeters(amr.today_odo)}
              </td>
              <td className="px-4 py-3 text-center">
                <span className={`font-bold text-sm ${amr.reconnect_count > 0 ? 'text-red-400' : 'text-gray-500'}`}>
                  {amr.reconnect_count > 0 ? `~${amr.reconnect_count}` : '0'}
                </span>
              </td>
              <td className="px-4 py-3 text-center">
                <span className={`font-bold text-sm ${amr.disconnect_count > 0 ? 'text-rose-300' : 'text-gray-500'}`}>
                  {amr.disconnect_count > 0 ? amr.disconnect_count : '0'}
                </span>
              </td>
              <td className="px-4 py-3 whitespace-nowrap">
                <span className={`text-xs font-medium ${amr.total_offline_sec > 0 ? 'text-amber-300' : 'text-gray-500'}`}>{fmtOffline(amr.total_offline_sec)}</span>
              </td>
              <td className="px-4 py-3 whitespace-nowrap">
                <span className={`text-xs font-medium ${amr.worst_drop_sec > 0 ? 'text-red-300' : 'text-gray-500'}`}>{fmtDuration(amr.worst_drop_sec)}</span>
              </td>
              <td className="px-4 py-3">
                <button
                  onClick={() => onInvestigate(amr)}
                  className="flex items-center gap-1 text-[11px] font-medium px-2.5 py-1 rounded-md bg-indigo-700 hover:bg-indigo-600 text-white transition-colors whitespace-nowrap"
                >
                  Investigate <ChevronRight size={11} />
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {sorted.length === 0 && (
        <div className="text-center py-10 text-gray-500 text-sm">No AMRs match the current filter.</div>
      )}
    </div>
  )
}

// ── AMR Card ───────────────────────────────────────────────────────────────────

function AMRCard({
  amr, selected, onToggle, onInvestigate,
}: {
  amr: AMRStatus
  selected: boolean
  onToggle: (amr: AMRStatus) => void
  onInvestigate: (amr: AMRStatus) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const hasIssue = !!amr.last_issue?.trim()
  const hasConnStats = amr.reconnect_count > 0 || amr.total_offline_sec > 0

  const ringCls: Record<string, string> = {
    error: 'ring-2 ring-red-500', warning: 'ring-2 ring-amber-400',
    ok: 'ring-2 ring-green-500', unknown: 'ring-1 ring-gray-600',
  }

  return (
    <div
      className={`bg-gray-800 border border-gray-700 rounded-xl overflow-hidden transition-all cursor-pointer ${selected ? 'ring-2 ring-indigo-500' : ringCls[amr.status]}`}
      onClick={() => onToggle(amr)}
    >
      <div className="flex items-center gap-3 px-4 py-3">
        <input
          type="checkbox"
          checked={selected}
          onChange={() => onToggle(amr)}
          onClick={e => e.stopPropagation()}
          className="rounded border-gray-600 bg-gray-700 text-indigo-500 cursor-pointer flex-shrink-0"
        />
        <span className={`w-2 h-2 rounded-full flex-shrink-0 ${STATUS_DOT[amr.status]}`} />
        <StatusIcon status={amr.status} />
        <span className="text-base font-bold text-white tracking-wide flex-1">{amr.name}</span>
        {amr.last_ip && <span className="font-mono text-[11px] text-gray-400">{amr.last_ip}</span>}
        <span className={`text-[11px] font-semibold px-2.5 py-0.5 rounded-full ${STATUS_BADGE[amr.status]}`}>
          {STATUS_LABEL[amr.status]}
        </span>
      </div>

      <div className="grid grid-cols-3 gap-2 px-4 py-2 border-t border-gray-700 bg-gray-900/30" onClick={e => e.stopPropagation()}>
        <div className="min-w-0">
          <p className="text-[10px] uppercase text-gray-500">Plant</p>
          <p className="truncate text-xs text-gray-300">{amr.plant || '-'}</p>
        </div>
        <div className="min-w-0">
          <p className="text-[10px] uppercase text-gray-500">RDS</p>
          <p className="truncate text-xs text-gray-300" title={amr.status_code !== undefined ? `RDS code ${amr.status_code}` : undefined}>{rdsState(amr)}</p>
        </div>
        <div className="min-w-0">
          <p className="text-[10px] uppercase text-gray-500">Today</p>
          <p className="truncate text-xs text-gray-300">{fmtMeters(amr.today_odo)}</p>
        </div>
        <div className="min-w-0">
          <p className="text-[10px] uppercase text-gray-500">Battery</p>
          <p className={`truncate text-xs font-semibold ${batteryLevelTone(amr.battery_level)}`}>{amr.battery_level === undefined ? '—' : `${Math.round(amr.battery_level)}%`}</p>
        </div>
        <div className="min-w-0">
          <p className="text-[10px] uppercase text-gray-500">Temperature</p>
          <p className={`truncate text-xs font-semibold ${batteryTempTone(amr.battery_temp_c)}`}>{amr.battery_temp_c === undefined ? '—' : `${amr.battery_temp_c.toFixed(1)} °C`}</p>
        </div>
        <div className="min-w-0">
          <p className="text-[10px] uppercase text-gray-500">Battery State</p>
          <p className="truncate text-xs text-gray-300">{amr.battery_state || '—'}</p>
        </div>
      </div>

      <div className="grid grid-cols-3 divide-x divide-gray-700 border-t border-gray-700" onClick={e => e.stopPropagation()}>
        {[
          { label: 'Disconnects', val: amr.disconnect_count, tone: amr.disconnect_count > 0 ? 'text-red-400' : 'text-gray-400' },
          { label: 'Reconnects',  val: amr.reconnect_count > 0 ? `~${amr.reconnect_count}` : 0, tone: amr.reconnect_count > 0 ? 'text-rose-300' : 'text-gray-400' },
          { label: 'Events',      val: amr.total_events, tone: 'text-gray-300' },
        ].map(m => (
          <div key={m.label} className="flex flex-col items-center py-2.5 px-1">
            <span className={`text-base font-bold ${m.tone}`}>{m.val}</span>
            <span className="text-[10px] text-gray-500 mt-0.5 uppercase tracking-wide">{m.label}</span>
          </div>
        ))}
      </div>

      {hasConnStats && (
        <div className="grid grid-cols-2 divide-x divide-gray-700 border-t border-gray-700 bg-gray-900/40">
          <div className="flex flex-col items-center py-2">
            <span className="text-[11px] text-gray-500">~Time offline</span>
            <span className="text-xs font-semibold text-amber-300">{fmtOffline(amr.total_offline_sec)}</span>
          </div>
          <div className="flex flex-col items-center py-2">
            <span className="text-[11px] text-gray-500">Worst drop</span>
            <span className="text-xs font-semibold text-red-300">{fmtDuration(amr.worst_drop_sec)}</span>
          </div>
        </div>
      )}

      <div className="px-4 py-2.5 border-t border-gray-700 flex items-center justify-between gap-2" onClick={e => e.stopPropagation()}>
        <span className="text-[11px] text-gray-500">
          Last seen: <span className="text-gray-300">{relTime(amr.last_seen)}</span>
        </span>
        <div className="flex items-center gap-1.5">
          {hasIssue && (
            <button onClick={() => setExpanded(v => !v)} className="p-1 rounded text-gray-500 hover:text-gray-300 hover:bg-gray-700 transition-colors">
              {expanded ? <ChevronUp size={13} /> : <ChevronDown size={13} />}
            </button>
          )}
          <button
            onClick={() => onInvestigate(amr)}
            className="flex items-center gap-1 text-[11px] font-medium px-2.5 py-1 rounded-md bg-indigo-700 hover:bg-indigo-600 text-white transition-colors"
          >
            Investigate <ChevronRight size={11} />
          </button>
        </div>
      </div>

      {expanded && hasIssue && (
        <div className="px-4 pb-3 border-t border-gray-700 pt-2 bg-gray-900/50" onClick={e => e.stopPropagation()}>
          <p className={`text-xs leading-relaxed ${amr.status === 'error' ? 'text-red-200' : 'text-amber-200'}`}>{amr.last_issue}</p>
        </div>
      )}
    </div>
  )
}

// ── Summary bar ────────────────────────────────────────────────────────────────

function SummaryBar({ amrs, selected }: { amrs: AMRStatus[]; selected: Set<string> }) {
  const counts = amrs.reduce((acc, a) => { acc[a.status] = (acc[a.status] ?? 0) + 1; return acc }, {} as Record<string, number>)
  return (
    <div className="flex items-center gap-5 flex-wrap">
      {[
        { label: 'Issues',  key: 'error',   cls: 'text-red-400' },
        { label: 'Warning', key: 'warning', cls: 'text-amber-400' },
        { label: 'OK',      key: 'ok',      cls: 'text-green-400' },
        { label: 'No data', key: 'unknown', cls: 'text-gray-500' },
      ].map(({ label, key, cls }) => (
        <span key={key} className={`text-sm font-semibold ${cls}`}>
          {counts[key] ?? 0} <span className="font-normal text-gray-500">{label}</span>
        </span>
      ))}
      <span className="text-sm text-gray-500 ml-auto">
        {selected.size > 0 && <span className="text-indigo-400 mr-2">{selected.size} selected ·</span>}
        {amrs.length} AMRs total
      </span>
    </div>
  )
}

// ── Reconnect Timeline Drawer ──────────────────────────────────────────────────

// Classify an outage duration for highlighting. Thresholds match the task spec:
// anything over 30s is notable, over 60s is severe.
function dropSeverity(secs: number): 'severe' | 'notable' | 'brief' {
  if (secs >= 60) return 'severe'
  if (secs >= 30) return 'notable'
  return 'brief'
}

// Detect burst / flapping behavior: if N drops land within a short window, the
// robot is flapping. Returns null if there's no burst worth flagging.
function detectBurst(drops: AMRDropEvent[]): { count: number; windowMin: number } | null {
  if (drops.length < 3) return null
  // Look at the densest 8-minute sliding window over drop start times.
  const starts = drops.map(d => new Date(d.start).getTime()).sort((a, b) => a - b)
  const windowMs = 8 * 60 * 1000
  let best = 1
  for (let i = 0; i < starts.length; i++) {
    let n = 1
    for (let j = i + 1; j < starts.length && starts[j] - starts[i] <= windowMs; j++) n++
    if (n > best) best = n
  }
  return best >= 3 ? { count: best, windowMin: 8 } : null
}

const DROP_SEV_CLS: Record<string, string> = {
  severe:  'border-l-red-500 bg-red-950/40',
  notable: 'border-l-amber-500 bg-amber-950/30',
  brief:   'border-l-gray-600 bg-gray-800/40',
}

function TimelineDrawer({ robot, plant, timeRange, onClose }: {
  robot: string
  plant: string
  timeRange: TimeRange
  onClose: () => void
}) {
  const { data: drops = [], isLoading } = useQuery({
    queryKey: ['amr-timeline', plant, robot, timeRange.from],
    queryFn: () => getAMRTimeline(plant || undefined, robot, timeRange),
    staleTime: 30_000,
  })

  // Plain-English summary. Not auto-fetched (LLM call can be slow) — loaded on
  // demand via the Summarize button.
  const { data: summary, isFetching: summaryLoading, refetch: fetchSummary } = useQuery<AMRSummary>({
    queryKey: ['amr-summary', plant, robot, timeRange.from],
    queryFn: () => getAMRSummary(plant || undefined, robot, timeRange),
    enabled: false, // manual trigger only
    staleTime: 60_000,
  })

  const burst = detectBurst(drops)
  const openDrops = drops.filter(d => !d.resolved)
  const totalOff = drops.reduce((a, d) => a + (d.duration_sec || 0), 0)
  const worst = drops.reduce((a, d) => Math.max(a, d.duration_sec || 0), 0)

  // Export the per-robot drop timeline as CSV.
  function exportDropsCSV() {
    if (!drops.length) return
    exportCSV(
      drops.map(d => ({
        robot: d.robot,
        plant: d.plant,
        start: d.start,
        end: d.end ?? '',
        duration_sec: d.duration_sec,
        resolved: d.resolved ? 'yes' : 'no',
        ip: d.ip,
        location: d.location ?? '',
        plain_english: d.plain_english ?? '',
        raw_message: d.message,
      })),
      [
        { key: 'robot', header: 'Robot' },
        { key: 'plant', header: 'Plant' },
        { key: 'start', header: 'Start' },
        { key: 'end', header: 'End' },
        { key: 'duration_sec', header: 'Duration (s)' },
        { key: 'resolved', header: 'Resolved' },
        { key: 'ip', header: 'IP' },
        { key: 'location', header: 'Location' },
        { key: 'plain_english', header: 'Explanation' },
        { key: 'raw_message', header: 'Raw message' },
      ],
      `amr-${robot}-${plant || 'all'}-timeline-${format(new Date(), 'yyyy-MM-dd')}.csv`,
    )
  }

  // Export a printable PDF (Save-as-PDF) with the summary + timeline.
  function exportSummaryPDF() {
    const meta = `${plant || 'All Plants'} · ${drops.length} drops · worst ${humanDuration(worst)} · generated ${format(new Date(), 'yyyy-MM-dd HH:mm')}`
    const summaryBlock = summary
      ? `<div class="summary"><strong>Summary (${summary.via}):</strong><br>${esc(summary.summary)}</div>`
      : ''
    const rows = drops.length ? drops.map(d => {
      const sev = d.resolved ? (d.duration_sec >= 60 ? 'high' : d.duration_sec >= 30 ? 'med' : 'low') : 'high'
      const loc = d.location ? ` <span class="loc">@${esc(d.location)}</span>` : ''
      const expl = d.plain_english ? `<br><span style="color:#555">${esc(d.plain_english)}</span>` : ''
      return `<tr><td>${esc(absTime(d.start))}</td><td><span class="badge sev-${sev}">${d.resolved ? humanDuration(d.duration_sec) : 'OPEN'}</span>${loc}</td><td>${esc(d.ip || '—')}</td><td>${d.resolved ? esc(absTime(d.end ?? '')) : '—'}${expl}</td></tr>`
    }).join('') : '<tr><td colspan="4">No disconnects recorded.</td></tr>'
    exportPrintPDF(
      `AMR ${robot} — Reconnect Report`,
      `<h1>${esc(robot)} — Reconnect Report</h1><div class="meta">${esc(meta)}</div>
       ${summaryBlock}
       <table><thead><tr><th>Start</th><th>Outage</th><th>IP</th><th>End / Explanation</th></tr></thead><tbody>${rows}</tbody></table>`,
    )
  }

  return (
    <>
      {/* Backdrop */}
      <div className="fixed inset-0 bg-black/50 z-40" onClick={onClose} />

      {/* Drawer */}
      <div className="fixed right-0 top-0 bottom-0 w-full max-w-md bg-gray-900 border-l border-gray-700 z-50 flex flex-col shadow-2xl">
        <div className="flex items-center gap-3 px-5 py-4 border-b border-gray-700">
          <WifiOff size={16} className="text-red-400" />
          <div className="flex-1">
            <h2 className="text-sm font-semibold text-white">{robot} · Reconnect Timeline</h2>
            <p className="text-[11px] text-gray-500">{plant || 'All Plants'} · {drops.length} drop{drops.length !== 1 ? 's' : ''}</p>
          </div>
          <button
            onClick={exportDropsCSV}
            disabled={!drops.length}
            title="Export timeline as CSV"
            className="p-1.5 rounded-lg text-gray-400 hover:text-white hover:bg-gray-800 transition-colors disabled:opacity-40"
          >
            <Download size={15} />
          </button>
          <button
            onClick={exportSummaryPDF}
            title="Export report as PDF"
            className="p-1.5 rounded-lg text-gray-400 hover:text-white hover:bg-gray-800 transition-colors"
          >
            <FileText size={15} />
          </button>
          <button onClick={onClose} className="p-1.5 rounded-lg text-gray-400 hover:text-white hover:bg-gray-800 transition-colors">
            <X size={16} />
          </button>
        </div>

        {/* Summary strip */}
        <div className="grid grid-cols-3 divide-x divide-gray-700 border-b border-gray-700">
          <div className="px-3 py-3 text-center">
            <div className="text-lg font-bold text-red-400">{drops.length}</div>
            <div className="text-[10px] text-gray-500 uppercase tracking-wide">Drops</div>
          </div>
          <div className="px-3 py-3 text-center">
            <div className="text-lg font-bold text-amber-300">{fmtOffline(totalOff)}</div>
            <div className="text-[10px] text-gray-500 uppercase tracking-wide">Total offline</div>
          </div>
          <div className="px-3 py-3 text-center">
            <div className="text-lg font-bold text-rose-300">{fmtDuration(worst)}</div>
            <div className="text-[10px] text-gray-500 uppercase tracking-wide">Worst drop</div>
          </div>
        </div>

        {burst && (
          <div className="mx-4 mt-3 px-3 py-2 rounded-lg border border-amber-700 bg-amber-950/40 flex items-center gap-2">
            <TrendingDown size={14} className="text-amber-400 flex-shrink-0" />
            <span className="text-[11px] text-amber-200 leading-snug">
              Flapping: <strong>{burst.count} drops within {burst.windowMin} min</strong> — likely Wi-Fi roaming or unstable link, not a single outage.
            </span>
          </div>
        )}

        {/* Plain-English summary (on demand) */}
        <div className="mx-4 mt-3">
          {!summary ? (
            <button
              onClick={() => fetchSummary()}
              disabled={summaryLoading}
              className="w-full flex items-center justify-center gap-2 text-xs px-3 py-2 rounded-lg bg-indigo-700 hover:bg-indigo-600 disabled:opacity-60 border border-indigo-600 text-white transition-colors"
            >
              {summaryLoading
                ? <><Loader2 size={13} className="animate-spin" /> Analyzing…</>
                : <><Sparkles size={13} /> Summarize in plain English</>}
            </button>
          ) : (
            <div className="rounded-lg border border-indigo-700/60 bg-indigo-950/30 px-3 py-2.5">
              <div className="flex items-center gap-1.5 mb-1">
                <Sparkles size={12} className="text-indigo-400" />
                <span className="text-[10px] font-semibold text-indigo-300 uppercase tracking-wide">
                  Summary{summary.via === 'rules' ? ' · rule-based' : summary.model ? ` · ${summary.model}` : ''}
                </span>
                <button
                  onClick={() => fetchSummary()}
                  disabled={summaryLoading}
                  className="ml-auto text-[10px] text-gray-500 hover:text-gray-300 disabled:opacity-50"
                >
                  {summaryLoading ? '…' : 'regenerate'}
                </button>
              </div>
              <p className="text-[11px] text-gray-200 leading-relaxed">{summary.summary}</p>
              {summary.llm_note && (
                <p className="text-[10px] text-gray-500 mt-1.5">{summary.llm_note}</p>
              )}
            </div>
          )}
        </div>

        {/* Drop list */}
        <div className="flex-1 overflow-y-auto p-4 space-y-2">
          {isLoading && (
            <div className="flex items-center justify-center py-12 text-gray-500">
              <RefreshCw size={16} className="animate-spin mr-2" /> Loading timeline…
            </div>
          )}
          {!isLoading && drops.length === 0 && (
            <div className="flex flex-col items-center justify-center py-12 text-gray-500 gap-2">
              <CheckCircle size={28} className="text-green-600" />
              <p className="text-xs">No disconnects recorded for {robot}.</p>
            </div>
          )}
          {!isLoading && openDrops.length > 0 && (
            <div className="px-3 py-2 rounded-lg border border-red-800 bg-red-950/30 text-[11px] text-red-200">
              {openDrops.length} unresolved disconnect{openDrops.length > 1 ? 's' : ''} — no reconnect seen (may still be offline, or outside the synced window).
            </div>
          )}
          {!isLoading && drops.map((d, i) => {
            const sev = d.resolved ? dropSeverity(d.duration_sec) : 'severe'
            return (
              <div key={i} className={`rounded-lg border-l-4 ${DROP_SEV_CLS[sev]} px-3 py-2.5`}>
                <div className="flex items-center justify-between gap-2">
                  <span className="text-xs font-mono text-gray-200">{absTime(d.start)}</span>
                  <span className={`text-[10px] font-semibold px-2 py-0.5 rounded-full uppercase ${
                    d.resolved
                      ? sev === 'severe' ? 'bg-red-900 text-red-300'
                        : sev === 'notable' ? 'bg-amber-900 text-amber-300'
                        : 'bg-gray-700 text-gray-300'
                      : 'bg-red-900 text-red-300 animate-pulse'
                  }`}>
                    {d.resolved ? `${d.duration_sec}s` : 'open'}
                  </span>
                </div>
                <div className="flex items-center gap-3 mt-1 text-[11px] text-gray-500 flex-wrap">
                  {d.ip && <span className="font-mono">{d.ip}</span>}
                  {d.end && (
                    <span className="flex items-center gap-1">
                      <Clock size={10} /> → {absTime(d.end)}
                    </span>
                  )}
                  {d.location && (
                    <span className="flex items-center gap-1 text-amber-300">
                      <MapPin size={10} /> {d.location}
                    </span>
                  )}
                </div>
                {d.plain_english && (
                  <p className="text-[11px] text-gray-300 mt-1.5 leading-snug">{d.plain_english}</p>
                )}
              </div>
            )
          })}
        </div>

        <div className="px-4 py-2.5 border-t border-gray-700 text-[10px] text-gray-600">
          Built from Roboshop app logs · port→AMR off→on pairing · durations may be approximate when reconnects fall outside the synced window.
        </div>
      </div>
    </>
  )
}

const AMR_SITE_TIME_ZONE = 'America/Chicago'

// absolute timestamp formatter for the drawer. AMR plants are reviewed in site
// time, not the browser's time zone, so operators compare events to the floor.
function absTime(iso: string): string {
  try {
    const d = new Date(iso)
    if (isNaN(d.getTime())) return '-'
    return d.toLocaleString(undefined, {
      timeZone: AMR_SITE_TIME_ZONE,
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      timeZoneName: 'short',
    })
  } catch { return '-' }
}

// ── Bad Zones Panel ────────────────────────────────────────────────────────────
// Map points (LM/AP/PP) where disconnects cluster. A location hit by several
// different robots is the "likely Wi-Fi/coverage" signal from the task spec.

function BadZonesPanel({ plant, timeRange, onPickLocation, activeLocation }: {
  plant: string
  timeRange: TimeRange
  onPickLocation: (loc: string, robots: string[]) => void
  activeLocation: string
}) {
  const { data: zones = [], isLoading } = useQuery({
    queryKey: ['amr-badzones', plant, timeRange.from],
    queryFn: () => getAMRBadZones(plant || undefined, timeRange),
    staleTime: 60_000,
  })

  if (isLoading) {
    return (
      <div className="rounded-xl border border-gray-700 bg-gray-800/40 px-4 py-3 text-xs text-gray-500">
        <RefreshCw size={12} className="animate-spin inline mr-2" /> Loading bad zones…
      </div>
    )
  }
  if (zones.length === 0) {
    return null
  }

  // Flag locations where >1 distinct robot dropped → coverage issue signal.
  return (
    <div className="rounded-xl border border-gray-700 bg-gray-800/40 overflow-hidden">
      <div className="px-4 py-2.5 border-b border-gray-700 flex items-center gap-2">
        <MapPin size={14} className="text-amber-400" />
        <span className="text-sm font-semibold text-gray-200">Bad Zones</span>
        <span className="text-[11px] text-gray-500">· map points where AMRs drop</span>
        <span className="ml-auto text-[10px] text-gray-600">{zones.length} locations</span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="bg-gray-900/60">
            <tr className="text-[10px] font-semibold text-gray-500 uppercase tracking-wide">
              <th className="px-4 py-2 text-left">Location</th>
              <th className="px-4 py-2 text-left">Plant</th>
              <th className="px-4 py-2 text-center">Drops</th>
              <th className="px-4 py-2 text-center">Robots</th>
              <th className="px-4 py-2 text-center">Worst</th>
              <th className="px-4 py-2 text-left">Last drop</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-800">
            {zones.slice(0, 10).map((z, i) => {
              const multiRobot = z.robots.length > 1
              return (
                <tr key={`${z.location}-${z.plant}-${i}`} className="hover:bg-gray-800/60 transition-colors">
                  <td className="px-4 py-2.5">
                    <button
                      onClick={() => onPickLocation(activeLocation === z.location ? '' : z.location, z.robots)}
                      className={`font-mono font-bold hover:underline ${activeLocation === z.location ? 'text-white ring-1 ring-indigo-500 rounded px-1' : multiRobot ? 'text-red-300' : 'text-amber-300'}`}
                      title="Filter fleet to robots that dropped here"
                    >
                      {z.location}
                    </button>
                    {multiRobot && (
                      <span className="ml-2 text-[9px] font-semibold px-1.5 py-0.5 rounded bg-red-900/60 text-red-300 uppercase">coverage?</span>
                    )}
                  </td>
                  <td className="px-4 py-2.5 text-xs text-gray-400">{z.plant || '—'}</td>
                  <td className="px-4 py-2.5 text-center">
                    <span className="text-red-300 font-medium">{z.drop_count}</span>
                  </td>
                  <td className="px-4 py-2.5 text-center text-xs text-gray-300">{z.robots.join(', ')}</td>
                  <td className="px-4 py-2.5 text-center">
                    <span className="text-amber-300 text-xs">{fmtDuration(z.worst_drop_sec)}</span>
                  </td>
                  <td className="px-4 py-2.5 text-xs text-gray-500 font-mono">{absTime(z.last_drop)}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      <div className="px-4 py-2 text-[10px] text-gray-600 border-t border-gray-700">
        A location is attributed using the nearest navigation target before each drop. <span className="text-red-400">coverage?</span> = multiple AMRs dropped there → likely Wi-Fi/coverage, not robot-specific.
      </div>
    </div>
  )
}

// ── Page ───────────────────────────────────────────────────────────────────────

type FilterStatus = 'all' | 'error' | 'warning' | 'ok' | 'unknown'
type ViewMode = 'table' | 'cards'

function localDayRange(daysAgo: number) {
  const start = new Date()
  start.setDate(start.getDate() - daysAgo)
  start.setHours(0, 0, 0, 0)
  const end = new Date(start)
  end.setHours(23, 59, 59, 999)
  return { from: start.toISOString(), to: end.toISOString() }
}

export default function AMRFleetPage() {
  const nav = useNavigate()
  const [search, setSearch]             = useState('')
  const [filterStatus, setFilterStatus] = useState<FilterStatus>('all')
  const [viewMode, setViewMode]         = useState<ViewMode>('table')
  const [selected, setSelected]         = useState<Set<string>>(new Set())
  // '' = All Plants (default). Otherwise a plant name. ALL_PLANTS sentinel keeps
  // the <select> value stable when the user explicitly picks All Plants after
  // having selected one.
  const ALL_PLANTS = '__all__'
  const [plant, setPlant]               = useState('')
  // Which AMR's reconnect-timeline drawer is open (name), if any.
  const [timelineTarget, setTimelineTarget] = useState<{ robot: string; plant: string } | null>(null)

  // Time-range preset for the timeline/bad-zones/summary endpoints (does NOT
  // affect the fleet summary table, which is all-time per robot). '' = all time.
  type RangePreset = '' | '24h' | '7d' | '30d'
  const [rangePreset, setRangePreset]   = useState<RangePreset>('7d')
  // Issue-type filter chips applied client-side to the fleet rows.
  type IssueFilter = '' | 'flapping' | 'long_outage' | 'coverage'
  const [issueFilter, setIssueFilter]   = useState<IssueFilter>('')
  // Location filter set by clicking a bad-zone row (robots that dropped there).
  const [locFilter, setLocFilter]       = useState('')
  const [locRobots, setLocRobots]       = useState<Set<string>>(new Set())
  const [batteryRange, setBatteryRange] = useState(() => localDayRange(1))

  function pickLocation(loc: string, robots: string[]) {
    if (loc === '') {
      setLocFilter('')
      setLocRobots(new Set())
    } else {
      setLocFilter(loc)
      setLocRobots(new Set(robots))
    }
  }

  // Convert the preset into a concrete from date (undefined for "all time").
  const timeRange: TimeRange = useMemo(() => {
    if (!rangePreset) return {}
    const days = rangePreset === '24h' ? 1 : rangePreset === '7d' ? 7 : 30
    const from = new Date(Date.now() - days * 86400000)
    return { from: from.toISOString() }
  }, [rangePreset])

  // Plant list
  const { data: plants = [] } = useQuery({
    queryKey: ['rds-plants'],
    queryFn: getRdsPlants,
  })

  const activePlant = plant


  // Fleet status from DB (connectivity stats, event counts). activePlant is
  // passed through as '' for All Plants, which the backend treats as no filter.
  const { data: fleetStatus = [], isLoading: fleetLoading, error, refetch, isFetching } = useQuery({
    queryKey: ['amr-fleet', activePlant],
    queryFn: () => getAMRFleet(activePlant),
    refetchInterval: 30_000,
  })

  const { data: batteryHistory = [], isLoading: batteryHistoryLoading } = useQuery({
    queryKey: ['amr-battery-history', activePlant, batteryRange.from, batteryRange.to],
    queryFn: () => getAMRBatteryHistory({
      plant: activePlant || undefined,
      from: batteryRange.from,
      to: batteryRange.to,
    }),
    refetchInterval: 60_000,
  })

  const batteryReport = useMemo(() => {
    const grouped = new Map<string, typeof batteryHistory>()
    for (const sample of batteryHistory) {
      const key = `${sample.plant}|${sample.amr}`
      grouped.set(key, [...(grouped.get(key) || []), sample])
    }
    return Array.from(grouped.values()).map(samples => {
      const chronological = [...samples].sort((a, b) => a.captured_at.localeCompare(b.captured_at))
      const levels = chronological.map(s => s.battery_level).filter((v): v is number => v != null)
      const temps = chronological.map(s => s.battery_temp_c).filter((v): v is number => v != null)
      const latest = chronological[chronological.length - 1]
      return {
        plant: latest.plant,
        amr: latest.amr,
        start: levels[0],
        end: levels[levels.length - 1],
        min: levels.length ? Math.min(...levels) : undefined,
        maxTemp: temps.length ? Math.max(...temps) : undefined,
        state: latest.battery_state,
        samples: chronological.length,
      }
    }).sort((a, b) => a.plant.localeCompare(b.plant) || a.amr.localeCompare(b.amr))
  }, [batteryHistory])

  // The backend already merges the live RDS Core roster/status with historical
  // connectivity metrics, so keep the page aligned with that single source.
  const amrs = useMemo(() => {
    return fleetStatus.map(a => ({ ...a, plant: activePlant || a.plant })).sort((a, b) => {
      const sd = statusRank(b.status) - statusRank(a.status)
      if (sd !== 0) return sd
      if (b.total_offline_sec !== a.total_offline_sec) return b.total_offline_sec - a.total_offline_sec
      const pd = (a.plant || '').localeCompare(b.plant || '')
      return pd !== 0 ? pd : a.name.localeCompare(b.name)
    })
  }, [fleetStatus, activePlant])

  const filtered = amrs.filter(a => {
    if (filterStatus !== 'all' && a.status !== filterStatus) return false
    if (search && !a.name.toLowerCase().includes(search.toLowerCase()) &&
        !a.plant.toLowerCase().includes(search.toLowerCase()) &&
        !a.last_ip.toLowerCase().includes(search.toLowerCase()) &&
        !rdsState(a).toLowerCase().includes(search.toLowerCase())) return false
    // Issue-type chips (client-side, derived from connectivity numbers).
    if (issueFilter === 'long_outage' && a.worst_drop_sec < 60) return false
    if (issueFilter === 'flapping' && a.reconnect_count < 3) return false
    if (issueFilter === 'coverage' && !locRobots.has(a.name) && locRobots.size === 0) return false
    // Location filter from the Bad Zones panel.
    if (locFilter && locRobots.size > 0 && !locRobots.has(a.name)) return false
    return true
  })

  function toggleSelected(amr: AMRStatus) {
    const key = amrKey(amr)
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  function toggleVisible(amrsToToggle: AMRStatus[]) {
    setSelected(prev => {
      const next = new Set(prev)
      const allVisibleSelected = amrsToToggle.length > 0 && amrsToToggle.every(a => next.has(amrKey(a)))
      for (const a of amrsToToggle) {
        const key = amrKey(a)
        if (allVisibleSelected) next.delete(key)
        else next.add(key)
      }
      return next
    })
  }

  // Open the reconnect-timeline drawer for one robot. Used by the per-row
  // "Investigate" button and the top-bar "investigate selected" action.
  function investigate(amr: AMRStatus) {
    setTimelineTarget({ robot: amr.name, plant: amr.plant || activePlant })
  }

  function investigateSelected() {
    const first = amrs.find(a => selected.has(amrKey(a)))
    if (first) investigate(first)
  }

  // Export the currently-filtered fleet rows as CSV.
  function exportFleetCSV() {
    if (!filtered.length) return
    exportCSV(
      filtered.map(a => ({
        robot: a.name,
        plant: a.plant || activePlant,
        status: a.status,
        last_ip: a.last_ip,
        last_mac: a.last_mac,
        rds_state: rdsState(a),
        status_code: a.status_code ?? '',
        battery_level_percent: a.battery_level ?? '',
        battery_temp_c: a.battery_temp_c ?? '',
        battery_state: a.battery_state ?? '',
        today_odo: a.today_odo ?? '',
        odo: a.odo ?? '',
        data_source: a.data_source ?? '',
        disconnect_count: a.disconnect_count,
        reconnect_count: a.reconnect_count,
        total_offline_sec: a.total_offline_sec,
        worst_drop_sec: a.worst_drop_sec,
        total_events: a.total_events,
        last_issue: a.last_issue,
      })),
      [
        { key: 'robot', header: 'Robot' },
        { key: 'plant', header: 'Plant' },
        { key: 'status', header: 'Status' },
        { key: 'last_ip', header: 'IP' },
        { key: 'last_mac', header: 'MAC' },
        { key: 'rds_state', header: 'RDS state' },
        { key: 'status_code', header: 'RDS status code' },
        { key: 'battery_level_percent', header: 'Battery (%)' },
        { key: 'battery_temp_c', header: 'Battery temperature (C)' },
        { key: 'battery_state', header: 'Battery state' },
        { key: 'today_odo', header: 'Today odometer' },
        { key: 'odo', header: 'Odometer' },
        { key: 'data_source', header: 'Data source' },
        { key: 'disconnect_count', header: 'Disconnects' },
        { key: 'reconnect_count', header: 'Reconnects' },
        { key: 'total_offline_sec', header: 'Total offline (s)' },
        { key: 'worst_drop_sec', header: 'Worst drop (s)' },
        { key: 'total_events', header: 'Total events' },
        { key: 'last_issue', header: 'Last issue' },
      ],
      `amr-fleet-${activePlant || 'all-plants'}-${format(new Date(), 'yyyy-MM-dd')}.csv`,
    )
  }

  function exportBatteryCSV() {
    if (!batteryReport.length) return
    exportCSV(
      batteryReport,
      [
        { key: 'plant', header: 'Plant' },
        { key: 'amr', header: 'AMR' },
        { key: 'start', header: 'Starting battery (%)' },
        { key: 'end', header: 'Ending battery (%)' },
        { key: 'min', header: 'Minimum battery (%)' },
        { key: 'maxTemp', header: 'Maximum temperature (C)' },
        { key: 'state', header: 'Latest state' },
        { key: 'samples', header: 'Samples' },
      ],
      `amr-battery-report-${format(new Date(batteryRange.from), 'yyyy-MM-dd')}.csv`,
    )
  }

  const selectedRows = amrs.filter(a => selected.has(amrKey(a)))
  const selectedLabel = selectedRows.length === 1 ? `${selectedRows[0].name} / ${selectedRows[0].plant || activePlant}` : `${selectedRows.length} selected`
  const isLoading = fleetLoading

  return (
    <div className="flex flex-col h-full bg-gray-900 text-gray-100">
      {/* Top bar */}
      <div className="flex items-center justify-between px-6 py-4 border-b border-gray-700 gap-4 flex-wrap">
        <div>
          <h1 className="text-base font-semibold text-white flex items-center gap-2">
            <Activity size={16} className="text-indigo-400" /> AMR Fleet Status
          </h1>
          <p className="text-xs text-gray-400 mt-0.5">Pulled from RDS Core · refreshes every 30 s</p>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          {/* Plant picker — "All Plants" is the default and aggregates across every plant. */}
          {plants.length >= 1 && (
            <select
              value={activePlant || ALL_PLANTS}
              onChange={e => { setPlant(e.target.value === ALL_PLANTS ? '' : e.target.value); setSelected(new Set()); setTimelineTarget(null) }}
              className="bg-gray-800 border border-gray-700 rounded-lg px-3 py-1.5 text-sm text-gray-200 focus:outline-none focus:ring-1 focus:ring-indigo-500"
            >
              <option value={ALL_PLANTS}>All Plants</option>
              {plants.map(p => (
                <option key={p.name} value={p.name}>{p.name}</option>
              ))}
            </select>
          )}

          {/* Investigate selected */}
          {selectedRows.length > 0 && (
            <button
              onClick={investigateSelected}
              className="flex items-center gap-2 text-xs px-3 py-1.5 rounded-lg bg-indigo-700 hover:bg-indigo-600 border border-indigo-600 text-white transition-colors"
            >
              Investigate {selectedLabel}
              <ChevronRight size={12} />
            </button>
          )}

          {/* View toggle */}
          <div className="inline-flex rounded-lg border border-gray-700 overflow-hidden">
            <button
              onClick={() => setViewMode('table')}
              className={`flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium transition-colors ${viewMode === 'table' ? 'bg-indigo-700 text-white' : 'bg-gray-800 text-gray-400 hover:text-gray-200'}`}
            >
              <Table2 size={13} /> Table
            </button>
            <button
              onClick={() => setViewMode('cards')}
              className={`flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium transition-colors ${viewMode === 'cards' ? 'bg-indigo-700 text-white' : 'bg-gray-800 text-gray-400 hover:text-gray-200'}`}
            >
              <LayoutGrid size={13} /> Cards
            </button>
          </div>

          <button
            onClick={() => refetch()}
            disabled={isFetching}
            className="flex items-center gap-2 text-xs px-3 py-1.5 rounded-lg bg-gray-700 hover:bg-gray-600 border border-gray-600 text-white transition-colors disabled:opacity-50"
          >
            <RefreshCw size={12} className={isFetching ? 'animate-spin' : ''} /> Refresh
          </button>

          <button
            onClick={exportFleetCSV}
            disabled={!filtered.length}
            className="flex items-center gap-2 text-xs px-3 py-1.5 rounded-lg bg-gray-700 hover:bg-gray-600 border border-gray-600 text-white transition-colors disabled:opacity-50"
            title="Export the filtered fleet rows as CSV"
          >
            <Download size={12} /> Export CSV
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto p-5 space-y-4">
        {/* Controls */}
        <div className="flex flex-col sm:flex-row gap-3">
          <div className="relative flex-1 max-w-xs">
            <Search size={13} className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500" />
            <input
              value={search}
              onChange={e => setSearch(e.target.value)}
              placeholder="Search AMR, IP, plant, state..."
              className="w-full bg-gray-800 border border-gray-700 rounded-lg pl-8 pr-3 py-2 text-sm text-gray-200 placeholder-gray-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
            />
          </div>
          <div className="flex gap-2 flex-wrap">
            {(['all', 'error', 'warning', 'ok', 'unknown'] as const).map(s => (
              <button
                key={s}
                onClick={() => setFilterStatus(s)}
                className={`text-xs font-medium px-3 py-1.5 rounded-lg border transition-colors ${
                  filterStatus === s
                    ? 'bg-indigo-700 border-indigo-600 text-white'
                    : 'bg-gray-800 border-gray-700 text-gray-400 hover:border-gray-500 hover:text-gray-200'
                }`}
              >
                {s === 'all' ? 'All' : STATUS_LABEL[s]}
              </button>
            ))}
            {selected.size > 0 && (
              <button
                onClick={() => setSelected(new Set())}
                className="text-xs px-3 py-1.5 rounded-lg border border-gray-600 text-gray-400 hover:text-white transition-colors"
              >
                Clear selection
              </button>
            )}
          </div>
        </div>

        {/* Time range + issue-type filters (affect timeline / bad zones / summary,
            and issue-type chips narrow the fleet rows client-side) */}
        <div className="flex flex-wrap items-center gap-2">
          <span className="flex items-center gap-1 text-[11px] text-gray-500 mr-1">
            <Calendar size={12} /> Range:
          </span>
          {([['', 'All time'], ['24h', '24h'], ['7d', '7d'], ['30d', '30d']] as const).map(([val, label]) => (
            <button
              key={val || 'all'}
              onClick={() => setRangePreset(val)}
              className={`text-[11px] font-medium px-2.5 py-1 rounded-lg border transition-colors ${
                rangePreset === val
                  ? 'bg-indigo-700 border-indigo-600 text-white'
                  : 'bg-gray-800 border-gray-700 text-gray-400 hover:border-gray-500 hover:text-gray-200'
              }`}
            >
              {label}
            </button>
          ))}

          <span className="w-px h-4 bg-gray-700 mx-1" />

          <span className="text-[11px] text-gray-500 mr-1">Issue:</span>
          {([['', 'All'], ['flapping', 'Flapping'], ['long_outage', 'Long outage >60s'], ['coverage', 'Coverage zones']] as const).map(([val, label]) => (
            <button
              key={val || 'all'}
              onClick={() => { setIssueFilter(val); if (val !== 'coverage') { setLocFilter(''); setLocRobots(new Set()) } }}
              className={`text-[11px] font-medium px-2.5 py-1 rounded-lg border transition-colors ${
                (issueFilter === val) && !(val === 'coverage' && !locFilter)
                  ? 'bg-indigo-700 border-indigo-600 text-white'
                  : 'bg-gray-800 border-gray-700 text-gray-400 hover:border-gray-500 hover:text-gray-200'
              }`}
            >
              {label}
            </button>
          ))}
          {locFilter && (
            <button
              onClick={() => pickLocation('', [])}
              className="text-[11px] px-2.5 py-1 rounded-lg border border-amber-700 bg-amber-950/40 text-amber-200 hover:bg-amber-900/40 transition-colors flex items-center gap-1"
            >
              <MapPin size={11} /> {locFilter} <X size={11} />
            </button>
          )}
        </div>

        {/* Summary */}
        {amrs.length > 0 && <SummaryBar amrs={amrs} selected={selected} />}

        {/* Persisted battery telemetry report */}
        <section className="rounded-xl border border-gray-700 bg-gray-800/50 overflow-hidden">
          <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 border-b border-gray-700">
            <div>
              <h2 className="text-sm font-semibold text-white">Battery History Report</h2>
              <p className="text-[11px] text-gray-400 mt-0.5">
                Recorded automatically from RDS Core while DRISHTI is running
              </p>
            </div>
            <div className="flex items-center gap-2">
              <button
                onClick={() => setBatteryRange(localDayRange(0))}
                className="text-[11px] px-2.5 py-1 rounded-lg border border-gray-600 bg-gray-800 text-gray-300 hover:text-white"
              >
                Today
              </button>
              <button
                onClick={() => setBatteryRange(localDayRange(1))}
                className="text-[11px] px-2.5 py-1 rounded-lg border border-indigo-600 bg-indigo-700 text-white"
              >
                Yesterday
              </button>
              <button
                onClick={exportBatteryCSV}
                disabled={!batteryReport.length}
                className="flex items-center gap-1.5 text-[11px] px-2.5 py-1 rounded-lg border border-gray-600 bg-gray-700 text-gray-200 disabled:opacity-40"
              >
                <Download size={11} /> Export report
              </button>
            </div>
          </div>
          {batteryHistoryLoading ? (
            <div className="px-4 py-5 text-xs text-gray-400">Loading battery history...</div>
          ) : batteryReport.length === 0 ? (
            <div className="px-4 py-5 text-xs text-amber-300">
              No stored samples for this period. Collection begins after this update is installed; it cannot recreate readings from before installation.
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead className="bg-gray-900/60 text-gray-400">
                  <tr>
                    <th className="px-4 py-2 text-left">Plant / AMR</th>
                    <th className="px-4 py-2 text-right">Start</th>
                    <th className="px-4 py-2 text-right">End</th>
                    <th className="px-4 py-2 text-right">Minimum</th>
                    <th className="px-4 py-2 text-right">Max temp</th>
                    <th className="px-4 py-2 text-left">Latest state</th>
                    <th className="px-4 py-2 text-right">Samples</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-700">
                  {batteryReport.map(row => (
                    <tr key={`${row.plant}|${row.amr}`} className="text-gray-200">
                      <td className="px-4 py-2"><span className="text-gray-500">{row.plant}</span> / {row.amr}</td>
                      <td className="px-4 py-2 text-right">{row.start == null ? '—' : `${row.start.toFixed(0)}%`}</td>
                      <td className="px-4 py-2 text-right">{row.end == null ? '—' : `${row.end.toFixed(0)}%`}</td>
                      <td className="px-4 py-2 text-right">{row.min == null ? '—' : `${row.min.toFixed(0)}%`}</td>
                      <td className="px-4 py-2 text-right">{row.maxTemp == null ? '—' : `${row.maxTemp.toFixed(1)}°C`}</td>
                      <td className="px-4 py-2">{row.state || '—'}</td>
                      <td className="px-4 py-2 text-right">{row.samples}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>

        {/* States */}
        {isLoading && (
          <div className="flex items-center justify-center py-20 text-gray-500">
            <RefreshCw size={18} className="animate-spin mr-2" /> Loading fleet from RDS Core…
          </div>
        )}
        {error && !isLoading && (
          <div className="rounded-lg border border-red-800 bg-red-950/40 px-4 py-3 text-sm text-red-200">
            <AlertTriangle size={14} className="inline mr-2" />
            Failed to load fleet status. Is the backend running?
          </div>
        )}
        {!isLoading && !error && amrs.length === 0 && (
          <div className="flex flex-col items-center justify-center py-20 text-gray-500 gap-3">
            <Radio size={32} className="text-gray-700" />
            <p className="text-sm">No AMRs found for <strong>{activePlant || 'this plant'}</strong>.</p>
            <p className="text-xs text-gray-600">Pull RDS logs first, or check the RDS Core connection in Setup.</p>
            <button onClick={() => nav('/rds-logs')} className="text-xs px-3 py-1.5 rounded-lg bg-indigo-700 hover:bg-indigo-600 text-white">
              Go to RDS Logs
            </button>
          </div>
        )}

        {/* Content */}
        {!isLoading && !error && filtered.length > 0 && (
          viewMode === 'table'
            ? <ConnectivityTable amrs={filtered} selected={selected} onToggle={toggleSelected} onToggleAll={toggleVisible} onInvestigate={investigate} activePlant={activePlant} />
            : (
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-3">
                {filtered.map(amr => (
                  <AMRCard
                    key={amrKey(amr)}
                    amr={amr}
                    selected={selected.has(amrKey(amr))}
                    onToggle={toggleSelected}
                    onInvestigate={investigate}
                  />
                ))}
              </div>
            )
        )}

        {!isLoading && !error && amrs.length > 0 && filtered.length === 0 && (
          <div className="text-center py-16 text-gray-500 text-sm">No AMRs match the current filter.</div>
        )}

        {viewMode === 'table' && amrs.length > 0 && (
          <div className="flex items-center gap-4 pt-1 text-[11px] text-gray-600">
            <span className="flex items-center gap-1"><CheckCircle size={11} className="text-green-600" /> 0 reconnects = stable</span>
            <span>·</span>
            <span>Time Offline is approximate, based on disconnect→connect event pairs</span>
            <span>·</span>
            <span>Click Investigate to open the reconnect timeline</span>
          </div>
        )}

        {/* Bad zones — where AMRs drop on the map */}
        {!isLoading && !error && amrs.length > 0 && (
          <BadZonesPanel
            plant={activePlant}
            timeRange={timeRange}
            onPickLocation={pickLocation}
            activeLocation={locFilter}
          />
        )}
      </div>

      {/* Reconnect timeline drawer */}
      {timelineTarget && (
        <TimelineDrawer
          robot={timelineTarget.robot}
          plant={timelineTarget.plant}
          timeRange={timeRange}
          onClose={() => setTimelineTarget(null)}
        />
      )}
    </div>
  )
}
