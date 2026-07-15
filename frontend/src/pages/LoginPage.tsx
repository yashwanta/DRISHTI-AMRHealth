import { FormEvent, useState } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { Lock, LogIn, ShieldCheck } from 'lucide-react'
import { useAuth } from '../auth'

export default function LoginPage() {
  const auth = useAuth()
  const navigate = useNavigate()
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  if (auth.isAuthenticated) {
    return <Navigate to="/" replace />
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      await auth.login(username, password)
      navigate('/', { replace: true })
    } catch (err: any) {
      setError(err.response?.data?.error || 'Login failed')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-gray-950 text-gray-100 flex items-center justify-center px-4">
      <div className="w-full max-w-sm">
        <div className="flex items-center gap-3 mb-8">
          <div className="h-10 w-10 rounded-lg bg-blue-600/20 border border-blue-500/40 flex items-center justify-center">
            <ShieldCheck size={22} className="text-blue-300" />
          </div>
          <div>
            <h1 className="text-xl font-semibold text-white">DRISHTI SiteOps</h1>
            <p className="text-sm text-gray-400">Site operations and automation console</p>
          </div>
        </div>

        <form onSubmit={submit} className="bg-gray-900 border border-gray-800 rounded-lg p-6 space-y-4 shadow-2xl">
          <div>
            <label className="block text-sm font-medium text-gray-300 mb-1.5">Username</label>
            <input
              className="input bg-gray-950 border-gray-700 text-white"
              value={username}
              onChange={e => setUsername(e.target.value)}
              autoComplete="username"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-300 mb-1.5">Password</label>
            <div className="relative">
              <Lock size={15} className="absolute left-3 top-2.5 text-gray-500" />
              <input
                className="input bg-gray-950 border-gray-700 text-white pl-9"
                type="password"
                value={password}
                onChange={e => setPassword(e.target.value)}
                autoComplete="current-password"
                autoFocus
              />
            </div>
          </div>
          {error && (
            <div className="text-sm text-red-300 bg-red-950/50 border border-red-800 rounded-md px-3 py-2">
              {error}
            </div>
          )}
          <button type="submit" disabled={loading} className="btn-primary w-full flex items-center justify-center gap-2">
            <LogIn size={16} />
            {loading ? 'Signing in...' : 'Sign in'}
          </button>
        </form>
      </div>
    </div>
  )
}
