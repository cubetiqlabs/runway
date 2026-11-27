import { useEffect, useMemo, useState } from "react"
import { createFileRoute } from "@tanstack/react-router"
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { HugeiconsIcon } from "@hugeicons/react"
import { RefreshIcon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { DataTable, type DataTableColumn, type SortDirection } from "@/components/data-table"
import { StatusBadge } from "@/components/status-badge"
import { Badge } from "@/components/ui/badge"
import { api } from "@/lib/api"
import {
  auditLogsQueryOptions,
  deliveriesQueryOptions,
  installationsQueryOptions,
  queryKeys,
  settingsStatusQueryOptions,
} from "@/lib/query"
import { useAppSelector } from "@/lib/store"
import type { AuditLog, Installation, WebhookDelivery } from "@/lib/types"
import { toast } from "sonner"

export const Route = createFileRoute("/installations")({
  component: InstallationsPage,
})

function InstallationsPage() {
  const queryClient = useQueryClient()
  const livePolling = useAppSelector((s) => s.livePolling)

  // Installations state
  const [instPage, setInstPage] = useState(1)
  const [instPageSize, setInstPageSize] = useState(25)
  const [instSearch, setInstSearch] = useState("")
  const [instSortBy, setInstSortBy] = useState("updated_at")
  const [instSortDir, setInstSortDir] = useState<SortDirection>("desc")

  // Webhook deliveries state
  const [delPage, setDelPage] = useState(1)
  const [delPageSize, setDelPageSize] = useState(25)
  const [delSearch, setDelSearch] = useState("")
  const [delStatus, setDelStatus] = useState("all")
  const [delSortBy, setDelSortBy] = useState("received_at")
  const [delSortDir, setDelSortDir] = useState<SortDirection>("desc")

  // Audit logs state
  const [audPage, setAudPage] = useState(1)
  const [audPageSize, setAudPageSize] = useState(25)
  const [audSearch, setAudSearch] = useState("")
  const [audSortBy, setAudSortBy] = useState("created_at")
  const [audSortDir, setAudSortDir] = useState<SortDirection>("desc")

  const settingsQuery = useQuery({
    ...settingsStatusQueryOptions(),
    refetchInterval: livePolling ? 15_000 : false,
  })

  const instQuery = useQuery({
    ...installationsQueryOptions({
      limit: instPageSize,
      page: instPage,
      search: instSearch.trim() || undefined,
      sort_by: instSortBy,
      sort_dir: instSortDir,
    }),
    placeholderData: keepPreviousData,
  })

  const delQuery = useQuery({
    ...deliveriesQueryOptions({
      limit: delPageSize,
      page: delPage,
      status: delStatus,
      search: delSearch.trim() || undefined,
      sort_by: delSortBy,
      sort_dir: delSortDir,
    }),
    placeholderData: keepPreviousData,
    refetchInterval: livePolling ? 15_000 : false,
  })

  const audQuery = useQuery({
    ...auditLogsQueryOptions({
      limit: audPageSize,
      page: audPage,
      search: audSearch.trim() || undefined,
      sort_by: audSortBy,
      sort_dir: audSortDir,
    }),
    placeholderData: keepPreviousData,
  })

  useEffect(() => {
    if (instQuery.error) {
      toast.error(
        instQuery.error instanceof Error ? instQuery.error.message : "Failed to load installations"
      )
    }
  }, [instQuery.error])

  useEffect(() => {
    if (delQuery.error) {
      toast.error(
        delQuery.error instanceof Error
          ? delQuery.error.message
          : "Failed to load webhook deliveries"
      )
    }
  }, [delQuery.error])

  useEffect(() => {
    if (audQuery.error) {
      toast.error(
        audQuery.error instanceof Error ? audQuery.error.message : "Failed to load audit logs"
      )
    }
  }, [audQuery.error])

  const syncMutation = useMutation({
    mutationFn: () => api.syncInstallations(),
    onSuccess: (res) => {
      toast.success(`Synced ${res.synced} installations from GitHub`)
      void queryClient.invalidateQueries({ queryKey: ["installations"] })
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview })
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Sync failed")
    },
  })

  const settings = settingsQuery.data

  const instColumns: DataTableColumn<Installation>[] = useMemo(
    () => [
      {
        id: "id",
        header: "Installation ID",
        sortable: true,
        sortType: "number",
        cell: (inst) => <span className="font-mono">{inst.id}</span>,
      },
      {
        id: "account_login",
        header: "Account",
        sortable: true,
        isRowHeader: true,
        hideable: false,
        cell: (inst) => inst.account_login,
      },
      {
        id: "account_type",
        header: "Type",
        sortable: true,
        cell: (inst) => inst.account_type,
      },
      {
        id: "repositories_selection",
        header: "Repositories",
        cell: (inst) => <span className="capitalize">{inst.repositories_selection}</span>,
      },
      {
        id: "status",
        header: "Status",
        cell: (inst) => <StatusBadge status={inst.suspended_at ? "offline" : "online"} />,
      },
    ],
    []
  )

  const delColumns: DataTableColumn<WebhookDelivery>[] = useMemo(
    () => [
      {
        id: "delivery_id",
        header: "Delivery ID",
        isRowHeader: true,
        hideable: false,
        cell: (d) => <span className="font-mono text-[11px] max-w-40 truncate block">{d.delivery_id}</span>,
      },
      {
        id: "event_name",
        header: "Event",
        sortable: true,
        cell: (d) => (
          <span>
            {d.event_name}
            {d.action ? `.${d.action}` : ""}
          </span>
        ),
      },
      {
        id: "repo_full_name",
        header: "Repository",
        sortable: true,
        cell: (d) => d.repo_full_name || "—",
      },
      {
        id: "hmac",
        header: "HMAC",
        cell: (d) => (
          <Badge variant={d.signature_valid ? "secondary" : "destructive"}>
            {d.signature_valid ? "Valid" : "Invalid"}
          </Badge>
        ),
      },
      {
        id: "status",
        header: "Status",
        sortable: true,
        cell: (d) => <StatusBadge status={d.status} />,
      },
      {
        id: "error_message",
        header: "Details",
        cell: (d) => (
          <span className="max-w-72 truncate block text-[11px] text-muted-foreground" title={d.error_message || undefined}>
            {d.error_message || "—"}
          </span>
        ),
      },
      {
        id: "processing_ms",
        header: "Latency",
        sortable: true,
        sortType: "number",
        cell: (d) => <span className="font-mono text-[11px]">{d.processing_ms} ms</span>,
      },
      {
        id: "received_at",
        header: "Received",
        sortable: true,
        cell: (d) => (
          <span className="text-[11px] text-muted-foreground whitespace-nowrap">
            {new Date(d.received_at).toLocaleString()}
          </span>
        ),
      },
    ],
    []
  )

  const auditColumns: DataTableColumn<AuditLog>[] = useMemo(
    () => [
      {
        id: "created_at",
        header: "Time",
        sortable: true,
        cell: (a) => (
          <span className="text-[11px] text-muted-foreground whitespace-nowrap">
            {new Date(a.created_at).toLocaleString()}
          </span>
        ),
      },
      {
        id: "actor_label",
        header: "Actor",
        sortable: true,
        isRowHeader: true,
        hideable: false,
        cell: (a) => a.actor_label,
      },
      {
        id: "action",
        header: "Action",
        sortable: true,
        cell: (a) => <span className="font-mono text-[11px]">{a.action}</span>,
      },
      {
        id: "resource_type",
        header: "Resource",
        sortable: true,
        cell: (a) => (
          <span className="font-mono text-[11px]">
            {a.resource_type}:{a.resource_id}
          </span>
        ),
      },
      {
        id: "ip_address",
        header: "IP",
        cell: (a) => <span className="font-mono text-[11px] text-muted-foreground">{a.ip_address || "—"}</span>,
      },
    ],
    []
  )

  const instTotal = instQuery.data?.total ?? 0
  const delTotal = delQuery.data?.total ?? 0
  const audTotal = audQuery.data?.total ?? 0

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">GitHub App &amp; Telemetry</h1>
          <p className="text-xs text-muted-foreground">
            Monitor GitHub App configuration, organization installations, webhook deliveries, and
            immutable audit events.
          </p>
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={syncMutation.isPending}
          onClick={() => syncMutation.mutate()}
        >
          <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
          <span>{syncMutation.isPending ? "Syncing..." : "Sync Installations"}</span>
        </Button>
      </div>

      {settings && (
        <Card>
          <CardHeader>
            <CardTitle>Platform Configuration Status</CardTitle>
            <CardDescription>
              Webhook endpoint: <code className="font-mono">/webhooks/github</code> (subscribe to{" "}
              <code className="font-mono">workflow_job</code> and{" "}
              <code className="font-mono">installation</code>)
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3 text-xs">
            <div className="grid gap-3 grid-cols-1 sm:grid-cols-2 lg:grid-cols-6">
              <div>
                <span className="text-muted-foreground block">App ID</span>
                <span className="font-mono font-medium">{settings.app_id || "Not configured"}</span>
              </div>
              <div>
                <span className="text-muted-foreground block">Webhook HMAC Secret</span>
                <Badge variant={settings.webhook_secret_set ? "secondary" : "destructive"}>
                  {settings.webhook_secret_set ? "Configured" : "Unset"}
                </Badge>
              </div>
              <div>
                <span className="text-muted-foreground block">Private Key (RS256)</span>
                <Badge variant={settings.private_key_configured ? "secondary" : "destructive"}>
                  {settings.private_key_configured ? "Loaded" : "Demo / Unset"}
                </Badge>
              </div>
              <div>
                <span className="text-muted-foreground block">Orchestrator</span>
                <div className="flex flex-wrap items-center gap-1.5 mt-0.5">
                  <span className="font-medium capitalize">
                    {settings.orchestrator_provider} · {settings.moderation_default_action}
                  </span>
                  <Badge variant={settings.orchestrator_healthy ? "secondary" : "destructive"}>
                    {settings.orchestrator_healthy ? "Connected" : "Unavailable"}
                  </Badge>
                </div>
                {!settings.orchestrator_healthy && settings.orchestrator_error && (
                  <span className="mt-1 block text-[11px] text-destructive break-all">
                    {settings.orchestrator_error}
                  </span>
                )}
              </div>
              <div>
                <span className="text-muted-foreground block">EventStore</span>
                <div className="flex flex-wrap items-center gap-1.5 mt-0.5">
                  <span className="font-medium capitalize">{settings.eventstore_driver || "postgres"}</span>
                  <Badge variant={settings.eventstore_healthy ? "secondary" : "destructive"}>
                    {settings.eventstore_healthy ? "Ready" : "Error"}
                  </Badge>
                </div>
                {settings.eventstore_stats && (
                  <span className="mt-1 block text-[11px] text-muted-foreground">
                    {settings.eventstore_stats.flushed} flushed · queue {settings.eventstore_stats.queue_depth}
                  </span>
                )}
              </div>
              <div>
                <span className="text-muted-foreground block">Cache &amp; Cluster</span>
                <div className="flex flex-wrap items-center gap-1.5 mt-0.5">
                  <span className="font-medium capitalize">{settings.cache_driver || "memory"}</span>
                  <Badge variant={settings.cache_healthy ? "secondary" : "destructive"}>
                    {settings.cache_healthy
                      ? settings.cache_cluster_enabled
                        ? "Clustered"
                        : "Local"
                      : "Error"}
                  </Badge>
                </div>
                {settings.cache_stats && (
                  <span className="mt-1 block text-[11px] text-muted-foreground">
                    {settings.cache_stats.hit_rate.toFixed(1)}% hit · {settings.cache_stats.hits} hits /{" "}
                    {settings.cache_stats.misses} misses
                  </span>
                )}
                {!settings.cache_healthy && settings.cache_error && (
                  <span className="mt-1 block text-[11px] text-destructive break-all">
                    {settings.cache_error}
                  </span>
                )}
              </div>
            </div>

            {settings.cluster_nodes && settings.cluster_nodes.length > 0 && (
              <div className="border-t pt-2.5">
                <span className="text-muted-foreground block mb-1.5">
                  Control-Plane Cluster Nodes ({settings.cluster_nodes.length})
                </span>
                <div className="flex flex-wrap gap-2">
                  {settings.cluster_nodes.map((node) => {
                    const isSelf = node.instance_id === settings.instance_id
                    return (
                      <div
                        key={node.instance_id}
                        className="inline-flex items-center gap-1.5 rounded-none border px-2 py-1 text-[11px]"
                      >
                        <span className="font-mono font-medium">{node.instance_id}</span>
                        <span className="text-muted-foreground">
                          ({node.hostname} · {node.version})
                        </span>
                        {isSelf && <Badge variant="secondary">current</Badge>}
                      </div>
                    )
                  })}
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      <Tabs defaultValue="installations">
        <TabsList className="max-w-full overflow-x-auto justify-start">
          <TabsTrigger value="installations">Installations ({instTotal})</TabsTrigger>
          <TabsTrigger value="webhooks">Webhook Deliveries ({delTotal})</TabsTrigger>
          <TabsTrigger value="audit">Audit Log ({audTotal})</TabsTrigger>
        </TabsList>

        <TabsContent value="installations" className="pt-2">
          <DataTable
            id="installations-table"
            caption="GitHub App installations"
            data={instQuery.data?.items ?? []}
            columns={instColumns}
            getRowId={(inst) => String(inst.id)}
            loading={instQuery.isPending}
            fetching={instQuery.isFetching && !instQuery.isPending}
            emptyMessage="No GitHub App installations recorded yet."
            searchValue={instSearch}
            onSearchChange={(val) => {
              setInstPage(1)
              setInstSearch(val)
            }}
            searchPlaceholder="Search account login..."
            sortBy={instSortBy}
            sortDir={instSortDir}
            onSortChange={(col, dir) => {
              setInstPage(1)
              setInstSortBy(col)
              setInstSortDir(dir)
            }}
            pagination={{
              page: instPage,
              pageSize: instPageSize,
              total: instTotal,
              onPageChange: setInstPage,
              onPageSizeChange: (n) => {
                setInstPage(1)
                setInstPageSize(n)
              },
            }}
          />
        </TabsContent>

        <TabsContent value="webhooks" className="pt-2">
          <DataTable
            id="deliveries-table"
            caption="GitHub webhook delivery log"
            data={delQuery.data?.items ?? []}
            columns={delColumns}
            getRowId={(d) => d.delivery_id}
            loading={delQuery.isPending}
            fetching={delQuery.isFetching && !delQuery.isPending}
            emptyMessage="No webhook deliveries logged yet."
            searchValue={delSearch}
            onSearchChange={(val) => {
              setDelPage(1)
              setDelSearch(val)
            }}
            searchPlaceholder="Search delivery ID, repo, or error..."
            filters={[
              {
                id: "status",
                label: "Status",
                searchable: true,
                value: delStatus,
                onChange: (v) => {
                  setDelPage(1)
                  setDelStatus(v)
                },
                options: [
                  { label: "All statuses", value: "all" },
                  { label: "Processed", value: "processed" },
                  { label: "Ignored", value: "ignored" },
                  { label: "Failed", value: "failed" },
                ],
              },
            ]}
            onResetFilters={() => {
              setDelPage(1)
              setDelSearch("")
              setDelStatus("all")
            }}
            sortBy={delSortBy}
            sortDir={delSortDir}
            onSortChange={(col, dir) => {
              setDelPage(1)
              setDelSortBy(col)
              setDelSortDir(dir)
            }}
            pagination={{
              page: delPage,
              pageSize: delPageSize,
              total: delTotal,
              onPageChange: setDelPage,
              onPageSizeChange: (n) => {
                setDelPage(1)
                setDelPageSize(n)
              },
            }}
          />
        </TabsContent>

        <TabsContent value="audit" className="pt-2">
          <DataTable
            id="audit-table"
            caption="Immutable control plane audit logs"
            data={audQuery.data?.items ?? []}
            columns={auditColumns}
            getRowId={(a) => String(a.id)}
            loading={audQuery.isPending}
            fetching={audQuery.isFetching && !audQuery.isPending}
            emptyMessage="No audit log entries yet."
            searchValue={audSearch}
            onSearchChange={(val) => {
              setAudPage(1)
              setAudSearch(val)
            }}
            searchPlaceholder="Search actor, action, resource..."
            sortBy={audSortBy}
            sortDir={audSortDir}
            onSortChange={(col, dir) => {
              setAudPage(1)
              setAudSortBy(col)
              setAudSortDir(dir)
            }}
            pagination={{
              page: audPage,
              pageSize: audPageSize,
              total: audTotal,
              onPageChange: setAudPage,
              onPageSizeChange: (n) => {
                setAudPage(1)
                setAudPageSize(n)
              },
            }}
          />
        </TabsContent>
      </Tabs>
    </div>
  )
}
