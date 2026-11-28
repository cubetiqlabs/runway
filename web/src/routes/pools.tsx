import { useEffect, useState } from "react"
import { createFileRoute } from "@tanstack/react-router"
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useForm } from "@tanstack/react-form"
import { z } from "zod"
import { HugeiconsIcon } from "@hugeicons/react"
import { Add01Icon, Edit02Icon, Delete02Icon, RefreshIcon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { Field, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
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
import { SearchableCombobox, type ComboboxOption } from "@/components/searchable-combobox"
import { api } from "@/lib/api"
import { poolsQueryOptions, queryKeys } from "@/lib/query"
import type { BuildProfile, CacheScope, RunnerPool } from "@/lib/types"
import { toast } from "sonner"

export const Route = createFileRoute("/pools")({
  component: PoolsPage,
})

const OS_OPTIONS: ComboboxOption[] = [
  { value: "linux", label: "Linux" },
  { value: "macos", label: "macOS" },
  { value: "windows", label: "Windows" },
]

const ARCH_OPTIONS: ComboboxOption[] = [
  { value: "x64", label: "x64 (amd64)" },
  { value: "arm64", label: "arm64" },
]

const PROVIDER_OPTIONS: ComboboxOption[] = [
  { value: "simulated", label: "Simulated", description: "In-memory simulated provisioner" },
  { value: "docker", label: "Docker Engine", description: "Local/remote Docker ephemeral containers" },
  { value: "agent", label: "Runway Agent (Remote Host)", description: "Distributed runway-agent fleet" },
]

const BUILD_PROFILE_OPTIONS: ComboboxOption[] = [
  { value: "standard", label: "Standard" },
  { value: "fast", label: "Fast (tmpfs + BuildKit)" },
  { value: "ultra", label: "Ultra Build (tmpfs + DinD Layer Cache)" },
]

const SANDBOX_OPTIONS: ComboboxOption[] = [
  { value: "auto", label: "Auto (gVisor if available)" },
  { value: "gvisor", label: "gVisor (runsc)" },
  { value: "kata", label: "Kata Containers" },
  { value: "runc", label: "Standard OCI (runc)" },
]

const CACHE_SCOPE_OPTIONS: ComboboxOption[] = [
  { value: "repo", label: "Per-Repo (Isolated)" },
  { value: "pool", label: "Pool-Wide" },
  { value: "none", label: "Disabled" },
]

const poolFormSchema = z.object({
  id: z.string(),
  name: z.string().trim().min(1, "Pool name is required."),
  slug: z.string(),
  description: z.string(),
  labelsText: z.string().trim().min(1, "At least one runner label is required."),
  os: z.string(),
  arch: z.string(),
  vcpu: z.string(),
  memory_mb: z.string(),
  disk_gb: z.string(),
  max_concurrency: z.string(),
  provider: z.enum(["simulated", "docker", "agent"]),
  container_image: z.string(),
  build_profile: z.enum(["standard", "fast", "ultra"]),
  cache_enabled: z.boolean(),
  cache_scope: z.enum(["repo", "pool", "none"]),
  cache_max_gb: z.string(),
  sandbox_mode: z.string(),
  enabled: z.boolean(),
})

type PoolFormValues = z.infer<typeof poolFormSchema>

const EMPTY_FORM: PoolFormValues = {
  id: "",
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
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const pageSize = 20
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingId, setEditingId] = useState("")
  const [deleteTarget, setDeleteTarget] = useState<RunnerPool | null>(null)

  const poolsQuery = useQuery({
    ...poolsQueryOptions({ limit: pageSize, page }),
    placeholderData: keepPreviousData,
  })

  useEffect(() => {
    if (poolsQuery.error) {
      toast.error(
        poolsQuery.error instanceof Error ? poolsQuery.error.message : "Failed to load runner pools"
      )
    }
  }, [poolsQuery.error])

  const saveMutation = useMutation({
    mutationFn: ({ id, payload }: { id?: string; payload: Partial<RunnerPool> }) =>
      id ? api.updatePool(id, payload) : api.createPool(payload),
    onSuccess: (_res, vars) => {
      toast.success(vars.id ? "Runner pool updated" : "Runner pool created")
      setDialogOpen(false)
      void queryClient.invalidateQueries({ queryKey: ["pools"] })
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview })
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to save runner pool")
    },
  })

  const toggleMutation = useMutation({
    mutationFn: ({ pool, enabled }: { pool: RunnerPool; enabled: boolean }) =>
      api.updatePool(pool.id, { ...pool, enabled }),
    onSuccess: (_res, vars) => {
      toast.success(`${vars.pool.name} ${vars.enabled ? "enabled" : "disabled"}`)
      void queryClient.invalidateQueries({ queryKey: ["pools"] })
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview })
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to toggle pool")
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deletePool(id),
    onSuccess: () => {
      toast.success(`Deleted pool ${deleteTarget?.name ?? ""}`)
      setDeleteTarget(null)
      void queryClient.invalidateQueries({ queryKey: ["pools"] })
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview })
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to delete pool")
    },
  })

  const form = useForm({
    defaultValues: EMPTY_FORM,
    validators: {
      onSubmit: poolFormSchema,
    },
    onSubmit: async ({ value }) => {
      const payload: Partial<RunnerPool> = {
        name: value.name.trim(),
        slug: value.slug.trim() || undefined,
        description: value.description.trim(),
        labels: value.labelsText
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean),
        os: value.os,
        arch: value.arch,
        vcpu: Number(value.vcpu) || 4,
        memory_mb: Number(value.memory_mb) || 8192,
        disk_gb: Number(value.disk_gb) || 40,
        max_concurrency: Number(value.max_concurrency) || 10,
        provider: value.provider,
        container_image: value.container_image.trim(),
        build_profile: value.build_profile as BuildProfile,
        cache_enabled: value.cache_enabled,
        cache_scope: value.cache_scope as CacheScope,
        cache_max_gb: Number(value.cache_max_gb) || 20,
        sandbox_mode: value.sandbox_mode,
        enabled: value.enabled,
      }
      await saveMutation.mutateAsync({ id: value.id || undefined, payload })
    },
  })

  const openCreate = () => {
    setEditingId("")
    form.reset(EMPTY_FORM)
    setDialogOpen(true)
  }

  const openEdit = (p: RunnerPool) => {
    setEditingId(p.id)
    form.reset({
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
      build_profile: (p.build_profile || "standard") as "standard" | "fast" | "ultra",
      cache_enabled: p.cache_enabled ?? true,
      cache_scope: (p.cache_scope || "repo") as "repo" | "pool" | "none",
      cache_max_gb: String(p.cache_max_gb || 20),
      sandbox_mode: p.sandbox_mode || "auto",
      enabled: p.enabled,
    })
    setDialogOpen(true)
  }

  const pools = poolsQuery.data?.items ?? []
  const total = poolsQuery.data?.total ?? 0
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
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={poolsQuery.isFetching}
            onClick={() => void poolsQuery.refetch()}
          >
            <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
            <span>Refresh</span>
          </Button>
          <Button type="button" size="sm" onClick={openCreate}>
            <HugeiconsIcon icon={Add01Icon} strokeWidth={2} />
            <span>New Pool</span>
          </Button>
        </div>
      </div>

      {poolsQuery.isPending ? (
        <div className="grid gap-4 md:grid-cols-2">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-44 w-full" />
          ))}
        </div>
      ) : (
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
                  disabled={toggleMutation.isPending}
                  onCheckedChange={(checked) => toggleMutation.mutate({ pool: p, enabled: checked })}
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
      )}

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
            <DialogTitle>{editingId ? "Edit Runner Pool" : "Create Runner Pool"}</DialogTitle>
            <DialogDescription>
              Jobs whose GitHub Actions <code className="font-mono">runs-on</code> labels match
              this pool will route here with configured cache and Ultra Build optimizations.
            </DialogDescription>
          </DialogHeader>
          <form
            onSubmit={(e) => {
              e.preventDefault()
              e.stopPropagation()
              void form.handleSubmit()
            }}
            className="space-y-3"
          >
            <div className="grid gap-3 sm:grid-cols-2">
              <form.Field name="name">
                {(field) => {
                  const invalid =
                    (field.state.meta.isTouched || form.state.submissionAttempts > 0) &&
                    !field.state.meta.isValid
                  return (
                    <Field data-invalid={invalid}>
                      <FieldLabel htmlFor="pool-name">Name</FieldLabel>
                      <Input
                        id="pool-name"
                        required
                        aria-invalid={invalid ? "true" : "false"}
                        aria-describedby={invalid ? "pool-name-err" : undefined}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(e) => field.handleChange(e.target.value)}
                        placeholder="Ubuntu 24.04 · 16 vCPU Ultra"
                      />
                      {invalid && <FieldError id="pool-name-err" errors={field.state.meta.errors} />}
                    </Field>
                  )
                }}
              </form.Field>
              <form.Field name="slug">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pool-slug">Slug</FieldLabel>
                    <Input
                      id="pool-slug"
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(e) => field.handleChange(e.target.value)}
                      placeholder="runway-ubuntu-16vcpu"
                      autoCorrect="off"
                      autoCapitalize="none"
                      spellCheck={false}
                    />
                  </Field>
                )}
              </form.Field>
            </div>

            <form.Field name="description">
              {(field) => (
                <Field>
                  <FieldLabel htmlFor="pool-desc">Description</FieldLabel>
                  <Textarea
                    id="pool-desc"
                    rows={2}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                  />
                </Field>
              )}
            </form.Field>

            <form.Field name="labelsText">
              {(field) => {
                const invalid =
                  (field.state.meta.isTouched || form.state.submissionAttempts > 0) &&
                  !field.state.meta.isValid
                return (
                  <Field data-invalid={invalid}>
                    <FieldLabel htmlFor="pool-labels">Runner Labels (comma-separated)</FieldLabel>
                    <Input
                      id="pool-labels"
                      required
                      aria-invalid={invalid ? "true" : "false"}
                      aria-describedby={invalid ? "pool-labels-err" : undefined}
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(e) => field.handleChange(e.target.value)}
                      autoCorrect="off"
                      autoCapitalize="none"
                      spellCheck={false}
                    />
                    {invalid && <FieldError id="pool-labels-err" errors={field.state.meta.errors} />}
                  </Field>
                )
              }}
            </form.Field>

            <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
              <form.Field name="vcpu">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pool-vcpu">vCPU</FieldLabel>
                    <Input
                      id="pool-vcpu"
                      inputMode="numeric"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                    />
                  </Field>
                )}
              </form.Field>
              <form.Field name="memory_mb">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pool-mem">RAM (MB)</FieldLabel>
                    <Input
                      id="pool-mem"
                      inputMode="numeric"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                    />
                  </Field>
                )}
              </form.Field>
              <form.Field name="disk_gb">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pool-disk">Disk (GB)</FieldLabel>
                    <Input
                      id="pool-disk"
                      inputMode="numeric"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                    />
                  </Field>
                )}
              </form.Field>
              <form.Field name="max_concurrency">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pool-max">Max Jobs</FieldLabel>
                    <Input
                      id="pool-max"
                      inputMode="numeric"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                    />
                  </Field>
                )}
              </form.Field>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <form.Field name="os">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pool-os">OS</FieldLabel>
                    <SearchableCombobox
                      id="pool-os"
                      ariaLabel="OS"
                      options={OS_OPTIONS}
                      value={field.state.value}
                      onValueChange={(v) => field.handleChange(v || "linux")}
                    />
                  </Field>
                )}
              </form.Field>
              <form.Field name="arch">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pool-arch">Architecture</FieldLabel>
                    <SearchableCombobox
                      id="pool-arch"
                      ariaLabel="Architecture"
                      options={ARCH_OPTIONS}
                      value={field.state.value}
                      onValueChange={(v) => field.handleChange(v || "x64")}
                    />
                  </Field>
                )}
              </form.Field>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <form.Field name="provider">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pool-provider">Provider</FieldLabel>
                    <SearchableCombobox
                      id="pool-provider"
                      ariaLabel="Provider"
                      options={PROVIDER_OPTIONS}
                      value={field.state.value}
                      onValueChange={(v) =>
                        field.handleChange((v as "simulated" | "docker" | "agent") || "simulated")
                      }
                    />
                  </Field>
                )}
              </form.Field>
              <form.Field name="container_image">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pool-image">Container Image</FieldLabel>
                    <Input
                      id="pool-image"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      autoCorrect="off"
                      autoCapitalize="none"
                      spellCheck={false}
                    />
                  </Field>
                )}
              </form.Field>
            </div>

            <div className="grid gap-3 sm:grid-cols-2 border-t border-border pt-3">
              <form.Field name="build_profile">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pool-build-profile">Build Speed Profile</FieldLabel>
                    <SearchableCombobox
                      id="pool-build-profile"
                      ariaLabel="Build Speed Profile"
                      options={BUILD_PROFILE_OPTIONS}
                      value={field.state.value}
                      onValueChange={(v) =>
                        field.handleChange((v as "standard" | "fast" | "ultra") || "standard")
                      }
                    />
                  </Field>
                )}
              </form.Field>
              <form.Field name="sandbox_mode">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pool-sandbox">Sandbox Mode</FieldLabel>
                    <SearchableCombobox
                      id="pool-sandbox"
                      ariaLabel="Sandbox Mode"
                      options={SANDBOX_OPTIONS}
                      value={field.state.value}
                      onValueChange={(v) => field.handleChange(v || "auto")}
                    />
                  </Field>
                )}
              </form.Field>
            </div>

            <div className="grid gap-3 sm:grid-cols-3 items-end">
              <form.Field name="cache_enabled">
                {(field) => (
                  <div className="flex items-center justify-between border border-border px-3 py-2">
                    <FieldLabel htmlFor="pool-cache-enabled">Persistent Cache</FieldLabel>
                    <Switch
                      id="pool-cache-enabled"
                      checked={field.state.value}
                      onCheckedChange={(checked) => field.handleChange(checked)}
                    />
                  </div>
                )}
              </form.Field>
              <form.Field name="cache_scope">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pool-cache-scope">Cache Scope</FieldLabel>
                    <SearchableCombobox
                      id="pool-cache-scope"
                      ariaLabel="Cache Scope"
                      options={CACHE_SCOPE_OPTIONS}
                      value={field.state.value}
                      onValueChange={(v) =>
                        field.handleChange((v as "repo" | "pool" | "none") || "repo")
                      }
                    />
                  </Field>
                )}
              </form.Field>
              <form.Field name="cache_max_gb">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="pool-cache-gb">Cache Max (GB)</FieldLabel>
                    <Input
                      id="pool-cache-gb"
                      inputMode="numeric"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                    />
                  </Field>
                )}
              </form.Field>
            </div>

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setDialogOpen(false)}>
                Cancel
              </Button>
              <form.Subscribe selector={(state) => state.isSubmitting}>
                {(isSubmitting) => (
                  <Button type="submit" disabled={isSubmitting || saveMutation.isPending}>
                    {isSubmitting || saveMutation.isPending ? "Saving..." : "Save Pool"}
                  </Button>
                )}
              </form.Subscribe>
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
            <AlertDialogAction
              variant="destructive"
              onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget.id)}
            >
              Delete Pool
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
