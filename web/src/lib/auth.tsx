import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react"
import { api, setCsrfToken } from "./api"
import type { User } from "./types"

interface AuthContextValue {
  user: User | null
  loading: boolean
  refresh: () => Promise<void>
  login: (email: string, password: string) => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    try {
      const res = await api.me()
      setCsrfToken(res.csrf_token || "")
      setUser(res.user)
    } catch {
      setCsrfToken("")
      setUser(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const login = useCallback(async (email: string, password: string) => {
    const res = await api.login(email, password)
    setCsrfToken(res.csrf_token || "")
    setUser(res.user)
  }, [])

  const logout = useCallback(async () => {
    try {
      await api.logout()
    } finally {
      setCsrfToken("")
      setUser(null)
    }
  }, [])

  const value = useMemo(
    () => ({ user, loading, refresh, login, logout }),
    [user, loading, refresh, login, logout]
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error("useAuth must be used inside AuthProvider")
  }
  return ctx
}
