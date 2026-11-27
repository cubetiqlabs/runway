import { useCallback, useEffect, useMemo, useState } from "react"
import { createFileRoute } from "@tanstack/react-router"
import { HugeiconsIcon } from "@hugeicons/react"
import { RefreshIcon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { DataTable, type DataTableColumn, type SortDirection } from "@/components/data-table"
import { StatusBadge } from "@/components/status-badge"
import { Badge } from "@/components/ui/badge"
import { api } from "@/lib/api"
import type { AuditLog, GitHubAppSettingsStatus, Installation, WebhookDelivery } from "@/lib/types"
import { toast } from "sonner"

export const Route = createFileRoute("/installations")({
  component: InstallationsPage,
})

function InstallationsPage() {
  const [settings, setSettings] = useState<GitHubAppSettingsStatus | null>(null)
  const [syncing, setSyncing] = useState(false)

  // Installations state
  const [installations, setInstallations] = useState<Installation[]>([])
  const [instTotal, setInstTotal] = useState(0)
  const [instLoading, setInstLoading] = useState(true)
  const [instPage, setInstPage] = useState(1)
  const [instPageSize, setInstPageSize] = useState(25)
  const [instSearch, setInstSearch] = useState("")
  const [instSortBy, setInstSortBy] = useState("updated_at")
  const [instSortDir, setInstSortDir] = useState<SortDirection>("desc")

  // Webhook deliveries state
  const [deliveries, setDeliveries] = useState<WebhookDelivery[]>([])
  const [delTotal, setDelTotal] = useState(0)
  const [delLoading, setDelLoading] = useState(true)
  const [delPage, setDelPage] = useState(1)
  const [delPageSize, setDelPageSize] = useState(25)
  const [delSearch, setDelSearch] = useState("")
  const [delStatus, setDelStatus] = useState("all")
  const [delSortBy, setDelSortBy] = useState("received_at")
  const [delSortDir, setDelSortDir] = useState<SortDirection>("desc")

  // Audit logs state
  const [audits, setAudits] = useState<AuditLog[]>([])
  const [audTotal, setAudTotal] = useState(0)
  const [audLoading, setAudLoading] = useState(true)
  const [audPage, setAudPage] = useState(1)
  const [audPageSize, setAudPageSize] = useState(25)
  const [audSearch, setAudSearch] = useState("")
  const [audSortBy, setAudSortBy] = useState("created_at")
  const [audSortDir, setAudSortDir] = useState<SortDirection>("desc")

  const loadSettings = useCallback(async () => {
    try {
      const res = await api.settingsStatus()
      setSettings(res)
    } catch {
      // handled in list loaders
    }
  }, [])

  const loadInstallations = useCallback(async () => {
    setInstLoading(true)
    try {
      const res = await api.listInstallations({
        limit: instPageSize,
        page: instPage,
        search: instSearch.trim() || undefined,
        sort_by: instSortBy,
        sort_dir: instSortDir,
      })
      setInstallations(res.items)
      setInstTotal(res.total)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load installations")
    } finally {
      setInstLoading(false)
    }
  }, [instPage, instPageSize, instSearch, instSortBy, instSortDir])

  const loadDeliveries = useCallback(async () => {
    setDelLoading(true)
    try {
      const res = await api.listDeliveries({
        limit: delPageSize,
        page: delPage,
        status: delStatus,
        search: delSearch.trim() || undefined,
        sort_by: delSortBy,
        sort_dir: delSortDir,
      })
      setDeliveries(res.items)
      setDelTotal(res.total)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load webhook deliveries")
    } finally {
      setDelLoading(false)
    }
  }, [delPage, delPageSize, delStatus, delSearch, delSortBy, delSortDir])

  const loadAudits = useCallback(async () => {
    setAudLoading(true)
    try {
      const res = await api.listAuditLogs({
        limit: audPageSize,
        page: audPage,
        search: audSearch.trim() || undefined,
        sort_by: audSortBy,
        sort_dir: audSortDir,
      })
      setAudits(res.items)
      setAudTotal(res.total)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load audit logs")
    } finally {
      setAudLoading(false)
    }
  }, [audPage, audPageSize, audSearch, audSortBy, audSortDir])

  useEffect(() => {
    void loadSettings()
  }, [loadSettings])

  useEffect(() => {
    void loadInstallations()
  }, [loadInstallations])

  useEffect(() => {
    void loadDeliveries()
  }, [loadDeliveries])

  useEffect(() => {
    void loadAudits()
  }, [loadAudits])

  const handleSync = async () => {
    setSyncing(true)
    try {
      const res = await api.syncInstallations()
      toast.success(`Synced ${res.synced} installations from GitHub`)
      void loadInstallations()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Sync failed")
    } finally {
      setSyncing(false)
    }
  }

  const instColumns: DataTableColumn<Installation>[] = useMemo(
    () => [
      {
        id: "id",
        header: "Installation ID",
        sortable: true,
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
        <Button type="button" variant="outline" size="sm" disabled={syncing} onClick={() => void handleSync()}>
          <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
          <span>{syncing ? "Syncing..." : "Sync Installations"}</span>
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
          <CardContent className="grid gap-3 grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 text-xs">
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
            data={installations}
            columns={instColumns}
            getRowId={(inst) => String(inst.id)}
            loading={instLoading}
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
            data={deliveries}
            columns={delColumns}
            getRowId={(d) => d.delivery_id}
            loading={delLoading}
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
            data={audits}
            columns={auditColumns}
            getRowId={(a) => String(a.id)}
            loading={audLoading}
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
