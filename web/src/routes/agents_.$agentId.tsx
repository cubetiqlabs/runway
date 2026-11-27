import { useCallback, useEffect, useMemo, useState } from "react"
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  ArrowLeft01Icon,
  RefreshIcon,
  PauseCircleIcon,
  PlayIcon,
  Delete02Icon,
  Key01Icon,
  Copy01Icon,
} from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { DataTable, type DataTableColumn, type SortDirection } from "@/components/data-table"
import { StatusBadge } from "@/components/status-badge"
import { api } from "@/lib/api"
import type { AgentDetail, AgentMetricPoint, AgentTask, EventRecord, RegisterAgentResponse } from "@/lib/types"
import { toast } from "sonner"

export const Route = createFileRoute("/agents_/$agentId")({
  component: AgentDetailPage,
})

function formatDuration(sec?: number): string {
  if (!sec || sec <= 0) return "—"
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

function formatMS(ms?: number): string {
  if (!ms || ms <= 0) return "—"
  if (ms < 1000) return `${ms} ms`
  const s = Math.round(ms / 100) / 10
  if (s < 60) return `${s}s`
  return `${Math.floor(s / 60)}m ${Math.round(s % 60)}s`
}

function MetricChart({
  title,
  unit,
  color,
  points,
  getValue,
  maxScale = 100,
}: {
  title: string
  unit: string
  color: string
  points: AgentMetricPoint[]
  getValue: (pt: AgentMetricPoint) => number
  maxScale?: number
}) {
  const latest = points.length > 0 ? getValue(points[points.length - 1]) : 0
  const peak = points.reduce((acc, p) => Math.max(acc, getValue(p)), 0)
  const width = 420
  const height = 96
  const padX = 4
  const padY = 8

  const coords = points.map((pt, i) => {
    const x =
      points.length <= 1
        ? width / 2
        : padX + (i / (points.length - 1)) * (width - padX * 2)
    const v = Math.min(maxScale, Math.max(0, getValue(pt)))
    const y = height - padY - (v / Math.max(1, maxScale)) * (height - padY * 2)
    return `${x.toFixed(1)},${y.toFixed(1)}`
  })
  const linePath = coords.length > 0 ? `M ${coords.join(" L ")}` : ""
  const areaPath =
    coords.length > 1
      ? `${linePath} L ${(width - padX).toFixed(1)},${height - padY} L ${padX},${height - padY} Z`
      : ""

  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between gap-2">
          <CardDescription>{title}</CardDescription>
          <span className="font-mono text-sm font-semibold">
            {latest.toFixed(1)}
            {unit}
          </span>
        </div>
      </CardHeader>
      <CardContent className="space-y-1.5">
        {points.length === 0 ? (
          <div className="flex h-24 items-center justify-center border border-dashed border-border text-[11px] text-muted-foreground">
            Waiting for telemetry samples
          </div>
        ) : (
          <svg
            viewBox={`0 0 ${width} ${height}`}
            className="h-24 w-full overflow-visible border border-border/60 bg-muted/20"
            role="img"
            aria-label={`${title} history chart`}
          >
            {areaPath && <path d={areaPath} fill={color} fillOpacity={0.14} />}
            {linePath && (
              <path d={linePath} fill="none" stroke={color} strokeWidth={2} strokeLinejoin="round" />
            )}
          </svg>
        )}
        <div className="flex items-center justify-between text-[10px] text-muted-foreground">
          <span>{points.length} samples</span>
          <span>
            Peak: {peak.toFixed(1)}
            {unit}
          </span>
        </div>
      </CardContent>
    </Card>
  )
}

function AgentDetailPage() {
  const { agentId } = Route.useParams()
  const navigate = useNavigate()

  const [detail, setDetail] = useState<AgentDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [windowRange, setWindowRange] = useState("1h")

  // Tasks table state
  const [tasks, setTasks] = useState<AgentTask[]>([])
  const [tasksTotal, setTasksTotal] = useState(0)
  const [tasksLoading, setTasksLoading] = useState(false)
  const [taskPage, setTaskPage] = useState(1)
  const [taskPageSize, setTaskPageSize] = useState(10)
  const [taskSearch, setTaskSearch] = useState("")
  const [taskStatus, setTaskStatus] = useState("all")
  const [taskSortBy, setTaskSortBy] = useState("created_at")
  const [taskSortDir, setTaskSortDir] = useState<SortDirection>("desc")

  // Events table state
  const [events, setEvents] = useState<EventRecord[]>([])
  const [eventsTotal, setEventsTotal] = useState(0)
  const [eventsLoading, setEventsLoading] = useState(false)
  const [eventPage, setEventPage] = useState(1)
  const [eventPageSize, setEventPageSize] = useState(10)
  const [eventSearch, setEventSearch] = useState("")

  const [issued, setIssued] = useState<RegisterAgentResponse | null>(null)
  const [deleteOpen, setDeleteOpen] = useState(false)

  const loadDetail = useCallback(async () => {
    try {
      const res = await api.getAgent(agentId, windowRange)
      setDetail(res)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load agent details")
    } finally {
      setLoading(false)
    }
  }, [agentId, windowRange])

  const loadTasks = useCallback(async () => {
    setTasksLoading(true)
    try {
      const res = await api.listAgentTasks(agentId, {
        limit: taskPageSize,
        page: taskPage,
        status: taskStatus,
        search: taskSearch.trim() || undefined,
        sort_by: taskSortBy,
        sort_dir: taskSortDir,
      })
      setTasks(res.items)
      setTasksTotal(res.total)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load agent tasks")
    } finally {
      setTasksLoading(false)
    }
  }, [agentId, taskPage, taskPageSize, taskStatus, taskSearch, taskSortBy, taskSortDir])

  const loadEvents = useCallback(async () => {
    setEventsLoading(true)
    try {
      const res = await api.listEvents({
        resource_id: agentId,
        limit: eventPageSize,
        page: eventPage,
        search: eventSearch.trim() || undefined,
      })
      setEvents(res.items)
      setEventsTotal(res.total)
    } catch {
      // ignore if eventstore disabled
    } finally {
      setEventsLoading(false)
    }
  }, [agentId, eventPage, eventPageSize, eventSearch])

  useEffect(() => {
    void loadDetail()
  }, [loadDetail])

  useEffect(() => {
    void loadTasks()
  }, [loadTasks])

  useEffect(() => {
    void loadEvents()
  }, [loadEvents])

  useEffect(() => {
    const timer = window.setInterval(() => {
      void loadDetail()
      void loadTasks()
    }, 8000)
    return () => window.clearInterval(timer)
  }, [loadDetail, loadTasks])

  const taskColumns: DataTableColumn<AgentTask>[] = useMemo(
    () => [
      {
        id: "runner_name",
        header: "Runner",
        sortable: true,
        isRowHeader: true,
        cell: (t) => <span className="font-mono text-[11px]">{t.runner_name}</span>,
      },
      {
        id: "repo_full_name",
        header: "Repository",
        sortable: true,
        cell: (t) => (
          <div>
            <span className="text-xs font-medium">{t.repo_full_name || "—"}</span>
            {t.is_fork_pr && (
              <Badge variant="destructive" className="ml-1.5 text-[10px]">
                Fork PR
              </Badge>
            )}
          </div>
        ),
      },
      {
        id: "build_profile",
        header: "Profile",
        cell: (t) => (
          <Badge variant="outline" className="capitalize text-[10px]">
            {t.build_profile || "standard"}
          </Badge>
        ),
      },
      {
        id: "status",
        header: "Status",
        sortable: true,
        cell: (t) => <StatusBadge status={t.status} />,
      },
      {
        id: "duration",
        header: "Duration",
        cell: (t) => {
          if (!t.completed_at || !t.claimed_at) return <span className="text-muted-foreground">—</span>
          const ms = new Date(t.completed_at).getTime() - new Date(t.claimed_at).getTime()
          return <span className="font-mono text-[11px]">{formatMS(ms)}</span>
        },
      },
      {
        id: "created_at",
        header: "Created",
        sortable: true,
        cell: (t) => (
          <span className="text-[11px] text-muted-foreground whitespace-nowrap">
            {new Date(t.created_at).toLocaleString()}
          </span>
        ),
      },
    ],
    []
  )

  const eventColumns: DataTableColumn<EventRecord>[] = useMemo(
    () => [
      {
        id: "occurred_at",
        header: "Time",
        isRowHeader: true,
        cell: (ev) => (
          <span className="font-mono text-[11px] text-muted-foreground whitespace-nowrap">
            {new Date(ev.occurred_at).toLocaleString()}
          </span>
        ),
      },
      {
        id: "event_type",
        header: "Event",
        cell: (ev) => <span className="font-mono text-[11px]">{ev.event_type}</span>,
      },
      {
        id: "severity",
        header: "Severity",
        cell: (ev) => <StatusBadge status={ev.severity} />,
      },
      {
        id: "message",
        header: "Message",
        cell: (ev) => (
          <span className="text-xs max-w-md truncate block" title={ev.message}>
            {ev.message || "—"}
          </span>
        ),
      },
    ],
    []
  )

  if (loading && !detail) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-10 w-64" />
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-24 w-full" />
          ))}
        </div>
      </div>
    )
  }

  if (!detail) {
    return (
      <div className="space-y-4">
        <Link to="/agents" className="inline-flex items-center gap-1 text-xs text-primary hover:underline">
          <HugeiconsIcon icon={ArrowLeft01Icon} strokeWidth={2} className="size-3.5" />
          <span>Back to Agents</span>
        </Link>
        <Card>
          <CardContent className="p-6 text-xs text-muted-foreground">Agent not found.</CardContent>
        </Card>
      </div>
    )
  }

  const { agent, stats, metric_history } = detail
  const m = agent.metrics ?? {}

  const handleDrain = async () => {
    try {
      const res = await api.drainAgent(agent.id)
      toast.success(`Agent ${agent.name} status: ${res.status}`)
      void loadDetail()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Drain failed")
    }
  }

  const handleResume = async () => {
    try {
      const res = await api.resumeAgent(agent.id)
      toast.success(`Agent ${agent.name} resumed (${res.status})`)
      void loadDetail()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Resume failed")
    }
  }

  const handleRotate = async () => {
    try {
      const res = await api.rotateAgentToken(agent.id)
      setIssued(res)
      toast.success("Agent token rotated")
      void loadDetail()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Token rotation failed")
    }
  }

  const confirmDelete = async () => {
    try {
      await api.deleteAgent(agent.id)
      toast.success(`Deleted agent ${agent.name}`)
      await navigate({ to: "/agents" })
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Delete failed")
    }
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-1">
          <Link to="/agents" className="inline-flex items-center gap-1 text-xs text-primary hover:underline">
            <HugeiconsIcon icon={ArrowLeft01Icon} strokeWidth={2} className="size-3.5" />
            <span>Agents</span>
          </Link>
          <div className="flex flex-wrap items-center gap-2.5">
            <h1 className="font-heading text-xl font-semibold">{agent.name}</h1>
            <StatusBadge status={agent.status} />
            <Badge variant="secondary" className="capitalize text-[10px]">
              {agent.runtime}
            </Badge>
            {agent.sandbox_runtime && (
              <Badge variant="outline" className="font-mono text-[10px]">
                sandbox: {agent.sandbox_runtime}
              </Badge>
            )}
          </div>
          <p className="text-xs text-muted-foreground font-mono">
            {agent.hostname || agent.token_prefix} · {agent.os}/{agent.arch} · v{agent.version || "dev"}
            {agent.pool_name ? ` · Pool: ${agent.pool_name}` : ""}
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Select value={windowRange} onValueChange={(v) => setWindowRange(v || "1h")}>
            <SelectTrigger aria-label="Telemetry time window" className="h-8 w-28 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="15m">Last 15m</SelectItem>
              <SelectItem value="1h">Last 1h</SelectItem>
              <SelectItem value="6h">Last 6h</SelectItem>
              <SelectItem value="24h">Last 24h</SelectItem>
            </SelectContent>
          </Select>
          <Button type="button" variant="outline" size="sm" onClick={() => void loadDetail()}>
            <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
            <span>Refresh</span>
          </Button>
          {agent.status === "draining" || agent.status === "drained" ? (
            <Button type="button" variant="outline" size="sm" onClick={() => void handleResume()}>
              <HugeiconsIcon icon={PlayIcon} strokeWidth={2} />
              <span>Resume</span>
            </Button>
          ) : (
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={agent.status === "offline"}
              onClick={() => void handleDrain()}
            >
              <HugeiconsIcon icon={PauseCircleIcon} strokeWidth={2} />
              <span>Drain</span>
            </Button>
          )}
          <Button type="button" variant="outline" size="sm" onClick={() => void handleRotate()}>
            <HugeiconsIcon icon={Key01Icon} strokeWidth={2} />
            <span>Rotate Token</span>
          </Button>
          <Button type="button" variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}>
            <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
            <span>Delete</span>
          </Button>
        </div>
      </div>

      {/* KPI Strip */}
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="pb-1">
            <CardDescription>CPU &amp; Load</CardDescription>
            <CardTitle className="text-2xl font-bold">
              {Number(m.cpu_usage_percent ?? 0).toFixed(1)}%
            </CardTitle>
          </CardHeader>
          <CardContent className="text-[11px] text-muted-foreground">
            {agent.cpu_count} vCPU · load {Number(m.load_avg_1 ?? 0).toFixed(2)} /{" "}
            {Number(m.load_avg_5 ?? 0).toFixed(2)}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-1">
            <CardDescription>Memory</CardDescription>
            <CardTitle className="text-2xl font-bold">
              {Number(m.memory_usage_percent ?? 0).toFixed(1)}%
            </CardTitle>
          </CardHeader>
          <CardContent className="text-[11px] text-muted-foreground">
            {m.memory_used_mb ?? 0} MB / {m.memory_total_mb || agent.memory_mb || 0} MB
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-1">
            <CardDescription>Disk &amp; Cache</CardDescription>
            <CardTitle className="text-2xl font-bold">
              {Number(m.disk_usage_percent ?? 0).toFixed(1)}%
            </CardTitle>
          </CardHeader>
          <CardContent className="text-[11px] text-muted-foreground">
            {Number(m.disk_free_gb ?? 0).toFixed(1)} GB free · Cache {m.cache_size_mb ?? 0} MB (
            {m.cache_hit_count ?? 0} hits)
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-1">
            <CardDescription>Task Throughput</CardDescription>
            <CardTitle className="text-2xl font-bold">
              {agent.active_jobs} / {agent.max_concurrency}
            </CardTitle>
          </CardHeader>
          <CardContent className="text-[11px] text-muted-foreground">
            {stats.total_tasks} tasks ({stats.success_rate.toFixed(0)}% pass) · avg{" "}
            {formatMS(stats.avg_duration_ms)}
          </CardContent>
        </Card>
      </div>

      {/* Telemetry Charts */}
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        <MetricChart
          title="CPU Usage"
          unit="%"
          color="var(--chart-1)"
          points={metric_history}
          getValue={(p) => p.cpu_usage_percent}
        />
        <MetricChart
          title="Memory Usage"
          unit="%"
          color="var(--chart-2)"
          points={metric_history}
          getValue={(p) => p.memory_usage_percent}
        />
        <MetricChart
          title="Disk Usage"
          unit="%"
          color="var(--chart-3)"
          points={metric_history}
          getValue={(p) => p.disk_usage_percent}
        />
        <MetricChart
          title="Active Jobs"
          unit=""
          color="var(--chart-4)"
          points={metric_history}
          getValue={(p) => p.active_jobs}
          maxScale={Math.max(1, agent.max_concurrency)}
        />
      </div>

      {/* Host Info */}
      <Card>
        <CardHeader>
          <CardTitle>Host Configuration &amp; Runtime</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4 text-xs">
          <div>
            <span className="text-muted-foreground block">Token Prefix</span>
            <span className="font-mono">{agent.token_prefix}...</span>
          </div>
          <div>
            <span className="text-muted-foreground block">Agent / Host Uptime</span>
            <span className="font-mono">
              {formatDuration(m.uptime_sec)} / {formatDuration(m.host_uptime_sec)}
            </span>
          </div>
          <div>
            <span className="text-muted-foreground block">24h Tasks / P95 Duration</span>
            <span className="font-mono">
              {stats.tasks_24h} runs · p95 {formatMS(stats.p95_duration_ms)}
            </span>
          </div>
          <div>
            <span className="text-muted-foreground block">Labels</span>
            <div className="flex flex-wrap gap-1 mt-0.5">
              {agent.labels.length === 0 ? (
                <span className="text-muted-foreground">Default</span>
              ) : (
                agent.labels.map((l) => (
                  <Badge key={l} variant="secondary" className="font-mono text-[10px]">
                    {l}
                  </Badge>
                ))
              )}
            </div>
          </div>
        </CardContent>
      </Card>

      <Tabs defaultValue="tasks">
        <TabsList>
          <TabsTrigger value="tasks">Execution Tasks ({tasksTotal})</TabsTrigger>
          <TabsTrigger value="events">Agent Events ({eventsTotal})</TabsTrigger>
        </TabsList>

        <TabsContent value="tasks" className="pt-2">
          <DataTable
            id={`agent-tasks-${agent.id}`}
            caption="Tasks executed by this agent"
            data={tasks}
            columns={taskColumns}
            getRowId={(t) => t.id}
            loading={tasksLoading}
            emptyMessage="No tasks executed by this agent yet."
            searchValue={taskSearch}
            onSearchChange={(val) => {
              setTaskPage(1)
              setTaskSearch(val)
            }}
            searchPlaceholder="Search repository or runner..."
            filters={[
              {
                id: "status",
                label: "Status",
                value: taskStatus,
                onChange: (v) => {
                  setTaskPage(1)
                  setTaskStatus(v)
                },
                options: [
                  { label: "All statuses", value: "all" },
                  { label: "Running", value: "running" },
                  { label: "Claimed", value: "claimed" },
                  { label: "Completed", value: "completed" },
                  { label: "Failed", value: "failed" },
                  { label: "Cancelled", value: "cancelled" },
                ],
              },
            ]}
            sortBy={taskSortBy}
            sortDir={taskSortDir}
            onSortChange={(col, dir) => {
              setTaskPage(1)
              setTaskSortBy(col)
              setTaskSortDir(dir)
            }}
            pagination={{
              page: taskPage,
              pageSize: taskPageSize,
              total: tasksTotal,
              onPageChange: setTaskPage,
              onPageSizeChange: (n) => {
                setTaskPage(1)
                setTaskPageSize(n)
              },
            }}
          />
        </TabsContent>

        <TabsContent value="events" className="pt-2">
          <DataTable
            id={`agent-events-${agent.id}`}
            caption="EventStore records for this agent"
            data={events}
            columns={eventColumns}
            getRowId={(ev) => ev.id}
            loading={eventsLoading}
            emptyMessage="No events recorded for this agent yet."
            searchValue={eventSearch}
            onSearchChange={(val) => {
              setEventPage(1)
              setEventSearch(val)
            }}
            searchPlaceholder="Search agent events..."
            pagination={{
              page: eventPage,
              pageSize: eventPageSize,
              total: eventsTotal,
              onPageChange: setEventPage,
              onPageSizeChange: (n) => {
                setEventPage(1)
                setEventPageSize(n)
              },
            }}
          />
        </TabsContent>
      </Tabs>

      <Dialog open={Boolean(issued)} onOpenChange={(o) => !o && setIssued(null)}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>New Agent Token Issued</DialogTitle>
            <DialogDescription>Copy this token now—it will not be shown again.</DialogDescription>
          </DialogHeader>
          {issued && (
            <div className="space-y-2 text-xs">
              <div className="flex justify-end">
                <Button
                  type="button"
                  size="xs"
                  variant="outline"
                  onClick={() => void navigator.clipboard.writeText(issued.token).then(() => toast.success("Copied token"))}
                >
                  <HugeiconsIcon icon={Copy01Icon} strokeWidth={2} />
                  <span>Copy</span>
                </Button>
              </div>
              <pre className="overflow-x-auto border border-border bg-muted/50 p-2 font-mono text-[11px]">
                {issued.token}
              </pre>
            </div>
          )}
          <DialogFooter>
            <Button type="button" onClick={() => setIssued(null)}>
              Done
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete Agent?</AlertDialogTitle>
            <AlertDialogDescription>
              Revoke credentials and remove <strong>{agent.name}</strong> from Runway?
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => void confirmDelete()}>
              Delete Agent
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
