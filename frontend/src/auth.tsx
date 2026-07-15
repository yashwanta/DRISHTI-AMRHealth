import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { getMe, login as loginRequest, logout as logoutRequest } from './api/client'
import type { UserRole } from './types'

interface AuthState {
  username: string | null
  role: UserRole | null
  isAuthenticated: boolean
  canAdmin: boolean
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [username, setUsername] = useState<string | null>(null)
  const [role, setRole] = useState<UserRole | null>(null)

  useEffect(() => {
    let mounted = true
    getMe()
      .then(user => {
        if (!mounted) return
        setUsername(user.username)
        setRole(user.role as UserRole)
      })
      .catch(() => {
        if (!mounted) return
        setUsername(null)
        setRole(null)
      })
    return () => {
      mounted = false
    }
  }, [])

  const value = useMemo<AuthState>(() => ({
    username,
    role,
    isAuthenticated: Boolean(username && role),
    canAdmin: role === 'Super Admin' || role === 'Global Admin' || role === 'Location Admin',
    login: async (nextUsername, password) => {
      const response = await loginRequest(nextUsername, password)
      setUsername(response.username)
      setRole(response.role)
    },
    logout: async () => {
      try {
        await logoutRequest()
      } finally {
        setUsername(null)
        setRole(null)
      }
    },
  }), [username, role])

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth must be used inside AuthProvider')
  }
  return ctx
}