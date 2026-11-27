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
import type { RunnerPool } from "@/lib/types"
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
  enabled: true,
}

function PoolsPage() {
  const [pools, setPools] = useState<RunnerPool[]>([])
  const [dialogOpen, setDialogOpen] = useState(false)
  const [form, setForm] = useState<PoolFormState>(EMPTY_FORM)
  const [saving, setSaving] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<RunnerPool | null>(null)

  const load = useCallback(async () => {
    try {
      const res = await api.listPools()
      setPools(res.items)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load runner pools")
    }
  }, [])

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

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="font-heading text-xl font-semibold">Runner Pools</h1>
          <p className="text-xs text-muted-foreground">
            Define Blacksmith-style ephemeral machine profiles, label matchers, and concurrency caps.
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
                <CardTitle>{p.name}</CardTitle>
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
              <div className="grid grid-cols-3 gap-2 border-t border-border pt-3 text-[11px]">
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
                  <span className="text-muted-foreground block">Provider</span>
                  <span className="font-medium capitalize">{p.provider}</span>
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

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{form.id ? "Edit Runner Pool" : "Create Runner Pool"}</DialogTitle>
            <DialogDescription>
              Jobs whose GitHub Actions <code className="font-mono">runs-on</code> labels are a subset
              of this pool&apos;s labels and slug will route here.
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
                  placeholder="Ubuntu 24.04 · 16 vCPU"
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

            <div className="grid grid-cols-4 gap-2">
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
