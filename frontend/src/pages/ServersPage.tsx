import React, { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { Plus, RefreshCw, Trash2, Pencil, Wifi, WifiOff, AlertCircle, HelpCircle } from 'lucide-react'
import { format, parseISO } from 'date-fns'
import { getServers, createServer, updateServer, deleteServer, syncServer, deepSync, syncAll } from '../api/client'
import type { Server, ServerRequest } from '../types'
import ServerForm from '../components/servers/ServerForm'

const statusIcon: Record<string, React.ReactNode> = {
  online:  <Wifi size={14} className="text-green-400" />,
  offline: <WifiOff size={14} className="text-gray-500" />,
  error:   <AlertCircle size={14} className="text-red-400" />,
  unknown: <HelpCircle size={14} className="text-gray-500" />,
}
const statusBadge: Record<string, string> = {
  online:  'bg-green-900/50 text-green-400 border border-green-700',
  offline: 'bg-gray-700 text-gray-400 border border-gray-600',
  error:   'bg-red-900/50 text-red-400 border border-red-700',
  unknown: 'bg-gray-700 text-gray-400 border border-gray-600',
}

function DeepSyncButton({ serverId, onDone }: { serverId: number; onDone: () => void }) {
  const [open, setOpen] = React.useState(false)
  const [since, setSince] = React.useState('')
  const [loading, setLoading] = React.useState(false)
  const [done, setDone] = React.useState(false)

  async function run() {
    if (!since) return
    setLoading(true)
    try { await deepSync(serverId, new Date(since).toISOString()); setDone(true); onDone() } catch {}
    setLoading(false); setTimeout(() => { setOpen(false); setDone(false) }, 2000)
  }

  return (
    <div className="relative">
      <button onClick={() => setOpen(v => !v)}
        className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-lg bg-indigo-800/50 hover:bg-indigo-700/60 text-indigo-300 border border-indigo-700 transition-colors"
        title="Pull logs from a specific date">
        📅 Deep Sync
      </button>
      {open && (
        <div className="absolute right-0 top-9 z-50 bg-gray-800 border border-gray-600 rounded-xl p-4 shadow-2xl w-72">
          <p className="text-xs text-gray-300 mb-2 font-semibold">Pull logs from date</p>
          <p className="text-xs text-gray-500 mb-3">Use this to recover historical logs (restarts, crashes) from before the normal sync window.</p>
          <input type="datetime-local" value={since} onChange={e => setSince(e.target.value)}
            className="w-full text-xs bg-gray-900 border border-gray-600 text-gray-200 rounded-lg px-2 py-1.5 mb-3 focus:outline-none focus:border-indigo-500" />
          <div className="flex gap-2 flex-wrap mb-2">
            {['2 days ago', '3 days ago', '7 days ago', '14 days ago'].map((label, i) => {
              const d = new Date(); d.setDate(d.getDate() - [2,3,7,14][i]); d.setHours(0,0,0,0)
              const v = d.toISOString().slice(0,16)
              return <button key={label} onClick={() => setSince(v)}
                className="text-xs px-2 py-1 rounded-full border border-gray-600 text-gray-400 hover:bg-gray-700">{label}</button>
            })}
          </div>
          <div className="flex gap-2">
            <button onClick={run} disabled={!since || loading}
              className="flex-1 text-xs py-2 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white disabled:opacity-50 transition-colors">
              {loading ? 'Pulling...' : done ? '✓ Done' : 'Pull Logs'}
            </button>
            <button onClick={() => setOpen(false)} className="text-xs px-3 py-2 rounded-lg bg-gray-700 text-gray-300">Cancel</button>
          </div>
        </div>
      )}
    </div>
  )
}

export default function ServersPage() {
  const qc = useQueryClient()
  const nav = useNavigate()
  const { data: servers = [] } = useQuery({ queryKey: ['servers'], queryFn: getServers })
  const serverAssets = servers.filter(s => (s.asset_type ?? 'server') !== 'endpoint')
  const [modal, setModal] = useState<'add' | 'edit' | null>(null)
  const [editing, setEditing] = useState<Server | null>(null)
  const [syncMessage, setSyncMessage] = useState('')
  const [syncError, setSyncError] = useState('')

  const refreshSyncState = () => {
    qc.invalidateQueries({ queryKey: ['servers'] })
    qc.invalidateQueries({ queryKey: ['sync-history'] })
  }

  const createM = useMutation({ mutationFn: createServer, onSuccess: () => { qc.invalidateQueries({ queryKey: ['servers'] }); setModal(null) } })
  const updateM = useMutation({ mutationFn: ({ id, data }: { id: number; data: ServerRequest }) => updateServer(id, data), onSuccess: () => { qc.invalidateQueries({ queryKey: ['servers'] }); setModal(null) } })
  const deleteM = useMutation({ mutationFn: deleteServer, onSuccess: () => qc.invalidateQueries({ queryKey: ['servers'] }) })
  const syncM   = useMutation({ mutationFn: syncServer,   onSuccess: () => qc.invalidateQueries({ queryKey: ['servers'] }) })
  const syncAllM = useMutation({
    mutationFn: () => syncAll('server'),
    onMutate: () => {
      setSyncMessage('')
      setSyncError('')
    },
    onSuccess: data => {
      setSyncMessage(`Queued sync for ${data.server_ids.length} server(s). Check Sync Jobs for progress.`)
      refreshSyncState()
      window.setTimeout(refreshSyncState, 12_000)
    },
    onError: err => setSyncError(err instanceof Error ? err.message : 'Sync request failed.'),
  })

  return (
    <div className="flex flex-col h-full bg-gray-900 text-gray-100">
      <div className="flex items-center justify-between px-6 py-4 bg-gray-900 border-b border-gray-700">
        <div>
          <h1 className="text-base font-semibold text-white">Servers</h1>
          <p className="text-xs text-gray-400 mt-0.5">{serverAssets.length} server{serverAssets.length !== 1 ? 's' : ''} configured</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => syncAllM.mutate()}
            disabled={syncAllM.isPending || serverAssets.length === 0}
            className="flex items-center gap-2 text-sm font-medium px-4 py-2 rounded-lg bg-gray-800 hover:bg-gray-700 text-gray-200 border border-gray-600 transition-colors disabled:opacity-50"
            title="Sync all servers in this tab"
          >
            <RefreshCw size={14} className={syncAllM.isPending ? 'animate-spin' : ''} />
            {syncAllM.isPending ? 'Syncing servers...' : 'Sync All Servers'}
          </button>
          <button onClick={() => { setEditing(null); setModal('add') }}
            className="flex items-center gap-2 text-sm font-medium px-4 py-2 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white transition-colors">
            <Plus size={14} /> Add Server
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto p-5">
        {(syncMessage || syncError) && (
          <div className={`mb-4 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 rounded-lg border px-4 py-3 text-sm ${syncError ? 'bg-red-950/40 border-red-800 text-red-100' : 'bg-blue-950/40 border-blue-800 text-blue-100'}`}>
            <span>{syncError || syncMessage}</span>
            {!syncError && (
              <button onClick={() => nav('/sync')} className="self-start sm:self-auto text-xs font-semibold px-3 py-1.5 rounded-md bg-blue-700 hover:bg-blue-600 text-white">
                View Sync Jobs
              </button>
            )}
          </div>
        )}

        {serverAssets.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-64 text-gray-500">
            <p className="text-lg font-medium text-gray-400 mb-2">No servers yet</p>
            <p className="text-sm">Click "Add Server" to connect your first server via SSH.</p>
          </div>
        ) : (
          <div className="grid gap-3">
            {serverAssets.map(s => (
              <div key={s.id} className="bg-gray-800 border border-gray-700 rounded-xl p-5">
                <div className="flex items-start justify-between gap-4">
                  <div className="flex items-center gap-3 min-w-0">
                    <div className="mt-0.5">{statusIcon[s.status] ?? statusIcon.unknown}</div>
                    <div className="min-w-0">
                      <div className="flex items-center gap-3 flex-wrap">
                        <h3 className="font-semibold text-white">{s.name}</h3>
                        <span className={`text-xs px-2 py-0.5 rounded-full font-medium ${statusBadge[s.status] ?? statusBadge.unknown}`}>
                          {s.status}
                        </span>
                        <span className="text-xs px-2 py-0.5 rounded-full font-medium bg-blue-950/50 text-blue-200 border border-blue-800">
                          Server
                        </span>
                        <span className="text-xs px-2 py-0.5 rounded-full font-medium bg-slate-950/50 text-slate-200 border border-slate-700">
                          Infrastructure / app host
                        </span>
                      </div>
                      <p className="text-sm text-gray-400 mt-1 font-mono">
                        {s.username}@{s.host}:{s.port}
                      </p>
                      {(s.proxmox_host || s.vmid || s.app_log_paths) && (
                        <div className="flex flex-wrap gap-2 mt-2 text-xs">
                          {s.proxmox_host && <span className="px-2 py-0.5 rounded-md bg-purple-900/40 text-purple-200 border border-purple-800">PVE {s.proxmox_host}</span>}
                          {s.vmid && <span className="px-2 py-0.5 rounded-md bg-sky-900/40 text-sky-200 border border-sky-800">VMIDs {s.vmid}</span>}
                          {s.app_log_paths && <span className="px-2 py-0.5 rounded-md bg-gray-900 text-gray-300 border border-gray-700">Custom app logs</span>}
                        </div>
                      )}
                      <p className="text-xs text-gray-500 mt-1">
                        Last synced: {s.last_sync_at ? format(parseISO(s.last_sync_at), 'MMM d, h:mm a') : 'Never'}
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center gap-2 flex-shrink-0">
                    <button
                      onClick={() => syncM.mutate(s.id)}
                      disabled={syncM.isPending}
                      className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-lg bg-gray-700 hover:bg-gray-600 text-gray-300 border border-gray-600 transition-colors disabled:opacity-50"
                      title="Sync last 12 hours"
                    >
                      <RefreshCw size={12} className={syncM.isPending ? 'animate-spin' : ''} />
                      Sync
                    </button>
                    <DeepSyncButton serverId={s.id} onDone={() => qc.invalidateQueries({ queryKey: ['servers'] })} />
                    <button onClick={() => { setEditing(s); setModal('edit') }}
                      className="p-1.5 rounded-lg text-gray-400 hover:text-white hover:bg-gray-700 transition-colors">
                      <Pencil size={14} />
                    </button>
                    <button onClick={() => { if (confirm(`Delete ${s.name}?`)) deleteM.mutate(s.id) }}
                      className="p-1.5 rounded-lg text-gray-500 hover:text-red-400 hover:bg-red-900/30 transition-colors">
                      <Trash2 size={14} />
                    </button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {modal && (
        <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50 p-4">
          <div className="bg-gray-800 border border-gray-700 rounded-xl shadow-2xl w-full max-w-3xl max-h-[90vh] overflow-y-auto">
            <div className="flex items-center justify-between px-6 py-4 border-b border-gray-700">
              <h2 className="font-semibold text-white">{modal === 'add' ? 'Add Server' : 'Edit Server'}</h2>
              <button onClick={() => setModal(null)} className="text-gray-400 hover:text-white text-xl leading-none">&times;</button>
            </div>
            <div className="p-6">
              <ServerForm
                initial={editing ?? undefined}
                defaultAssetType="server"
                onSubmit={async data => {
                  if (modal === 'add') await createM.mutateAsync(data)
                  else if (editing) await updateM.mutateAsync({ id: editing.id, data })
                }}
                onCancel={() => setModal(null)}
              />
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
