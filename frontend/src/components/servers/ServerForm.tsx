import { useState } from 'react'
import type { Server, ServerRequest } from '../../types'
import { testConnection } from '../../api/client'
import { CheckCircle, XCircle, Loader } from 'lucide-react'

interface Props {
  initial?: Server
  defaultAssetType?: 'server' | 'endpoint'
  submitLabel?: string
  onSubmit: (data: ServerRequest) => Promise<void>
  onCancel: () => void
}

const inputCls = 'w-full bg-gray-900 border border-gray-600 rounded-lg px-3 py-2 text-sm text-gray-200 placeholder-gray-500 focus:outline-none focus:border-indigo-500 transition-colors'
const labelCls = 'block text-xs font-medium text-gray-400 mb-1'

export default function ServerForm({ initial, defaultAssetType = 'server', submitLabel, onSubmit, onCancel }: Props) {
  const [form, setForm] = useState<ServerRequest>({
    name: initial?.name ?? '',
    host: initial?.host ?? '',
    port: initial?.port ?? 22,
    username: initial?.username ?? '',
    auth_type: initial?.auth_type ?? 'password',
    asset_type: initial?.asset_type ?? defaultAssetType,
    password: '',
    private_key: '',
    proxmox_host: initial?.proxmox_host ?? '',
    proxmox_port: initial?.proxmox_port ?? 22,
    proxmox_username: initial?.proxmox_username ?? '',
    proxmox_auth_type: initial?.proxmox_auth_type ?? 'password',
    proxmox_password: '',
    proxmox_private_key: '',
    vmid: initial?.vmid ?? '',
    app_log_paths: initial?.app_log_paths ?? '',
  })
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<{ success: boolean; msg: string } | null>(null)

  const set = (k: keyof ServerRequest, v: string | number) => setForm(f => ({ ...f, [k]: v }))

  const privateKeyHint = (value?: string) => {
    const key = (value ?? '').trim()
    if (!key) return ''
    if (key.startsWith('ssh-ed25519 ') || key.startsWith('ssh-rsa ') || key.startsWith('ecdsa-sha2-')) {
      return 'This is a public key. Paste the private key instead. It starts with -----BEGIN OPENSSH PRIVATE KEY-----.'
    }
    if (!key.includes('BEGIN') || !key.includes('PRIVATE KEY')) {
      return 'Paste the full private key block, not a file path or public key.'
    }
    if (!key.includes('END') || !key.includes('PRIVATE KEY-----')) {
      return 'The private key looks incomplete. Include the BEGIN line, all middle lines, and the END line.'
    }
    return ''
  }

  const errorMessage = (error: unknown) => {
    const candidate = error as { response?: { data?: { error?: string; message?: string } }; message?: string }
    return candidate.response?.data?.error ?? candidate.response?.data?.message ?? candidate.message ?? 'Save failed. Please try again.'
  }
  const ubuntuKeyHint = form.auth_type === 'key' ? privateKeyHint(form.private_key) : ''
  const pveKeyHint = (form.proxmox_auth_type ?? 'password') === 'key' ? privateKeyHint(form.proxmox_private_key) : ''

  const handleTest = async () => {
    if (ubuntuKeyHint) {
      setTestResult({ success: false, msg: ubuntuKeyHint })
      return
    }
    setTesting(true)
    setTestResult(null)
    try {
      const res = await testConnection(form)
      setTestResult({ success: res.success, msg: res.error ?? res.info ?? '' })
    } catch (error) {
      setTestResult({ success: false, msg: errorMessage(error) })
    } finally {
      setTesting(false)
    }
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (ubuntuKeyHint || pveKeyHint) {
      setTestResult({ success: false, msg: ubuntuKeyHint || pveKeyHint })
      return
    }
    setSaving(true)
    setTestResult(null)
    try {
      await onSubmit(form)
    } catch (error) {
      setTestResult({ success: false, msg: errorMessage(error) })
    } finally {
      setSaving(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <div className="grid grid-cols-2 gap-3">
        <div className="col-span-2">
          <label className={labelCls}>Display name</label>
          <input className={inputCls} value={form.name} onChange={e => set('name', e.target.value)} required placeholder="FleetManager VM 104" />
        </div>
        <div className="col-span-2">
          <label className={labelCls}>Folder / asset type</label>
          <select className={inputCls} value={form.asset_type ?? 'server'} onChange={e => set('asset_type', e.target.value)}>
            <option value="server">Server - infrastructure / app host</option>
            <option value="endpoint">Workstation - endpoint computer</option>
          </select>
        </div>

        <div className="col-span-2 pt-1">
          <p className="text-xs font-semibold text-gray-300">Ubuntu / FleetManager SSH</p>
        </div>
        <div>
          <label className={labelCls}>Ubuntu IP / host</label>
          <input className={inputCls} value={form.host} onChange={e => set('host', e.target.value)} required placeholder="192.168.1.10" />
        </div>
        <div>
          <label className={labelCls}>SSH port</label>
          <input className={inputCls} type="number" value={form.port} onChange={e => set('port', parseInt(e.target.value))} min={1} max={65535} />
        </div>
        <div>
          <label className={labelCls}>Username</label>
          <input className={inputCls} value={form.username} onChange={e => set('username', e.target.value)} required placeholder="logpull" />
        </div>
        <div>
          <label className={labelCls}>Auth type</label>
          <select className={inputCls} value={form.auth_type} onChange={e => set('auth_type', e.target.value)}>
            <option value="password">Password</option>
            <option value="key">Private Key</option>
          </select>
        </div>
        {form.auth_type === 'password' ? (
          <div className="col-span-2">
            <label className={labelCls}>Password {initial && <span className="text-gray-500 font-normal">(leave blank to keep)</span>}</label>
            <input className={inputCls} type="password" value={form.password ?? ''} onChange={e => set('password', e.target.value)} placeholder="Password" />
          </div>
        ) : (
          <div className="col-span-2">
            <label className={labelCls}>Private key {initial && <span className="text-gray-500 font-normal">(leave blank to keep)</span>}</label>
            <textarea className={`${inputCls} font-mono text-xs`} rows={6} value={form.private_key ?? ''} onChange={e => set('private_key', e.target.value)} placeholder={'-----BEGIN OPENSSH PRIVATE KEY-----\n...\n-----END OPENSSH PRIVATE KEY-----'} />
            <p className="text-xs text-gray-500 mt-1">Do not paste the public key that starts with ssh-ed25519. Paste the private key from the app host.</p>
            {ubuntuKeyHint && <p className="text-xs text-red-300 mt-1">{ubuntuKeyHint}</p>}
          </div>
        )}

        <div className="col-span-2 pt-2 border-t border-gray-700">
          <p className="text-xs font-semibold text-gray-300">Proxmox / PVE mapping</p>
        </div>
        <div>
          <label className={labelCls}>Proxmox host</label>
          <input className={inputCls} value={form.proxmox_host ?? ''} onChange={e => set('proxmox_host', e.target.value)} placeholder="pve01.local or 10.0.0.5" />
        </div>
        <div>
          <label className={labelCls}>Monitored VMIDs</label>
          <input className={inputCls} value={form.vmid ?? ''} onChange={e => set('vmid', e.target.value)} placeholder="Blank = all, or 104, 113, 260003" />
        </div>
        <div>
          <label className={labelCls}>PVE SSH user</label>
          <input className={inputCls} value={form.proxmox_username ?? ''} onChange={e => set('proxmox_username', e.target.value)} placeholder="root" />
        </div>
        <div>
          <label className={labelCls}>PVE SSH port</label>
          <input className={inputCls} type="number" value={form.proxmox_port ?? 22} onChange={e => set('proxmox_port', parseInt(e.target.value))} min={1} max={65535} />
        </div>
        <div className="col-span-2">
          <label className={labelCls}>PVE auth type</label>
          <select className={inputCls} value={form.proxmox_auth_type ?? 'password'} onChange={e => set('proxmox_auth_type', e.target.value)}>
            <option value="password">Password</option>
            <option value="key">Private Key</option>
          </select>
        </div>
        {(form.proxmox_auth_type ?? 'password') === 'password' ? (
          <div className="col-span-2">
            <label className={labelCls}>PVE password {initial && <span className="text-gray-500 font-normal">(leave blank to keep)</span>}</label>
            <input className={inputCls} type="password" value={form.proxmox_password ?? ''} onChange={e => set('proxmox_password', e.target.value)} placeholder="Password" />
          </div>
        ) : (
          <div className="col-span-2">
            <label className={labelCls}>PVE private key {initial && <span className="text-gray-500 font-normal">(leave blank to keep)</span>}</label>
            <textarea className={`${inputCls} font-mono text-xs`} rows={6} value={form.proxmox_private_key ?? ''} onChange={e => set('proxmox_private_key', e.target.value)} placeholder={'-----BEGIN OPENSSH PRIVATE KEY-----\n...\n-----END OPENSSH PRIVATE KEY-----'} />
            <p className="text-xs text-gray-500 mt-1">Do not paste the public key that starts with ssh-ed25519. Paste the private key from the app host.</p>
            {pveKeyHint && <p className="text-xs text-red-300 mt-1">{pveKeyHint}</p>}
          </div>
        )}
        <div className="col-span-2">
          <label className={labelCls}>Application log paths</label>
          <textarea
            className={`${inputCls} font-mono text-xs`}
            rows={3}
            value={form.app_log_paths ?? ''}
            onChange={e => set('app_log_paths', e.target.value)}
            placeholder={'/opt/Roboshop/bin/location/appInfo/log\n/opt/fleetmanager/logs'}
          />
        </div>
      </div>

      {testResult && (
        <div className={`flex items-center gap-2 text-sm p-3 rounded-lg border ${testResult.success ? 'bg-green-900/30 text-green-300 border-green-700' : 'bg-red-900/30 text-red-300 border-red-700'}`}>
          {testResult.success ? <CheckCircle size={15} /> : <XCircle size={15} />}
          <span>{testResult.success ? 'Ubuntu connection successful' : testResult.msg}</span>
        </div>
      )}

      <div className="flex items-center justify-between pt-1">
        <button type="button" onClick={handleTest} disabled={testing || !form.host || !form.username}
          className="text-sm text-indigo-400 hover:text-indigo-300 disabled:opacity-40 flex items-center gap-1 transition-colors">
          {testing && <Loader size={13} className="animate-spin" />}
          Test Ubuntu SSH
        </button>
        <div className="flex gap-2">
          <button type="button" onClick={onCancel}
            className="text-sm px-4 py-2 rounded-lg bg-gray-700 hover:bg-gray-600 text-gray-300 border border-gray-600 transition-colors">
            Cancel
          </button>
          <button type="submit" disabled={saving}
            className="text-sm px-4 py-2 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white transition-colors disabled:opacity-60">
            {saving ? 'Saving...' : submitLabel ?? (initial ? 'Update' : 'Add Server')}
          </button>
        </div>
      </div>
    </form>
  )
}
