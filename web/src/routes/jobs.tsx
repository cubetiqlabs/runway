import React, { useCallback, useEffect, useState } from "react"
import { createFileRoute } from "@tanstack/react-router"
import { HugeiconsIcon } from "@hugeicons/react"
import { CheckmarkCircle02Icon, CancelCircleIcon, RefreshIcon, Search01Icon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { ResponsiveTable } from "@/components/responsive-table"
import { StatusBadge } from "@/components/status-badge"
import { Badge } from "@/components/ui/badge"
import { api } from "@/lib/api"
import type { WorkflowJob } from "@/lib/types"
import { toast } from "sonner"

export const Route = createFileRoute("/jobs")({
  component: JobsPage,
})

function JobsPage() {
  const [jobs, setJobs] = useState<WorkflowJob[]>([])
  const [statusTab, setStatusTab] = useState("all")
  const [search, setSearch] = useState("")
  const [loading, setLoading] = useState(true)
  const [actionJob, setActionJob] = useState<{ job: WorkflowJob; mode: "approve" | "reject" } | null>(null)
  const [reason, setReason] = useState("")

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.listJobs({
        status: statusTab === "all" ? undefined : statusTab,
        search: search.trim() || undefined,
      })
      setJobs(res.items)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load workflow jobs")
    } finally {
      setLoading(false)
    }
  }, [statusTab, search])

  useEffect(() => {
    void load()
  }, [load])

  const submitModeration = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!actionJob) return
    try {
      if (actionJob.mode === "approve") {
        await api.approveJob(actionJob.job.id, reason.trim() || undefined)
        toast.success("Job approved and queued for runner provisioning")
      } else {
        await api.rejectJob(actionJob.job.id, reason.trim() || undefined)
        toast.success("Job rejected")
      }
      setActionJob(null)
      setReason("")
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Moderation action failed")
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">Job Moderation Queue</h1>
          <p className="text-xs text-muted-foreground">
            Review queued workflow jobs, inspect policy decisions, and approve or reject gated runs.
          </p>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={() => void load()}>
          <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
          <span>Refresh</span>
        </Button>
      </div>

      <div className="flex flex-wrap items-end justify-between gap-3">
        <Tabs value={statusTab} onValueChange={(v) => setStatusTab(v || "all")} className="w-full sm:w-auto">
          <TabsList className="w-full sm:w-auto max-w-full overflow-x-auto justify-start">
            <TabsTrigger value="all">All</TabsTrigger>
            <TabsTrigger value="awaiting_approval">Awaiting Approval</TabsTrigger>
            <TabsTrigger value="in_progress">Running</TabsTrigger>
            <TabsTrigger value="rejected">Rejected</TabsTrigger>
            <TabsTrigger value="completed">Completed</TabsTrigger>
          </TabsList>
        </Tabs>

        <search className="w-full sm:w-auto">
          <form
            onSubmit={(e) => {
              e.preventDefault()
              void load()
            }}
            className="flex items-center gap-2"
          >
            <Label htmlFor="job-search" className="sr-only">
              Search repository, job, or actor
            </Label>
            <Input
              id="job-search"
              type="search"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="owner/repo or actor..."
              className="w-full sm:w-56"
              autoCorrect="off"
              autoCapitalize="none"
              spellCheck={false}
            />
            <Button type="submit" variant="secondary" size="default">
              <HugeiconsIcon icon={Search01Icon} strokeWidth={2} />
              <span>Search</span>
            </Button>
          </form>
        </search>
      </div>

      <ResponsiveTable
        id="jobs-table"
        caption="Moderated GitHub Actions workflow jobs"
        headers={["Repository", "Job / Branch", "Actor", "Pool", "Policy Decision", "Status", "Actions"]}
        empty={!loading && jobs.length === 0}
        emptyMessage="No workflow jobs match this filter."
      >
        {jobs.map((job) => (
          <tr key={job.id}>
            <th scope="row">
              <div className="font-medium">{job.repo_full_name}</div>
              {job.is_fork_pr && (
                <Badge variant="destructive" className="mt-1 text-[10px]">
                  Fork PR
                </Badge>
              )}
            </th>
            <td data-label="Job / Branch">
              <div className="font-medium">{job.job_name}</div>
              <div className="font-mono text-[11px] text-muted-foreground">
                {job.head_branch || "—"}
              </div>
            </td>
            <td data-label="Actor">{job.actor || "—"}</td>
            <td data-label="Pool">{job.matched_pool_name || "—"}</td>
            <td data-label="Policy Decision">
              <div className="space-y-1">
                <StatusBadge status={job.moderation_decision} />
                {job.moderation_reason && (
                  <p className="text-[11px] text-muted-foreground max-w-52 truncate" title={job.moderation_reason}>
                    {job.moderation_reason}
                  </p>
                )}
              </div>
            </td>
            <td data-label="Status">
              <StatusBadge status={job.status} />
            </td>
            <td data-label="Actions">
              {job.status === "awaiting_approval" || job.status === "queued" ? (
                <div className="flex items-center gap-1.5">
                  <Button
                    type="button"
                    size="xs"
                    onClick={() => {
                      setReason("")
                      setActionJob({ job, mode: "approve" })
                    }}
                  >
                    <HugeiconsIcon icon={CheckmarkCircle02Icon} strokeWidth={2} />
                    <span>Approve</span>
                  </Button>
                  <Button
                    type="button"
                    size="xs"
                    variant="destructive"
                    onClick={() => {
                      setReason("")
                      setActionJob({ job, mode: "reject" })
                    }}
                  >
                    <HugeiconsIcon icon={CancelCircleIcon} strokeWidth={2} />
                    <span>Reject</span>
                  </Button>
                </div>
              ) : (
                <span className="text-[11px] text-muted-foreground">
                  {job.assigned_runner_name || "Finalized"}
                </span>
              )}
            </td>
          </tr>
        ))}
      </ResponsiveTable>

      <Dialog open={Boolean(actionJob)} onOpenChange={(o) => !o && setActionJob(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {actionJob?.mode === "approve" ? "Approve Workflow Job" : "Reject Workflow Job"}
            </DialogTitle>
            <DialogDescription>
              {actionJob?.job.repo_full_name} — {actionJob?.job.job_name}
            </DialogDescription>
          </DialogHeader>
          <form onSubmit={submitModeration} className="space-y-3">
            <div className="space-y-1">
              <Label htmlFor="mod-reason">Moderation Reason (recorded in audit log)</Label>
              <Input
                id="mod-reason"
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                placeholder={
                  actionJob?.mode === "approve"
                    ? "Verified PR diff and trusted contributor"
                    : "Untrusted workflow modification"
                }
              />
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setActionJob(null)}>
                Cancel
              </Button>
              <Button
                type="submit"
                variant={actionJob?.mode === "reject" ? "destructive" : "default"}
              >
                {actionJob?.mode === "approve" ? "Approve & Provision" : "Reject Job"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
