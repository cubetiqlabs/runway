import { useCallback, useEffect, useMemo, useState } from "react"
import { createFileRoute } from "@tanstack/react-router"
import { HugeiconsIcon } from "@hugeicons/react"
import { RefreshIcon, ViewIcon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { DataTable, type DataTableColumn, type SortDirection } from "@/components/data-table"
import { StatusBadge } from "@/components/status-badge"
import { api } from "@/lib/api"
import type { EventRecord, EventSummary } from "@/lib/types"
import { toast } from "sonner"

export const Route = createFileRoute("/events")({
  component: EventsPage,
})

function EventsPage() {
  const [events, setEvents] = useState<EventRecord[]>([])
  const [total, setTotal] = useState(0)
  const [summary, setSummary] = useState<EventSummary | null>(null)
  const [loading, setLoading] = useState(true)

  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(25)
  const [search, setSearch] = useState("")
  const [category, setCategory] = useState("all")
  const [severity, setSeverity] = useState("all")
  const [windowRange, setWindowRange] = useState("24h")
  const [sortBy, setSortBy] = useState("occurred_at")
  const [sortDir, setSortDir] = useState<SortDirection>("desc")

  const [selectedEvent, setSelectedEvent] = useState<EventRecord | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [evRes, sumRes] = await Promise.all([
        api.listEvents({
          limit: pageSize,
          page,
          category,
          severity,
          window: windowRange,
          search: search.trim() || undefined,
          sort_by: sortBy,
          sort_dir: sortDir,
        }),
        api.eventSummary(windowRange),
      ])
      setEvents(evRes.items)
      setTotal(evRes.total)
      setSummary(sumRes)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load EventStore")
    } finally {
      setLoading(false)
    }
  }, [page, pageSize, category, severity, windowRange, search, sortBy, sortDir])

  useEffect(() => {
    void load()
  }, [load])

  const columns: DataTableColumn<EventRecord>[] = useMemo(
    () => [
      {
        id: "occurred_at",
        header: "Occurred At",
        sortable: true,
        isRowHeader: true,
        cell: (ev) => (
          <span className="font-mono text-[11px] text-muted-foreground whitespace-nowrap">
            {new Date(ev.occurred_at).toLocaleString()}
          </span>
        ),
      },
      {
        id: "category",
        header: "Category",
        sortable: true,
        cell: (ev) => (
          <Badge variant="secondary" className="font-mono text-[10px]">
            {ev.category}
          </Badge>
        ),
      },
      {
        id: "event_type",
        header: "Event Type",
        sortable: true,
        cell: (ev) => <span className="font-mono text-[11px]">{ev.event_type}</span>,
      },
      {
        id: "severity",
        header: "Severity",
        sortable: true,
        cell: (ev) => <StatusBadge status={ev.severity} />,
      },
      {
        id: "resource_type",
        header: "Resource",
        sortable: true,
        cell: (ev) => (
          <span className="font-mono text-[11px] text-muted-foreground">
            {ev.resource_type ? `${ev.resource_type}:${ev.resource_id}` : "—"}
          </span>
        ),
      },
      {
        id: "repo_full_name",
        header: "Repository",
        sortable: true,
        defaultVisible: true,
        cell: (ev) => <span className="text-xs">{ev.repo_full_name || "—"}</span>,
      },
      {
        id: "duration_ms",
        header: "Duration",
        sortable: true,
        defaultVisible: false,
        cell: (ev) => (
          <span className="font-mono text-[11px] text-muted-foreground">
            {ev.duration_ms > 0 ? `${ev.duration_ms} ms` : "—"}
          </span>
        ),
      },
      {
        id: "message",
        header: "Message",
        cell: (ev) => (
          <span className="text-xs max-w-xs truncate block" title={ev.message}>
            {ev.message || "—"}
          </span>
        ),
      },
      {
        id: "actions",
        header: "Inspect",
        hideable: false,
        cell: (ev) => (
          <Button type="button" size="xs" variant="outline" onClick={() => setSelectedEvent(ev)}>
            <HugeiconsIcon icon={ViewIcon} strokeWidth={2} />
            <span>JSON</span>
          </Button>
        ),
      },
    ],
    []
  )

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">Event Store Explorer</h1>
          <p className="text-xs text-muted-foreground">
            Unified, high-throughput event stream across webhooks, audit logs, runner logs, and agent telemetry.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Select value={windowRange} onValueChange={(v) => { setPage(1); setWindowRange(v || "24h") }}>
            <SelectTrigger aria-label="Event time window" className="h-8 w-28 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="1h">Last 1h</SelectItem>
              <SelectItem value="6h">Last 6h</SelectItem>
              <SelectItem value="24h">Last 24h</SelectItem>
              <SelectItem value="168h">Last 7d</SelectItem>
            </SelectContent>
          </Select>
          <Button type="button" variant="outline" size="sm" onClick={() => void load()}>
            <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
            <span>Refresh</span>
          </Button>
        </div>
      </div>

      {summary && (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Card>
            <CardHeader className="pb-1">
              <CardDescription>Storage Backend</CardDescription>
              <CardTitle className="text-xl font-bold capitalize">{summary.driver}</CardTitle>
            </CardHeader>
            <CardContent className="text-[11px] text-muted-foreground">
              Total in window: {summary.total_events.toLocaleString()}
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-1">
              <CardDescription>Webhooks &amp; Audits</CardDescription>
              <CardTitle className="text-xl font-bold">
                {((summary.by_category?.webhook ?? 0) + (summary.by_category?.audit ?? 0)).toLocaleString()}
              </CardTitle>
            </CardHeader>
            <CardContent className="text-[11px] text-muted-foreground">
              {summary.by_category?.webhook ?? 0} webhooks · {summary.by_category?.audit ?? 0} audits
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-1">
              <CardDescription>Runner Logs &amp; Metrics</CardDescription>
              <CardTitle className="text-xl font-bold">
                {((summary.by_category?.runner_log ?? 0) + (summary.by_category?.agent_metric ?? 0)).toLocaleString()}
              </CardTitle>
            </CardHeader>
            <CardContent className="text-[11px] text-muted-foreground">
              {summary.by_category?.runner_log ?? 0} logs · {summary.by_category?.agent_metric ?? 0} metrics
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-1">
              <CardDescription>Ingestion Pipeline</CardDescription>
              <CardTitle className="text-xl font-bold">
                {summary.ingestion.flushed.toLocaleString()} flushed
              </CardTitle>
            </CardHeader>
            <CardContent className="text-[11px] text-muted-foreground">
              Queue {summary.ingestion.queue_depth}/{summary.ingestion.buffer_capacity} ·{" "}
              {summary.ingestion.dropped} dropped · {summary.by_severity?.error ?? 0} errors
            </CardContent>
          </Card>
        </div>
      )}

      <DataTable
        id="events-table"
        caption="EventStore unified event stream"
        data={events}
        columns={columns}
        getRowId={(ev) => ev.id}
        loading={loading}
        emptyMessage="No events match the current filters."
        searchValue={search}
        onSearchChange={(val) => {
          setPage(1)
          setSearch(val)
        }}
        searchPlaceholder="Search event type, message, actor, repo..."
        filters={[
          {
            id: "category",
            label: "Category",
            value: category,
            onChange: (v) => {
              setPage(1)
              setCategory(v)
            },
            options: [
              { label: "All categories", value: "all" },
              { label: "Webhooks", value: "webhook" },
              { label: "Audit Logs", value: "audit" },
              { label: "Runner Logs", value: "runner_log" },
              { label: "Runner Lifecycle", value: "runner_lifecycle" },
              { label: "Agent Lifecycle", value: "agent_lifecycle" },
              { label: "Agent Metrics", value: "agent_metric" },
            ],
          },
          {
            id: "severity",
            label: "Severity",
            value: severity,
            onChange: (v) => {
              setPage(1)
              setSeverity(v)
            },
            options: [
              { label: "All severities", value: "all" },
              { label: "Info", value: "info" },
              { label: "Warn", value: "warn" },
              { label: "Error", value: "error" },
            ],
          },
        ]}
        onResetFilters={() => {
          setPage(1)
          setSearch("")
          setCategory("all")
          setSeverity("all")
        }}
        sortBy={sortBy}
        sortDir={sortDir}
        onSortChange={(col, dir) => {
          setPage(1)
          setSortBy(col)
          setSortDir(dir)
        }}
        pagination={{
          page,
          pageSize,
          total,
          onPageChange: setPage,
          onPageSizeChange: (n) => {
            setPage(1)
            setPageSize(n)
          },
        }}
      />

      <Dialog open={Boolean(selectedEvent)} onOpenChange={(o) => !o && setSelectedEvent(null)}>
        <DialogContent className="sm:max-w-2xl max-h-[85dvh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="font-mono text-sm">{selectedEvent?.event_type}</DialogTitle>
            <DialogDescription>
              {selectedEvent?.category} · {selectedEvent ? new Date(selectedEvent.occurred_at).toLocaleString() : ""}
            </DialogDescription>
          </DialogHeader>
          {selectedEvent && (
            <div className="space-y-3 text-xs">
              <div className="grid gap-2 sm:grid-cols-2 border border-border p-3">
                <div>
                  <span className="text-muted-foreground block">Actor</span>
                  <span className="font-mono">{selectedEvent.actor_label || "system"}</span>
                </div>
                <div>
                  <span className="text-muted-foreground block">Resource</span>
                  <span className="font-mono">
                    {selectedEvent.resource_type}:{selectedEvent.resource_id}
                  </span>
                </div>
                <div>
                  <span className="text-muted-foreground block">Correlation ID</span>
                  <span className="font-mono break-all">{selectedEvent.correlation_id || "—"}</span>
                </div>
                <div>
                  <span className="text-muted-foreground block">Repository / IP</span>
                  <span className="font-mono">
                    {selectedEvent.repo_full_name || "—"} · {selectedEvent.ip_address || "—"}
                  </span>
                </div>
              </div>
              {selectedEvent.message && (
                <div>
                  <span className="text-muted-foreground block mb-1">Message</span>
                  <div className="border border-border bg-muted/30 p-2 font-mono text-[11px] break-all">
                    {selectedEvent.message}
                  </div>
                </div>
              )}
              <div>
                <span className="text-muted-foreground block mb-1">Payload</span>
                <pre className="overflow-x-auto border border-border bg-muted/50 p-3 font-mono text-[11px]">
                  {JSON.stringify(selectedEvent.payload, null, 2)}
                </pre>
              </div>
            </div>
          )}
          <DialogFooter>
            <Button type="button" onClick={() => setSelectedEvent(null)}>
              Close
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
