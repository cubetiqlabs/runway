import { useEffect, useMemo, useState } from "react"
import { createFileRoute, Link } from "@tanstack/react-router"
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useForm } from "@tanstack/react-form"
import { useSelector } from "@tanstack/react-store"
import { z } from "zod"
import { HugeiconsIcon } from "@hugeicons/react"
import {
  Add01Icon,
  RefreshIcon,
  PauseCircleIcon,
  PlayIcon,
  Delete02Icon,
  Copy01Icon,
  Key01Icon,
  ViewIcon,
} from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Field, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
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
import { api } from "@/lib/api"
import { agentsQueryOptions, poolsQueryOptions, queryKeys } from "@/lib/query"
import { appStore } from "@/lib/store"
import type { Agent, RegisterAgentResponse } from "@/lib/types"
import { toast } from "sonner"

export const Route = createFileRoute("/agents")({
  component: AgentsPage,
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

const RUNTIME_OPTIONS: ComboboxOption[] = [
  { value: "auto", label: "Auto (Docker or Native)" },
  { value: "native", label: "Native Host Runner" },
  { value: "docker", label: "Docker Containers" },
]

const agentFormSchema = z.object({
  name: z.string().trim().min(1, "Agent name is required."),
  os: z.string(),
  arch: z.string(),
  runtime: z.string(),
  labelsText: z.string(),
  pool_id: z.string(),
  max_concurrency: z.string(),
})

type AgentFormValues = z.infer<typeof agentFormSchema>

const INITIAL_FORM: AgentFormValues = {
  name: "",
  os: "linux",
  arch: "x64",
  runtime: "auto",
  labelsText: "",
  pool_id: "any",
  max_concurrency: "2",
}

function AgentsPage() {
  const queryClient = useQueryClient()
  const livePolling = useSelector(appStore, (s) => s.livePolling)

  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(25)
  const [search, setSearch] = useState("")
  const [statusFilter, setStatusFilter] = useState("all")
  const [osFilter, setOsFilter] = useState("all")
  const [runtimeFilter, setRuntimeFilter] = useState("all")
  const [sortBy, setSortBy] = useState("created_at")
  const [sortDir, setSortDir] = useState<SortDirection>("desc")

  const [createOpen, setCreateOpen] = useState(false)
  const [issued, setIssued] = useState<RegisterAgentResponse | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<Agent | null>(null)

  const queryParams = useMemo(
    () => ({
      limit: pageSize,
      page,
      search: search.trim() || undefined,
      status: statusFilter,
      os: osFilter,
      runtime: runtimeFilter,
      sort_by: sortBy,
      sort_dir: sortDir,
    }),
    [pageSize, page, search, statusFilter, osFilter, runtimeFilter, sortBy, sortDir]
  )

  const agentsQuery = useQuery({
    ...agentsQueryOptions(queryParams),
    placeholderData: keepPreviousData,
    refetchInterval: (query) => {
      if (!livePolling) return false
      const items = query.state.data?.items ?? []
      return items.some((a) => a.status === "draining") ? 4_000 : 15_000
    },
  })

  const poolsQuery = useQuery(poolsQueryOptions({ limit: 100 }))

  useEffect(() => {
    if (agentsQuery.error) {
      toast.error(
        agentsQuery.error instanceof Error ? agentsQuery.error.message : "Failed to load agents"
      )
    }
  }, [agentsQuery.error])

  const registerMutation = useMutation({
    mutationFn: (value: AgentFormValues) =>
      api.registerAgent({
        name: value.name.trim(),
        os: value.os,
        arch: value.arch,
        runtime: value.runtime,
        labels: value.labelsText
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean),
        pool_id: value.pool_id === "any" ? null : value.pool_id,
        max_concurrency: Number(value.max_concurrency) || 2,
      }),
    onSuccess: (res) => {
      setCreateOpen(false)
      form.reset(INITIAL_FORM)
      setIssued(res)
      void queryClient.invalidateQueries({ queryKey: ["agents"] })
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview })
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to register agent")
    },
  })

  const drainMutation = useMutation({
    mutationFn: (agent: Agent) => api.drainAgent(agent.id),
    onSuccess: (updated, agent) => {
      toast.success(
        updated.status === "drained"
          ? `Agent ${agent.name} drained (idle)`
          : `Agent ${agent.name} draining active jobs`
      )
      void queryClient.invalidateQueries({ queryKey: ["agents"] })
      void queryClient.invalidateQueries({ queryKey: ["agents", agent.id] })
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview })
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to drain agent")
    },
  })

  const resumeMutation = useMutation({
    mutationFn: (agent: Agent) => api.resumeAgent(agent.id),
    onSuccess: (updated, agent) => {
      toast.success(`Agent ${agent.name} resumed (${updated.status})`)
      void queryClient.invalidateQueries({ queryKey: ["agents"] })
      void queryClient.invalidateQueries({ queryKey: ["agents", agent.id] })
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview })
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to resume agent")
    },
  })

  const rotateMutation = useMutation({
    mutationFn: (agent: Agent) => api.rotateAgentToken(agent.id),
    onSuccess: (res, agent) => {
      setIssued(res)
      toast.success(`Rotated token for ${agent.name}`)
      void queryClient.invalidateQueries({ queryKey: ["agents"] })
      void queryClient.invalidateQueries({ queryKey: ["agents", agent.id] })
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to rotate token")
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (agent: Agent) => api.deleteAgent(agent.id),
    onSuccess: (_res, agent) => {
      toast.success(`Deleted agent ${agent.name}`)
      setDeleteTarget(null)
      void queryClient.invalidateQueries({ queryKey: ["agents"] })
      void queryClient.invalidateQueries({ queryKey: queryKeys.overview })
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Failed to delete agent")
    },
  })

  const form = useForm({
    defaultValues: INITIAL_FORM,
    validators: {
      onSubmit: agentFormSchema,
    },
    onSubmit: async ({ value }) => {
      await registerMutation.mutateAsync(value)
    },
  })

  const pools = poolsQuery.data?.items ?? []
  const poolOptions = useMemo<ComboboxOption[]>(
    () => [
      { value: "any", label: "Any compatible pool" },
      ...pools.map((p) => ({
        value: p.id,
        label: `${p.name} (${p.os}/${p.arch})`,
        description: p.slug,
      })),
    ],
    [pools]
  )

  const copyText = async (label: string, text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      toast.success(`Copied ${label}`)
    } catch {
      toast.error("Clipboard copy unavailable")
    }
  }

  const columns: DataTableColumn<Agent>[] = useMemo(
    () => [
      {
        id: "name",
        header: "Agent",
        sortable: true,
        isRowHeader: true,
        hideable: false,
        cell: (a) => (
          <div>
            <Link
              to="/agents/$agentId"
              params={{ agentId: a.id }}
              className="font-medium text-primary hover:underline"
            >
              {a.name}
            </Link>
            <div className="font-mono text-[10px] text-muted-foreground">
              {a.hostname || a.token_prefix}
              {a.pool_name ? ` · ${a.pool_name}` : ""}
            </div>
          </div>
        ),
      },
      {
        id: "os",
        header: "Platform",
        sortable: true,
        cell: (a) => (
          <span className="text-xs whitespace-nowrap">
            <span className="capitalize">{a.os}</span> / <span className="font-mono">{a.arch}</span>
          </span>
        ),
      },
      {
        id: "runtime",
        header: "Runtime",
        sortable: true,
        cell: (a) => (
          <div className="flex flex-wrap items-center gap-1">
            <Badge variant="secondary" className="capitalize text-[10px]">
              {a.runtime}
            </Badge>
            {a.sandbox_runtime && a.sandbox_runtime !== "auto" && (
              <Badge variant="outline" className="font-mono text-[10px]">
                {a.sandbox_runtime}
              </Badge>
            )}
          </div>
        ),
      },
      {
        id: "metrics",
        header: "Resources",
        cell: (a) => {
          const m = a.metrics
          if (!m || m.collected_at === undefined) {
            return <span className="text-[11px] text-muted-foreground">—</span>
          }
          return (
            <div className="font-mono text-[11px] whitespace-nowrap space-y-0.5">
              <div>
                CPU {Number(m.cpu_usage_percent ?? 0).toFixed(0)}% · RAM{" "}
                {Number(m.memory_usage_percent ?? 0).toFixed(0)}%
              </div>
              <div className="text-[10px] text-muted-foreground">
                Disk {Number(m.disk_usage_percent ?? 0).toFixed(0)}%
                {m.cache_hit_count || m.cache_miss_count
                  ? ` · Cache ${m.cache_hit_count ?? 0}/${(m.cache_hit_count ?? 0) + (m.cache_miss_count ?? 0)}`
                  : ""}
              </div>
            </div>
          )
        },
      },
      {
        id: "active_jobs",
        header: "Capacity",
        sortable: true,
        cell: (a) => (
          <span className="font-mono text-[11px] whitespace-nowrap">
            {a.active_jobs} / {a.max_concurrency}
          </span>
        ),
      },
      {
        id: "version",
        header: "Version",
        defaultVisible: true,
        cell: (a) => <span className="font-mono text-[11px]">{a.version || "—"}</span>,
      },
      {
        id: "status",
        header: "Status",
        sortable: true,
        cell: (a) => <StatusBadge status={a.status} />,
      },
      {
        id: "last_heartbeat_at",
        header: "Last Heartbeat",
        sortable: true,
        cell: (a) => (
          <span className="text-[11px] text-muted-foreground whitespace-nowrap">
            {a.last_heartbeat_at ? new Date(a.last_heartbeat_at).toLocaleString() : "Never"}
          </span>
        ),
      },
      {
        id: "actions",
        header: "Actions",
        hideable: false,
        cell: (a) => (
          <div className="flex flex-wrap items-center gap-1.5">
            <Link
              to="/agents/$agentId"
              params={{ agentId: a.id }}
              className="inline-flex h-6 items-center gap-1 border border-border bg-background px-2 text-[11px] font-medium hover:bg-muted"
            >
              <HugeiconsIcon icon={ViewIcon} strokeWidth={2} className="size-3" />
              <span>Details</span>
            </Link>
            {a.status === "draining" || a.status === "drained" ? (
              <Button
                type="button"
                size="xs"
                variant="outline"
                disabled={resumeMutation.isPending}
                onClick={() => resumeMutation.mutate(a)}
              >
                <HugeiconsIcon icon={PlayIcon} strokeWidth={2} />
                <span>Resume</span>
              </Button>
            ) : (
              <Button
                type="button"
                size="xs"
                variant="outline"
                disabled={a.status === "offline" || drainMutation.isPending}
                onClick={() => drainMutation.mutate(a)}
              >
                <HugeiconsIcon icon={PauseCircleIcon} strokeWidth={2} />
                <span>Drain</span>
              </Button>
            )}
            <Button
              type="button"
              size="xs"
              variant="outline"
              disabled={rotateMutation.isPending}
              onClick={() => rotateMutation.mutate(a)}
            >
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
        ),
      },
    ],
    [drainMutation, resumeMutation, rotateMutation]
  )

  const agents = agentsQuery.data?.items ?? []
  const total = agentsQuery.data?.total ?? 0

  const origin = typeof window !== "undefined" ? window.location.origin : "https://runway.example.com"
  const unixInstallCmd = issued
    ? `curl -fsSL https://raw.githubusercontent.com/cubetiqlabs/runway/main/scripts/install-agent.sh | sudo sh -s -- --server ${origin} --token ${issued.token}`
    : ""
  const gvisorInstallCmd = issued
    ? `curl -fsSL https://raw.githubusercontent.com/cubetiqlabs/runway/main/scripts/install-agent.sh | sudo sh -s -- --server ${origin} --token ${issued.token} --with-gvisor`
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
            macOS, and Windows hosts with real-time resource telemetry.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => void agentsQuery.refetch()}
          >
            <HugeiconsIcon icon={RefreshIcon} strokeWidth={2} />
            <span>Refresh</span>
          </Button>
          <Button
            type="button"
            size="sm"
            onClick={() => {
              form.reset(INITIAL_FORM)
              setCreateOpen(true)
            }}
          >
            <HugeiconsIcon icon={Add01Icon} strokeWidth={2} />
            <span>Register Agent</span>
          </Button>
        </div>
      </div>

      <DataTable
        id="agents-table"
        caption="Registered Runway distributed worker agents"
        data={agents}
        columns={columns}
        getRowId={(a) => a.id}
        loading={agentsQuery.isLoading}
        fetching={agentsQuery.isFetching}
        emptyMessage="No distributed agents match the current filters."
        searchValue={search}
        onSearchChange={(val) => {
          setPage(1)
          setSearch(val)
        }}
        searchPlaceholder="Search agent or hostname..."
        filters={[
          {
            id: "status",
            label: "Status",
            value: statusFilter,
            searchable: true,
            onChange: (v) => {
              setPage(1)
              setStatusFilter(v)
            },
            options: [
              { label: "All statuses", value: "all" },
              { label: "Online", value: "online" },
              { label: "Draining", value: "draining" },
              { label: "Drained", value: "drained" },
              { label: "Offline", value: "offline" },
            ],
          },
          {
            id: "os",
            label: "OS",
            value: osFilter,
            searchable: true,
            onChange: (v) => {
              setPage(1)
              setOsFilter(v)
            },
            options: [
              { label: "All OS", value: "all" },
              { label: "Linux", value: "linux" },
              { label: "macOS", value: "macos" },
              { label: "Windows", value: "windows" },
            ],
          },
          {
            id: "runtime",
            label: "Runtime",
            value: runtimeFilter,
            searchable: true,
            onChange: (v) => {
              setPage(1)
              setRuntimeFilter(v)
            },
            options: [
              { label: "All runtimes", value: "all" },
              { label: "Docker", value: "docker" },
              { label: "Native", value: "native" },
              { label: "Auto", value: "auto" },
            ],
          },
        ]}
        onResetFilters={() => {
          setPage(1)
          setSearch("")
          setStatusFilter("all")
          setOsFilter("all")
          setRuntimeFilter("all")
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

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="sm:max-w-lg max-h-[90dvh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Register Distributed Agent</DialogTitle>
            <DialogDescription>
              Create credentials for a Linux, macOS, or Windows host running{" "}
              <code className="font-mono">runway-agent</code>.
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
            <form.Field
              name="name"
              validators={{
                onBlur: ({ value }) => (!value.trim() ? "Agent name is required." : undefined),
              }}
            >
              {(field) => {
                const isInvalid =
                  (field.state.meta.isTouched || form.state.submissionAttempts > 0) &&
                  field.state.meta.errors.length > 0
                return (
                  <Field data-invalid={isInvalid || undefined}>
                    <FieldLabel htmlFor="agent-name">Agent Name</FieldLabel>
                    <Input
                      id="agent-name"
                      required
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(e) => field.handleChange(e.target.value)}
                      placeholder="build-mac-m3-01"
                      autoCorrect="off"
                      autoCapitalize="none"
                      spellCheck={false}
                      aria-invalid={isInvalid ? "true" : "false"}
                      aria-describedby={isInvalid ? "agent-name-err" : undefined}
                    />
                    <FieldError
                      id="agent-name-err"
                      errors={
                        isInvalid
                          ? field.state.meta.errors.map((err) =>
                              typeof err === "string" ? { message: err } : err
                            )
                          : []
                      }
                    />
                  </Field>
                )
              }}
            </form.Field>

            <div className="grid gap-3 sm:grid-cols-3">
              <form.Field name="os">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="agent-os">OS</FieldLabel>
                    <SearchableCombobox
                      id="agent-os"
                      ariaLabel="Agent operating system"
                      options={OS_OPTIONS}
                      value={field.state.value}
                      onValueChange={(v) => field.handleChange(v || "linux")}
                      placeholder="Search OS..."
                    />
                  </Field>
                )}
              </form.Field>

              <form.Field name="arch">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="agent-arch">Arch</FieldLabel>
                    <SearchableCombobox
                      id="agent-arch"
                      ariaLabel="Agent CPU architecture"
                      options={ARCH_OPTIONS}
                      value={field.state.value}
                      onValueChange={(v) => field.handleChange(v || "x64")}
                      placeholder="Search arch..."
                    />
                  </Field>
                )}
              </form.Field>

              <form.Field name="max_concurrency">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="agent-concurrency">Concurrency</FieldLabel>
                    <Input
                      id="agent-concurrency"
                      inputMode="numeric"
                      value={field.state.value}
                      onBlur={field.handleBlur}
                      onChange={(e) => field.handleChange(e.target.value)}
                    />
                  </Field>
                )}
              </form.Field>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <form.Field name="runtime">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="agent-runtime">Preferred Runtime</FieldLabel>
                    <SearchableCombobox
                      id="agent-runtime"
                      ariaLabel="Preferred runtime"
                      options={RUNTIME_OPTIONS}
                      value={field.state.value}
                      onValueChange={(v) => field.handleChange(v || "auto")}
                      placeholder="Search runtime..."
                    />
                  </Field>
                )}
              </form.Field>

              <form.Field name="pool_id">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor="agent-pool">Bind to Pool (optional)</FieldLabel>
                    <SearchableCombobox
                      id="agent-pool"
                      ariaLabel="Bind agent to runner pool"
                      options={poolOptions}
                      value={field.state.value}
                      onValueChange={(v) => field.handleChange(v || "any")}
                      placeholder="Search runner pool..."
                    />
                  </Field>
                )}
              </form.Field>
            </div>

            <form.Field name="labelsText">
              {(field) => (
                <Field>
                  <FieldLabel htmlFor="agent-labels">
                    Extra Labels (comma-separated, optional)
                  </FieldLabel>
                  <Input
                    id="agent-labels"
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder="runway-ubuntu-4vcpu, gpu"
                    autoCorrect="off"
                    autoCapitalize="none"
                    spellCheck={false}
                  />
                </Field>
              )}
            </form.Field>

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setCreateOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={registerMutation.isPending}>
                {registerMutation.isPending ? "Registering..." : "Create Agent Token"}
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
                  <span className="font-medium">Linux Sandboxed Install (gVisor + Isolated DinD)</span>
                  <Button
                    type="button"
                    size="xs"
                    variant="outline"
                    onClick={() => void copyText("Linux gVisor command", gvisorInstallCmd)}
                  >
                    <HugeiconsIcon icon={Copy01Icon} strokeWidth={2} />
                    <span>Copy</span>
                  </Button>
                </div>
                <pre className="overflow-x-auto border border-border bg-muted/50 p-2 font-mono text-[11px] whitespace-pre-wrap break-all">
                  {gvisorInstallCmd}
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
            <AlertDialogAction
              variant="destructive"
              disabled={deleteMutation.isPending}
              onClick={() => {
                if (deleteTarget) deleteMutation.mutate(deleteTarget)
              }}
            >
              Delete Agent
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
