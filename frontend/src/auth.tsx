import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { getMe, login as loginRequest, logout as logoutRequest } from './api/client'
import type { AdminPermission, UserRole } from './types'

interface AuthState {
  username: string | null
  role: UserRole | null
  ready: boolean
  permissions: AdminPermission[]
  hasPermission: (permission: AdminPermission) => boolean
  isAuthenticated: boolean
  canAdmin: boolean
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [username, setUsername] = useState<string | null>(null)
  const [role, setRole] = useState<UserRole | null>(null)
  const [ready, setReady] = useState(false)
  const [permissions, setPermissions] = useState<AdminPermission[]>([])

  useEffect(() => {
    let mounted = true
    getMe()
      .then(user => {
        if (!mounted) return
        setUsername(user.username)
        setRole(user.role as UserRole)
        setPermissions(user.permissions || [])
      })
      .catch(() => {
        if (!mounted) return
        setUsername(null)
        setRole(null)
        setPermissions([])
      })
      .finally(() => {
        if (mounted) setReady(true)
      })
    return () => {
      mounted = false
    }
  }, [])

  const value = useMemo<AuthState>(() => ({
    username,
    role,
    ready,
    permissions,
    isAuthenticated: Boolean(username && role),
    canAdmin: role === 'Super Admin' || permissions.length > 0,
    hasPermission: permission => role === 'Super Admin' || permissions.includes(permission),
    login: async (nextUsername, password) => {
      const response = await loginRequest(nextUsername, password)
      setUsername(response.username)
      setRole(response.role)
      setReady(true)
      const user = await getMe()
      setPermissions(user.permissions || [])
    },
    logout: async () => {
      try {
        await logoutRequest()
      } finally {
        setUsername(null)
        setRole(null)
        setPermissions([])
      }
    },
  }), [username, role, ready, permissions])

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth must be used inside AuthProvider')
  }
  return ctx
}
