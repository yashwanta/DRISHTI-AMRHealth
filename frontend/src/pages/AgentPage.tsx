import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { clsx } from 'clsx'
import {
  BrainCircuit, Search, RefreshCw, AlertTriangle, CheckCircle2, XCircle,
  ChevronDown, ChevronRight, Loader2, Circle, Clock, FileText, Languages, Columns2
} from 'lucide-react'

// ── Plain-English translator for Roboshop / AMR log messages ──────────────────
type ViewMode = 'raw' | 'english' | 'both'

function translateLog(message: string): string {
  const m = message ?? ''

  // Map load / download
  if (/Robot successfully load(ed)? the map/i.test(m)) return 'Robot successfully loaded the navigation map.'
  if (/Downloading map/i.test(m)) return 'Robot is downloading a new navigation map.'
  if (/Download map successfully/i.test(m)) return 'Navigation map downloaded successfully.'
  const loadMapMatch = m.match(/loadMap[:\s]+(\d+)/i)
  if (loadMapMatch) return `Map loaded (map ID: ${loadMapMatch[1]}).`
  const mapBytesMatch = m.match(/bytes\.size\[(\d+)\]/i)
  if (mapBytesMatch) {
    const name = m.match(/Map name:\[([^\]]+)\]/i)?.[1] ?? 'unknown'
    return `Map "${name.split('/').pop()}" downloaded (${Number(mapBytesMatch[1]).toLocaleString()} bytes).`
  }

  // TCP / network
  if (/SocketState:ConnectedState/i.test(m)) {
    const server = m.match(/Server:([\d.:]+)/i)?.[1]
    return `Network connection established${server ? ` to server ${server}` : ''}.`
  }
  if (/SocketState:UnconnectedState/i.test(m) || /disconnected/i.test(m)) return 'Network connection lost.'
  if (/connection refused/i.test(m)) return 'Connection refused — remote host not accepting connections.'
  if (/timeout/i.test(m)) return 'Operation timed out.'

  // Robot control window
  if (/RobotControlWindow/i.test(m)) {
    const action = m.replace(/.*RobotControlWindow\s*/i, '').trim()
    return action ? `Robot control: ${action}` : 'Robot control window event.'
  }

  // Config / factory reset
  if (/factory.?default|config.?reset/i.test(m)) return 'Robot configuration reset to factory defaults.'
  if (/config.*load|load.*config/i.test(m)) return 'Robot configuration loaded.'

  // Battery
  if (/battery.*low|low.*battery/i.test(m)) return 'Low battery warning.'
  if (/battery.*error|battery.*fail/i.test(m)) return 'Battery error detected.'
  if (/charging/i.test(m) && /start|begin/i.test(m)) return 'Robot started charging.'
  if (/charging/i.test(m) && /stop|end|complete/i.test(m)) return 'Robot finished charging.'

  // Error / warn patterns
  if (/\[error\]|error:/i.test(m)) {
    const detail = m.replace(/.*(?:\[error\]|error:)\s*/i, '').trim().slice(0, 120)
    return `Error: ${detail}`
  }
  if (/\[warn\]|warning:/i.test(m)) {
    const detail = m.replace(/.*(?:\[warn\]|warning:)\s*/i, '').trim().slice(0, 120)
    return `Warning: ${detail}`
  }

  // RDS / map updates
  if (/smap.*push|push.*smap|map.*upload|upload.*map/i.test(m)) return 'Scene map pushed / uploaded to RDS.'
  if (/rds.*connect|connect.*rds/i.test(m)) return 'Connected to RDS (Robot Data System).'

  // Scheduler / sync
  if (/sync.*start|start.*sync/i.test(m)) return 'Log synchronisation started.'
  if (/sync.*finish|finish.*sync/i.test(m)) return 'Log synchronisation finished.'

  // Fallback: strip bracket noise and return cleaned message
  const cleaned = m
    .replace(/\[[\d\s:.]+\]/g, '')
    .replace(/\[Roboshop\]\[\d+\]/g, '')
    .replace(/\[(?:info|warn|error|debug)\]/gi, '')
    .replace(/\[[\w:]+\]/g, '')
    .replace(/\s{2,}/g, ' ')
    .trim()
  return cleaned || m
}

function ViewToggle({ mode, onChange }: { mode: ViewMode; onChange: (m: ViewMode) => void }) {
  const opts: { value: ViewMode; icon: React.ReactNode; label: string }[] = [
    { value: 'raw',     icon: <FileText size={12} />,   label: 'Raw' },
    { value: 'english', icon: <Languages size={12} />,  label: 'Plain English' },
    { value: 'both',    icon: <Columns2 size={12} />,   label: 'Both' },
  ]
  return (
    <div className="inline-flex rounded-md border border-gray-600 overflow-hidden text-[11px] font-medium">
      {opts.map(o => (
        <button
          key={o.value}
          onClick={() => onChange(o.value)}
          className={clsx(
            'flex items-center gap-1 px-2.5 py-1 transition-colors',
            mode === o.value
              ? 'bg-blue-600 text-white'
              : 'bg-gray-800 text-gray-400 hover:text-gray-200'
          )}
        >
          {o.icon}{o.label}
        </button>
      ))}
    </div>
  )
}
import { getRdsPlants, getAgentRobots, startAgentJob, getAgentJob } from '../api/client'
import type { AgentJob, AgentSourceStatus, SourceState } from '../types'

const INVESTIGATION_TYPES = [
  'Config Reset / Factory Default',
  'Robot Offline',
  'Connectivity Loss',
  'Battery Error',
  'RDS Map Update Failure',
  'General Log Analysis',
]

const inputCls = 'text-sm bg-gray-800 border border-gray-600 text-gray-200 rounded-md px-3 py-2 focus:outline-none focus:border-blue-500 w-full'
const labelCls = 'text-[11px] font-semibold text-gray-400 uppercase tracking-wide'

function nowLocalDateTime(offsetHours = 0): string {
  const d = new Date(Date.now() + offsetHours * 3600_000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export default function AgentPage() {
  const [searchParams] = useSearchParams()
  const urlPlant = searchParams.get('plant') ?? ''
  const urlRobot = searchParams.get('robot') ?? ''

  const [plant, setPlant] = useState(urlPlant)
  const [robotId, setRobotId] = useState(urlRobot)
  const [robotText, setRobotText] = useState(urlRobot)
  const [invType, setInvType] = useState(INVESTIGATION_TYPES[1])
  const [focus, setFocus] = useState('')
  const [windowStart, setWindowStart] = useState(nowLocalDateTime(-3))
  const [windowEnd, setWindowEnd] = useState(nowLocalDateTime(0))

  const [jobId, setJobId] = useState<string | null>(null)
  const [starting, setStarting] = useState(false)
  const [startError, setStartError] = useState('')

  const { data: plants = [] } = useQuery({ queryKey: ['rds-plants'], queryFn: getRdsPlants, staleTime: 5 * 60_000 })
  // Pre-select from URL param first, then fall back to first plant
  useEffect(() => {
    if (plants.length === 0) return
    if (!plant) setPlant(urlPlant || plants[0].name)
  }, [plants]) // eslint-disable-line react-hooks/exhaustive-deps

  // Robot list for the selected plant (best-effort from RDS / log inference).
  const { data: robots = [] } = useQuery({
    queryKey: ['agent-robots', plant],
    queryFn: () => getAgentRobots(plant),
    enabled: !!plant,
    staleTime: 60_000,
  })

  // Poll job status every 2s while in flight.
  const jobQuery = useQuery<AgentJob>({
    queryKey: ['agent-job', jobId],
    queryFn: () => getAgentJob(jobId!),
    enabled: !!jobId,
    refetchInterval: (q) => {
      const st = q.state.data?.status
      return st === 'complete' || st === 'error' ? false : 2000
    },
  })
  const job = jobQuery.data
  const inFlight = job !== undefined && job.status !== 'complete' && job.status !== 'error'

  async function investigate() {
    setStartError('')
    if (!plant || !invType) { setStartError('Plant and investigation type are required.'); return }
    setStarting(true)
    try {
      const rid = robotText.trim() || robotId.trim()
      const { job_id } = await startAgentJob({
        plant_id: plant,
        robot_id: rid,
        investigation_type: invType,
        focus: focus.trim(),
        window_start: new Date(windowStart).toISOString(),
        window_end: new Date(windowEnd).toISOString(),
      })
      setJobId(job_id)
    } catch (e) {
      setStartError(e instanceof Error ? e.message : 'Failed to start investigation.')
    } finally {
      setStarting(false)
    }
  }

  function reset() {
    setJobId(null)
  }

  return (
    <div className="flex flex-col h-full bg-gray-900 text-gray-100 overflow-auto">
      <div className="px-6 py-4 border-b border-gray-700 bg-gray-900">
        <div className="flex items-center gap-2">
          <BrainCircuit size={18} className="text-blue-400" />
          <h1 className="text-base font-semibold text-white">Agent</h1>
          <span className="text-[10px] font-semibold px-1.5 py-0.5 rounded-full bg-blue-500 text-white">New</span>
          <span className="text-xs text-gray-500 ml-2">Robot incident investigation</span>
        </div>
      </div>

      <div className="p-6 space-y-5 max-w-5xl">
        {/* Panel A — Investigation Trigger */}
        <section className="bg-gray-800 border border-gray-700 rounded-lg p-5">
          <h2 className="text-sm font-semibold text-gray-200 mb-4 flex items-center gap-2">
            <Search size={14} className="text-blue-400" /> Investigation Trigger
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="space-y-1.5">
              <label className={labelCls}>Plant</label>
              <select className={inputCls} value={plant} onChange={e => { setPlant(e.target.value); setRobotId(''); setRobotText('') }}>
                {plants.map(p => <option key={p.name} value={p.name}>{p.name}</option>)}
              </select>
            </div>
            <div className="space-y-1.5">
              <label className={labelCls}>Robot ID</label>
              {robots.length > 0 ? (
                <select className={inputCls} value={robotId} onChange={e => { setRobotId(e.target.value); setRobotText(e.target.value) }}>
                  <option value="">— select robot —</option>
                  {robots.map(r => <option key={r.id} value={r.id}>{r.name}</option>)}
                </select>
              ) : (
                <input className={inputCls} placeholder="e.g. AMR-007" value={robotText} onChange={e => setRobotText(e.target.value)} />
              )}
            </div>
            <div className="space-y-1.5">
              <label className={labelCls}>Window Start</label>
              <input type="datetime-local" className={inputCls} value={windowStart} onChange={e => setWindowStart(e.target.value)} />
            </div>
            <div className="space-y-1.5">
              <label className={labelCls}>Window End</label>
              <input type="datetime-local" className={inputCls} value={windowEnd} onChange={e => setWindowEnd(e.target.value)} />
            </div>
            <div className="space-y-1.5 md:col-span-2">
              <label className={labelCls}>Investigation Type</label>
              <select className={inputCls} value={invType} onChange={e => setInvType(e.target.value)}>
                {INVESTIGATION_TYPES.map(t => <option key={t} value={t}>{t}</option>)}
              </select>
            </div>
            <div className="space-y-1.5 md:col-span-2">
              <label className={labelCls}>What should I look for? (optional)</label>
              <input className={inputCls} placeholder="e.g. AMR-5 config reset around 3pm" value={focus} onChange={e => setFocus(e.target.value)} />
            </div>
          </div>
          <div className="flex items-center gap-3 mt-4">
            <button
              onClick={investigate}
              disabled={starting || inFlight}
              className="inline-flex items-center gap-2 text-sm font-medium px-4 py-2 rounded-lg bg-blue-600 hover:bg-blue-700 text-white transition-colors disabled:opacity-50"
            >
              {starting || inFlight ? <RefreshCw size={14} className="animate-spin" /> : <Search size={14} />}
              {inFlight ? 'Investigating…' : 'Investigate'}
            </button>
            {jobId && !inFlight && (
              <button onClick={reset} className="text-xs text-gray-400 hover:text-white">New investigation</button>
            )}
            {startError && <span className="text-xs text-red-400">{startError}</span>}
          </div>
        </section>

        {/* Panel B — Log Pull Status */}
        {job && <LogPullStatus job={job} />}

        {/* Panel C — Findings */}
        {job && (job.status === 'complete' || job.status === 'error') && (
          <Findings job={job} />
        )}
      </div>
    </div>
  )
}

function StatusDot({ state }: { state: SourceState }) {
  if (state === 'done') return <CheckCircle2 size={15} className="text-green-400" />
  if (state === 'in_progress') return <Loader2 size={15} className="text-blue-400 animate-spin" />
  if (state === 'unavailable') return <XCircle size={15} className="text-red-400" />
  return <Circle size={15} className="text-gray-500" />
}

function LogPullStatus({ job }: { job: AgentJob }) {
  const label = {
    pending: 'Pending', collecting: 'Collecting logs…', analyzing: 'Analyzing…',
    complete: 'Complete', error: 'Error',
  }[job.status]
  return (
    <section className="bg-gray-800 border border-gray-700 rounded-lg p-5">
      <h2 className="text-sm font-semibold text-gray-200 mb-1 flex items-center gap-2">
        <Clock size={14} className="text-blue-400" /> Log Pull Status
      </h2>
      <p className="text-xs text-gray-500 mb-4">{label}{job.error ? ` — ${job.error}` : ''}</p>
      <div className="divide-y divide-gray-700/60">
        {(job.sources ?? []).map((s: AgentSourceStatus) => (
          <div key={s.source} className="flex items-center gap-3 py-2">
            <StatusDot state={s.state} />
            <span className="text-sm text-gray-200 w-56">{s.source}</span>
            <span className="text-xs text-gray-400 flex-1">{s.result}{s.error ? ` (${s.error})` : ''}</span>
          </div>
        ))}
      </div>
    </section>
  )
}

const CONFIDENCE_TONE: Record<string, string> = {
  high: 'bg-green-900/60 text-green-300 border border-green-700',
  medium: 'bg-yellow-900/60 text-yellow-300 border border-yellow-700',
  low: 'bg-gray-700 text-gray-300 border border-gray-600',
}

function Findings({ job }: { job: AgentJob }) {
  const [showRaw, setShowRaw] = useState(false)
  const [viewMode, setViewMode] = useState<ViewMode>('both')

  if (job.status === 'error') {
    return (
      <section className="bg-gray-800 border border-red-800 rounded-lg p-5">
        <div className="flex items-center gap-2 mb-1">
          <AlertTriangle size={16} className="text-red-400" />
          <h2 className="text-sm font-semibold text-red-300">Investigation could not complete</h2>
        </div>
        <p className="text-sm text-gray-400">{job.error || 'Unknown error.'}</p>
      </section>
    )
  }

  const f = job.finding
  if (!f) return null

  return (
    <section className="bg-gray-800 border border-gray-700 rounded-lg p-5 space-y-4">
      <h2 className="text-sm font-semibold text-gray-200 flex items-center gap-2">
        <BrainCircuit size={14} className="text-blue-400" /> Agent Findings
      </h2>

      {/* Root cause */}
      <div className="bg-blue-950/40 border border-blue-800 rounded-lg p-4">
        <div className="text-[11px] font-semibold text-blue-300 uppercase tracking-wide mb-1">Root Cause</div>
        <p className="text-sm text-gray-100">{f.root_cause}</p>
      </div>

      <div className="flex items-center gap-2 flex-wrap">
        <span className={clsx('text-[11px] font-semibold px-2 py-0.5 rounded-full capitalize', CONFIDENCE_TONE[f.confidence] ?? CONFIDENCE_TONE.low)}>
          Confidence: {f.confidence}
        </span>
        <span className="text-[11px] text-gray-500">via {f.via}</span>
      </div>

      {f.llm_note && (
        <p className="text-xs text-yellow-400 bg-yellow-950/30 border border-yellow-800 rounded-md px-3 py-2">{f.llm_note}</p>
      )}

      {/* Contributing factors */}
      {(f.factors ?? []).length > 0 && (
        <div>
          <div className="text-[11px] font-semibold text-gray-400 uppercase tracking-wide mb-2">Contributing Factors</div>
          <ul className="space-y-1">
            {(f.factors ?? []).map((fac, i) => (
              <li key={i} className="text-sm text-gray-300 flex gap-2"><span className="text-gray-600">•</span>{fac}</li>
            ))}
          </ul>
        </div>
      )}

      {/* Timeline */}
      {(f.timeline ?? []).length > 0 && (
        <div>
          <div className="flex items-center justify-between mb-2">
            <div className="text-[11px] font-semibold text-gray-400 uppercase tracking-wide">Timeline</div>
            <ViewToggle mode={viewMode} onChange={setViewMode} />
          </div>
          <div className="space-y-2">
            {(f.timeline ?? []).map((t, i) => {
              const english = translateLog(t.event)
              return (
                <div key={i} className="text-xs flex gap-2">
                  <span className="text-gray-600 font-mono shrink-0 pt-0.5">{shortTime(t.timestamp)}</span>
                  <div className="flex-1 space-y-0.5">
                    {(viewMode === 'raw' || viewMode === 'both') && (
                      <div className="text-gray-400 font-mono break-all">
                        <span className="text-gray-600">[{t.source}]</span> {t.event}
                      </div>
                    )}
                    {(viewMode === 'english' || viewMode === 'both') && (
                      <div className={clsx(
                        'text-gray-200',
                        viewMode === 'both' && 'pl-2 border-l-2 border-blue-700/50 text-blue-200'
                      )}>
                        {english}
                      </div>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* Prevention */}
      <div>
        <div className="text-[11px] font-semibold text-gray-400 uppercase tracking-wide mb-1">Prevention Recommendation</div>
        <p className="text-sm text-gray-300">{f.prevention}</p>
      </div>

      {/* Raw logs toggle */}
      <div>
        <div className="flex items-center justify-between">
          <button onClick={() => setShowRaw(v => !v)} className="inline-flex items-center gap-1 text-xs text-gray-400 hover:text-gray-200">
            {showRaw ? <ChevronDown size={14} /> : <ChevronRight size={14} />} View Raw Logs ({(f.raw_logs ?? []).length})
          </button>
          {showRaw && <ViewToggle mode={viewMode} onChange={setViewMode} />}
        </div>
        {showRaw && (
          <div className="mt-2 bg-gray-900 border border-gray-700 rounded-md p-3 max-h-72 overflow-auto space-y-2">
            {(f.raw_logs ?? []).length === 0 ? (
              <p className="text-xs text-gray-500">No raw logs attached.</p>
            ) : (f.raw_logs ?? []).map((l, i) => {
              const english = translateLog(l.message)
              const levelCls = l.level === 'error' ? 'text-red-400' : l.level === 'warn' ? 'text-yellow-400' : 'text-gray-500'
              return (
                <div key={i} className="text-xs flex gap-2">
                  <span className="text-gray-600 font-mono shrink-0 pt-0.5">{shortTime(l.timestamp)}</span>
                  <div className="flex-1 space-y-0.5">
                    {(viewMode === 'raw' || viewMode === 'both') && (
                      <div className="font-mono text-gray-400 break-all">
                        <span className={clsx('shrink-0', levelCls)}>[{l.level}]</span>{' '}
                        <span className="text-gray-600">{l.source}</span> {l.message}
                      </div>
                    )}
                    {(viewMode === 'english' || viewMode === 'both') && (
                      <div className={clsx(
                        'text-gray-200',
                        viewMode === 'both' && 'pl-2 border-l-2 border-blue-700/50 text-blue-200'
                      )}>
                        {english}
                      </div>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </div>
    </section>
  )
}

function shortTime(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  return d.toLocaleString(undefined, { month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' })
}
