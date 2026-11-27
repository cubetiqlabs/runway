import React, { useEffect } from "react"
import {
  HeadContent,
  Outlet,
  Scripts,
  createRootRouteWithContext,
  useLocation,
  useNavigate,
} from "@tanstack/react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { ReactQueryDevtools } from "@tanstack/react-query-devtools"
import appCss from "../styles.css?url"
import { ThemeProvider } from "@/lib/theme"
import { AuthProvider, useAuth } from "@/lib/auth"
import { TooltipProvider } from "@/components/ui/tooltip"
import { Toaster } from "@/components/ui/sonner"
import { AppShell } from "@/components/app-shell"

export interface RouterContext {
  queryClient: QueryClient
}

export const Route = createRootRouteWithContext<RouterContext>()({
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { name: "color-scheme", content: "light dark" },
      { title: "Runway — Self-Hosted GitHub Runner Moderator" },
    ],
    links: [{ rel: "stylesheet", href: appCss }],
  }),
  component: RootLayout,
  notFoundComponent: () => (
    <main className="mx-auto max-w-lg p-8 text-center space-y-2">
      <h1 className="font-heading text-2xl font-bold">404</h1>
      <p className="text-xs text-muted-foreground">The requested view could not be found.</p>
    </main>
  ),
  shellComponent: RootDocument,
})

function RootDocument({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className="dark">
      <head>
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}

function AuthGate({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth()
  const location = useLocation()
  const navigate = useNavigate()
  const isLogin = location.pathname === "/login"

  useEffect(() => {
    if (!loading && !user && !isLogin) {
      void navigate({ to: "/login" })
    }
    if (!loading && user && isLogin) {
      void navigate({ to: "/" })
    }
  }, [loading, user, isLogin, navigate])

  if (loading) {
    return (
      <div className="min-h-dvh flex items-center justify-center bg-background text-xs text-muted-foreground">
        Loading Runway control plane...
      </div>
    )
  }

  if (isLogin) {
    return <>{children}</>
  }

  if (!user) {
    return null
  }

  return <AppShell>{children}</AppShell>
}

function RootLayout() {
  const { queryClient } = Route.useRouteContext()

  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <AuthProvider>
          <TooltipProvider>
            <AuthGate>
              <Outlet />
            </AuthGate>
            <Toaster position="bottom-right" />
          </TooltipProvider>
        </AuthProvider>
      </ThemeProvider>
      {import.meta.env.DEV && <ReactQueryDevtools buttonPosition="bottom-left" />}
    </QueryClientProvider>
  )
}
