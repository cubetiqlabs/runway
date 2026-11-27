import React, { useCallback, useEffect, useMemo, useState } from "react"
import { createFileRoute } from "@tanstack/react-router"
import { HugeiconsIcon } from "@hugeicons/react"
import { CheckmarkCircle02Icon, CancelCircleIcon, RefreshIcon } from "@hugeicons/core-free-icons"
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
import { DataTable, type DataTableColumn, type SortDirection } from "@/components/data-table"
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
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(25)
  const [statusTab, setStatusTab] = useState("all")
  const [search, setSearch] = useState("")
  const [sortBy, setSortBy] = useState("queued_at")
  const [sortDir, setSortDir] = useState<SortDirection>("desc")
  const [loading, setLoading] = useState(true)
  const [actionJob, setActionJob] = useState<{ job: WorkflowJob; mode: "approve" | "reject" } | null>(null)
  const [reason, setReason] = useState("")

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.listJobs({
        status: statusTab,
        search: search.trim() || undefined,
        limit: pageSize,
        page,
        sort_by: sortBy,
        sort_dir: sortDir,
      })
      setJobs(res.items)
      setTotal(res.total)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load workflow jobs")
    } finally {
      setLoading(false)
    }
  }, [statusTab, search, pageSize, page, sortBy, sortDir])

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

  const columns: DataTableColumn<WorkflowJob>[] = useMemo(
    () => [
      {
        id: "repo_full_name",
        header: "Repository",
        sortable: true,
        isRowHeader: true,
        hideable: false,
        cell: (job) => (
          <div>
            <div className="font-medium">{job.repo_full_name}</div>
            {job.is_fork_pr && (
              <Badge variant="destructive" className="mt-1 text-[10px]">
                Fork PR
              </Badge>
            )}
          </div>
        ),
      },
      {
        id: "job_name",
        header: "Job / Branch",
        sortable: true,
        cell: (job) => (
          <div>
            <div className="font-medium">{job.job_name}</div>
            <div className="font-mono text-[11px] text-muted-foreground">
              {job.head_branch || "—"}
            </div>
          </div>
        ),
      },
      {
        id: "actor",
        header: "Actor",
        sortable: true,
        cell: (job) => job.actor || "—",
      },
      {
        id: "matched_pool_name",
        header: "Pool",
        sortable: true,
        cell: (job) => job.matched_pool_name || "—",
      },
      {
        id: "moderation_decision",
        header: "Policy Decision",
        sortable: true,
        cell: (job) => (
          <div className="space-y-1">
            <StatusBadge status={job.moderation_decision} />
            {job.moderation_reason && (
              <p className="text-[11px] text-muted-foreground max-w-52 truncate" title={job.moderation_reason}>
                {job.moderation_reason}
              </p>
            )}
          </div>
        ),
      },
      {
        id: "status",
        header: "Status",
        sortable: true,
        cell: (job) => <StatusBadge status={job.status} />,
      },
      {
        id: "queued_at",
        header: "Queued",
        sortable: true,
        defaultVisible: true,
        cell: (job) => (
          <span className="text-[11px] text-muted-foreground whitespace-nowrap">
            {new Date(job.queued_at).toLocaleString()}
          </span>
        ),
      },
      {
        id: "actions",
        header: "Actions",
        hideable: false,
        cell: (job) =>
          job.status === "awaiting_approval" || job.status === "queued" ? (
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
          ),
      },
    ],
    []
  )

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

      <Tabs
        value={statusTab}
        onValueChange={(v) => {
          setPage(1)
          setStatusTab(v || "all")
        }}
        className="w-full"
      >
        <TabsList className="w-full sm:w-auto max-w-full overflow-x-auto justify-start">
          <TabsTrigger value="all">All</TabsTrigger>
          <TabsTrigger value="awaiting_approval">Awaiting Approval</TabsTrigger>
          <TabsTrigger value="queued">Queued</TabsTrigger>
          <TabsTrigger value="provisioning">Provisioning</TabsTrigger>
          <TabsTrigger value="in_progress">Running</TabsTrigger>
          <TabsTrigger value="rejected">Rejected</TabsTrigger>
          <TabsTrigger value="completed">Completed</TabsTrigger>
        </TabsList>
      </Tabs>

      <DataTable
        id="jobs-table"
        caption="Moderated GitHub Actions workflow jobs"
        data={jobs}
        columns={columns}
        getRowId={(j) => j.id}
        loading={loading}
        emptyMessage="No workflow jobs match this filter."
        searchValue={search}
        onSearchChange={(val) => {
          setPage(1)
          setSearch(val)
        }}
        searchPlaceholder="Search owner/repo, job, or actor..."
        onResetFilters={() => {
          setPage(1)
          setSearch("")
          setStatusTab("all")
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
