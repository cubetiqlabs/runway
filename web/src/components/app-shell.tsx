import React, { useEffect, useState } from "react"
import { Link, useLocation, useNavigate } from "@tanstack/react-router"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  DashboardSquare02Icon,
  ServerStack02Icon,
  Layers01Icon,
  WorkflowSquare06Icon,
  Shield01Icon,
  GitBranchIcon,
  Sun03Icon,
  Moon02Icon,
  ComputerIcon,
  Logout03Icon,
  Menu01Icon,
  Cancel01Icon,
} from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { useAuth } from "@/lib/auth"
import { useTheme, type ThemeMode } from "@/lib/theme"
import { cn } from "@/lib/utils"

const NAV_ITEMS = [
  { to: "/", label: "Overview", icon: DashboardSquare02Icon },
  { to: "/runners", label: "Runners", icon: ServerStack02Icon },
  { to: "/agents", label: "Agents", icon: ComputerIcon },
  { to: "/pools", label: "Runner Pools", icon: Layers01Icon },
  { to: "/jobs", label: "Job Moderation", icon: WorkflowSquare06Icon },
  { to: "/policies", label: "Policies", icon: Shield01Icon },
  { to: "/installations", label: "GitHub App & Logs", icon: GitBranchIcon },
] as const

export function AppShell({ children }: { children: React.ReactNode }) {
  const { user, logout } = useAuth()
  const { mode, setMode } = useTheme()
  const location = useLocation()
  const navigate = useNavigate()
  const [mobileOpen, setMobileOpen] = useState(false)

  useEffect(() => {
    if (!mobileOpen) return
    const prevOverflow = document.body.style.overflow
    document.body.style.overflow = "hidden"
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setMobileOpen(false)
    }
    window.addEventListener("keydown", onKeyDown)
    return () => {
      document.body.style.overflow = prevOverflow
      window.removeEventListener("keydown", onKeyDown)
    }
  }, [mobileOpen])

  const handleLogout = async () => {
    await logout()
    await navigate({ to: "/login" })
  }

  const themeIcon = mode === "dark" ? Moon02Icon : mode === "light" ? Sun03Icon : ComputerIcon

  const navContent = (
    <nav aria-label="Primary navigation" className="flex flex-1 flex-col gap-1 overflow-y-auto p-3">
      {NAV_ITEMS.map((item) => {
        const active =
          item.to === "/"
            ? location.pathname === "/"
            : location.pathname.startsWith(item.to)
        return (
          <Link
            key={item.to}
            to={item.to}
            onClick={() => setMobileOpen(false)}
            className={cn(
              "flex items-center gap-2.5 px-3 py-2 text-xs font-medium transition-colors border-l-2",
              active
                ? "border-primary bg-sidebar-accent text-sidebar-accent-foreground"
                : "border-transparent text-muted-foreground hover:bg-sidebar-accent/60 hover:text-foreground"
            )}
          >
            <HugeiconsIcon icon={item.icon} strokeWidth={2} className="size-4 shrink-0" />
            <span>{item.label}</span>
          </Link>
        )
      })}
    </nav>
  )

  return (
    <div className="min-h-dvh flex bg-background text-foreground">
      {/* Desktop Sticky Sidebar */}
      <aside className="hidden md:flex sticky top-0 h-dvh w-60 shrink-0 flex-col overflow-hidden border-r border-sidebar-border bg-sidebar">
        <div className="flex h-14 items-center gap-2.5 border-b border-sidebar-border px-4">
          <div className="flex size-7 items-center justify-center bg-primary text-primary-foreground font-heading text-sm font-bold">
            R
          </div>
          <div className="flex flex-col">
            <span className="font-heading text-sm font-semibold tracking-tight leading-none">
              Runway
            </span>
            <span className="text-[10px] text-muted-foreground mt-0.5">
              GitHub Runner Moderator
            </span>
          </div>
        </div>
        {navContent}
        <div className="border-t border-sidebar-border p-3 text-[11px] text-muted-foreground">
          <div className="flex items-center justify-between">
            <span>Control Plane</span>
            <Badge variant="outline" className="text-[10px]">
              v1.0
            </Badge>
          </div>
        </div>
      </aside>

      {/* Mobile Drawer */}
      {mobileOpen && (
        <div className="fixed inset-0 z-50 md:hidden flex">
          <div
            className="fixed inset-0 bg-black/50"
            onClick={() => setMobileOpen(false)}
            aria-hidden="true"
          />
          <aside className="relative z-10 flex h-dvh w-64 flex-col overflow-hidden border-r border-sidebar-border bg-sidebar">
            <div className="flex h-14 items-center justify-between border-b border-sidebar-border px-4">
              <span className="font-heading text-sm font-semibold">Runway</span>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={() => setMobileOpen(false)}
                aria-label="Close navigation"
              >
                <HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} />
              </Button>
            </div>
            {navContent}
          </aside>
        </div>
      )}

      {/* Main Content Area */}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-30 flex h-14 items-center justify-between gap-3 border-b border-border bg-background/90 backdrop-blur px-4 md:px-6">
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              className="md:hidden"
              onClick={() => setMobileOpen(true)}
              aria-label="Open navigation"
            >
              <HugeiconsIcon icon={Menu01Icon} strokeWidth={2} />
            </Button>
            <span className="text-xs font-medium text-muted-foreground hidden sm:inline">
              GitHub App Runner Fleet &amp; Policy Control
            </span>
          </div>

          <div className="flex items-center gap-2">
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button type="button" variant="outline" size="sm" aria-label="Switch color theme" />
                }
              >
                <HugeiconsIcon icon={themeIcon} strokeWidth={2} className="size-3.5" />
                <span className="capitalize hidden sm:inline">{mode}</span>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                {(["system", "light", "dark"] as ThemeMode[]).map((opt) => (
                  <DropdownMenuItem key={opt} onClick={() => setMode(opt)}>
                    <span className="capitalize">{opt}</span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>

            {user && (
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={<Button type="button" variant="outline" size="sm" />}
                >
                  <span className="max-w-36 truncate">{user.email}</span>
                  <Badge variant="secondary" className="text-[10px] uppercase">
                    {user.role}
                  </Badge>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem variant="destructive" onClick={handleLogout}>
                    <HugeiconsIcon icon={Logout03Icon} strokeWidth={2} />
                    <span>Sign out</span>
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            )}
          </div>
        </header>

        <main className="flex-1 min-w-0 p-4 md:p-6 max-w-7xl w-full mx-auto space-y-6">
          {children}
        </main>
      </div>
    </div>
  )
}
