import { FormEvent, useState } from 'react'
import { KeyRound } from 'lucide-react'
import { changePassword } from '../api/client'

export default function ChangePasswordPage() {
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  async function submit(event: FormEvent) {
    event.preventDefault()
    setMessage('')
    setError('')
    if (newPassword !== confirmPassword) {
      setError('New password and confirmation do not match.')
      return
    }
    if (newPassword.length < 12 || !/[A-Za-z]/.test(newPassword) || !/[0-9]/.test(newPassword)) {
      setError('Use at least 12 characters with at least one letter and one number.')
      return
    }
    setSaving(true)
    try {
      await changePassword(currentPassword, newPassword)
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
      setMessage('Password changed successfully. Use the new password next time you sign in.')
    } catch (err: any) {
      setError(err.response?.data?.error || 'Password change failed.')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="h-full overflow-y-auto bg-gray-950 text-gray-100 p-6">
      <div className="max-w-xl">
        <div className="flex items-center gap-3 mb-6">
          <div className="h-10 w-10 rounded-lg bg-blue-600/20 border border-blue-500/40 flex items-center justify-center">
            <KeyRound size={20} className="text-blue-300" />
          </div>
          <div>
            <h1 className="text-xl font-semibold text-white">Change Password</h1>
            <p className="text-sm text-gray-400">Update your DRISHTI AMR Health sign-in password.</p>
          </div>
        </div>

        <form onSubmit={submit} className="bg-gray-900 border border-gray-800 rounded-lg p-6 space-y-5">
          <PasswordField label="Current password" value={currentPassword} onChange={setCurrentPassword} autoComplete="current-password" />
          <PasswordField label="New password" value={newPassword} onChange={setNewPassword} autoComplete="new-password" />
          <PasswordField label="Confirm new password" value={confirmPassword} onChange={setConfirmPassword} autoComplete="new-password" />
          <p className="text-xs text-gray-400">Minimum 12 characters, including at least one letter and one number. Symbols are also allowed.</p>
          {error && <div className="rounded-md border border-red-800 bg-red-950/50 px-3 py-2 text-sm text-red-200">{error}</div>}
          {message && <div className="rounded-md border border-green-800 bg-green-950/40 px-3 py-2 text-sm text-green-200">{message}</div>}
          <button type="submit" disabled={saving} className="btn-primary px-5 py-2 disabled:opacity-50">
            {saving ? 'Changing password...' : 'Change password'}
          </button>
        </form>
      </div>
    </div>
  )
}

function PasswordField({ label, value, onChange, autoComplete }: { label: string; value: string; onChange: (value: string) => void; autoComplete: string }) {
  return (
    <div>
      <label className="block text-sm font-medium text-gray-300 mb-1.5">{label}</label>
      <input
        className="input bg-gray-950 border-gray-700 text-white"
        type="password"
        required
        value={value}
        onChange={event => onChange(event.target.value)}
        autoComplete={autoComplete}
      />
    </div>
  )
}
