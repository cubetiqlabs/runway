import { useEffect, useMemo, useState } from "react"
import { createFileRoute } from "@tanstack/react-router"
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  RefreshIcon,
  PauseCircleIcon,
  Delete02Icon,
  File01Icon,
  Copy01Icon,
} from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
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
import { queryKeys, runnerLogsQueryOptions, runnersQueryOptions } from "@/lib/query"
import { useAppSelector } from "@/lib/store"
import type { Runner } from "@/lib/types"
import { toast } from "sonner"

export const Route = createFileRoute("/runners")({
  component: RunnersPage,
})

function RunnersPage() {
  const queryClient = useQueryClient()
  const livePolling = useAppSelector((s) => s.livePolling)

  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(25)
  const [statusFilter, setStatusFilter] = useState<string>("all")
  const [providerFilter, setProviderFilter] = useState<string>("all")
  const [search, setSearch] = useState("")
  const [sortBy, setSortBy] = useState("created_at")
  const [sortDir, setSortDir] = useState<SortDirection>("desc")
  const [confirmTarget, setConfirmTarget] = useState<Runner | null>(null)
  const [logsTarget, setLogsTarget] = useState<Runner | null>(null)

  const queryParams = useMemo(
    () => ({
      status: statusFilter,
      provider: providerFilter,
      search: search.trim() || undefined,
      limit: pageSize,
      page,
      sort_by: sortBy,
      sort_dir: sortDir,
    }),
    [statusFilter, providerFilter, search, pageSize, page, sortBy, sortDir]
  )

  const runnersQuery = useQuery({
    ...runnersQueryOptions(queryParams),
    placeholderData: keepPreviousData,
    refetchInterval: (query) => {
      if (!livePolling) return false
      const items = query.state.data?.items ?? []
      return items.some((r) => r.status === "provisioning" || r.status === "busy" || r.status === "draining")
        ? 4_000
        : 15_000
    },
  })

  useEffect(() => {
    if (runnersQuery.error) {
      toast.error(
        runnersQuery.error instanceof Error ? runnersQuery.error.message : "Failed to load runners"
      )
    }
  }, [runnersQuery.error])

  const logsQuery = useQuery({
    ...runnerLogsQueryOptions(logsTarget?.id ?? "", { limit: 200 }),
    enabled: Boolean(logsTarget?.id),
    refetchInterval: logsTarget && livePolling ? 2_500 : false,
  })

  const drainMutation = useMutation({
    mutationFn: (runner: Runner) => api.drainRunner(runner.id),
    onSuccess: (updated, runner) => {
      toast.success(
        updated.status === "terminated"
          ? `Drained and terminated idle runner ${runner.name}`
          : `Draining active runner ${runner.name}`
      )
      void queryClient.invalidateQueries({ queryKey: ["runners"] })
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview })
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to drain runner")
    },
  })

  const terminateMutation = useMutation({
    mutationFn: (id: string) => api.terminateRunner(id),
    onSuccess: () => {
      toast.success(`Terminated runner ${confirmTarget?.name ?? ""}`)
      setConfirmTarget(null)
      void queryClient.invalidateQueries({ queryKey: ["runners"] })
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview })
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to terminate runner")
    },
  })

  const logs = logsQuery.data?.items ?? []

  const columns: DataTableColumn<Runner>[] = useMemo(
    () => [
      {
        id: "name",
        header: "Runner Name",
        sortable: true,
        isRowHeader: true,
        hideable: false,
        cell: (rn) => <span className="font-mono text-[11px]">{rn.name}</span>,
      },
      {
        id: "pool_name",
        header: "Pool",
        sortable: true,
        cell: (rn) => rn.pool_name || "—",
      },
      {
        id: "provider",
        header: "Provider",
        sortable: true,
        cell: (rn) => <span className="capitalize">{rn.provider}</span>,
      },
      {
        id: "status",
        header: "Status",
        sortable: true,
        cell: (rn) => <StatusBadge status={rn.status} />,
      },
      {
        id: "labels",
        header: "Labels",
        cell: (rn) => (
          <span className="max-w-52 truncate block text-muted-foreground" title={rn.labels.join(", ")}>
            {rn.labels.join(", ") || "—"}
          </span>
        ),
      },
      {
        id: "current_job_id",
        header: "Job ID",
        sortable: true,
        cell: (rn) => <span className="font-mono text-[11px]">{rn.current_job_id ?? "—"}</span>,
      },
      {
        id: "created_at",
        header: "Created",
        sortable: true,
        defaultVisible: true,
        cell: (rn) => (
          <span className="text-[11px] text-muted-foreground whitespace-nowrap">
            {new Date(rn.created_at).toLocaleString()}
          </span>
        ),
      },
      {
        id: "actions",
        header: "Actions",
        hideable: false,
        cell: (rn) => (
          <div className="flex flex-wrap items-center gap-1.5">
            <Button type="button" size="xs" variant="outline" onClick={() => setLogsTarget(rn)}>
              <HugeiconsIcon icon={File01Icon} strokeWidth={2} />
              <span>Logs</span>
            </Button>
            <Button
              type="button"
              size="xs"
              variant="outline"
              disabled={rn.status === "draining" || rn.status === "terminated" || drainMutation.isPending}
              onClick={() => drainMutation.mutate(rn)}
            >
              <HugeiconsIcon icon={PauseCircleIcon} strokeWidth={2} />
              <span>Drain</span>
            </Button>
            <Button
              type="button"
              size="xs"
              variant="destructive"
              disabled={rn.status === "terminated"}
              onClick={() => setConfirmTarget(rn)}
            >
              <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
              <span>Terminate</span>
            </Button>
          </div>
        ),
      },
    ],
    [drainMutation]
  )

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">Runner Fleet</h1>
          <p className="text-xs text-muted-foreground">
            Inspect active ephemeral runners, drain capacity safely, view logs, or force-terminate instances.
          </p>
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={runnersQuery.isFetching}
          onClick={() => void runnersQuery.refetch()}
        >
          <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
          <span>{runnersQuery.isFetching ? "Refreshing..." : "Refresh"}</span>
        </Button>
      </div>

      <DataTable
        id="runners-table"
        caption="Self-hosted GitHub Actions runners managed by Runway"
        data={runnersQuery.data?.items ?? []}
        columns={columns}
        getRowId={(rn) => rn.id}
        loading={runnersQuery.isPending}
        fetching={runnersQuery.isFetching && !runnersQuery.isPending}
        emptyMessage="No runners match the current filters."
        searchValue={search}
        onSearchChange={(val) => {
          setPage(1)
          setSearch(val)
        }}
        searchPlaceholder="Search runner name..."
        filters={[
          {
            id: "status",
            label: "Status",
            searchable: true,
            value: statusFilter,
            onChange: (v) => {
              setPage(1)
              setStatusFilter(v)
            },
            options: [
              { label: "All statuses", value: "all" },
              { label: "Provisioning", value: "provisioning" },
              { label: "Online", value: "online" },
              { label: "Busy", value: "busy" },
              { label: "Draining", value: "draining" },
              { label: "Offline", value: "offline" },
              { label: "Terminated", value: "terminated" },
            ],
          },
          {
            id: "provider",
            label: "Provider",
            searchable: true,
            value: providerFilter,
            onChange: (v) => {
              setPage(1)
              setProviderFilter(v)
            },
            options: [
              { label: "All providers", value: "all" },
              { label: "Docker", value: "docker" },
              { label: "Agent", value: "agent" },
              { label: "Simulated", value: "simulated" },
            ],
          },
        ]}
        onResetFilters={() => {
          setPage(1)
          setSearch("")
          setStatusFilter("all")
          setProviderFilter("all")
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
          total: runnersQuery.data?.total ?? 0,
          onPageChange: setPage,
          onPageSizeChange: (n) => {
            setPage(1)
            setPageSize(n)
          },
        }}
      />

      <Dialog open={Boolean(logsTarget)} onOpenChange={(o) => !o && setLogsTarget(null)}>
        <DialogContent className="sm:max-w-3xl max-h-[85dvh] flex flex-col">
          <DialogHeader>
            <DialogTitle className="font-mono text-sm">
              Runner Logs · {logsTarget?.name}
            </DialogTitle>
            <DialogDescription>
              Captured stdout/stderr events from EventStore and live container logs.
            </DialogDescription>
          </DialogHeader>
          <div className="flex-1 overflow-auto border border-border bg-muted/30 p-3 font-mono text-[11px] space-y-1 max-h-96">
            {logsQuery.isPending ? (
              <div className="text-muted-foreground">Loading logs...</div>
            ) : logs.length === 0 ? (
              <div className="text-muted-foreground">No runner logs recorded yet.</div>
            ) : (
              logs.map((entry) => (
                <div key={entry.id} className="flex gap-2 leading-relaxed">
                  <span className="text-muted-foreground shrink-0">
                    {new Date(entry.occurred_at).toLocaleTimeString()}
                  </span>
                  <span
                    className={
                      entry.severity === "error" ? "text-destructive break-all" : "text-foreground break-all"
                    }
                  >
                    {entry.message}
                  </span>
                </div>
              ))
            )}
          </div>
          <DialogFooter className="gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={logsQuery.isFetching}
              onClick={() => void logsQuery.refetch()}
            >
              <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
              <span>{logsQuery.isFetching ? "Refreshing..." : "Refresh Logs"}</span>
            </Button>
            {logs.length > 0 && (
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() =>
                  void navigator.clipboard
                    .writeText(logs.map((l) => `${l.occurred_at} ${l.message}`).join("\n"))
                    .then(() => toast.success("Copied runner logs"))
                }
              >
                <HugeiconsIcon icon={Copy01Icon} strokeWidth={2} />
                <span>Copy</span>
              </Button>
            )}
            <Button type="button" onClick={() => setLogsTarget(null)}>
              Close
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={Boolean(confirmTarget)} onOpenChange={(o) => !o && setConfirmTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Terminate Runner?</AlertDialogTitle>
            <AlertDialogDescription>
              This will stop and decommission <strong>{confirmTarget?.name}</strong> immediately. Any
              active workflow job on this runner will be interrupted.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => confirmTarget && terminateMutation.mutate(confirmTarget.id)}
            >
              Terminate Runner
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
