import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { format, subDays, startOfDay, endOfDay } from 'date-fns'
import { deepSync, explainLogErrors, getIncidentSummary, getLogs, getServers } from '../api/client'
import type { LogFilters } from '../api/client'
import type { AgentLogExplanation, IncidentSummary } from '../types'
import LogsTable from '../components/logs/LogsTable'
import { EVENT_TYPES, SEVERITIES, SOURCE_OPTIONS } from '../eventTaxonomy'

const DATE_SHORTCUTS = [
  { label: 'Today', fn: () => ({ from: format(startOfDay(new Date()), "yyyy-MM-dd'T'HH:mm"), to: '' }) },
  { label: 'Yesterday', fn: () => ({ from: format(startOfDay(subDays(new Date(), 1)), "yyyy-MM-dd'T'HH:mm"), to: format(endOfDay(subDays(new Date(), 1)), "yyyy-MM-dd'T'HH:mm") }) },
  { label: 'Last 7 days', fn: () => ({ from: format(subDays(new Date(), 7), "yyyy-MM-dd'T'HH:mm"), to: '' }) },
  { label: 'All time', fn: () => ({ from: '', to: '' }) },
]

const inputCls = 'text-xs bg-gray-900 border border-gray-600 text-gray-200 rounded-md px-2 py-1.5 focus:outline-none focus:border-blue-500'

const QUICK_FILTERS = [
  { label: 'Out of Memory', event_type: '', q: 'oom out memory killed qemu kvm' },
  { label: 'VM Killed', event_type: 'vm_killed_by_oom', q: 'killed process' },
  { label: 'Server Reboot', event_type: 'ubuntu_server_reboot', q: 'reboot' },
  { label: 'Server Shutdown', event_type: 'ubuntu_server_shutdown', q: 'shutdown' },
  { label: 'Backup', event_type: 'backup_job', q: 'backup vzdump' },
  { label: 'HA', event_type: 'ha_action', q: 'ha-manager pve-ha' },
  { label: 'Robot Offline', event_type: 'robot_offline', q: 'UnconnectedState disconnect' },
  { label: 'RDS Core', event_type: 'rds_core_issue', q: 'rdscore RDS API database timeout failed' },
  { label: 'RDS Map Update', event_type: 'rds_map_update', q: 'map smap scene push upload update deploy' },
  { label: 'RDS Model / MD5', event_type: 'rds_model_update', q: 'model md5 checksum robot.cp models modified updated' },
  { label: 'Charge Command', event_type: 'roboshop_charge_command', q: 'charge charging charger dock docking command Roboshop' },
  { label: 'chargeDI', event_type: 'roboshop_chargedi_change', q: 'chargeDI charge_di chargingDI trigger model applied source IP' },
  { label: 'WarLink', event_type: 'warlink_failure', q: 'WarLink SendUnitDataTransaction WriteTag not connected returned 500' },
  { label: 'App Crash', event_type: 'crash', q: 'segfault fatal core dumped' },
  { label: 'SSH Login', event_type: 'ssh_login_activity', q: 'sshd accepted failed password' },
  { label: 'Network Failure', event_type: 'network_dhcp_failure', q: 'dhcp link down network unreachable' },
  { label: 'Disk Error', event_type: 'disk_smart_issue', q: 'smart disk error' },
]

const AMR_RDS_FILTERS = [
  { label: 'Battery Error', event_type: 'battery_error', q: 'battery low fault error voltage soc power low' },
  { label: 'Battery Status', event_type: 'battery_status', q: 'battery_level batteryLevel GetBatteryLevel robot_status_battery_req soc voltage' },
  { label: 'Charge Command', event_type: 'amr_charge_command', q: 'robot_other_setchargingrelay_req setchargingrelay chargingrelay charge_req goCharge go_charge' },
  { label: 'Dock Command', event_type: 'amr_dock_command', q: 'dock_req docking dock command go dock return dock charger dock' },
  { label: 'GoTarget Station', event_type: 'amr_gotarget_station', q: 'robot_task_gotarget_req gotarget PP65 PP66 station charger' },
  { label: 'Settings Reset', event_type: 'rds_settings_reset', q: 'config reset model reset settings reset reloadRobodMakeIni empty config restore recover' },
  { label: 'Settings Defaulted', event_type: 'rds_settings_defaulted', q: 'default factory active:false echoid features active:false' },
  { label: 'RDS Upgrade', event_type: 'rds_upgrade_reset', q: 'robot_core_upgrade_robot_req upgrade.zip upgradeStatus startup.sh stop startup.sh start Robod upgrade RDS upgrade' },
  { label: 'RDS Core Activation', event_type: 'rds_core_activation_issue', q: 'core is not activated license inactive activation failed active:false echoid' },
  { label: 'RDS Scene Error', event_type: 'rds_scene_map_error', q: 'scene.zip error rds.scene map upload scene cannot be uploaded map md5 model_md5' },
  { label: 'Admin Evidence Search', event_type: 'admin_evidence_search', q: 'grep journalctl COMMAND' },
  { label: 'Template / Code Reference', event_type: 'template_code_reference', q: 'seer-task rbklib.py project-templates static JavaScript config block template' },
]

const AMR_RDS_TYPES = new Set(AMR_RDS_FILTERS.map(f => f.event_type))

export default function LogsPage() {
  const [searchParams] = useSearchParams()
  const [keyword, setKeyword] = useState(searchParams.get('q') ?? '')
  const [fromDate, setFromDate] = useState('')
  const [toDate, setToDate] = useState('')
  const [incident, setIncident] = useState<IncidentSummary | null>(null)
  const [investigating, setInvestigating] = useState(false)
  const [deepSyncing, setDeepSyncing] = useState(false)
  const [agentExplaining, setAgentExplaining] = useState(false)
  const [agentExplanation, setAgentExplanation] = useState<AgentLogExplanation | null>(null)
  const [agentError, setAgentError] = useState('')
  const [filters, setFilters] = useState<LogFilters>(() => ({
    limit: 500,
    q: searchParams.get('q') ?? undefined,
    source: searchParams.get('source') ?? undefined,
    severity: searchParams.get('severity') ?? undefined,
    proxmox_host: searchParams.get('proxmox_host') ?? undefined,
    vmid: searchParams.get('vmid') ?? undefined,
    event_type: searchParams.get('event_type') ?? undefined,
    event_types: searchParams.get('event_types') ?? undefined,
    server_id: searchParams.get('server_id') ? Number(searchParams.get('server_id')) : undefined,
  }))

  useEffect(() => {
    const timeout = window.setTimeout(() => {
      setFilters(f => ({ ...f, q: keyword.trim() || undefined }))
    }, 250)
    return () => window.clearTimeout(timeout)
  }, [keyword])

  useEffect(() => {
    setFilters(f => ({
      ...f,
      from: fromDate ? new Date(fromDate).toISOString() : undefined,
      to: toDate ? new Date(toDate).toISOString() : undefined,
    }))
  }, [fromDate, toDate])

  const { data: servers = [] } = useQuery({ queryKey: ['servers'], queryFn: getServers })
  const { data: events = [], isLoading } = useQuery({
    queryKey: ['logs', filters],
    queryFn: () => getLogs(filters),
    refetchInterval: 30_000,
  })

  const sourceOptions = useMemo(() => {
    const seen = new Set<string>(SOURCE_OPTIONS.map(s => s.value))
    const discovered = events
      .map(ev => ev.source)
      .filter(source => source && !seen.has(source))
      .map(source => ({ value: source, label: source }))
    return [...SOURCE_OPTIONS, ...discovered]
  }, [events])

  const proxmoxHosts = useMemo(() => [...new Set(servers.map(s => s.proxmox_host).filter(Boolean))], [servers])
  const vmids = useMemo(() => {
    const values = servers.flatMap(s => (s.vmid ?? '').split(/[\s,;]+/).map(v => v.trim()).filter(Boolean))
    return [...new Set(values)].sort((a, b) => Number(a) - Number(b))
  }, [servers])
  const amrEvents = useMemo(() => events.filter(ev => AMR_RDS_TYPES.has(ev.event_type)), [events])
  const amrSummary = useMemo(() => {
    const has = (type: string) => amrEvents.some(ev => ev.event_type === type)
    const targetIds = [...new Set(amrEvents.flatMap(ev => ev.target_ids ?? []))]
    const onlyAdminOrTemplate = amrEvents.length > 0 && amrEvents.every(ev =>
      ev.event_type === 'admin_evidence_search' ||
      ev.event_type === 'template_code_reference' ||
      ev.event_type === 'not_execution_evidence')
    let conclusion = 'No AMR/RDS investigation evidence is visible in the current filters.'
    if (onlyAdminOrTemplate) {
      conclusion = 'No actual AMR charge/dock command was found. The only keyword hits are administrator evidence searches or template/code references.'
    } else if (has('rds_upgrade_reset') && (has('rds_settings_reset') || has('rds_settings_defaulted'))) {
      conclusion = 'RDS/Robod upgrade/reset activity was detected near reset/default indicators. This is more likely to explain settings returning to default than a normal charge command.'
    } else if (has('amr_gotarget_station')) {
      conclusion = `A go-target command was issued${targetIds.length ? ` to ${targetIds.join(', ')}` : ''}. Confirm whether the target is configured as a charger/station point before calling it a charge command.`
    } else if (has('amr_charge_command') || has('amr_dock_command')) {
      conclusion = 'Charge/dock command evidence was found. Use the confidence badge to separate executed runtime commands from supporting evidence.'
    } else if (amrEvents.length > 0) {
      conclusion = 'AMR/RDS evidence was found. Review confidence badges before treating keyword matches as real robot execution.'
    }
    return { has, targetIds, conclusion }
  }, [amrEvents])

  const set = (k: keyof LogFilters, v: string | number | undefined) =>
    setFilters(f => ({ ...f, [k]: v || undefined }))

  function applyShortcut(s: typeof DATE_SHORTCUTS[0]) {
    const r = s.fn()
    setFromDate(r.from)
    setToDate(r.to)
  }

  async function investigate() {
    if (!filters.server_id) return
    setInvestigating(true)
    try {
      const summary = await getIncidentSummary({
        server_id: filters.server_id,
        from: fromDate ? new Date(fromDate).toISOString() : undefined,
        to: toDate ? new Date(toDate).toISOString() : undefined,
      })
      setIncident(summary)
    } finally {
      setInvestigating(false)
    }
  }

  async function runDeepSync() {
    if (!filters.server_id) return
    const since = fromDate ? new Date(fromDate).toISOString() : new Date(Date.now() - 7 * 24 * 60 * 60 * 1000).toISOString()
    setDeepSyncing(true)
    try {
      await deepSync(filters.server_id, since)
    } finally {
      setDeepSyncing(false)
    }
  }

  async function explainVisibleErrors() {
    if (events.length === 0) return
    setAgentExplaining(true)
    setAgentError('')
    try {
      const prioritized = [...events].sort((a, b) => {
        const rank = (severity: string) => ({ critical: 4, high: 3, medium: 2, low: 1, info: 0 }[severity] ?? 0)
        return rank(b.severity) - rank(a.severity)
      }).slice(0, 100)
      const selectedServer = servers.find(server => server.id === filters.server_id)?.name ?? 'all visible servers'
      const context = `Server: ${selectedServer}; query: ${keyword || 'none'}; event type: ${filters.event_type || 'all'}; source: ${filters.source || 'all'}; date range: ${fromDate || 'all'} to ${toDate || 'now'}`
      setAgentExplanation(await explainLogErrors(prioritized, context))
    } catch (error) {
      setAgentError(error instanceof Error ? error.message : 'Agent could not explain the visible evidence.')
    } finally {
      setAgentExplaining(false)
    }
  }

  return (
    <div className="flex flex-col h-full bg-gray-900 text-gray-100">
      <div className="px-6 py-4 bg-gray-900 border-b border-gray-700">
        <h1 className="text-base font-semibold text-white">Logs</h1>
        <p className="text-xs text-gray-400 mt-0.5">Review robot, server, host, VM, power, network, and unknown events</p>
      </div>

      <div className="flex-1 overflow-y-auto p-5 space-y-4">
        <div className="bg-gray-800 border border-gray-700 rounded-lg p-4 space-y-3">
          <div className="grid grid-cols-1 md:grid-cols-7 gap-3">
            <select className={inputCls} value={filters.server_id ?? ''} onChange={e => set('server_id', e.target.value ? Number(e.target.value) : undefined)}>
              <option value="">All servers</option>
              {servers.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
            </select>
            <select className={inputCls} value={filters.proxmox_host ?? ''} onChange={e => set('proxmox_host', e.target.value)}>
              <option value="">All PVE hosts</option>
              {proxmoxHosts.map(host => <option key={host} value={host}>{host}</option>)}
            </select>
            <select className={inputCls} value={filters.vmid ?? ''} onChange={e => set('vmid', e.target.value)}>
              <option value="">All VMIDs</option>
              {vmids.map(vmid => <option key={vmid} value={vmid}>{vmid}</option>)}
            </select>
            <select className={inputCls} value={filters.source ?? ''} onChange={e => set('source', e.target.value)}>
              {sourceOptions.map(s => <option key={s.value} value={s.value}>{s.label}</option>)}
            </select>
            <select className={inputCls} value={filters.event_type ?? ''} onChange={e => set('event_type', e.target.value)}>
              {EVENT_TYPES.map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
            </select>
            <select className={inputCls} value={filters.severity ?? ''} onChange={e => set('severity', e.target.value)}>
              {SEVERITIES.map(s => <option key={s.value} value={s.value}>{s.label}</option>)}
            </select>
            <input
              type="search"
              placeholder="Search logs, source, server..."
              className={`${inputCls} placeholder-gray-500`}
              value={keyword}
              onChange={e => setKeyword(e.target.value)}
            />
          </div>

          <div className="flex flex-wrap items-center gap-3">
            <span className="text-xs text-gray-400 font-medium">Date range</span>
            <input type="datetime-local" className={inputCls} value={fromDate} onChange={e => setFromDate(e.target.value)} />
            <span className="text-xs text-gray-500">to</span>
            <input type="datetime-local" className={inputCls} value={toDate} onChange={e => setToDate(e.target.value)} />
            <div className="flex gap-1.5 flex-wrap">
              {DATE_SHORTCUTS.map(s => (
                <button key={s.label} onClick={() => applyShortcut(s)}
                  className="text-xs px-2.5 py-1 rounded-md border border-gray-600 text-gray-400 hover:bg-gray-700 hover:text-gray-200 transition-colors">
                  {s.label}
                </button>
              ))}
            </div>
            {(fromDate || toDate || keyword || filters.source || filters.event_type || filters.event_types || filters.severity || filters.server_id) && (
              <button onClick={() => {
                setFromDate('')
                setToDate('')
                setKeyword('')
                setIncident(null)
                setFilters({ limit: 500 })
              }} className="text-xs text-red-400 hover:text-red-300">
                Clear filters
              </button>
            )}
          </div>

          <div className="flex flex-wrap gap-2">
            <span className="text-xs text-gray-400 self-center mr-1">Quick</span>
            {QUICK_FILTERS.map(f => (
              <button key={f.label} onClick={() => {
                setKeyword(f.q)
                set('event_type', f.event_type || undefined)
              }} className="text-xs px-2.5 py-1 rounded-md border border-gray-600 text-gray-400 hover:bg-gray-700 hover:text-gray-200">
                {f.label}
              </button>
            ))}
          </div>

          <div className="border-t border-gray-700 pt-3 space-y-2">
            <div className="flex flex-wrap gap-2">
              <span className="text-xs text-cyan-300 self-center mr-1 font-semibold">AMR/RDS Investigation</span>
              {AMR_RDS_FILTERS.map(f => (
                <button key={f.label} onClick={() => {
                  setKeyword(f.q)
                  set('event_type', f.event_type)
                }} className="text-xs px-2.5 py-1 rounded-md border border-cyan-800 text-cyan-200 hover:bg-cyan-950/50">
                  {f.label}
                </button>
              ))}
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2 border-t border-gray-700 pt-3">
            <button onClick={explainVisibleErrors} disabled={events.length === 0 || agentExplaining}
              className="text-xs px-3 py-2 rounded-md bg-cyan-600 hover:bg-cyan-500 text-white disabled:opacity-40">
              {agentExplaining ? 'Agent analyzing...' : 'Agent: Explain & Remediate'}
            </button>
            <button onClick={investigate} disabled={!filters.server_id || investigating}
              className="text-xs px-3 py-2 rounded-md bg-blue-600 hover:bg-blue-500 text-white disabled:opacity-40">
              {investigating ? 'Investigating...' : 'Investigate selected server'}
            </button>
            <button onClick={runDeepSync} disabled={!filters.server_id || deepSyncing}
              className="text-xs px-3 py-2 rounded-md bg-indigo-600 hover:bg-indigo-500 text-white disabled:opacity-40">
              {deepSyncing ? 'Deep syncing...' : 'Deep Sync selected server'}
            </button>
            <span className="text-xs text-gray-500">Agent explains the current filtered evidence; it recommends checks but never executes remediation.</span>
          </div>
        </div>

        {(agentExplanation || agentError) && (
          <section className="bg-gray-800 border border-cyan-900 rounded-lg p-4 space-y-4">
            <div className="flex items-start justify-between gap-3">
              <div>
                <h2 className="text-sm font-semibold text-white">Agent Error Explanation</h2>
                <p className="text-xs text-gray-400 mt-1">Plain-English interpretation of the current filtered log evidence.</p>
              </div>
              {agentExplanation && (
                <div className="text-right">
                  <span className="text-xs rounded-md border border-cyan-800 bg-cyan-950/40 text-cyan-200 px-2 py-1 capitalize">{agentExplanation.confidence} confidence</span>
                  <div className="text-[10px] text-gray-500 mt-2">via {agentExplanation.via}</div>
                </div>
              )}
            </div>
            {agentError && <div className="text-sm text-red-300 bg-red-950/30 border border-red-900 rounded-md p-3">{agentError}</div>}
            {agentExplanation && (
              <>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                  <div className="bg-gray-900 border border-gray-700 rounded-md p-3">
                    <div className="text-[11px] uppercase tracking-wide text-cyan-300 font-semibold mb-1">What it means</div>
                    <p className="text-sm text-gray-200">{agentExplanation.plain_english}</p>
                  </div>
                  <div className="bg-gray-900 border border-gray-700 rounded-md p-3">
                    <div className="text-[11px] uppercase tracking-wide text-cyan-300 font-semibold mb-1">Likely cause</div>
                    <p className="text-sm text-gray-200">{agentExplanation.likely_cause}</p>
                  </div>
                </div>
                <div>
                  <div className="text-[11px] uppercase tracking-wide text-gray-400 font-semibold mb-2">Evidence used</div>
                  <ul className="space-y-1 list-disc list-inside text-sm text-gray-300">{agentExplanation.evidence.map((item, index) => <li key={index}>{item}</li>)}</ul>
                </div>
                <div>
                  <div className="text-[11px] uppercase tracking-wide text-green-300 font-semibold mb-2">Suggested remediation</div>
                  <ol className="space-y-2 list-decimal list-inside text-sm text-gray-200">{agentExplanation.remediation_steps.map((step, index) => <li key={index}>{step}</li>)}</ol>
                </div>
                {agentExplanation.caveats.length > 0 && (
                  <div className="bg-yellow-950/20 border border-yellow-900 rounded-md p-3">
                    <div className="text-[11px] uppercase tracking-wide text-yellow-300 font-semibold mb-1">Verify before action</div>
                    <ul className="space-y-1 list-disc list-inside text-xs text-yellow-100/80">{agentExplanation.caveats.map((item, index) => <li key={index}>{item}</li>)}</ul>
                  </div>
                )}
              </>
            )}
          </section>
        )}

        {amrEvents.length > 0 && (
          <div className="bg-gray-800 border border-gray-700 rounded-lg p-4 space-y-3">
            <div className="flex items-start justify-between gap-3">
              <div>
                <h2 className="text-sm font-semibold text-white">AMR/RDS Evidence Analyzer</h2>
                <p className="text-xs text-gray-400 mt-1">Classifies evidence confidence on top of the existing pulled logs.</p>
              </div>
              <span className="text-xs rounded-md border border-cyan-800 bg-cyan-950/40 text-cyan-200 px-2 py-1">{amrEvents.length} evidence rows</span>
            </div>
            <div className="grid grid-cols-2 md:grid-cols-5 gap-2 text-xs">
              {[
                ['Battery error', amrSummary.has('battery_error')],
                ['Charge command', amrSummary.has('amr_charge_command')],
                ['Dock command', amrSummary.has('amr_dock_command')],
                ['GoTarget station', amrSummary.has('amr_gotarget_station')],
                ['RDS upgrade/reset', amrSummary.has('rds_upgrade_reset')],
                ['Settings reset/defaulted', amrSummary.has('rds_settings_reset') || amrSummary.has('rds_settings_defaulted')],
                ['Scene/map error', amrSummary.has('rds_scene_map_error')],
                ['Activation issue', amrSummary.has('rds_core_activation_issue')],
                ['Admin searches', amrSummary.has('admin_evidence_search')],
                ['Template/code only', amrSummary.has('template_code_reference')],
              ].map(([label, yes]) => (
                <div key={String(label)} className="bg-gray-900 border border-gray-700 rounded-md p-2">
                  <div className="text-gray-400">{label}</div>
                  <div className={yes ? 'text-green-300 font-semibold' : 'text-gray-500'}>{yes ? 'Yes' : 'No'}</div>
                </div>
              ))}
            </div>
            {amrSummary.targetIds.length > 0 && (
              <div className="text-xs text-gray-300">Target IDs found: <span className="text-white font-semibold">{amrSummary.targetIds.join(', ')}</span></div>
            )}
            <div className="bg-gray-900 border border-gray-700 rounded-md p-3 text-sm text-gray-100">{amrSummary.conclusion}</div>
            <div className="space-y-1 max-h-44 overflow-y-auto">
              {amrEvents.slice().sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()).slice(0, 30).map(ev => (
                <div key={ev.id} className="grid grid-cols-[8rem_12rem_1fr] gap-2 text-xs border-b border-gray-800 py-1">
                  <span className="text-gray-500">{new Date(ev.timestamp).toLocaleString()}</span>
                  <span className="text-cyan-200">{EVENT_TYPES.find(t => t.value === ev.event_type)?.label ?? ev.event_type}</span>
                  <span className="text-gray-300 truncate">{ev.plain_english ?? ev.message}</span>
                </div>
              ))}
            </div>
          </div>
        )}

        {incident && (
          <div className="bg-gray-800 border border-gray-700 rounded-lg p-4 space-y-3">
            <div className="flex items-start justify-between gap-3">
              <div>
                <h2 className="text-sm font-semibold text-white">Incident Summary</h2>
                <p className="text-xs text-gray-500 mt-1">{incident.server_name}{incident.vmid ? ` / VM ${incident.vmid}` : ''}{incident.proxmox_host ? ` on ${incident.proxmox_host}` : ''}</p>
              </div>
              <div className="text-xs text-gray-500 text-right">
                <div>Started: {incident.started_at ? new Date(incident.started_at).toLocaleString() : 'Not found'}</div>
                <div>Recovered: {incident.recovered_at ? new Date(incident.recovered_at).toLocaleString() : 'Not found'}</div>
              </div>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
              <div className="bg-gray-900 border border-gray-700 rounded-lg p-3">
                <p className="text-xs font-semibold text-gray-400 mb-1">What happened</p>
                <p className="text-sm text-gray-100">{incident.what_happened}</p>
              </div>
              <div className="bg-gray-900 border border-gray-700 rounded-lg p-3">
                <p className="text-xs font-semibold text-gray-400 mb-1">Likely root cause</p>
                <p className="text-sm text-gray-100">{incident.root_cause}</p>
              </div>
              <div className="bg-gray-900 border border-gray-700 rounded-lg p-3">
                <p className="text-xs font-semibold text-gray-400 mb-1">Recommended fix</p>
                <p className="text-sm text-gray-100">{incident.recommended_fix}</p>
              </div>
            </div>
            {incident.oom_analysis && (
              <div className="bg-red-950/20 border border-red-800/60 rounded-lg p-3">
                <div className="flex items-start justify-between gap-3 mb-3">
                  <div>
                    <p className="text-xs font-semibold text-red-300 uppercase tracking-wide">Memory culprit</p>
                    <p className="text-sm text-gray-100 mt-1">{incident.oom_analysis.explanation}</p>
                  </div>
                  <span className="text-xs rounded-full border border-red-700 bg-red-900/40 px-2 py-1 text-red-200">
                    {incident.oom_analysis.confidence} confidence
                  </span>
                </div>
                <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
                  <div className="bg-gray-950/60 border border-gray-700 rounded-md p-2">
                    <p className="text-xs text-gray-500">Killed VM</p>
                    <p className="text-sm font-semibold text-white">
                      {incident.oom_analysis.killed_vmid ? `VM ${incident.oom_analysis.killed_vmid}` : 'Not found'}
                    </p>
                    {incident.oom_analysis.killed_vm_name && <p className="text-xs text-gray-400 truncate">{incident.oom_analysis.killed_vm_name}</p>}
                  </div>
                  <div className="bg-gray-950/60 border border-gray-700 rounded-md p-2">
                    <p className="text-xs text-gray-500">Highest memory VM</p>
                    <p className="text-sm font-semibold text-white">
                      {incident.oom_analysis.top_vmid ? `VM ${incident.oom_analysis.top_vmid}` : 'Not found'}
                    </p>
                    {incident.oom_analysis.top_vm_name && <p className="text-xs text-gray-400 truncate">{incident.oom_analysis.top_vm_name}</p>}
                  </div>
                  <div className="bg-gray-950/60 border border-gray-700 rounded-md p-2">
                    <p className="text-xs text-gray-500">Memory evidence</p>
                    <p className="text-sm font-semibold text-white">
                      {incident.oom_analysis.killed_anon_gb ? `${incident.oom_analysis.killed_anon_gb.toFixed(2)} GB killed RSS` :
                        incident.oom_analysis.top_rss_gb ? `${incident.oom_analysis.top_rss_gb.toFixed(2)} GB live RSS` : 'Not found'}
                    </p>
                    {incident.oom_analysis.top_config_mb ? <p className="text-xs text-gray-400">{incident.oom_analysis.top_config_mb} MB configured</p> : null}
                  </div>
                  <div className="bg-gray-950/60 border border-gray-700 rounded-md p-2">
                    <p className="text-xs text-gray-500">Proxmox / process</p>
                    <p className="text-sm font-semibold text-white truncate">{incident.oom_analysis.proxmox_host || 'Unknown host'}</p>
                    <p className="text-xs text-gray-400 truncate">
                      {[incident.oom_analysis.killed_process, incident.oom_analysis.killed_pid ? `PID ${incident.oom_analysis.killed_pid}` : ''].filter(Boolean).join(' / ') || 'Process not found'}
                    </p>
                  </div>
                </div>
                <p className="text-xs text-gray-300 mt-3">{incident.oom_analysis.recommendation}</p>
              </div>
            )}
            <div>
              <p className="text-xs font-semibold text-gray-400 mb-2">Evidence</p>
              <div className="space-y-1.5">
                {incident.evidence.length === 0 && <p className="text-xs text-gray-500">No categorized evidence found in this window.</p>}
                {incident.evidence.map((ev, idx) => (
                  <div key={`${ev.timestamp}-${idx}`} className="grid grid-cols-12 gap-2 text-xs bg-gray-900 border border-gray-700 rounded-md px-3 py-2">
                    <span className="col-span-2 text-gray-500 font-mono">{new Date(ev.timestamp).toLocaleString()}</span>
                    <span className="col-span-2 text-blue-300">{ev.event_type}</span>
                    <span className="col-span-2 text-gray-400">{ev.source}</span>
                    <span className="col-span-6 text-gray-300 truncate">{ev.message}</span>
                  </div>
                ))}
              </div>
            </div>
          </div>
        )}

        <div className="flex items-center gap-3">
          <span className="text-xs text-gray-500">
            {isLoading ? 'Loading...' : `${events.length} events`}
            {fromDate && <span className="ml-2 text-blue-400">from {new Date(fromDate).toLocaleDateString()}</span>}
            {toDate && <span className="ml-1 text-blue-400">to {new Date(toDate).toLocaleDateString()}</span>}
          </span>
        </div>

        <LogsTable events={events} loading={isLoading} />
      </div>
    </div>
  )
}
