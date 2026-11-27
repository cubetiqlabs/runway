import React, { useCallback, useEffect, useState } from "react"
import { createFileRoute } from "@tanstack/react-router"
import { HugeiconsIcon } from "@hugeicons/react"
import { Add01Icon, Edit02Icon, Delete02Icon, PlayIcon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
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
import { Badge } from "@/components/ui/badge"
import { api } from "@/lib/api"
import type { ModerationPolicy, PolicyAction, RunnerPool } from "@/lib/types"
import { toast } from "sonner"

export const Route = createFileRoute("/policies")({
  component: PoliciesPage,
})

interface PolicyFormState {
  id?: string
  name: string
  description: string
  priority: string
  action: PolicyAction
  enabled: boolean
  repoPatterns: string
  branchPatterns: string
  actorPatterns: string
  labelPatterns: string
  block_fork_prs: boolean
  max_concurrent_per_repo: string
}

const EMPTY_POLICY: PolicyFormState = {
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

function splitPatterns(raw: string): string[] {
  return raw
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean)
}

function PoliciesPage() {
  const [policies, setPolicies] = useState<ModerationPolicy[]>([])
  const [dialogOpen, setDialogOpen] = useState(false)
  const [form, setForm] = useState<PolicyFormState>(EMPTY_POLICY)
  const [deleteTarget, setDeleteTarget] = useState<ModerationPolicy | null>(null)

  // Simulator state
  const [simOpen, setSimOpen] = useState(false)
  const [simRepo, setSimRepo] = useState("cubetiq/api")
  const [simBranch, setSimBranch] = useState("main")
  const [simActor, setSimActor] = useState("octocat")
  const [simLabels, setSimLabels] = useState("self-hosted, linux, x64, runway-ubuntu-4vcpu")
  const [simFork, setSimFork] = useState(false)
  const [simResult, setSimResult] = useState<{
    decision: { action: PolicyAction; policy_name: string; reason: string }
    matched_pool: RunnerPool | null
  } | null>(null)

  const load = useCallback(async () => {
    try {
      const res = await api.listPolicies()
      setPolicies(res.items)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load policies")
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const openCreate = () => {
    setForm(EMPTY_POLICY)
    setDialogOpen(true)
  }

  const openEdit = (p: ModerationPolicy) => {
    setForm({
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

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!form.name.trim()) {
      toast.error("Policy name is required")
      return
    }
    const maxConc = form.max_concurrent_per_repo.trim()
      ? Number(form.max_concurrent_per_repo.trim())
      : null
    const payload: Partial<ModerationPolicy> = {
      name: form.name.trim(),
      description: form.description.trim(),
      priority: Number(form.priority) || 100,
      action: form.action,
      enabled: form.enabled,
      repo_patterns: splitPatterns(form.repoPatterns),
      branch_patterns: splitPatterns(form.branchPatterns),
      actor_patterns: splitPatterns(form.actorPatterns),
      label_patterns: splitPatterns(form.labelPatterns),
      block_fork_prs: form.block_fork_prs,
      max_concurrent_per_repo: maxConc,
    }
    try {
      if (form.id) {
        await api.updatePolicy(form.id, payload)
        toast.success("Policy updated")
      } else {
        await api.createPolicy(payload)
        toast.success("Policy created")
      }
      setDialogOpen(false)
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to save policy")
    }
  }

  const handleSimulate = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      const res = await api.simulatePolicy({
        repo_full_name: simRepo.trim(),
        head_branch: simBranch.trim(),
        actor: simActor.trim(),
        labels: splitPatterns(simLabels),
        is_fork_pr: simFork,
      })
      setSimResult(res)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Simulation failed")
    }
  }

  const confirmDelete = async () => {
    if (!deleteTarget) return
    try {
      await api.deletePolicy(deleteTarget.id)
      toast.success(`Deleted policy ${deleteTarget.name}`)
      setDeleteTarget(null)
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to delete policy")
    }
  }

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

      <ResponsiveTable
        id="policies-table"
        caption="Runner moderation policies ordered by priority"
        headers={["Priority", "Policy", "Action", "Repo / Branch Scope", "Guards", "Enabled", "Actions"]}
        empty={policies.length === 0}
        emptyMessage="No moderation policies defined."
      >
        {policies.map((p) => (
          <tr key={p.id}>
            <td data-label="Priority" className="font-mono font-semibold">
              {p.priority}
            </td>
            <th scope="row">
              <div className="font-medium">{p.name}</div>
              {p.description && (
                <div className="text-[11px] text-muted-foreground max-w-60 truncate">
                  {p.description}
                </div>
              )}
            </th>
            <td data-label="Action">
              <StatusBadge status={p.action} />
            </td>
            <td data-label="Scope" className="font-mono text-[11px] text-muted-foreground">
              <div>repos: {p.repo_patterns.join(", ") || "*"}</div>
              <div>branches: {p.branch_patterns.join(", ") || "*"}</div>
            </td>
            <td data-label="Guards">
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
            </td>
            <td data-label="Enabled">
              <Switch
                checked={p.enabled}
                onCheckedChange={(checked) =>
                  void api.updatePolicy(p.id, { ...p, enabled: checked }).then(() => load())
                }
                aria-label={`Toggle ${p.name}`}
              />
            </td>
            <td data-label="Actions">
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
            </td>
          </tr>
        ))}
      </ResponsiveTable>

      {/* Policy Editor Dialog */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="sm:max-w-lg max-h-[90dvh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{form.id ? "Edit Policy" : "Create Moderation Policy"}</DialogTitle>
            <DialogDescription>
              Lower priority numbers evaluate first. First matching policy determines job fate.
            </DialogDescription>
          </DialogHeader>
          <form onSubmit={handleSave} className="space-y-3">
            <div className="grid gap-3 sm:grid-cols-3">
              <div className="space-y-1 sm:col-span-2">
                <Label htmlFor="pol-name">Policy Name</Label>
                <Input
                  id="pol-name"
                  required
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor="pol-priority">Priority</Label>
                <Input
                  id="pol-priority"
                  inputMode="numeric"
                  value={form.priority}
                  onChange={(e) => setForm({ ...form, priority: e.target.value })}
                />
              </div>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label htmlFor="pol-action">Action</Label>
                <Select
                  value={form.action}
                  onValueChange={(v) =>
                    setForm({ ...form, action: (v as PolicyAction) || "allow" })
                  }
                >
                  <SelectTrigger id="pol-action" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="allow">Allow</SelectItem>
                    <SelectItem value="require_approval">Require Approval</SelectItem>
                    <SelectItem value="deny">Deny</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1">
                <Label htmlFor="pol-cap">Max Concurrent / Repo</Label>
                <Input
                  id="pol-cap"
                  inputMode="numeric"
                  placeholder="Unlimited"
                  value={form.max_concurrent_per_repo}
                  onChange={(e) => setForm({ ...form, max_concurrent_per_repo: e.target.value })}
                />
              </div>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label htmlFor="pol-repos">Repo Patterns (glob)</Label>
                <Input
                  id="pol-repos"
                  value={form.repoPatterns}
                  onChange={(e) => setForm({ ...form, repoPatterns: e.target.value })}
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor="pol-branches">Branch Patterns</Label>
                <Input
                  id="pol-branches"
                  value={form.branchPatterns}
                  onChange={(e) => setForm({ ...form, branchPatterns: e.target.value })}
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </div>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label htmlFor="pol-actors">Actor Patterns</Label>
                <Input
                  id="pol-actors"
                  value={form.actorPatterns}
                  onChange={(e) => setForm({ ...form, actorPatterns: e.target.value })}
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor="pol-labels">Label Patterns</Label>
                <Input
                  id="pol-labels"
                  value={form.labelPatterns}
                  onChange={(e) => setForm({ ...form, labelPatterns: e.target.value })}
                  placeholder="Optional"
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </div>
            </div>

            <div className="flex items-center justify-between border border-border p-3">
              <div>
                <Label htmlFor="pol-fork">Block Fork Pull Requests</Label>
                <p className="text-[11px] text-muted-foreground">
                  Deny jobs triggered from external repository forks before provisioning.
                </p>
              </div>
              <Switch
                id="pol-fork"
                checked={form.block_fork_prs}
                onCheckedChange={(checked) => setForm({ ...form, block_fork_prs: checked })}
              />
            </div>

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setDialogOpen(false)}>
                Cancel
              </Button>
              <Button type="submit">Save Policy</Button>
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
          <form onSubmit={handleSimulate} className="space-y-3">
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label htmlFor="sim-repo">Repository</Label>
                <Input
                  id="sim-repo"
                  value={simRepo}
                  onChange={(e) => setSimRepo(e.target.value)}
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor="sim-branch">Head Branch</Label>
                <Input
                  id="sim-branch"
                  value={simBranch}
                  onChange={(e) => setSimBranch(e.target.value)}
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </div>
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label htmlFor="sim-actor">Actor</Label>
                <Input
                  id="sim-actor"
                  value={simActor}
                  onChange={(e) => setSimActor(e.target.value)}
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </div>
              <div className="space-y-1 flex items-end justify-between border border-border px-3 py-2">
                <Label htmlFor="sim-fork">Fork PR?</Label>
                <Switch id="sim-fork" checked={simFork} onCheckedChange={setSimFork} />
              </div>
            </div>
            <div className="space-y-1">
              <Label htmlFor="sim-labels">Job Labels</Label>
              <Input
                id="sim-labels"
                value={simLabels}
                onChange={(e) => setSimLabels(e.target.value)}
                autoCorrect="off"
                autoCapitalize="none"
                spellCheck={false}
              />
            </div>

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
              <Button type="submit">Run Simulation</Button>
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
            <AlertDialogAction variant="destructive" onClick={() => void confirmDelete()}>
              Delete Policy
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
