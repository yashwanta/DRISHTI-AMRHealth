import { FormEvent, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, Users } from 'lucide-react'
import { createUser, getUsers, updateUser } from '../api/client'
import type { AdminPermission, AppUser, AppUserRequest, UserRole } from '../types'

const roles: UserRole[] = ['Super Admin', 'Global Admin', 'Global Admin Read Only', 'Location Admin', 'IT User']
const permissionOptions: { value: AdminPermission; label: string }[] = [
  { value: 'users', label: 'User Management' }, { value: 'discovery', label: 'Discovery' },
  { value: 'heatmap', label: 'Heat Map' }, { value: 'servers', label: 'Servers' },
  { value: 'sync', label: 'Sync Jobs' }, { value: 'change_password', label: 'Change Password' },
]
const emptyForm: AppUserRequest = { username: '', password: '', role: 'IT User', location: '', status: 'active', permissions: [] }

export default function UserManagementPage() {
  const qc = useQueryClient()
  const { data: users = [], isLoading } = useQuery({ queryKey: ['users'], queryFn: getUsers })
  const [editing, setEditing] = useState<AppUser | null>(null)
  const [form, setForm] = useState<AppUserRequest>(emptyForm)
  const [open, setOpen] = useState(false)
  const [error, setError] = useState('')
  const save = useMutation({
    mutationFn: () => editing ? updateUser(editing.id, form) : createUser(form),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['users'] }); setOpen(false); setEditing(null); setForm(emptyForm); setError('') },
    onError: (err: any) => setError(err.response?.data?.error || 'Could not save user.'),
  })

  function startCreate() { setEditing(null); setForm(emptyForm); setError(''); setOpen(true) }
  function startEdit(user: AppUser) { setEditing(user); setForm({ role: user.role, location: user.location, status: user.status, password: '', permissions: user.permissions || [] }); setError(''); setOpen(true) }
  function submit(event: FormEvent) { event.preventDefault(); setError(''); save.mutate() }

  return <div className="h-full overflow-y-auto bg-gray-950 text-gray-100 p-6">
    <div className="max-w-6xl mx-auto">
      <header className="flex items-center justify-between gap-4 mb-6">
        <div className="flex items-center gap-3"><div className="h-10 w-10 rounded-lg bg-blue-600/20 border border-blue-500/40 flex items-center justify-center"><Users size={20} className="text-blue-300" /></div><div><h1 className="text-xl font-semibold text-white">User Management</h1><p className="text-sm text-gray-400">Create accounts and assign DRISHTI access roles.</p></div></div>
        <button onClick={startCreate} className="btn-primary px-4 py-2 flex items-center gap-2"><Plus size={16}/> Create User</button>
      </header>
      <div className="rounded-xl border border-gray-800 bg-gray-900 overflow-hidden">
        <table className="w-full text-sm"><thead className="bg-gray-800/80 text-gray-400"><tr><th className="text-left p-3">Username</th><th className="text-left p-3">Role</th><th className="text-left p-3">Admin Access</th><th className="text-left p-3">Location</th><th className="text-left p-3">Status</th><th className="text-left p-3">Updated</th><th className="p-3"></th></tr></thead>
          <tbody>{isLoading ? <tr><td colSpan={7} className="p-6 text-center text-gray-500">Loading users...</td></tr> : users.length ? users.map(user => <tr key={user.id} className="border-t border-gray-800"><td className="p-3 font-medium text-white">{user.username}</td><td className="p-3">{user.role}</td><td className="p-3 text-xs text-blue-300">{user.role === 'Super Admin' ? 'All admin pages' : (user.permissions || []).map(value => permissionOptions.find(item => item.value === value)?.label).filter(Boolean).join(', ') || 'None'}</td><td className="p-3 text-gray-400">{user.location || 'All locations'}</td><td className="p-3"><span className={`px-2 py-1 rounded-full text-xs border ${user.status === 'active' ? 'bg-green-950 text-green-300 border-green-800' : 'bg-gray-800 text-gray-400 border-gray-700'}`}>{user.status}</span></td><td className="p-3 text-gray-500">{new Date(user.updated_at).toLocaleString()}</td><td className="p-3 text-right"><button onClick={() => startEdit(user)} className="p-2 rounded-lg hover:bg-gray-700 text-gray-400 hover:text-white" aria-label={`Edit ${user.username}`}><Pencil size={15}/></button></td></tr>) : <tr><td colSpan={7} className="p-6 text-center text-gray-500">No managed users yet. The environment administrator can still sign in.</td></tr>}</tbody>
        </table>
      </div>
    </div>
    {open && <div className="fixed inset-0 z-50 bg-black/70 flex items-center justify-center p-4"><form onSubmit={submit} className="w-full max-w-lg rounded-xl border border-gray-700 bg-gray-900 shadow-2xl"><header className="flex items-center justify-between p-5 border-b border-gray-800"><div><h2 className="font-semibold text-white">{editing ? `Edit ${editing.username}` : 'Create User'}</h2><p className="text-xs text-gray-400 mt-1">Passwords require 12 characters with a letter and number.</p></div><button type="button" onClick={() => setOpen(false)} className="text-xl text-gray-500 hover:text-white">&times;</button></header><div className="p-5 grid grid-cols-2 gap-4">
      {!editing && <Field label="Username"><input className="input bg-gray-950 border-gray-700 text-white" required minLength={3} value={form.username || ''} onChange={e => setForm({...form, username:e.target.value})}/></Field>}
      <Field label={editing ? 'New password (optional)' : 'Temporary password'}><input className="input bg-gray-950 border-gray-700 text-white" type="password" required={!editing} minLength={editing ? undefined : 12} value={form.password || ''} onChange={e => setForm({...form, password:e.target.value})}/></Field>
      <Field label="Role"><select className="input bg-gray-950 border-gray-700 text-white" value={form.role} onChange={e => setForm({...form, role:e.target.value as UserRole})}>{roles.map(role => <option key={role}>{role}</option>)}</select></Field>
      <Field label="Location / Plant"><input className="input bg-gray-950 border-gray-700 text-white" value={form.location || ''} placeholder="Blank = all locations" onChange={e => setForm({...form, location:e.target.value})}/></Field>
      <Field label="Status"><select className="input bg-gray-950 border-gray-700 text-white" value={form.status || 'active'} onChange={e => setForm({...form, status:e.target.value as 'active'|'disabled'})}><option value="active">Active</option><option value="disabled">Disabled</option></select></Field>
      <div className="col-span-2"><span className="block text-sm font-medium text-gray-300 mb-2">Admin section access</span><div className="grid grid-cols-2 gap-2 rounded-lg border border-gray-700 bg-gray-950 p-3">{permissionOptions.map(item => <label key={item.value} className="flex items-center gap-2 text-sm text-gray-300"><input type="checkbox" disabled={form.role === 'Super Admin'} checked={form.role === 'Super Admin' || (form.permissions || []).includes(item.value)} onChange={e => setForm({...form, permissions: e.target.checked ? [...(form.permissions || []), item.value] : (form.permissions || []).filter(value => value !== item.value)})}/><span>{item.label}</span></label>)}</div>{form.role === 'Super Admin' && <p className="text-xs text-blue-300 mt-2">Super Admin always has access to every protected page.</p>}</div>
      {error && <div className="col-span-2 rounded-md border border-red-800 bg-red-950/50 px-3 py-2 text-sm text-red-200">{error}</div>}
    </div><footer className="flex justify-end gap-3 p-5 border-t border-gray-800"><button type="button" onClick={() => setOpen(false)} className="px-4 py-2 rounded-lg bg-gray-800 text-gray-300">Cancel</button><button type="submit" disabled={save.isPending} className="btn-primary px-5 py-2 disabled:opacity-50">{save.isPending ? 'Saving...' : editing ? 'Save Changes' : 'Create User'}</button></footer></form></div>}
  </div>
}

function Field({label, children}:{label:string; children:ReactNode}) { return <label className="block"><span className="block text-sm font-medium text-gray-300 mb-1.5">{label}</span>{children}</label> }
