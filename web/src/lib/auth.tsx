import React, { createContext, useCallback, useContext, useEffect, useMemo } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { api, setCsrfToken } from "./api"
import { authMeQueryOptions, overviewQueryOptions, queryKeys } from "./query"
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
  const queryClient = useQueryClient()

  const meQuery = useQuery(authMeQueryOptions())

  useEffect(() => {
    if (meQuery.data) {
      setCsrfToken(meQuery.data.csrf_token || "")
    } else if (meQuery.isError) {
      setCsrfToken("")
    }
  }, [meQuery.data, meQuery.isError])

  const loginMutation = useMutation({
    mutationFn: ({ email, password }: { email: string; password: string }) =>
      api.login(email, password),
    onSuccess: (res) => {
      setCsrfToken(res.csrf_token || "")
      queryClient.setQueryData(queryKeys.authMe, {
        user: res.user,
        csrf_token: res.csrf_token || "",
      })
      void queryClient.prefetchQuery(overviewQueryOptions())
    },
  })

  const logoutMutation = useMutation({
    mutationFn: () => api.logout(),
    onSettled: () => {
      setCsrfToken("")
      queryClient.clear()
    },
  })

  const refresh = useCallback(async () => {
    await queryClient.invalidateQueries({ queryKey: queryKeys.authMe })
  }, [queryClient])

  const login = useCallback(
    async (email: string, password: string) => {
      await loginMutation.mutateAsync({ email, password })
    },
    [loginMutation]
  )

  const logout = useCallback(async () => {
    await logoutMutation.mutateAsync()
  }, [logoutMutation])

  const user = meQuery.isError ? null : (meQuery.data?.user ?? null)
  const loading = meQuery.isPending

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
