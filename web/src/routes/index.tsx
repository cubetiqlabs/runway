import { useEffect, useId, useMemo } from "react"
import { createFileRoute, Link } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { HugeiconsIcon } from "@hugeicons/react"
import { RefreshIcon, ServerStack02Icon, WorkflowSquare06Icon, Shield01Icon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import {
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { AreaLineMetricChart, CategoryBarChart } from "@/components/ui/chart"
import { StatusBadge } from "@/components/status-badge"
import { overviewQueryOptions } from "@/lib/query"
import { useAppSelector } from "@/lib/store"
import { toast } from "sonner"

export const Route = createFileRoute("/")({
  component: OverviewPage,
})

function OverviewPage() {
  const livePolling = useAppSelector((s) => s.livePolling)
  const jobsCaptionId = useId()
  const hooksCaptionId = useId()

  const overviewQuery = useQuery({
    ...overviewQueryOptions(),
    refetchInterval: livePolling ? 15_000 : false,
  })

  useEffect(() => {
    if (overviewQuery.error) {
      toast.error(
        overviewQuery.error instanceof Error
          ? overviewQuery.error.message
          : "Failed to load overview"
      )
    }
  }, [overviewQuery.error])

  const data = overviewQuery.data
  const loading = overviewQuery.isPending
  const pools = data?.pool_utilization ?? []
  const recentJobs = data?.recent_jobs ?? []
  const recentDeliveries = data?.recent_deliveries ?? []

  const poolChartData = useMemo(
    () =>
      pools.map((p) => ({
        category: p.name.length > 16 ? `${p.name.slice(0, 14)}…` : p.name,
        value: p.online_runners + p.busy_runners,
      })),
    [pools]
  )

  const webhookLatencyData = useMemo(() => {
    const items = [...recentDeliveries].reverse()
    return items.map((d, idx) => ({
      label: d.received_at
        ? new Date(d.received_at).toLocaleTimeString([], {
            hour: "2-digit",
            minute: "2-digit",
            second: "2-digit",
          })
        : `#${idx + 1}`,
      value: d.processing_ms,
    }))
  }, [recentDeliveries])

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold tracking-tight">Fleet Overview</h1>
          <p className="text-xs text-muted-foreground">
            Real-time self-hosted runner capacity, moderation queue, and GitHub webhook throughput.
          </p>
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={overviewQuery.isFetching}
          onClick={() => void overviewQuery.refetch()}
        >
          <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
          <span>{overviewQuery.isFetching ? "Refreshing..." : "Refresh"}</span>
        </Button>
      </div>

      {/* KPI Grid */}
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {loading || !data ? (
          Array.from({ length: 4 }).map((_, idx) => <Skeleton key={idx} className="h-24 w-full" />)
        ) : (
          <>
            <Card>
              <CardHeader className="pb-1">
                <CardDescription>Active Runners</CardDescription>
                <CardTitle className="text-2xl font-bold">
                  {data.online_runners + data.busy_runners}
                </CardTitle>
              </CardHeader>
              <CardContent className="text-[11px] text-muted-foreground">
                {data.online_runners} online · {data.busy_runners} busy ·{" "}
                {data.provisioning_runners} provisioning
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-1">
                <CardDescription>Moderation Queue</CardDescription>
                <CardTitle className="text-2xl font-bold">{data.awaiting_approval}</CardTitle>
              </CardHeader>
              <CardContent className="text-[11px] text-muted-foreground">
                {data.active_jobs} active jobs · {data.denied_jobs_24h} denied (24h)
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-1">
                <CardDescription>Avg Queue Latency</CardDescription>
                <CardTitle className="text-2xl font-bold">{data.avg_queue_latency_ms} ms</CardTitle>
              </CardHeader>
              <CardContent className="text-[11px] text-muted-foreground">
                {data.completed_jobs_24h} jobs completed in last 24h
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-1">
                <CardDescription>GitHub Installations</CardDescription>
                <CardTitle className="text-2xl font-bold">{data.installations}</CardTitle>
              </CardHeader>
              <CardContent className="text-[11px] text-muted-foreground">
                {pools.length} configured runner pools
              </CardContent>
            </Card>
          </>
        )}
      </div>

      {/* TanStack Charts Telemetry Grid */}
      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle>Active Runners by Pool</CardTitle>
            <CardDescription>Concurrent online &amp; busy runners per profile</CardDescription>
          </CardHeader>
          <CardContent>
            <CategoryBarChart
              data={poolChartData}
              ariaLabel="Active runners by runner pool bar chart"
              color="var(--chart-3)"
              height={180}
              emptyMessage="No runner pool utilization samples yet"
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle>Webhook Dispatch Latency (ms)</CardTitle>
            <CardDescription>Processing time across recent GitHub App deliveries</CardDescription>
          </CardHeader>
          <CardContent>
            <AreaLineMetricChart
              data={webhookLatencyData}
              ariaLabel="Webhook processing latency area chart"
              color="var(--chart-2)"
              height={180}
              emptyMessage="Waiting for GitHub webhook deliveries"
            />
          </CardContent>
        </Card>
      </div>

      {/* Pool Utilization */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between gap-2">
          <div>
            <CardTitle>Runner Pool Capacity</CardTitle>
            <CardDescription>Ephemeral runner allocation across configured machine profiles</CardDescription>
          </div>
          <Link to="/pools" className="text-xs text-primary hover:underline">
            Manage pools
          </Link>
        </CardHeader>
        <CardContent>
          {pools.length === 0 ? (
            <p className="text-xs text-muted-foreground">No runner pools configured yet.</p>
          ) : (
            <div className="grid gap-3 md:grid-cols-2">
              {pools.map((pool) => {
                const used = pool.online_runners + pool.busy_runners
                const pct = Math.min(100, Math.round((used / Math.max(1, pool.max_concurrency)) * 100))
                return (
                  <div key={pool.pool_id} className="border border-border p-3 space-y-2">
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex items-center gap-2 min-w-0">
                        <HugeiconsIcon icon={ServerStack02Icon} strokeWidth={2} className="size-4 text-primary shrink-0" />
                        <span className="font-medium text-xs truncate">{pool.name}</span>
                      </div>
                      <span className="text-[11px] text-muted-foreground">
                        {used} / {pool.max_concurrency}
                      </span>
                    </div>
                    <div className="h-1.5 w-full bg-muted overflow-hidden">
                      <div className="h-full bg-primary transition-all" style={{ width: `${pct}%` }} />
                    </div>
                    <div className="flex items-center justify-between text-[11px] text-muted-foreground">
                      <span>
                        {pool.vcpu} vCPU · {Math.round(pool.memory_mb / 1024)} GB RAM · {pool.os}/{pool.arch}
                      </span>
                      <span>{pool.enabled ? "Enabled" : "Disabled"}</span>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Recent Jobs & Deliveries */}
      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <div className="flex items-center gap-2">
              <HugeiconsIcon icon={WorkflowSquare06Icon} strokeWidth={2} className="size-4 text-primary" />
              <CardTitle>Recent Workflow Jobs</CardTitle>
            </div>
            <Link to="/jobs" className="text-xs text-primary hover:underline">
              View queue
            </Link>
          </CardHeader>
          <CardContent>
            {recentJobs.length === 0 ? (
              <div className="border border-dashed border-border p-6 text-center text-xs text-muted-foreground">
                No workflow_job webhooks received yet.
              </div>
            ) : (
              <div
                tabIndex={0}
                role="region"
                aria-labelledby={jobsCaptionId}
                className="runway-table-region border border-border bg-card focus-visible:outline-2 focus-visible:outline-ring"
              >
                <table className="runway-table">
                  <caption id={jobsCaptionId} className="sr-only">
                    Recent moderated GitHub Actions workflow jobs
                  </caption>
                  <TableHeader className="[&_tr]:border-b-0">
                    <TableRow className="border-b-0 hover:bg-transparent">
                      <TableHead scope="col">Repository</TableHead>
                      <TableHead scope="col">Job</TableHead>
                      <TableHead scope="col">Decision</TableHead>
                      <TableHead scope="col">Status</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {recentJobs.map((job) => (
                      <TableRow key={job.id} className="border-b-0">
                        <th scope="row" className="max-w-44 truncate">
                          {job.repo_full_name}
                        </th>
                        <TableCell data-label="Job" className="max-w-36 truncate">
                          {job.job_name}
                        </TableCell>
                        <TableCell data-label="Decision">
                          <StatusBadge status={job.moderation_decision} />
                        </TableCell>
                        <TableCell data-label="Status">
                          <StatusBadge status={job.status} />
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </table>
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <div className="flex items-center gap-2">
              <HugeiconsIcon icon={Shield01Icon} strokeWidth={2} className="size-4 text-primary" />
              <CardTitle>Recent Webhook Deliveries</CardTitle>
            </div>
            <Link to="/installations" className="text-xs text-primary hover:underline">
              View logs
            </Link>
          </CardHeader>
          <CardContent>
            {recentDeliveries.length === 0 ? (
              <div className="border border-dashed border-border p-6 text-center text-xs text-muted-foreground">
                No webhook deliveries logged yet.
              </div>
            ) : (
              <div
                tabIndex={0}
                role="region"
                aria-labelledby={hooksCaptionId}
                className="runway-table-region border border-border bg-card focus-visible:outline-2 focus-visible:outline-ring"
              >
                <table className="runway-table">
                  <caption id={hooksCaptionId} className="sr-only">
                    Recent GitHub App webhook deliveries
                  </caption>
                  <TableHeader className="[&_tr]:border-b-0">
                    <TableRow className="border-b-0 hover:bg-transparent">
                      <TableHead scope="col">Event</TableHead>
                      <TableHead scope="col">Repository</TableHead>
                      <TableHead scope="col">Status</TableHead>
                      <TableHead scope="col">Latency</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {recentDeliveries.map((d) => (
                      <TableRow key={d.delivery_id} className="border-b-0">
                        <th scope="row">
                          {d.event_name}
                          {d.action ? `.${d.action}` : ""}
                        </th>
                        <TableCell data-label="Repository" className="max-w-40 truncate">
                          {d.repo_full_name || "—"}
                        </TableCell>
                        <TableCell data-label="Status">
                          <StatusBadge status={d.status} />
                        </TableCell>
                        <TableCell data-label="Latency" className="text-muted-foreground">
                          {d.processing_ms} ms
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </table>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
