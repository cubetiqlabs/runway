import React, { createContext, useContext, useEffect, useMemo, useState } from "react"

export type ThemeMode = "system" | "light" | "dark"

interface ThemeContextValue {
  mode: ThemeMode
  resolved: "light" | "dark"
  setMode: (mode: ThemeMode) => void
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

const STORAGE_KEY = "runway-theme-mode"

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(() => {
    if (typeof window === "undefined") return "dark"
    const saved = window.localStorage.getItem(STORAGE_KEY)
    if (saved === "light" || saved === "dark" || saved === "system") return saved
    return "dark"
  })

  const [systemDark, setSystemDark] = useState<boolean>(() => {
    if (typeof window === "undefined") return true
    return window.matchMedia("(prefers-color-scheme: dark)").matches
  })

  useEffect(() => {
    const mq = window.matchMedia("(prefers-color-scheme: dark)")
    const onChange = (e: MediaQueryListEvent) => setSystemDark(e.matches)
    setSystemDark(mq.matches)
    mq.addEventListener("change", onChange)
    return () => mq.removeEventListener("change", onChange)
  }, [])

  const resolved: "light" | "dark" = mode === "system" ? (systemDark ? "dark" : "light") : mode

  useEffect(() => {
    const root = document.documentElement
    root.classList.remove("light", "dark")
    root.classList.add(resolved)
    root.style.colorScheme = mode === "system" ? "light dark" : resolved
  }, [mode, resolved])

  const setMode = (next: ThemeMode) => {
    setModeState(next)
    if (typeof window !== "undefined") {
      window.localStorage.setItem(STORAGE_KEY, next)
    }
  }

  const value = useMemo(() => ({ mode, resolved, setMode }), [mode, resolved])
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext)
  if (!ctx) {
    return { mode: "dark", resolved: "dark", setMode: () => {} }
  }
  return ctx
}
