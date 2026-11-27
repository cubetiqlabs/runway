import React, { useCallback, useEffect, useState } from "react"
import { createFileRoute } from "@tanstack/react-router"
import { HugeiconsIcon } from "@hugeicons/react"
import { Add01Icon, Edit02Icon, Delete02Icon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
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
import { api } from "@/lib/api"
import type { BuildProfile, CacheScope, RunnerPool } from "@/lib/types"
import { toast } from "sonner"

export const Route = createFileRoute("/pools")({
  component: PoolsPage,
})

interface PoolFormState {
  id?: string
  name: string
  slug: string
  description: string
  labelsText: string
  os: string
  arch: string
  vcpu: string
  memory_mb: string
  disk_gb: string
  max_concurrency: string
  provider: "simulated" | "docker" | "agent"
  container_image: string
  build_profile: BuildProfile
  cache_enabled: boolean
  cache_scope: CacheScope
  cache_max_gb: string
  sandbox_mode: string
  enabled: boolean
}

const EMPTY_FORM: PoolFormState = {
  name: "",
  slug: "",
  description: "",
  labelsText: "self-hosted, linux, x64",
  os: "linux",
  arch: "x64",
  vcpu: "4",
  memory_mb: "8192",
  disk_gb: "40",
  max_concurrency: "10",
  provider: "simulated",
  container_image: "ghcr.io/cubetiqlabs/runway-runner:latest",
  build_profile: "fast",
  cache_enabled: true,
  cache_scope: "repo",
  cache_max_gb: "20",
  sandbox_mode: "auto",
  enabled: true,
}

function PoolsPage() {
  const [pools, setPools] = useState<RunnerPool[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const pageSize = 20
  const [dialogOpen, setDialogOpen] = useState(false)
  const [form, setForm] = useState<PoolFormState>(EMPTY_FORM)
  const [saving, setSaving] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<RunnerPool | null>(null)

  const load = useCallback(async () => {
    try {
      const res = await api.listPools({ limit: pageSize, page })
      setPools(res.items)
      setTotal(res.total)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load runner pools")
    }
  }, [page])

  useEffect(() => {
    void load()
  }, [load])

  const openCreate = () => {
    setForm(EMPTY_FORM)
    setDialogOpen(true)
  }

  const openEdit = (p: RunnerPool) => {
    setForm({
      id: p.id,
      name: p.name,
      slug: p.slug,
      description: p.description,
      labelsText: p.labels.join(", "),
      os: p.os,
      arch: p.arch,
      vcpu: String(p.vcpu),
      memory_mb: String(p.memory_mb),
      disk_gb: String(p.disk_gb),
      max_concurrency: String(p.max_concurrency),
      provider: p.provider,
      container_image: p.container_image,
      build_profile: p.build_profile || "standard",
      cache_enabled: p.cache_enabled ?? true,
      cache_scope: p.cache_scope || "repo",
      cache_max_gb: String(p.cache_max_gb || 20),
      sandbox_mode: p.sandbox_mode || "auto",
      enabled: p.enabled,
    })
    setDialogOpen(true)
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!form.name.trim()) {
      toast.error("Pool name is required")
      return
    }
    setSaving(true)
    const payload: Partial<RunnerPool> = {
      name: form.name.trim(),
      slug: form.slug.trim() || undefined,
      description: form.description.trim(),
      labels: form.labelsText
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean),
      os: form.os,
      arch: form.arch,
      vcpu: Number(form.vcpu) || 4,
      memory_mb: Number(form.memory_mb) || 8192,
      disk_gb: Number(form.disk_gb) || 40,
      max_concurrency: Number(form.max_concurrency) || 10,
      provider: form.provider,
      container_image: form.container_image.trim(),
      build_profile: form.build_profile,
      cache_enabled: form.cache_enabled,
      cache_scope: form.cache_scope,
      cache_max_gb: Number(form.cache_max_gb) || 20,
      sandbox_mode: form.sandbox_mode,
      enabled: form.enabled,
    }
    try {
      if (form.id) {
        await api.updatePool(form.id, payload)
        toast.success("Runner pool updated")
      } else {
        await api.createPool(payload)
        toast.success("Runner pool created")
      }
      setDialogOpen(false)
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to save runner pool")
    } finally {
      setSaving(false)
    }
  }

  const toggleEnabled = async (p: RunnerPool, enabled: boolean) => {
    try {
      await api.updatePool(p.id, { ...p, enabled })
      toast.success(`${p.name} ${enabled ? "enabled" : "disabled"}`)
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to toggle pool")
    }
  }

  const confirmDelete = async () => {
    if (!deleteTarget) return
    try {
      await api.deletePool(deleteTarget.id)
      toast.success(`Deleted pool ${deleteTarget.name}`)
      setDeleteTarget(null)
      void load()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to delete pool")
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">Runner Pools</h1>
          <p className="text-xs text-muted-foreground">
            Define high-speed machine profiles, Ultra Build acceleration, isolated caching, and sandbox runtimes.
          </p>
        </div>
        <Button type="button" size="sm" onClick={openCreate}>
          <HugeiconsIcon icon={Add01Icon} strokeWidth={2} />
          <span>New Pool</span>
        </Button>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        {pools.map((p) => (
          <Card key={p.id}>
            <CardHeader className="flex flex-row items-start justify-between gap-3">
              <div>
                <div className="flex flex-wrap items-center gap-2">
                  <CardTitle>{p.name}</CardTitle>
                  <Badge
                    variant={p.build_profile === "ultra" ? "default" : "outline"}
                    className="uppercase text-[10px]"
                  >
                    {p.build_profile || "standard"}
                  </Badge>
                </div>
                <CardDescription className="font-mono text-[11px]">{p.slug}</CardDescription>
              </div>
              <Switch
                checked={p.enabled}
                onCheckedChange={(checked) => void toggleEnabled(p, checked)}
                aria-label={`Toggle ${p.name}`}
              />
            </CardHeader>
            <CardContent className="space-y-3">
              {p.description && <p className="text-xs text-muted-foreground">{p.description}</p>}
              <div className="flex flex-wrap gap-1.5">
                {p.labels.map((lbl) => (
                  <Badge key={lbl} variant="secondary" className="font-mono text-[10px]">
                    {lbl}
                  </Badge>
                ))}
              </div>
              <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 border-t border-border pt-3 text-[11px]">
                <div>
                  <span className="text-muted-foreground block">Compute</span>
                  <span className="font-medium">
                    {p.vcpu} vCPU · {Math.round(p.memory_mb / 1024)} GB
                  </span>
                </div>
                <div>
                  <span className="text-muted-foreground block">Concurrency</span>
                  <span className="font-medium">Max {p.max_concurrency}</span>
                </div>
                <div>
                  <span className="text-muted-foreground block">Cache</span>
                  <span className="font-medium capitalize">
                    {p.cache_enabled ? `${p.cache_scope} (${p.cache_max_gb || 20}GB)` : "Off"}
                  </span>
                </div>
                <div>
                  <span className="text-muted-foreground block">Provider / Sandbox</span>
                  <span className="font-medium capitalize">
                    {p.provider} · {p.sandbox_mode || "auto"}
                  </span>
                </div>
              </div>
              <div className="flex items-center justify-end gap-2 pt-1">
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
            </CardContent>
          </Card>
        ))}
      </div>

      {total > pageSize && (
        <div className="flex items-center justify-between border border-border bg-card px-3 py-2 text-xs">
          <span className="text-muted-foreground">
            Page {page} of {totalPages} ({total} pools)
          </span>
          <div className="flex items-center gap-1.5">
            <Button
              type="button"
              size="xs"
              variant="outline"
              disabled={page <= 1}
              onClick={() => setPage((p) => p - 1)}
            >
              Previous
            </Button>
            <Button
              type="button"
              size="xs"
              variant="outline"
              disabled={page >= totalPages}
              onClick={() => setPage((p) => p + 1)}
            >
              Next
            </Button>
          </div>
        </div>
      )}

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="sm:max-w-lg max-h-[90dvh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{form.id ? "Edit Runner Pool" : "Create Runner Pool"}</DialogTitle>
            <DialogDescription>
              Jobs whose GitHub Actions <code className="font-mono">runs-on</code> labels match
              this pool will route here with configured cache and Ultra Build optimizations.
            </DialogDescription>
          </DialogHeader>
          <form onSubmit={handleSave} className="space-y-3">
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label htmlFor="pool-name">Name</Label>
                <Input
                  id="pool-name"
                  required
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  placeholder="Ubuntu 24.04 · 16 vCPU Ultra"
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor="pool-slug">Slug</Label>
                <Input
                  id="pool-slug"
                  value={form.slug}
                  onChange={(e) => setForm({ ...form, slug: e.target.value })}
                  placeholder="runway-ubuntu-16vcpu"
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </div>
            </div>

            <div className="space-y-1">
              <Label htmlFor="pool-desc">Description</Label>
              <Textarea
                id="pool-desc"
                rows={2}
                value={form.description}
                onChange={(e) => setForm({ ...form, description: e.target.value })}
              />
            </div>

            <div className="space-y-1">
              <Label htmlFor="pool-labels">Runner Labels (comma-separated)</Label>
              <Input
                id="pool-labels"
                required
                value={form.labelsText}
                onChange={(e) => setForm({ ...form, labelsText: e.target.value })}
                autoCorrect="off"
                autoCapitalize="none"
                spellCheck={false}
              />
            </div>

            <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
              <div className="space-y-1">
                <Label htmlFor="pool-vcpu">vCPU</Label>
                <Input
                  id="pool-vcpu"
                  inputMode="numeric"
                  value={form.vcpu}
                  onChange={(e) => setForm({ ...form, vcpu: e.target.value })}
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor="pool-mem">RAM (MB)</Label>
                <Input
                  id="pool-mem"
                  inputMode="numeric"
                  value={form.memory_mb}
                  onChange={(e) => setForm({ ...form, memory_mb: e.target.value })}
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor="pool-disk">Disk (GB)</Label>
                <Input
                  id="pool-disk"
                  inputMode="numeric"
                  value={form.disk_gb}
                  onChange={(e) => setForm({ ...form, disk_gb: e.target.value })}
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor="pool-max">Max Jobs</Label>
                <Input
                  id="pool-max"
                  inputMode="numeric"
                  value={form.max_concurrency}
                  onChange={(e) => setForm({ ...form, max_concurrency: e.target.value })}
                />
              </div>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label htmlFor="pool-os">OS</Label>
                <Select value={form.os} onValueChange={(v) => setForm({ ...form, os: v || "linux" })}>
                  <SelectTrigger id="pool-os" className="w-full">
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
                <Label htmlFor="pool-arch">Architecture</Label>
                <Select
                  value={form.arch}
                  onValueChange={(v) => setForm({ ...form, arch: v || "x64" })}
                >
                  <SelectTrigger id="pool-arch" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="x64">x64 (amd64)</SelectItem>
                    <SelectItem value="arm64">arm64</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-1">
                <Label htmlFor="pool-provider">Provider</Label>
                <Select
                  value={form.provider}
                  onValueChange={(v) =>
                    setForm({
                      ...form,
                      provider: (v as "simulated" | "docker" | "agent") || "simulated",
                    })
                  }
                >
                  <SelectTrigger id="pool-provider" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="simulated">Simulated</SelectItem>
                    <SelectItem value="docker">Docker Engine</SelectItem>
                    <SelectItem value="agent">Runway Agent (Remote Host)</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1">
                <Label htmlFor="pool-image">Container Image</Label>
                <Input
                  id="pool-image"
                  value={form.container_image}
                  onChange={(e) => setForm({ ...form, container_image: e.target.value })}
                  autoCorrect="off"
                  autoCapitalize="none"
                  spellCheck={false}
                />
              </div>
            </div>

            <div className="grid gap-3 sm:grid-cols-2 border-t border-border pt-3">
              <div className="space-y-1">
                <Label htmlFor="pool-build-profile">Build Speed Profile</Label>
                <Select
                  value={form.build_profile}
                  onValueChange={(v) =>
                    setForm({ ...form, build_profile: (v as BuildProfile) || "standard" })
                  }
                >
                  <SelectTrigger id="pool-build-profile" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="standard">Standard</SelectItem>
                    <SelectItem value="fast">Fast (tmpfs + BuildKit)</SelectItem>
                    <SelectItem value="ultra">Ultra Build (tmpfs + DinD Layer Cache)</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1">
                <Label htmlFor="pool-sandbox">Sandbox Mode</Label>
                <Select
                  value={form.sandbox_mode}
                  onValueChange={(v) => setForm({ ...form, sandbox_mode: v || "auto" })}
                >
                  <SelectTrigger id="pool-sandbox" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="auto">Auto (gVisor if available)</SelectItem>
                    <SelectItem value="gvisor">gVisor (runsc)</SelectItem>
                    <SelectItem value="kata">Kata Containers</SelectItem>
                    <SelectItem value="runc">Standard OCI (runc)</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className="grid gap-3 sm:grid-cols-3 items-end">
              <div className="flex items-center justify-between border border-border px-3 py-2">
                <Label htmlFor="pool-cache-enabled">Persistent Cache</Label>
                <Switch
                  id="pool-cache-enabled"
                  checked={form.cache_enabled}
                  onCheckedChange={(checked) => setForm({ ...form, cache_enabled: checked })}
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor="pool-cache-scope">Cache Scope</Label>
                <Select
                  value={form.cache_scope}
                  onValueChange={(v) => setForm({ ...form, cache_scope: (v as CacheScope) || "repo" })}
                >
                  <SelectTrigger id="pool-cache-scope" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="repo">Per-Repo (Isolated)</SelectItem>
                    <SelectItem value="pool">Pool-Wide</SelectItem>
                    <SelectItem value="none">Disabled</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1">
                <Label htmlFor="pool-cache-gb">Cache Max (GB)</Label>
                <Input
                  id="pool-cache-gb"
                  inputMode="numeric"
                  value={form.cache_max_gb}
                  onChange={(e) => setForm({ ...form, cache_max_gb: e.target.value })}
                />
              </div>
            </div>

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setDialogOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={saving}>
                {saving ? "Saving..." : "Save Pool"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <AlertDialog open={Boolean(deleteTarget)} onOpenChange={(o) => !o && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete Runner Pool?</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to delete <strong>{deleteTarget?.name}</strong>? Existing
              historical job records will remain intact.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => void confirmDelete()}>
              Delete Pool
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
