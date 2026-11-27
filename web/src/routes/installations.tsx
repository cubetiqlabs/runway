import { useCallback, useEffect, useState } from "react"
import { createFileRoute } from "@tanstack/react-router"
import { HugeiconsIcon } from "@hugeicons/react"
import { RefreshIcon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ResponsiveTable } from "@/components/responsive-table"
import { StatusBadge } from "@/components/status-badge"
import { Badge } from "@/components/ui/badge"
import { api } from "@/lib/api"
import type { AuditLog, GitHubAppSettingsStatus, Installation, WebhookDelivery } from "@/lib/types"
import { toast } from "sonner"

export const Route = createFileRoute("/installations")({
  component: InstallationsPage,
})

function InstallationsPage() {
  const [installations, setInstallations] = useState<Installation[]>([])
  const [deliveries, setDeliveries] = useState<WebhookDelivery[]>([])
  const [audits, setAudits] = useState<AuditLog[]>([])
  const [settings, setSettings] = useState<GitHubAppSettingsStatus | null>(null)
  const [syncing, setSyncing] = useState(false)

  const load = useCallback(async () => {
    try {
      const [instRes, delRes, audRes, setRes] = await Promise.all([
        api.listInstallations(),
        api.listDeliveries(),
        api.listAuditLogs(),
        api.settingsStatus(),
      ])
      setInstallations(instRes.items)
      setDeliveries(delRes.items)
      setAudits(audRes.items)
      setSettings(setRes)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load GitHub App telemetry")
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const handleSync = async () => {
    setSyncing(true)
    try {
      const res = await api.syncInstallations()
      toast.success(`Synced ${res.synced} installations from GitHub`)
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Sync failed")
    } finally {
      setSyncing(false)
    }
  }

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
            <CardTitle>GitHub App Configuration Status</CardTitle>
            <CardDescription>
              Webhook endpoint: <code className="font-mono">/webhooks/github</code> (subscribe to{" "}
              <code className="font-mono">workflow_job</code> and{" "}
              <code className="font-mono">installation</code>)
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3 sm:grid-cols-4 text-xs">
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
                  {settings.orchestrator_provider} · Default: {settings.moderation_default_action}
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
          </CardContent>
        </Card>
      )}

      <Tabs defaultValue="installations">
        <TabsList>
          <TabsTrigger value="installations">Installations ({installations.length})</TabsTrigger>
          <TabsTrigger value="webhooks">Webhook Deliveries ({deliveries.length})</TabsTrigger>
          <TabsTrigger value="audit">Audit Log ({audits.length})</TabsTrigger>
        </TabsList>

        <TabsContent value="installations" className="pt-2">
          <ResponsiveTable
            id="installations-table"
            caption="GitHub App installations"
            headers={["Installation ID", "Account", "Type", "Repositories", "Status"]}
            empty={installations.length === 0}
            emptyMessage="No GitHub App installations recorded yet. Deliver an installation webhook or click Sync."
          >
            {installations.map((inst) => (
              <tr key={inst.id}>
                <td data-label="ID" className="font-mono">
                  {inst.id}
                </td>
                <th scope="row">{inst.account_login}</th>
                <td data-label="Type">{inst.account_type}</td>
                <td data-label="Repositories" className="capitalize">
                  {inst.repositories_selection}
                </td>
                <td data-label="Status">
                  <StatusBadge status={inst.suspended_at ? "offline" : "online"} />
                </td>
              </tr>
            ))}
          </ResponsiveTable>
        </TabsContent>

        <TabsContent value="webhooks" className="pt-2">
          <ResponsiveTable
            id="deliveries-table"
            caption="GitHub webhook delivery log"
            headers={["Delivery ID", "Event", "Repository", "HMAC", "Status", "Details", "Latency"]}
            empty={deliveries.length === 0}
            emptyMessage="No webhook deliveries logged yet."
          >
            {deliveries.map((d) => (
              <tr key={d.delivery_id}>
                <th scope="row" className="font-mono text-[11px] max-w-40 truncate">
                  {d.delivery_id}
                </th>
                <td data-label="Event">
                  {d.event_name}
                  {d.action ? `.${d.action}` : ""}
                </td>
                <td data-label="Repository">{d.repo_full_name || "—"}</td>
                <td data-label="HMAC">
                  <Badge variant={d.signature_valid ? "secondary" : "destructive"}>
                    {d.signature_valid ? "Valid" : "Invalid"}
                  </Badge>
                </td>
                <td data-label="Status">
                  <StatusBadge status={d.status} />
                </td>
                <td
                  data-label="Details"
                  className="max-w-72 truncate text-[11px] text-muted-foreground"
                  title={d.error_message || undefined}
                >
                  {d.error_message || "—"}
                </td>
                <td data-label="Latency">{d.processing_ms} ms</td>
              </tr>
            ))}
          </ResponsiveTable>
        </TabsContent>

        <TabsContent value="audit" className="pt-2">
          <ResponsiveTable
            id="audit-table"
            caption="Immutable control plane audit logs"
            headers={["Time", "Actor", "Action", "Resource", "IP"]}
            empty={audits.length === 0}
            emptyMessage="No audit log entries yet."
          >
            {audits.map((a) => (
              <tr key={a.id}>
                <td data-label="Time" className="text-[11px] text-muted-foreground whitespace-nowrap">
                  {new Date(a.created_at).toLocaleString()}
                </td>
                <th scope="row">{a.actor_label}</th>
                <td data-label="Action" className="font-mono text-[11px]">
                  {a.action}
                </td>
                <td data-label="Resource" className="font-mono text-[11px]">
                  {a.resource_type}:{a.resource_id}
                </td>
                <td data-label="IP" className="font-mono text-[11px] text-muted-foreground">
                  {a.ip_address || "—"}
                </td>
              </tr>
            ))}
          </ResponsiveTable>
        </TabsContent>
      </Tabs>
    </div>
  )
}
