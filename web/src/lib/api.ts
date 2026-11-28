import { appStore, setCsrfTokenState } from "./store"
import type {
  Agent,
  AgentDetail,
  AgentMetricPoint,
  AgentTask,
  AuditLog,
  DashboardOverview,
  EventRecord,
  EventSummary,
  GitHubAppSettingsStatus,
  Installation,
  ModerationPolicy,
  PageResponse,
  PolicyAction,
  RegisterAgentResponse,
  Runner,
  RunnerPool,
  User,
  WebhookDelivery,
  WorkflowJob,
} from "./types"

export function setCsrfToken(token: string) {
  setCsrfTokenState(token)
}

export class ApiError extends Error {
  status: number
  code: string
  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

export interface ListQueryParams {
  limit?: number
  offset?: number
  page?: number
  sort_by?: string
  sort_dir?: "asc" | "desc" | ""
  search?: string
  [key: string]: string | number | boolean | undefined
}

function buildQuery(params: ListQueryParams = {}, defaultLimit = 25): string {
  const qs = new URLSearchParams()
  for (const [key, val] of Object.entries(params)) {
    if (val === undefined || val === null || val === "" || val === "all") {
      continue
    }
    qs.set(key, String(val))
  }
  if (!qs.has("limit")) {
    qs.set("limit", String(defaultLimit))
  }
  const encoded = qs.toString()
  return encoded ? `?${encoded}` : ""
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method || "GET").toUpperCase()
  const headers = new Headers(init.headers)
  if (!headers.has("Accept")) {
    headers.set("Accept", "application/json")
  }
  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json")
  }
  const csrfToken = appStore.state.csrfToken
  if (
    csrfToken &&
    (method === "POST" || method === "PUT" || method === "PATCH" || method === "DELETE")
  ) {
    headers.set("X-CSRF-Token", csrfToken)
  }

  const res = await fetch(path, {
    ...init,
    method,
    headers,
    credentials: "include",
  })

  if (!res.ok) {
    let code = "http_error"
    let message = `Request failed (${res.status})`
    try {
      const data = await res.json()
      if (data?.error?.message) {
        code = data.error.code || code
        message = data.error.message
      }
    } catch {
      // ignore non-JSON error
    }
    throw new ApiError(res.status, code, message)
  }

  if (res.status === 204) {
    return undefined as T
  }
  return (await res.json()) as T
}

export const api = {
  login: (email: string, password: string) =>
    request<{ user: User; csrf_token: string; expires_at: string }>("/api/v1/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    }),
  logout: () => request<{ ok: boolean }>("/api/v1/auth/logout", { method: "POST" }),
  me: (signal?: AbortSignal) =>
    request<{ user: User; csrf_token: string }>("/api/v1/auth/me", { signal }),

  overview: (signal?: AbortSignal) =>
    request<DashboardOverview>("/api/v1/dashboard/overview", { signal }),

  listPools: (params: ListQueryParams = {}, signal?: AbortSignal) =>
    request<PageResponse<RunnerPool>>(`/api/v1/pools${buildQuery(params, 50)}`, { signal }),
  createPool: (pool: Partial<RunnerPool>) =>
    request<RunnerPool>("/api/v1/pools", { method: "POST", body: JSON.stringify(pool) }),
  updatePool: (id: string, pool: Partial<RunnerPool>) =>
    request<RunnerPool>(`/api/v1/pools/${id}`, { method: "PUT", body: JSON.stringify(pool) }),
  deletePool: (id: string) =>
    request<{ ok: boolean }>(`/api/v1/pools/${id}`, { method: "DELETE" }),

  listRunners: (params: ListQueryParams = {}, signal?: AbortSignal) =>
    request<PageResponse<Runner>>(`/api/v1/runners${buildQuery(params, 25)}`, { signal }),
  getRunnerLogs: (id: string, params: ListQueryParams = {}, signal?: AbortSignal) =>
    request<PageResponse<EventRecord>>(`/api/v1/runners/${id}/logs${buildQuery(params, 200)}`, {
      signal,
    }),
  drainRunner: (id: string) =>
    request<Runner>(`/api/v1/runners/${id}/drain`, { method: "POST" }),
  terminateRunner: (id: string) =>
    request<{ ok: boolean }>(`/api/v1/runners/${id}/terminate`, { method: "POST" }),

  listJobs: (params: ListQueryParams = {}, signal?: AbortSignal) =>
    request<PageResponse<WorkflowJob>>(`/api/v1/jobs${buildQuery(params, 25)}`, { signal }),
  approveJob: (id: string, reason?: string) =>
    request<WorkflowJob>(`/api/v1/jobs/${id}/approve`, {
      method: "POST",
      body: JSON.stringify({ reason }),
    }),
  rejectJob: (id: string, reason?: string) =>
    request<WorkflowJob>(`/api/v1/jobs/${id}/reject`, {
      method: "POST",
      body: JSON.stringify({ reason }),
    }),

  listPolicies: (params: ListQueryParams = {}, signal?: AbortSignal) =>
    request<PageResponse<ModerationPolicy>>(`/api/v1/policies${buildQuery(params, 25)}`, {
      signal,
    }),
  createPolicy: (policy: Partial<ModerationPolicy>) =>
    request<ModerationPolicy>("/api/v1/policies", {
      method: "POST",
      body: JSON.stringify(policy),
    }),
  updatePolicy: (id: string, policy: Partial<ModerationPolicy>) =>
    request<ModerationPolicy>(`/api/v1/policies/${id}`, {
      method: "PUT",
      body: JSON.stringify(policy),
    }),
  deletePolicy: (id: string) =>
    request<{ ok: boolean }>(`/api/v1/policies/${id}`, { method: "DELETE" }),
  simulatePolicy: (payload: {
    repo_full_name: string
    head_branch: string
    actor: string
    labels: string[]
    is_fork_pr: boolean
  }) =>
    request<{
      decision: { action: PolicyAction; policy_name: string; reason: string }
      matched_pool: RunnerPool | null
    }>("/api/v1/policies/simulate", {
      method: "POST",
      body: JSON.stringify(payload),
    }),

  listInstallations: (params: ListQueryParams = {}, signal?: AbortSignal) =>
    request<PageResponse<Installation>>(`/api/v1/installations${buildQuery(params, 25)}`, {
      signal,
    }),
  syncInstallations: () =>
    request<{ synced: number }>("/api/v1/installations/sync", { method: "POST" }),
  listDeliveries: (params: ListQueryParams = {}, signal?: AbortSignal) =>
    request<PageResponse<WebhookDelivery>>(`/api/v1/webhooks/deliveries${buildQuery(params, 25)}`, {
      signal,
    }),
  listAuditLogs: (params: ListQueryParams = {}, signal?: AbortSignal) =>
    request<PageResponse<AuditLog>>(`/api/v1/audit-logs${buildQuery(params, 25)}`, { signal }),
  listEvents: (params: ListQueryParams = {}, signal?: AbortSignal) =>
    request<PageResponse<EventRecord>>(`/api/v1/events${buildQuery(params, 25)}`, { signal }),
  eventSummary: (window = "24h", signal?: AbortSignal) =>
    request<EventSummary>(`/api/v1/events/summary?window=${encodeURIComponent(window)}`, {
      signal,
    }),
  settingsStatus: (signal?: AbortSignal) =>
    request<GitHubAppSettingsStatus>("/api/v1/settings/github-app", { signal }),

  listAgents: (params: ListQueryParams = {}, signal?: AbortSignal) =>
    request<PageResponse<Agent>>(`/api/v1/agents${buildQuery(params, 25)}`, { signal }),
  getAgent: (id: string, window = "1h", signal?: AbortSignal) =>
    request<AgentDetail>(`/api/v1/agents/${id}?window=${encodeURIComponent(window)}`, { signal }),
  getAgentMetrics: (id: string, window = "1h", limit = 240, signal?: AbortSignal) =>
    request<{ items: AgentMetricPoint[] }>(
      `/api/v1/agents/${id}/metrics?window=${encodeURIComponent(window)}&limit=${limit}`,
      { signal }
    ),
  listAgentTasks: (id: string, params: ListQueryParams = {}, signal?: AbortSignal) =>
    request<PageResponse<AgentTask>>(`/api/v1/agents/${id}/tasks${buildQuery(params, 25)}`, {
      signal,
    }),
  registerAgent: (payload: {
    name: string
    os: string
    arch: string
    runtime: string
    labels: string[]
    pool_id?: string | null
    max_concurrency: number
  }) =>
    request<RegisterAgentResponse>("/api/v1/agents", {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  drainAgent: (id: string) => request<Agent>(`/api/v1/agents/${id}/drain`, { method: "POST" }),
  resumeAgent: (id: string) => request<Agent>(`/api/v1/agents/${id}/resume`, { method: "POST" }),
  rotateAgentToken: (id: string) =>
    request<RegisterAgentResponse>(`/api/v1/agents/${id}/rotate-token`, { method: "POST" }),
  deleteAgent: (id: string) =>
    request<{ ok: boolean }>(`/api/v1/agents/${id}`, { method: "DELETE" }),
}
