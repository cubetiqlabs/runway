import React, { useCallback, useEffect, useState } from "react"
import { createFileRoute } from "@tanstack/react-router"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  Add01Icon,
  RefreshIcon,
  PauseCircleIcon,
  Delete02Icon,
  Copy01Icon,
  Key01Icon,
} from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
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
import { ResponsiveTable } from "@/components/responsive-table"
import { StatusBadge } from "@/components/status-badge"
import { api } from "@/lib/api"
import type { Agent, RegisterAgentResponse, RunnerPool } from "@/lib/types"
import { toast } from "sonner"

export const Route = createFileRoute("/agents")({
  component: AgentsPage,
})

interface AgentFormState {
  name: string
  os: string
  arch: string
  runtime: string
  labelsText: string
  pool_id: string
  max_concurrency: string
}

const INITIAL_FORM: AgentFormState = {
  name: "",
  os: "linux",
  arch: "x64",
  runtime: "auto",
  labelsText: "",
  pool_id: "any",
  max_concurrency: "2",
}

function AgentsPage() {
  const [agents, setAgents] = useState<Agent[]>([])
  const [pools, setPools] = useState<RunnerPool[]>([])
  const [loading, setLoading] = useState(true)
  const [createOpen, setCreateOpen] = useState(false)
  const [form, setForm] = useState<AgentFormState>(INITIAL_FORM)
  const [saving, setSaving] = useState(false)
  const [issued, setIssued] = useState<RegisterAgentResponse | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<Agent | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [agRes, poolRes] = await Promise.all([api.listAgents(), api.listPools()])
      setAgents(agRes.items)
      setPools(poolRes.items)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load agents")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const handleRegister = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!form.name.trim()) {
      toast.error("Agent name is required")
      return
    }
    setSaving(true)
    try {
      const res = await api.registerAgent({
        name: form.name.trim(),
        os: form.os,
        arch: form.arch,
        runtime: form.runtime,
        labels: form.labelsText
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean),
        pool_id: form.pool_id === "any" ? null : form.pool_id,
        max_concurrency: Number(form.max_concurrency) || 2,
      })
      setCreateOpen(false)
      setForm(INITIAL_FORM)
      setIssued(res)
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to register agent")
    } finally {
      setSaving(false)
    }
  }

  const handleDrain = async (a: Agent) => {
    try {
      await api.drainAgent(a.id)
      toast.success(`Agent ${a.name} set to draining`)
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to drain agent")
    }
  }

  const handleRotate = async (a: Agent) => {
    try {
      const res = await api.rotateAgentToken(a.id)
      setIssued(res)
      toast.success(`Rotated token for ${a.name}`)
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to rotate token")
    }
  }

  const confirmDelete = async () => {
    if (!deleteTarget) return
    try {
      await api.deleteAgent(deleteTarget.id)
      toast.success(`Deleted agent ${deleteTarget.name}`)
      setDeleteTarget(null)
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to delete agent")
    }
  }

  const copyText = async (label: string, text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      toast.success(`Copied ${label}`)
    } catch {
      toast.error("Clipboard copy unavailable")
    }
  }

  const origin = typeof window !== "undefined" ? window.location.origin : "https://runway.example.com"
  const unixInstallCmd = issued
    ? `curl -fsSL https://raw.githubusercontent.com/cubetiqlabs/runway/main/scripts/install-agent.sh | sudo sh -s -- --server ${origin} --token ${issued.token}`
    : ""
  const winInstallCmd = issued
    ? `$env:RUNWAY_AGENT_SERVER="${origin}"; $env:RUNWAY_AGENT_TOKEN="${issued.token}"; irm https://raw.githubusercontent.com/cubetiqlabs/runway/main/scripts/install-agent.ps1 | iex`
    : ""

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">Distributed Runner Agents</h1>
          <p className="text-xs text-muted-foreground">
            Deploy lightweight <code className="font-mono">runway-agent</code> workers across Linux,
            macOS, and Windows hosts with auto-updates enabled.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button type="button" variant="outline" size="sm" onClick={() => void load()}>
            <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
            <span>Refresh</span>
          </Button>
          <Button type="button" size="sm" onClick={() => setCreateOpen(true)}>
            <HugeiconsIcon icon={Add01Icon} strokeWidth={2} />
            <span>Register Agent</span>
          </Button>
        </div>
      </div>

      <ResponsiveTable
        id="agents-table"
        caption="Registered Runway distributed worker agents"
        headers={["Agent", "Platform", "Runtime", "Capacity", "Version", "Status", "Last Heartbeat", "Actions"]}
        empty={!loading && agents.length === 0}
        emptyMessage="No distributed agents registered yet. Click Register Agent to connect a Linux, macOS, or Windows host."
      >
        {agents.map((a) => (
          <tr key={a.id}>
            <th scope="row">
              <div className="font-medium">{a.name}</div>
              <div className="font-mono text-[10px] text-muted-foreground">
                {a.hostname || a.token_prefix}
                {a.pool_name ? ` · ${a.pool_name}` : ""}
              </div>
            </th>
            <td data-label="Platform" className="text-xs">
              <span className="capitalize">{a.os}</span> / <span className="font-mono">{a.arch}</span>
            </td>
            <td data-label="Runtime">
              <Badge variant="secondary" className="capitalize text-[10px]">
                {a.runtime}
              </Badge>
            </td>
            <td data-label="Capacity" className="font-mono text-[11px]">
              {a.active_jobs} / {a.max_concurrency}
            </td>
            <td data-label="Version" className="font-mono text-[11px]">
              {a.version || "—"}
            </td>
            <td data-label="Status">
              <StatusBadge status={a.status} />
            </td>
            <td data-label="Last Heartbeat" className="text-[11px] text-muted-foreground whitespace-nowrap">
              {a.last_heartbeat_at ? new Date(a.last_heartbeat_at).toLocaleString() : "Never"}
            </td>
            <td data-label="Actions">
              <div className="flex flex-wrap items-center gap-1.5">
                <Button
                  type="button"
                  size="xs"
                  variant="outline"
                  disabled={a.status === "draining"}
                  onClick={() => void handleDrain(a)}
                >
                  <HugeiconsIcon icon={PauseCircleIcon} strokeWidth={2} />
                  <span>Drain</span>
                </Button>
                <Button type="button" size="xs" variant="outline" onClick={() => void handleRotate(a)}>
                  <HugeiconsIcon icon={Key01Icon} strokeWidth={2} />
                  <span>Rotate</span>
                </Button>
                <Button
                  type="button"
                  size="xs"
                  variant="destructive"
                  onClick={() => setDeleteTarget(a)}
                >
                  <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
                  <span>Delete</span>
                </Button>
              </div>
            </td>
          </tr>
        ))}
      </ResponsiveTable>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="sm:max-w-lg max-h-[90dvh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Register Distributed Agent</DialogTitle>
            <DialogDescription>
              Create credentials for a Linux, macOS, or Windows host running{" "}
              <code className="font-mono">runway-agent</code>.
            </DialogDescription>
          </DialogHeader>
          <form onSubmit={handleRegister} className="space-y-3">
            <div className="space-y-1">
              <Label htmlFor="agent-name">Agent Name</Label>
              <Input
                id="agent-name"
                required
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="build-mac-m3-01"
                autoCorrect="off"
                autoCapitalize="none"
                spellCheck={false}
              />
            </div>

            <div className="grid gap-3 sm:grid-cols-3">
              <div className="space-y-1">
                <Label htmlFor="agent-os">OS</Label>
                <Select value={form.os} onValueChange={(v) => setForm({ ...form, os: v || "linux" })}>
                  <SelectTrigger id="agent-os" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="linux">Linux</SelectItem>
                    <SelectItem value="macos">macOS</SelectItem>
                    <SelectItem value="windows">Windows</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1">
                <Label htmlFor="agent-arch">Arch</Label>
                <Select
                  value={form.arch}
                  onValueChange={(v) => setForm({ ...form, arch: v || "x64" })}
                >
                  <SelectTrigger id="agent-arch" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="x64">x64 (amd64)</SelectItem>
                    <SelectItem value="arm64">arm64</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1">
                <Label htmlFor="agent-concurrency">Concurrency</Label>
                <Input
                  id="agent-concurrency"
                  inputMode="numeric"
                  value={form.max_concurrency}
                  onChange={(e) => setForm({ ...form, max_concurrency: e.target.value })}
                />
              </div>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label htmlFor="agent-runtime">Preferred Runtime</Label>
                <Select
                  value={form.runtime}
                  onValueChange={(v) => setForm({ ...form, runtime: v || "auto" })}
                >
                  <SelectTrigger id="agent-runtime" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="auto">Auto (Docker or Native)</SelectItem>
                    <SelectItem value="native">Native Host Runner</SelectItem>
                    <SelectItem value="docker">Docker Containers</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1">
                <Label htmlFor="agent-pool">Bind to Pool (optional)</Label>
                <Select
                  value={form.pool_id}
                  onValueChange={(v) => setForm({ ...form, pool_id: v || "any" })}
                >
                  <SelectTrigger id="agent-pool" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="any">Any compatible pool</SelectItem>
                    {pools.map((p) => (
                      <SelectItem key={p.id} value={p.id}>
                        {p.name} ({p.os}/{p.arch})
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className="space-y-1">
              <Label htmlFor="agent-labels">Extra Labels (comma-separated, optional)</Label>
              <Input
                id="agent-labels"
                value={form.labelsText}
                onChange={(e) => setForm({ ...form, labelsText: e.target.value })}
                placeholder="runway-ubuntu-4vcpu, gpu"
                autoCorrect="off"
                autoCapitalize="none"
                spellCheck={false}
              />
            </div>

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setCreateOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={saving}>
                {saving ? "Registering..." : "Create Agent Token"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={Boolean(issued)} onOpenChange={(o) => !o && setIssued(null)}>
        <DialogContent className="sm:max-w-2xl max-h-[90dvh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Install Runway Agent ({issued?.agent.name})</DialogTitle>
            <DialogDescription>
              Copy this token now—it will not be displayed again.
            </DialogDescription>
          </DialogHeader>
          {issued && (
            <div className="space-y-3 text-xs">
              <div className="space-y-1">
                <div className="flex items-center justify-between">
                  <span className="font-medium">Agent Token</span>
                  <Button
                    type="button"
                    size="xs"
                    variant="outline"
                    onClick={() => void copyText("token", issued.token)}
                  >
                    <HugeiconsIcon icon={Copy01Icon} strokeWidth={2} />
                    <span>Copy Token</span>
                  </Button>
                </div>
                <pre className="overflow-x-auto border border-border bg-muted/50 p-2 font-mono text-[11px]">
                  {issued.token}
                </pre>
              </div>

              <div className="space-y-1">
                <div className="flex items-center justify-between">
                  <span className="font-medium">Linux &amp; macOS One-Line Install (systemd / launchd)</span>
                  <Button
                    type="button"
                    size="xs"
                    variant="outline"
                    onClick={() => void copyText("Linux/macOS command", unixInstallCmd)}
                  >
                    <HugeiconsIcon icon={Copy01Icon} strokeWidth={2} />
                    <span>Copy</span>
                  </Button>
                </div>
                <pre className="overflow-x-auto border border-border bg-muted/50 p-2 font-mono text-[11px] whitespace-pre-wrap break-all">
                  {unixInstallCmd}
                </pre>
              </div>

              <div className="space-y-1">
                <div className="flex items-center justify-between">
                  <span className="font-medium">Windows PowerShell Install (Scheduled Task)</span>
                  <Button
                    type="button"
                    size="xs"
                    variant="outline"
                    onClick={() => void copyText("Windows command", winInstallCmd)}
                  >
                    <HugeiconsIcon icon={Copy01Icon} strokeWidth={2} />
                    <span>Copy</span>
                  </Button>
                </div>
                <pre className="overflow-x-auto border border-border bg-muted/50 p-2 font-mono text-[11px] whitespace-pre-wrap break-all">
                  {winInstallCmd}
                </pre>
              </div>
            </div>
          )}
          <DialogFooter>
            <Button type="button" onClick={() => setIssued(null)}>
              Done
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={Boolean(deleteTarget)} onOpenChange={(o) => !o && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete Registered Agent?</AlertDialogTitle>
            <AlertDialogDescription>
              Revoke credentials and remove <strong>{deleteTarget?.name}</strong> from Runway?
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
