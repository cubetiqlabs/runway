import { useCallback, useEffect, useState } from "react"
import { createFileRoute, Link } from "@tanstack/react-router"
import { HugeiconsIcon } from "@hugeicons/react"
import { RefreshIcon, ServerStack02Icon, WorkflowSquare06Icon, Shield01Icon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { StatusBadge } from "@/components/status-badge"
import { ResponsiveTable } from "@/components/responsive-table"
import { api } from "@/lib/api"
import type { DashboardOverview } from "@/lib/types"
import { toast } from "sonner"

export const Route = createFileRoute("/")({
  component: OverviewPage,
})

function OverviewPage() {
  const [data, setData] = useState<DashboardOverview | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    try {
      const ov = await api.overview()
      setData(ov)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load overview")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
    const timer = window.setInterval(() => void load(), 15000)
    return () => window.clearInterval(timer)
  }, [load])

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold tracking-tight">Fleet Overview</h1>
          <p className="text-xs text-muted-foreground">
            Real-time self-hosted runner capacity, moderation queue, and GitHub webhook throughput.
          </p>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={() => void load()}>
          <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
          <span>Refresh</span>
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
                {data.pool_utilization.length} configured runner pools
              </CardContent>
            </Card>
          </>
        )}
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
          {!data || data.pool_utilization.length === 0 ? (
            <p className="text-xs text-muted-foreground">No runner pools configured yet.</p>
          ) : (
            <div className="grid gap-3 md:grid-cols-2">
              {data.pool_utilization.map((pool) => {
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
            <ResponsiveTable
              id="overview-jobs-table"
              caption="Recent moderated GitHub Actions workflow jobs"
              headers={["Repository", "Job", "Decision", "Status"]}
              empty={!data || data.recent_jobs.length === 0}
              emptyMessage="No workflow_job webhooks received yet."
            >
              {data?.recent_jobs.map((job) => (
                <tr key={job.id}>
                  <th scope="row" className="max-w-44 truncate">
                    {job.repo_full_name}
                  </th>
                  <td data-label="Job" className="max-w-36 truncate">
                    {job.job_name}
                  </td>
                  <td data-label="Decision">
                    <StatusBadge status={job.moderation_decision} />
                  </td>
                  <td data-label="Status">
                    <StatusBadge status={job.status} />
                  </td>
                </tr>
              ))}
            </ResponsiveTable>
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
            <ResponsiveTable
              id="overview-webhooks-table"
              caption="Recent GitHub App webhook deliveries"
              headers={["Event", "Repository", "Status", "Latency"]}
              empty={!data || data.recent_deliveries.length === 0}
              emptyMessage="No webhook deliveries logged yet."
            >
              {data?.recent_deliveries.map((d) => (
                <tr key={d.delivery_id}>
                  <th scope="row">
                    {d.event_name}
                    {d.action ? `.${d.action}` : ""}
                  </th>
                  <td data-label="Repository" className="max-w-40 truncate">
                    {d.repo_full_name || "—"}
                  </td>
                  <td data-label="Status">
                    <StatusBadge status={d.status} />
                  </td>
                  <td data-label="Latency" className="text-muted-foreground">
                    {d.processing_ms} ms
                  </td>
                </tr>
              ))}
            </ResponsiveTable>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
