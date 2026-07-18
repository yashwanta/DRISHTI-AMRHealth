import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { format, subDays, startOfDay } from 'date-fns'
import { clsx } from 'clsx'
import {
  Wifi, WifiOff, CheckCircle, XCircle, AlertTriangle,
  RefreshCw, Search, Download, ChevronDown, Eye, Zap, Radio, KeyRound, BrainCircuit
} from 'lucide-react'
import {
  getRdsPlants, getRdsConnectionStatus, testRdsConnection,
  discoverRdsSources, explainRdsIncident, getRdsLogs, saveRdsCredentials, getServers, syncServer
} from '../api/client'
import { useAuth } from '../auth'
import type {
  AgentLogExplanation, RdsLogEntry, RdsLogFilters, RdsLogSource, RdsTestResult
} from '../types'

const SEVERITY_CLASS: Record<string, string> = {
  critical: 'bg-red-900/80 text-red-300',
  high: 'bg-orange-900/80 text-orange-300',
  medium: 'bg-yellow-900/80 text-yellow-300',
  low: 'bg-blue-900/80 text-blue-300',
  info: 'bg-gray-700 text-gray-300',
}

const CATEGORIES = ['charge', 'dock', 'navigation', 'status', 'error', 'update', 'settings', 'health', 'unknown']
const SEVERITIES = ['critical', 'high', 'medium', 'low', 'info']

const inputCls = 'text-xs bg-gray-800 border border-gray-600 text-gray-200 rounded-md px-2 py-1.5 focus:outline-none focus:border-blue-500'

const DATE_SHORTCUTS = [
  { label: 'Today', fn: () => ({ from: format(startOfDay(new Date()), "yyyy-MM-dd'T'HH:mm"), to: '' }) },
  { label: 'Yesterday', fn: () => ({ from: format(startOfDay(subDays(new Date(), 1)), "yyyy-MM-dd'T'HH:mm"), to: format(startOfDay(new Date()), "yyyy-MM-dd'T'HH:mm") }) },
  { label: 'Last 7 days', fn: () => ({ from: format(subDays(new Date(), 7), "yyyy-MM-dd'T'HH:mm"), to: '' }) },
  { label: 'All time', fn: () => ({ from: '', to: '' }) },
]

function StatusCard({ icon: Icon, label, value, sub, color }: {
  icon: typeof Wifi
  label: string
  value: string
  sub?: string
  color?: string
}) {
  return (
    <div className='bg-gray-800 border border-gray-700 rounded-lg px-4 py-3 flex items-center gap-3'>
      <div className={clsx('p-2 rounded-lg', color ?? 'bg-gray-700')}>
        <Icon size={18} className='text-gray-300' />
      </div>
      <div className='min-w-0'>
        <div className='text-[10px] text-gray-500 uppercase tracking-wide'>{label}</div>
        <div className='text-sm font-semibold text-white truncate'>{value}</div>
        {sub && <div className='text-xs text-gray-500 truncate'>{sub}</div>}
      </div>
    </div>
  )
}

export default function RdsLogsPage() {
  const qc = useQueryClient()
  const auth = useAuth()
  const [selectedPlant, setSelectedPlant] = useState<string>('')
  const [keyword, setKeyword] = useState('')
  const [fromDate, setFromDate] = useState('')
  const [toDate, setToDate] = useState('')
  const [filterRobot, setFilterRobot] = useState('')
  const [filterUser, setFilterUser] = useState('')
  const [filterCategory, setFilterCategory] = useState('')
  const [filterSeverity, setFilterSeverity] = useState('')
  const [filterExecEvidence, setFilterExecEvidence] = useState<boolean | ''>('')
  const [testResult, setTestResult] = useState<RdsTestResult | null>(null)
  const [discoveredSources, setDiscoveredSources] = useState<RdsLogSource[] | null>(null)
  const [testing, setTesting] = useState(false)
  const [discovering, setDiscovering] = useState(false)
  const [fetching, setFetching] = useState(false)
  const [fetchResult, setFetchResult] = useState<{ event_count?: number; message?: string } | null>(null)
  const [detailEntry, setDetailEntry] = useState<RdsLogEntry | null>(null)
  const [showExportMenu, setShowExportMenu] = useState(false)
  const [showCredentials, setShowCredentials] = useState(false)
  const [rdsUsername, setRdsUsername] = useState('robowatch')
  const [rdsPassword, setRdsPassword] = useState('')
  const [credentialError, setCredentialError] = useState('')
  const [savingCredentials, setSavingCredentials] = useState(false)
  const [agentAnalyzing, setAgentAnalyzing] = useState(false)
  const [agentFinding, setAgentFinding] = useState<AgentLogExplanation | null>(null)
  const [agentError, setAgentError] = useState('')

  // Plant config loaded from backend
  const { data: plants = [] } = useQuery({
    queryKey: ['rds-plants'],
    queryFn: getRdsPlants,
    staleTime: 5 * 60_000,
  })
  const { data: servers = [] } = useQuery({ queryKey: ['servers'], queryFn: getServers })

  // Auto-select first plant
  useEffect(() => {
    if (plants.length > 0 && !selectedPlant) {
      setSelectedPlant(plants[0].name)
    }
  }, [plants, selectedPlant])

  // Connection status for selected plant
  const { data: connStatus } = useQuery({
    queryKey: ['rds-status', selectedPlant],
    queryFn: () => getRdsConnectionStatus(selectedPlant),
    enabled: !!selectedPlant,
    refetchInterval: 60_000,
  })

  // Filters derived from UI state
  const [filters, setFilters] = useState<RdsLogFilters>(() => ({ limit: 500 }))

  useEffect(() => {
    const timeout = window.setTimeout(() => {
      setFilters(f => ({
        ...f,
        plant: selectedPlant || undefined,
        q: keyword.trim() || undefined,
        from: fromDate ? new Date(fromDate).toISOString() : undefined,
        to: toDate ? new Date(toDate).toISOString() : undefined,
        robot: filterRobot.trim() || undefined,
        user: filterUser.trim() || undefined,
        category: filterCategory || undefined,
        severity: filterSeverity || undefined,
        execution_evidence: filterExecEvidence === '' ? undefined : filterExecEvidence ?? undefined,
      }))
    }, 250)
    return () => window.clearTimeout(timeout)
  }, [selectedPlant, keyword, fromDate, toDate, filterRobot, filterUser, filterCategory, filterSeverity, filterExecEvidence])

  // Log entries
  const { data: entries = [], isLoading } = useQuery({
    queryKey: ['rds-logs', filters],
    queryFn: () => getRdsLogs(filters),
    enabled: !!selectedPlant,
  })

  async function handleTestConnection() {
    if (!selectedPlant) return
    setTesting(true)
    setTestResult(null)
    try {
      const result = await testRdsConnection(selectedPlant)
      setTestResult(result)
      qc.invalidateQueries({ queryKey: ['rds-status', selectedPlant] })
    } catch (err: any) {
      setTestResult({ reachable: false, authenticated: false, success: false, error: err?.response?.data?.error ?? 'Connection test failed' })
    } finally {
      setTesting(false)
    }
  }

  async function handleSaveCredentials() {
    if (!selectedPlant || !rdsPassword) return
    setSavingCredentials(true); setCredentialError('')
    try {
      await saveRdsCredentials(selectedPlant, rdsUsername, rdsPassword)
      setRdsPassword(''); setShowCredentials(false)
      await handleTestConnection()
    } catch (err: any) {
      setCredentialError(err?.response?.data?.error ?? 'Could not save RDS credentials')
    } finally { setSavingCredentials(false) }
  }

  async function handleDiscover() {
    if (!selectedPlant) return
    setDiscovering(true)
    setDiscoveredSources(null)
    try {
      const result = await discoverRdsSources(selectedPlant)
      setDiscoveredSources(result.sources)
      qc.invalidateQueries({ queryKey: ['rds-status', selectedPlant] })
    } catch {
      setDiscoveredSources(null)
    } finally {
      setDiscovering(false)
    }
  }

  async function handleFetch() {
    if (!selectedPlant) return
    setFetching(true)
    setFetchResult(null)
    try {
      const plant = plants.find(item => item.name === selectedPlant)
      const host = plant ? new URL(plant.base_url).hostname : ''
      const server = servers.find(item => item.host === host)
      if (!server) throw new Error(`No FleetManager SSH server is configured for ${selectedPlant}`)
      await syncServer(server.id)
      setFetchResult({ message: `FleetManager SSH sync queued for ${selectedPlant}. Existing events are shown below; new events will appear when the sync finishes.` })
      window.setTimeout(() => { qc.invalidateQueries({ queryKey: ['rds-logs'] }); qc.invalidateQueries({ queryKey: ['rds-status', selectedPlant] }) }, 5000)
      window.setTimeout(() => { qc.invalidateQueries({ queryKey: ['rds-logs'] }); qc.invalidateQueries({ queryKey: ['rds-status', selectedPlant] }) }, 15000)
    } catch (err: any) {
      setFetchResult({ message: err?.response?.data?.error ?? 'Fetch failed' })
    } finally {
      setFetching(false)
    }
  }

  async function handleAgentAnalyze() {
    if (entries.length === 0) return
    setAgentAnalyzing(true)
    setAgentError('')
    try {
      const severityRank = (severity: string) => ({ critical: 4, high: 3, medium: 2, low: 1, info: 0 }[severity] ?? 0)
      const prioritized = [...entries].sort((a, b) => {
        const severityDelta = severityRank(b.severity) - severityRank(a.severity)
        if (severityDelta !== 0) return severityDelta
        return Number(b.execution_evidence) - Number(a.execution_evidence)
      }).slice(0, 100)
      const context = `RDS/FleetManager incident; plant=${selectedPlant}; robot=${filterRobot || 'all'}; category=${filterCategory || 'all'}; severity=${filterSeverity || 'all'}; evidence=${filterExecEvidence === '' ? 'all' : filterExecEvidence ? 'execution only' : 'non-execution'}; query=${keyword || 'none'}; range=${fromDate || 'all'} to ${toDate || 'now'}`
      setAgentFinding(await explainRdsIncident(prioritized, context))
    } catch (error) {
      setAgentError(error instanceof Error ? error.message : 'Agent could not analyze the RDS evidence.')
    } finally {
      setAgentAnalyzing(false)
    }
  }

  function exportCSV() {
    const headers = ['id', 'plant', 'source_system', 'timestamp', 'robot', 'user', 'action', 'category', 'severity', 'message', 'raw_log', 'confidence', 'execution_evidence']
    const rows = entries.map(e => headers.map(h => {
      const v = (e as any)[h] ?? ''
      return String(v).includes(',') ? `"${v}"` : v
    }).join(','))
    const csv = [headers.join(','), ...rows].join('\n')
    const blob = new Blob([csv], { type: 'text/csv' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `rds-logs-${selectedPlant}-${format(new Date(), 'yyyy-MM-dd')}.csv`
    a.click()
    URL.revokeObjectURL(url)
    setShowExportMenu(false)
  }

  function exportJSON() {
    const blob = new Blob([JSON.stringify(entries, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `rds-logs-${selectedPlant}-${format(new Date(), 'yyyy-MM-dd')}.json`
    a.click()
    URL.revokeObjectURL(url)
    setShowExportMenu(false)
  }

  return (
    <div className='flex flex-col h-full bg-gray-900 text-gray-100'>
      {/* ── Header ──────────────────────────────────────────────── */}
      <div className='flex-shrink-0 px-6 py-4 border-b border-gray-700'>
        <div className='flex items-center justify-between'>
          <div className='flex items-center gap-3'>
            <div className='p-2 rounded-lg bg-blue-600/20'>
              <Radio size={18} className='text-blue-400' />
            </div>
            <div>
              <h1 className='text-base font-semibold text-white'>RDS Logs</h1>
              <p className='text-xs text-gray-500'>RoboWatch / FleetManager log ingestion</p>
            </div>
          </div>
          <div className='flex items-center gap-3'>
            <select
              value={selectedPlant}
              onChange={e => { setSelectedPlant(e.target.value); setTestResult(null); setDiscoveredSources(null); setFetchResult(null) }}
              className={inputCls}
            >
              <option value=''>Select plant...</option>
              {plants.map(p => (
                <option key={p.name} value={p.name}>{p.name} ({p.system_type})</option>
              ))}
            </select>
            {auth.canAdmin && <button onClick={() => { const plant = plants.find(p => p.name === selectedPlant); setRdsUsername(plant?.username || 'robowatch'); setCredentialError(''); setShowCredentials(true) }} disabled={!selectedPlant} className='inline-flex items-center gap-1.5 text-xs px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-40 text-white rounded-md transition-colors'><KeyRound size={12}/> RDS Password</button>}
            <button
              onClick={handleTestConnection}
              disabled={!selectedPlant || testing}
              className='inline-flex items-center gap-1.5 text-xs px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-40 text-white rounded-md transition-colors'
            >
              {testing ? <RefreshCw size={12} className='animate-spin' /> : <Wifi size={12} />}
              Test Connection
            </button>
            <button
              onClick={handleDiscover}
              disabled={!selectedPlant || discovering}
              className='inline-flex items-center gap-1.5 text-xs px-3 py-1.5 bg-purple-600 hover:bg-purple-700 disabled:opacity-40 text-white rounded-md transition-colors'
            >
              {discovering ? <RefreshCw size={12} className='animate-spin' /> : <Search size={12} />}
              Discover Sources
            </button>
            <button
              onClick={handleFetch}
              disabled={!selectedPlant || fetching}
              className='inline-flex items-center gap-1.5 text-xs px-3 py-1.5 bg-green-600 hover:bg-green-700 disabled:opacity-40 text-white rounded-md transition-colors'
            >
              {fetching ? <RefreshCw size={12} className='animate-spin' /> : <Download size={12} />}
              Pull Logs
            </button>
            <button
              onClick={handleAgentAnalyze}
              disabled={!selectedPlant || entries.length === 0 || agentAnalyzing}
              className='inline-flex items-center gap-1.5 text-xs px-3 py-1.5 bg-cyan-600 hover:bg-cyan-700 disabled:opacity-40 text-white rounded-md transition-colors'
            >
              {agentAnalyzing ? <RefreshCw size={12} className='animate-spin' /> : <BrainCircuit size={12} />}
              {agentAnalyzing ? 'Analyzing...' : 'Agent: Analyze RDS Incident'}
            </button>
          </div>
        </div>

        {/* Test result */}
        {testResult && (
          <div className={clsx(
            'mt-3 text-xs px-3 py-2 rounded-md border',
            testResult.success ? 'bg-green-900/30 border-green-700/50 text-green-400' : 'bg-red-900/30 border-red-700/50 text-red-400'
          )}>
            <span className='inline-flex items-center gap-1.5'>
              {testResult.success ? <CheckCircle size={12} /> : <XCircle size={12} />}
              {testResult.success
                ? `Connected to ${selectedPlant} successfully.`
                : testResult.error ?? 'Connection test failed.'}
            </span>
          </div>
        )}

        {/* Fetch result */}
        {fetchResult && (
          <div className={`mt-3 text-xs px-3 py-2 rounded-md border ${
            fetchResult.event_count && fetchResult.event_count > 0
              ? 'bg-green-900/20 border-green-700/50 text-green-300'
              : 'bg-amber-900/20 border-amber-700/50 text-amber-300'
          }`}>
            {fetchResult.event_count !== undefined && fetchResult.event_count > 0 ? (
              <span className='inline-flex items-center gap-1.5'>
                <CheckCircle size={12} />
                Stored {fetchResult.event_count} events from {selectedPlant}.
              </span>
            ) : fetchResult.event_count === 0 ? (
              <div className='space-y-1'>
                <span className='inline-flex items-center gap-1.5'>
                  <AlertTriangle size={12} />
                  <strong>No new log events were returned for {selectedPlant}.</strong>
                </span>
                <p className='text-gray-400 mt-1'>
                  RDS log events are collected through the FleetManager SSH journal sync and are also visible on the{' '}
                  <a href='/logs' className='text-blue-400 underline hover:text-blue-300'>Logs page</a>{' '}
                  and{' '}
                  <a href='/amr-logs' className='text-blue-400 underline hover:text-blue-300'>AMR Logs page</a>.
                </p>
              </div>
            ) : (
              <span className='inline-flex items-center gap-1.5'>
                <AlertTriangle size={12} />
                {fetchResult.message}
              </span>
            )}
          </div>
        )}

        {/* Discovered sources */}
        {discoveredSources !== null && (
          <div className='mt-3 text-xs'>
            {discoveredSources.length === 0 ? (
              <div className='px-3 py-2 rounded-md bg-yellow-900/20 border border-yellow-700/50 text-yellow-400'>
                No log sources found for this plant.
              </div>
            ) : (
              <div className='flex flex-wrap gap-2'>
                {discoveredSources.map(s => (
                  <span key={s.name} className='inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-md bg-gray-800 border border-gray-700 text-gray-300'>
                    <span className={clsx('w-1.5 h-1.5 rounded-full', s.type === 'api' ? 'bg-blue-400' : 'bg-orange-400')} />
                    <span className='font-medium'>{s.name}</span>
                    <span className='text-gray-500'>— {s.description}</span>
                  </span>
                ))}
              </div>
            )}
          </div>
        )}
      </div>
      {showCredentials && <div className='fixed inset-0 z-50 bg-black/70 flex items-center justify-center p-4'><div className='w-full max-w-md rounded-xl border border-gray-700 bg-gray-900 shadow-2xl'><div className='p-5 border-b border-gray-800'><h2 className='font-semibold text-white'>Configure {selectedPlant} RDS Login</h2><p className='text-xs text-gray-400 mt-1'>The password is encrypted before it is stored. Saving automatically runs Test Connection.</p></div><div className='p-5 space-y-4'><label className='block'><span className='block text-xs text-gray-400 mb-1'>Username</span><input className='w-full bg-gray-950 border border-gray-700 rounded-md px-3 py-2 text-sm text-white' value={rdsUsername} onChange={e => setRdsUsername(e.target.value)}/></label><label className='block'><span className='block text-xs text-gray-400 mb-1'>Password</span><input autoFocus type='password' autoComplete='new-password' className='w-full bg-gray-950 border border-gray-700 rounded-md px-3 py-2 text-sm text-white' value={rdsPassword} onChange={e => setRdsPassword(e.target.value)}/></label>{credentialError && <div className='text-xs text-red-300 border border-red-800 bg-red-950/40 rounded-md p-2'>{credentialError}</div>}</div><div className='p-5 border-t border-gray-800 flex justify-end gap-2'><button onClick={() => { setShowCredentials(false); setRdsPassword('') }} className='px-4 py-2 text-sm rounded-md bg-gray-800 text-gray-300'>Cancel</button><button onClick={handleSaveCredentials} disabled={!rdsPassword || savingCredentials} className='px-4 py-2 text-sm rounded-md bg-blue-600 text-white disabled:opacity-50'>{savingCredentials ? 'Saving...' : 'Save & Test'}</button></div></div></div>}

      {(agentFinding || agentError) && (
        <section className='flex-shrink-0 mx-6 my-3 bg-gray-800 border border-cyan-900 rounded-lg p-4 space-y-3 max-h-[42vh] overflow-y-auto'>
          <div className='flex items-start justify-between gap-3'>
            <div>
              <h2 className='text-sm font-semibold text-white flex items-center gap-2'><BrainCircuit size={14} className='text-cyan-400' /> RDS Agent Findings</h2>
              <p className='text-xs text-gray-400 mt-1'>Advisory analysis of the current plant and visible filters. No remediation was executed.</p>
            </div>
            {agentFinding && <div className='text-right'><span className='text-xs capitalize px-2 py-1 rounded border border-cyan-800 bg-cyan-950/40 text-cyan-200'>{agentFinding.confidence} confidence</span><div className='text-[10px] text-gray-500 mt-2'>via {agentFinding.via}</div></div>}
          </div>
          {agentError && <div className='text-sm text-red-300 bg-red-950/30 border border-red-900 rounded-md p-3'>{agentError}</div>}
          {agentFinding && <>
            <div className='grid grid-cols-1 md:grid-cols-2 gap-3'>
              <div className='bg-gray-900 border border-gray-700 rounded-md p-3'><div className='text-[11px] uppercase tracking-wide text-cyan-300 font-semibold mb-1'>What it means</div><p className='text-sm text-gray-200'>{agentFinding.plain_english}</p></div>
              <div className='bg-gray-900 border border-gray-700 rounded-md p-3'><div className='text-[11px] uppercase tracking-wide text-cyan-300 font-semibold mb-1'>Likely cause</div><p className='text-sm text-gray-200'>{agentFinding.likely_cause}</p></div>
            </div>
            <div><div className='text-[11px] uppercase tracking-wide text-gray-400 font-semibold mb-1'>Evidence used</div><ul className='list-disc list-inside space-y-1 text-xs text-gray-300'>{agentFinding.evidence.map((item, index) => <li key={index}>{item}</li>)}</ul></div>
            <div><div className='text-[11px] uppercase tracking-wide text-green-300 font-semibold mb-1'>Suggested remediation</div><ol className='list-decimal list-inside space-y-1 text-sm text-gray-200'>{agentFinding.remediation_steps.map((item, index) => <li key={index}>{item}</li>)}</ol></div>
            {agentFinding.caveats.length > 0 && <div className='bg-yellow-950/20 border border-yellow-900 rounded-md p-3'><div className='text-[11px] uppercase tracking-wide text-yellow-300 font-semibold mb-1'>Verify before action</div><ul className='list-disc list-inside space-y-1 text-xs text-yellow-100/80'>{agentFinding.caveats.map((item, index) => <li key={index}>{item}</li>)}</ul></div>}
          </>}
        </section>
      )}

      {/* ── Status cards ─────────────────────────────────────────── */}
      {selectedPlant && connStatus && (
        <div className='flex-shrink-0 px-6 py-3 bg-gray-900 border-b border-gray-700'>
          <div className='grid grid-cols-5 gap-3'>
            <StatusCard
              icon={connStatus.reachable ? Wifi : WifiOff}
              label='Connection'
              value={connStatus.reachable ? 'Online' : 'Offline'}
              sub={connStatus.authenticated ? 'Authenticated' : 'Not authenticated'}
              color={connStatus.reachable ? 'bg-green-900/30' : 'bg-red-900/30'}
            />
            <StatusCard
              icon={CheckCircle}
              label='Last Pull'
              value={connStatus.last_successful_pull
                ? format(new Date(connStatus.last_successful_pull), 'MMM d, HH:mm')
                : 'Never'}
            />
            <StatusCard
              icon={Zap}
              label='Logs Pulled'
              value={connStatus.logs_pulled.toLocaleString()}
            />
            <StatusCard
              icon={AlertTriangle}
              label='Last Error'
              value={connStatus.last_error ? 'Error' : 'None'}
              sub={connStatus.last_error ?? undefined}
              color={connStatus.last_error ? 'bg-red-900/30' : 'bg-green-900/30'}
            />
            <StatusCard
              icon={Search}
              label='Sources Found'
              value={(connStatus.available_sources ?? []).length.toString()}
              sub={(connStatus.available_sources ?? []).join(', ') || 'None detected'}
            />
          </div>
        </div>
      )}

      {/* ── Filters ──────────────────────────────────────────────── */}
      <div className='flex-shrink-0 px-6 py-3 bg-gray-850 border-b border-gray-700'>
        <div className='flex flex-wrap items-center gap-2'>
          {/* Date shortcuts */}
          <div className='flex items-center gap-1'>
            {DATE_SHORTCUTS.map(s => (
              <button key={s.label} onClick={() => {
                const d = s.fn()
                setFromDate(d.from)
                setToDate(d.to)
              }} className='text-xs px-2 py-1 rounded border border-gray-600 hover:bg-gray-700 text-gray-400 hover:text-gray-200 transition-colors'>
                {s.label}
              </button>
            ))}
          </div>

          <div className='h-4 w-px bg-gray-700' />

          <input
            type='datetime-local'
            value={fromDate}
            onChange={e => setFromDate(e.target.value)}
            className={inputCls}
          />
          <span className='text-xs text-gray-500'>to</span>
          <input
            type='datetime-local'
            value={toDate}
            onChange={e => setToDate(e.target.value)}
            className={inputCls}
          />

          <div className='h-4 w-px bg-gray-700' />

          <input
            type='text'
            placeholder='Robot...'
            value={filterRobot}
            onChange={e => setFilterRobot(e.target.value)}
            className={inputCls + ' w-28'}
          />
          <input
            type='text'
            placeholder='User...'
            value={filterUser}
            onChange={e => setFilterUser(e.target.value)}
            className={inputCls + ' w-28'}
          />

          <select value={filterCategory} onChange={e => setFilterCategory(e.target.value)} className={inputCls}>
            <option value=''>All categories</option>
            {CATEGORIES.map(c => <option key={c} value={c}>{c}</option>)}
          </select>

          <select value={filterSeverity} onChange={e => setFilterSeverity(e.target.value)} className={inputCls}>
            <option value=''>All severities</option>
            {SEVERITIES.map(s => <option key={s} value={s}>{s}</option>)}
          </select>

          <select value={filterExecEvidence === '' ? '' : String(filterExecEvidence)} onChange={e => setFilterExecEvidence(e.target.value === '' ? '' : e.target.value === 'true')} className={inputCls}>
            <option value=''>Evidence</option>
            <option value='true'>Evidence only</option>
            <option value='false'>Non-evidence</option>
          </select>

          <div className='flex-1' />

          <div className='relative'>
            <button
              onClick={() => setShowExportMenu(m => !m)}
              className='inline-flex items-center gap-1.5 text-xs px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-200 rounded-md transition-colors'
            >
              <Download size={12} />
              Export
              <ChevronDown size={10} />
            </button>
            {showExportMenu && (
              <div className='absolute right-0 top-full mt-1 bg-gray-800 border border-gray-600 rounded-md shadow-lg z-10 min-w-32'>
                <button onClick={exportCSV} className='w-full text-left px-3 py-2 text-xs text-gray-300 hover:bg-gray-700 rounded-t-md'>Export CSV</button>
                <button onClick={exportJSON} className='w-full text-left px-3 py-2 text-xs text-gray-300 hover:bg-gray-700 rounded-b-md'>Export JSON</button>
              </div>
            )}
          </div>
        </div>

        {/* Keyword search */}
        <div className='mt-2 flex items-center gap-2'>
          <div className='relative flex-1 max-w-sm'>
            <Search size={13} className='absolute left-2.5 top-1/2 -translate-y-1/2 text-gray-500' />
            <input
              type='text'
              placeholder='Search messages...'
              value={keyword}
              onChange={e => setKeyword(e.target.value)}
              className='w-full text-xs bg-gray-800 border border-gray-600 text-gray-200 rounded pl-8 pr-2 py-1.5 focus:outline-none focus:border-blue-500'
            />
          </div>
          <span className='text-xs text-gray-500'>{entries.length.toLocaleString()} entries</span>
        </div>
      </div>

      {/* ── Logs table ───────────────────────────────────────────── */}
      <div className='flex-1 overflow-auto'>
        {!selectedPlant ? (
          <div className='flex flex-col items-center justify-center h-full text-gray-500'>
            <Radio size={32} className='mb-3 opacity-40' />
            <p className='text-sm'>Select a plant above to view RDS logs.</p>
          </div>
        ) : isLoading ? (
          <div className='flex items-center justify-center h-full text-gray-500'>
            <RefreshCw size={24} className='animate-spin' />
          </div>
        ) : entries.length === 0 ? (
          <div className='flex flex-col items-center justify-center h-full text-gray-500'>
            <AlertTriangle size={32} className='mb-3 opacity-40' />
            <p className='text-sm'>No logs found for this plant and filter set.</p>
            <p className='text-xs text-gray-600 mt-1'>Click "Pull Logs" to queue the FleetManager SSH collector for this plant.</p>
          </div>
        ) : (
          <table className='w-full text-xs'>
            <thead className='bg-gray-800 border-b border-gray-700 sticky top-0 z-10'>
              <tr>
                <th className='px-3 py-2 text-left text-gray-500 font-medium'>Time</th>
                <th className='px-3 py-2 text-left text-gray-500 font-medium'>Robot</th>
                <th className='px-3 py-2 text-left text-gray-500 font-medium'>User</th>
                <th className='px-3 py-2 text-left text-gray-500 font-medium'>Action</th>
                <th className='px-3 py-2 text-left text-gray-500 font-medium'>Category</th>
                <th className='px-3 py-2 text-left text-gray-500 font-medium'>Severity</th>
                <th className='px-3 py-2 text-left text-gray-500 font-medium'>Message</th>
                <th className='px-3 py-2 text-center text-gray-500 font-medium'>Evidence</th>
                <th className='px-3 py-2 text-center text-gray-500 font-medium w-8'></th>
              </tr>
            </thead>
            <tbody className='divide-y divide-gray-800'>
              {entries.map(entry => (
                <tr key={entry.id} className='hover:bg-gray-800/50 transition-colors'>
                  <td className='px-3 py-2 text-gray-400 whitespace-nowrap font-mono'>
                    {format(new Date(entry.timestamp), 'MMM d HH:mm:ss')}
                  </td>
                  <td className='px-3 py-2 text-gray-300 font-mono whitespace-nowrap'>{entry.robot || '—'}</td>
                  <td className='px-3 py-2 text-gray-300 whitespace-nowrap'>{entry.user || '—'}</td>
                  <td className='px-3 py-2 text-gray-300 whitespace-nowrap'>{entry.action || '—'}</td>
                  <td className='px-3 py-2 whitespace-nowrap text-gray-400'>{entry.category || '—'}</td>
                  <td className='px-3 py-2 whitespace-nowrap'>
                    <span className={clsx('px-1.5 py-0.5 rounded text-[10px] font-medium', SEVERITY_CLASS[entry.severity] ?? 'bg-gray-700 text-gray-300')}>
                      {entry.severity}
                    </span>
                  </td>
                  <td className='px-3 py-2 text-gray-400 max-w-xs truncate' title={entry.message}>{entry.message}</td>
                  <td className='px-3 py-2 text-center'>
                    {entry.execution_evidence
                      ? <span className='text-green-400 text-xs'>&#10003;</span>
                      : <span className='text-gray-600 text-xs'>—</span>}
                  </td>
                  <td className='px-3 py-2 text-center'>
                    <button
                      onClick={() => setDetailEntry(entry)}
                      className='p-1 rounded hover:bg-gray-700 text-gray-500 hover:text-gray-300 transition-colors'
                      title='View details'
                    >
                      <Eye size={13} />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* ── Detail drawer ────────────────────────────────────────── */}
      {detailEntry && (
        <div className='fixed inset-0 z-50 flex'>
          <div className='flex-1 bg-black/50' onClick={() => setDetailEntry(null)} />
          <div className='w-[520px] bg-gray-900 h-full overflow-y-auto shadow-xl border-l border-gray-700'>
            <div className='px-6 py-4 border-b border-gray-700 flex items-center justify-between'>
              <h2 className='text-sm font-bold text-white'>Log Entry Detail</h2>
              <button onClick={() => setDetailEntry(null)} className='text-gray-500 hover:text-white text-lg transition-colors'>&times;</button>
            </div>
            <div className='px-6 py-4 space-y-3 text-xs'>
              {[
                ['ID', String(detailEntry.id)],
                ['Plant', detailEntry.plant],
                ['Source System', detailEntry.source_system],
                ['Timestamp', format(new Date(detailEntry.timestamp), 'yyyy-MM-dd HH:mm:ss')],
                ['Robot', detailEntry.robot || '—'],
                ['User', detailEntry.user || '—'],
                ['Action', detailEntry.action || '—'],
                ['Category', detailEntry.category || '—'],
                ['Severity', detailEntry.severity],
                ['Confidence', detailEntry.confidence],
                ['Execution Evidence', detailEntry.execution_evidence ? 'Yes' : 'No'],
                ['Message', detailEntry.message],
              ].map(([k, v]) => (
                <div key={k} className='grid grid-cols-[100px_1fr] gap-2'>
                  <div className='text-gray-500 font-medium'>{k}</div>
                  <div className={clsx('break-all', k === 'Severity' ? SEVERITY_CLASS[v] : 'text-gray-300', k === 'Severity' && 'inline-block px-1.5 py-0.5 rounded text-[10px] font-medium')}>
                    {v}
                  </div>
                </div>
              ))}
              <div>
                <div className='text-gray-500 font-medium mb-1'>Raw Log</div>
                <pre className='bg-gray-800 rounded-md p-3 text-gray-400 whitespace-pre-wrap break-all text-[11px] leading-relaxed font-mono'>
                  {detailEntry.raw_log}
                </pre>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
