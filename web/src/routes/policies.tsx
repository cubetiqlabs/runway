import { useEffect, useMemo, useState } from "react"
import { createFileRoute } from "@tanstack/react-router"
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useForm } from "@tanstack/react-form"
import { z } from "zod"
import { HugeiconsIcon } from "@hugeicons/react"
import { Add01Icon, Edit02Icon, Delete02Icon, PlayIcon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Field, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
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
import { SearchableCombobox, type ComboboxOption } from "@/components/searchable-combobox"
import { StatusBadge } from "@/components/status-badge"
import { Badge } from "@/components/ui/badge"
import { api } from "@/lib/api"
import { policiesQueryOptions } from "@/lib/query"
import type { ModerationPolicy, PolicyAction, RunnerPool } from "@/lib/types"
import { toast } from "sonner"

export const Route = createFileRoute("/policies")({
  component: PoliciesPage,
})

const ACTION_OPTIONS: ComboboxOption[] = [
  { value: "allow", label: "Allow", description: "Auto-approve and dispatch immediately" },
  {
    value: "require_approval",
    label: "Require Approval",
    description: "Hold in moderation queue for operator review",
  },
  { value: "deny", label: "Deny", description: "Reject workflow job before provisioning" },
]

const policyFormSchema = z.object({
  id: z.string(),
  name: z.string().trim().min(1, "Policy name is required."),
  description: z.string(),
  priority: z.string(),
  action: z.enum(["allow", "deny", "require_approval"]),
  enabled: z.boolean(),
  repoPatterns: z.string(),
  branchPatterns: z.string(),
  actorPatterns: z.string(),
  labelPatterns: z.string(),
  block_fork_prs: z.boolean(),
  max_concurrent_per_repo: z.string(),
})

type PolicyFormValues = z.infer<typeof policyFormSchema>

const EMPTY_POLICY: PolicyFormValues = {
  id: "",
  name: "",
  description: "",
  priority: "50",
  action: "allow",
  enabled: true,
  repoPatterns: "*/*",
  branchPatterns: "*",
  actorPatterns: "*",
  labelPatterns: "",
  block_fork_prs: true,
  max_concurrent_per_repo: "",
}

const simFormSchema = z.object({
  repo_full_name: z.string().trim().min(1, "Repository is required."),
  head_branch: z.string().trim().min(1, "Branch is required."),
  actor: z.string().trim().min(1, "Actor is required."),
  labels: z.string(),
  is_fork_pr: z.boolean(),
})

function splitPatterns(raw: string): string[] {
  return raw
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean)
}

function PoliciesPage() {
  const queryClient = useQueryClient()

  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(25)
  const [search, setSearch] = useState("")
  const [actionFilter, setActionFilter] = useState("all")
  const [enabledFilter, setEnabledFilter] = useState("all")
  const [sortBy, setSortBy] = useState("priority")
  const [sortDir, setSortDir] = useState<SortDirection>("asc")

  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingId, setEditingId] = useState("")
  const [deleteTarget, setDeleteTarget] = useState<ModerationPolicy | null>(null)

  const [simOpen, setSimOpen] = useState(false)
  const [simResult, setSimResult] = useState<{
    decision: { action: PolicyAction; policy_name: string; reason: string }
    matched_pool: RunnerPool | null
  } | null>(null)

  const queryParams = useMemo(
    () => ({
      limit: pageSize,
      page,
      search: search.trim() || undefined,
      action: actionFilter,
      enabled: enabledFilter,
      sort_by: sortBy,
      sort_dir: sortDir,
    }),
    [pageSize, page, search, actionFilter, enabledFilter, sortBy, sortDir]
  )

  const policiesQuery = useQuery({
    ...policiesQueryOptions(queryParams),
    placeholderData: keepPreviousData,
  })

  useEffect(() => {
    if (policiesQuery.error) {
      toast.error(
        policiesQuery.error instanceof Error
          ? policiesQuery.error.message
          : "Failed to load policies"
      )
    }
  }, [policiesQuery.error])

  const saveMutation = useMutation({
    mutationFn: ({ id, payload }: { id?: string; payload: Partial<ModerationPolicy> }) =>
      id ? api.updatePolicy(id, payload) : api.createPolicy(payload),
    onSuccess: (_res, vars) => {
      toast.success(vars.id ? "Policy updated" : "Policy created")
      setDialogOpen(false)
      void queryClient.invalidateQueries({ queryKey: ["policies"] })
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to save policy")
    },
  })

  const toggleMutation = useMutation({
    mutationFn: ({ policy, enabled }: { policy: ModerationPolicy; enabled: boolean }) =>
      api.updatePolicy(policy.id, { ...policy, enabled }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["policies"] })
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to toggle policy")
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deletePolicy(id),
    onSuccess: () => {
      toast.success(`Deleted policy ${deleteTarget?.name ?? ""}`)
      setDeleteTarget(null)
      void queryClient.invalidateQueries({ queryKey: ["policies"] })
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to delete policy")
    },
  })

  const simulateMutation = useMutation({
    mutationFn: (payload: {
      repo_full_name: string
      head_branch: string
      actor: string
      labels: string[]
      is_fork_pr: boolean
    }) => api.simulatePolicy(payload),
    onSuccess: (res) => {
      setSimResult(res)
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Simulation failed")
    },
  })

  const policyForm = useForm({
    defaultValues: EMPTY_POLICY,
    validators: {
      onSubmit: policyFormSchema,
    },
    onSubmit: async ({ value }) => {
      const maxConc = value.max_concurrent_per_repo.trim()
        ? Number(value.max_concurrent_per_repo.trim())
        : null
      const payload: Partial<ModerationPolicy> = {
        name: value.name.trim(),
        description: value.description.trim(),
        priority: Number(value.priority) || 100,
        action: value.action as PolicyAction,
        enabled: value.enabled,
        repo_patterns: splitPatterns(value.repoPatterns),
        branch_patterns: splitPatterns(value.branchPatterns),
        actor_patterns: splitPatterns(value.actorPatterns),
        label_patterns: splitPatterns(value.labelPatterns),
        block_fork_prs: value.block_fork_prs,
        max_concurrent_per_repo: maxConc,
      }
      await saveMutation.mutateAsync({ id: value.id || undefined, payload })
    },
  })

  const simForm = useForm({
    defaultValues: {
      repo_full_name: "cubetiq/api",
      head_branch: "main",
      actor: "octocat",
      labels: "self-hosted, linux, x64, runway-ubuntu-4vcpu",
      is_fork_pr: false,
    },
    validators: {
      onSubmit: simFormSchema,
    },
    onSubmit: async ({ value }) => {
      await simulateMutation.mutateAsync({
        repo_full_name: value.repo_full_name.trim(),
        head_branch: value.head_branch.trim(),
        actor: value.actor.trim(),
        labels: splitPatterns(value.labels),
        is_fork_pr: value.is_fork_pr,
      })
    },
  })

  const openCreate = () => {
    setEditingId("")
    policyForm.reset(EMPTY_POLICY)
    setDialogOpen(true)
  }

  const openEdit = (p: ModerationPolicy) => {
    setEditingId(p.id)
    policyForm.reset({
      id: p.id,
      name: p.name,
      description: p.description,
      priority: String(p.priority),
      action: p.action,
      enabled: p.enabled,
      repoPatterns: p.repo_patterns.join(", "),
      branchPatterns: p.branch_patterns.join(", "),
      actorPatterns: p.actor_patterns.join(", "),
      labelPatterns: p.label_patterns.join(", "),
      block_fork_prs: p.block_fork_prs,
      max_concurrent_per_repo: p.max_concurrent_per_repo ? String(p.max_concurrent_per_repo) : "",
    })
    setDialogOpen(true)
  }

  const columns: DataTableColumn<ModerationPolicy>[] = useMemo(
    () => [
      {
        id: "priority",
        header: "Priority",
        sortable: true,
        sortType: "number",
        cell: (p) => <span className="font-mono font-semibold">{p.priority}</span>,
      },
      {
        id: "name",
        header: "Policy",
        sortable: true,
        isRowHeader: true,
        hideable: false,
        cell: (p) => (
          <div>
            <div className="font-medium">{p.name}</div>
            {p.description && (
              <div className="text-[11px] text-muted-foreground max-w-60 truncate">
                {p.description}
              </div>
            )}
          </div>
        ),
      },
      {
        id: "action",
        header: "Action",
        sortable: true,
        cell: (p) => <StatusBadge status={p.action} />,
      },
      {
        id: "scope",
        header: "Repo / Branch Scope",
        cell: (p) => (
          <div className="font-mono text-[11px] text-muted-foreground">
            <div>repos: {p.repo_patterns.join(", ") || "*"}</div>
            <div>branches: {p.branch_patterns.join(", ") || "*"}</div>
          </div>
        ),
      },
      {
        id: "guards",
        header: "Guards",
        cell: (p) => (
          <div className="flex flex-wrap gap-1">
            {p.block_fork_prs && (
              <Badge variant="destructive" className="text-[10px]">
                Block Fork PRs
              </Badge>
            )}
            {p.max_concurrent_per_repo && (
              <Badge variant="outline" className="text-[10px]">
                Max {p.max_concurrent_per_repo}/repo
              </Badge>
            )}
          </div>
        ),
      },
      {
        id: "enabled",
        header: "Enabled",
        sortable: true,
        cell: (p) => (
          <Switch
            checked={p.enabled}
            disabled={toggleMutation.isPending}
            onCheckedChange={(checked) => toggleMutation.mutate({ policy: p, enabled: checked })}
            aria-label={`Toggle ${p.name}`}
          />
        ),
      },
      {
        id: "actions",
        header: "Actions",
        hideable: false,
        cell: (p) => (
          <div className="flex items-center gap-1.5">
            <Button type="button" size="xs" variant="outline" onClick={() => openEdit(p)}>
              <HugeiconsIcon icon={Edit02Icon} strokeWidth={2} />
              <span>Edit</span>
            </Button>
            <Button
              type="button"
              size="xs"
              variant="destructive"
              onClick={() => setDeleteTarget(p)}
            >
              <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
              <span>Delete</span>
            </Button>
          </div>
        ),
      },
    ],
    [toggleMutation]
  )

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">Moderation Policies</h1>
          <p className="text-xs text-muted-foreground">
            Deterministic priority-ordered guardrails controlling fork PRs, actors, branches, and
            concurrency limits.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button type="button" variant="outline" size="sm" onClick={() => setSimOpen(true)}>
            <HugeiconsIcon icon={PlayIcon} strokeWidth={2} />
            <span>Simulate</span>
          </Button>
          <Button type="button" size="sm" onClick={openCreate}>
            <HugeiconsIcon icon={Add01Icon} strokeWidth={2} />
            <span>New Policy</span>
          </Button>
        </div>
      </div>

      <DataTable
        id="policies-table"
        caption="Runner moderation policies ordered by priority"
        data={policiesQuery.data?.items ?? []}
        columns={columns}
        getRowId={(p) => p.id}
        loading={policiesQuery.isPending}
        fetching={policiesQuery.isFetching && !policiesQuery.isPending}
        emptyMessage="No moderation policies match this filter."
        searchValue={search}
        onSearchChange={(val) => {
          setPage(1)
          setSearch(val)
        }}
        searchPlaceholder="Search policy name or description..."
        filters={[
          {
            id: "action",
            label: "Action",
            searchable: true,
            value: actionFilter,
            onChange: (v) => {
              setPage(1)
              setActionFilter(v)
            },
            options: [
              { label: "All actions", value: "all" },
              { label: "Allow", value: "allow" },
              { label: "Require Approval", value: "require_approval" },
              { label: "Deny", value: "deny" },
            ],
          },
          {
            id: "enabled",
            label: "State",
            value: enabledFilter,
            onChange: (v) => {
              setPage(1)
              setEnabledFilter(v)
            },
            options: [
              { label: "All states", value: "all" },
              { label: "Enabled", value: "true" },
              { label: "Disabled", value: "false" },
            ],
          },
        ]}
        onResetFilters={() => {
          setPage(1)
          setSearch("")
          setActionFilter("all")
          setEnabledFilter("all")
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
          total: policiesQuery.data?.total ?? 0,
          onPageChange: setPage,
          onPageSizeChange: (n) => {
            setPage(1)
            setPageSize(n)
          },
        }}
      />

      {/* Policy Editor Dialog */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="sm:max-w-lg max-h-[90dvh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{editingId ? "Edit Policy" : "Create Moderation Policy"}</DialogTitle>
            <DialogDescription>
              Lower priority numbers evaluate first. First matching policy determines job fate.
            </DialogDescription>
          </DialogHeader>
          <form
            onSubmit={(e) => {
              e.preventDefault()
              e.stopPropagation()
              void policyForm.handleSubmit()
            }}
            className="space-y-3"
          >
            <div className="grid gap-3 sm:grid-cols-3">
              <policyForm.Field name="name">
                {(field) => {
                  const invalid =
                    (field.state.meta.isTouched || policyForm.state.submissionAttempts > 0) &&
                    !field.state.meta.isValid
                  return (
                    <Field data-invalid={invalid} className="sm:col-span-2">
                      <FieldLabel htmlFor="pol-name">Policy Name</FieldLabel>
                      <Input
                        id="pol-name"
                        required
                        aria-invalid={invalid ? "true" : "false"}
                        aria-describedby={invalid ? "pol-name-err" : undefined}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                      />
                      {invalid && <FieldError id="pol-name-err" errors={field.state.meta.errors} />}
                    </Field>
                  )
                }}
              </policyForm.Field>
              <policyForm.Field name="priority">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pol-priority">Priority</FieldLabel>
                    <Input
                      id="pol-priority"
                      inputMode="numeric"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                    />
                  </Field>
                )}
              </policyForm.Field>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <policyForm.Field name="action">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pol-action">Action</FieldLabel>
                    <SearchableCombobox
                      id="pol-action"
                      ariaLabel="Policy Action"
                      options={ACTION_OPTIONS}
                      value={field.state.value}
                      onValueChange={(v) => field.handleChange((v as PolicyAction) || "allow")}
                    />
                  </Field>
                )}
              </policyForm.Field>
              <policyForm.Field name="max_concurrent_per_repo">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pol-cap">Max Concurrent / Repo</FieldLabel>
                    <Input
                      id="pol-cap"
                      inputMode="numeric"
                      placeholder="Unlimited"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                    />
                  </Field>
                )}
              </policyForm.Field>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <policyForm.Field name="repoPatterns">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pol-repos">Repo Patterns (glob)</FieldLabel>
                    <Input
                      id="pol-repos"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      autoCorrect="off"
                      autoCapitalize="none"
                      spellCheck={false}
                    />
                  </Field>
                )}
              </policyForm.Field>
              <policyForm.Field name="branchPatterns">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pol-branches">Branch Patterns</FieldLabel>
                    <Input
                      id="pol-branches"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      autoCorrect="off"
                      autoCapitalize="none"
                      spellCheck={false}
                    />
                  </Field>
                )}
              </policyForm.Field>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <policyForm.Field name="actorPatterns">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pol-actors">Actor Patterns</FieldLabel>
                    <Input
                      id="pol-actors"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      autoCorrect="off"
                      autoCapitalize="none"
                      spellCheck={false}
                    />
                  </Field>
                )}
              </policyForm.Field>
              <policyForm.Field name="labelPatterns">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pol-labels">Label Patterns</FieldLabel>
                    <Input
                      id="pol-labels"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      placeholder="Optional"
                      autoCorrect="off"
                      autoCapitalize="none"
                      spellCheck={false}
                    />
                  </Field>
                )}
              </policyForm.Field>
            </div>

            <policyForm.Field name="block_fork_prs">
              {(field) => (
                <div className="flex items-center justify-between border border-border p-3">
                  <div>
                    <FieldLabel htmlFor="pol-fork">Block Fork Pull Requests</FieldLabel>
                    <p className="text-[11px] text-muted-foreground">
                      Deny jobs triggered from external repository forks before provisioning.
                    </p>
                  </div>
                  <Switch
                    id="pol-fork"
                    checked={field.state.value}
                    onCheckedChange={(checked) => field.handleChange(checked)}
                  />
                </div>
              )}
            </policyForm.Field>

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setDialogOpen(false)}>
                Cancel
              </Button>
              <policyForm.Subscribe selector={(state) => state.isSubmitting}>
                {(isSubmitting) => (
                  <Button type="submit" disabled={isSubmitting || saveMutation.isPending}>
                    {isSubmitting || saveMutation.isPending ? "Saving..." : "Save Policy"}
                  </Button>
                )}
              </policyForm.Subscribe>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Policy Simulator Dialog */}
      <Dialog open={simOpen} onOpenChange={setSimOpen}>
        <DialogContent className="sm:max-w-lg max-h-[90dvh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Moderation Policy Simulator</DialogTitle>
            <DialogDescription>
              Dry-run a simulated <code className="font-mono">workflow_job.queued</code> event against
              live policies and runner pools.
            </DialogDescription>
          </DialogHeader>
          <form
            onSubmit={(e) => {
              e.preventDefault()
              e.stopPropagation()
              void simForm.handleSubmit()
            }}
            className="space-y-3"
          >
            <div className="grid gap-3 sm:grid-cols-2">
              <simForm.Field name="repo_full_name">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="sim-repo">Repository</FieldLabel>
                    <Input
                      id="sim-repo"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      autoCorrect="off"
                      autoCapitalize="none"
                      spellCheck={false}
                    />
                  </Field>
                )}
              </simForm.Field>
              <simForm.Field name="head_branch">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="sim-branch">Head Branch</FieldLabel>
                    <Input
                      id="sim-branch"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      autoCorrect="off"
                      autoCapitalize="none"
                      spellCheck={false}
                    />
                  </Field>
                )}
              </simForm.Field>
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <simForm.Field name="actor">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="sim-actor">Actor</FieldLabel>
                    <Input
                      id="sim-actor"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      autoCorrect="off"
                      autoCapitalize="none"
                      spellCheck={false}
                    />
                  </Field>
                )}
              </simForm.Field>
              <simForm.Field name="is_fork_pr">
                {(field) => (
                  <div className="flex items-end justify-between border border-border px-3 py-2">
                    <FieldLabel htmlFor="sim-fork">Fork PR?</FieldLabel>
                    <Switch
                      id="sim-fork"
                      checked={field.state.value}
                      onCheckedChange={(checked) => field.handleChange(checked)}
                    />
                  </div>
                )}
              </simForm.Field>
            </div>
            <simForm.Field name="labels">
              {(field) => (
                <Field>
                  <FieldLabel htmlFor="sim-labels">Job Labels</FieldLabel>
                  <Input
                    id="sim-labels"
                    value={field.state.value}
                    onChange={(e) => field.handleChange(e.target.value)}
                    autoCorrect="off"
                    autoCapitalize="none"
                    spellCheck={false}
                  />
                </Field>
              )}
            </simForm.Field>

            {simResult && (
              <div className="border border-border bg-muted/40 p-3 space-y-1.5 text-xs">
                <div className="flex items-center justify-between">
                  <span className="font-medium">Decision</span>
                  <StatusBadge status={simResult.decision.action} />
                </div>
                <p className="text-muted-foreground">{simResult.decision.reason}</p>
                <div className="text-[11px]">
                  Matched pool:{" "}
                  <strong>{simResult.matched_pool?.name || "None (job ignored)"}</strong>
                </div>
              </div>
            )}

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setSimOpen(false)}>
                Close
              </Button>
              <Button type="submit" disabled={simulateMutation.isPending}>
                {simulateMutation.isPending ? "Simulating..." : "Run Simulation"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <AlertDialog open={Boolean(deleteTarget)} onOpenChange={(o) => !o && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete Policy?</AlertDialogTitle>
            <AlertDialogDescription>
              Delete moderation policy <strong>{deleteTarget?.name}</strong>?
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget.id)}
            >
              Delete Policy
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
