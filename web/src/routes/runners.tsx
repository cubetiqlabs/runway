import React, { useCallback, useEffect, useState } from "react"
import { createFileRoute } from "@tanstack/react-router"
import { HugeiconsIcon } from "@hugeicons/react"
import { RefreshIcon, PauseCircleIcon, Delete02Icon, Search01Icon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
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
import type { Runner } from "@/lib/types"
import { toast } from "sonner"

export const Route = createFileRoute("/runners")({
  component: RunnersPage,
})

function RunnersPage() {
  const [runners, setRunners] = useState<Runner[]>([])
  const [statusFilter, setStatusFilter] = useState<string>("all")
  const [search, setSearch] = useState("")
  const [loading, setLoading] = useState(true)
  const [confirmTarget, setConfirmTarget] = useState<Runner | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.listRunners({
        status: statusFilter === "all" ? undefined : statusFilter,
        search: search.trim() || undefined,
      })
      setRunners(res.items)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load runners")
    } finally {
      setLoading(false)
    }
  }, [statusFilter, search])

  useEffect(() => {
    void load()
  }, [load])

  const handleSearchSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    void load()
  }

  const handleDrain = async (runner: Runner) => {
    try {
      await api.drainRunner(runner.id)
      toast.success(`Draining runner ${runner.name}`)
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to drain runner")
    }
  }

  const handleTerminate = async () => {
    if (!confirmTarget) return
    try {
      await api.terminateRunner(confirmTarget.id)
      toast.success(`Terminated runner ${confirmTarget.name}`)
      setConfirmTarget(null)
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to terminate runner")
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">Runner Fleet</h1>
          <p className="text-xs text-muted-foreground">
            Inspect active ephemeral runners, drain capacity safely, or force-terminate instances.
          </p>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={() => void load()}>
          <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
          <span>Refresh</span>
        </Button>
      </div>

      <search>
        <form onSubmit={handleSearchSubmit} className="flex flex-wrap items-end gap-3">
          <div className="space-y-1 flex-1 min-w-48 max-w-xs">
            <Label htmlFor="runner-search">Search runner name</Label>
            <Input
              id="runner-search"
              type="search"
              name="search"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="runway-ubuntu..."
              autoCorrect="off"
              autoCapitalize="none"
              spellCheck={false}
            />
          </div>
          <div className="space-y-1 w-44">
            <Label htmlFor="runner-status-filter">Status</Label>
            <Select value={statusFilter} onValueChange={(v) => setStatusFilter(v || "all")}>
              <SelectTrigger id="runner-status-filter" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All statuses</SelectItem>
                <SelectItem value="provisioning">Provisioning</SelectItem>
                <SelectItem value="online">Online</SelectItem>
                <SelectItem value="busy">Busy</SelectItem>
                <SelectItem value="draining">Draining</SelectItem>
                <SelectItem value="offline">Offline</SelectItem>
                <SelectItem value="terminated">Terminated</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <Button type="submit" variant="secondary" size="default">
            <HugeiconsIcon icon={Search01Icon} strokeWidth={2} />
            <span>Filter</span>
          </Button>
        </form>
      </search>

      <ResponsiveTable
        id="runners-table"
        caption="Self-hosted GitHub Actions runners managed by Runway"
        headers={["Runner Name", "Pool", "Provider", "Status", "Labels", "Job ID", "Actions"]}
        empty={!loading && runners.length === 0}
        emptyMessage="No runners match the current filters."
      >
        {runners.map((rn) => (
          <tr key={rn.id}>
            <th scope="row" className="font-mono text-[11px]">
              {rn.name}
            </th>
            <td data-label="Pool">{rn.pool_name || "—"}</td>
            <td data-label="Provider" className="capitalize">
              {rn.provider}
            </td>
            <td data-label="Status">
              <StatusBadge status={rn.status} />
            </td>
            <td data-label="Labels" className="max-w-52 truncate text-muted-foreground">
              {rn.labels.join(", ")}
            </td>
            <td data-label="Job ID" className="font-mono text-[11px]">
              {rn.current_job_id ?? "—"}
            </td>
            <td data-label="Actions">
              <div className="flex items-center gap-1.5">
                <Button
                  type="button"
                  size="xs"
                  variant="outline"
                  disabled={rn.status === "draining" || rn.status === "terminated"}
                  onClick={() => void handleDrain(rn)}
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
            </td>
          </tr>
        ))}
      </ResponsiveTable>

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
            <AlertDialogAction variant="destructive" onClick={() => void handleTerminate()}>
              Terminate Runner
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
