import type {
  AuditLog,
  DashboardOverview,
  GitHubAppSettingsStatus,
  Installation,
  ModerationPolicy,
  PageResponse,
  PolicyAction,
  Runner,
  RunnerPool,
  User,
  WebhookDelivery,
  WorkflowJob,
} from "./types"

let csrfToken = ""

export function setCsrfToken(token: string) {
  csrfToken = token
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

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method || "GET").toUpperCase()
  const headers = new Headers(init.headers)
  if (!headers.has("Accept")) {
    headers.set("Accept", "application/json")
  }
  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json")
  }
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
  me: () => request<{ user: User; csrf_token: string }>("/api/v1/auth/me"),

  overview: () => request<DashboardOverview>("/api/v1/dashboard/overview"),

  listPools: (limit = 50, offset = 0) =>
    request<PageResponse<RunnerPool>>(`/api/v1/pools?limit=${limit}&offset=${offset}`),
  createPool: (pool: Partial<RunnerPool>) =>
    request<RunnerPool>("/api/v1/pools", { method: "POST", body: JSON.stringify(pool) }),
  updatePool: (id: string, pool: Partial<RunnerPool>) =>
    request<RunnerPool>(`/api/v1/pools/${id}`, { method: "PUT", body: JSON.stringify(pool) }),
  deletePool: (id: string) =>
    request<{ ok: boolean }>(`/api/v1/pools/${id}`, { method: "DELETE" }),

  listRunners: (params: { status?: string; search?: string; limit?: number } = {}) => {
    const qs = new URLSearchParams()
    if (params.status) qs.set("status", params.status)
    if (params.search) qs.set("search", params.search)
    qs.set("limit", String(params.limit ?? 50))
    return request<PageResponse<Runner>>(`/api/v1/runners?${qs.toString()}`)
  },
  drainRunner: (id: string) =>
    request<Runner>(`/api/v1/runners/${id}/drain`, { method: "POST" }),
  terminateRunner: (id: string) =>
    request<{ ok: boolean }>(`/api/v1/runners/${id}/terminate`, { method: "POST" }),

  listJobs: (params: { status?: string; search?: string; limit?: number } = {}) => {
    const qs = new URLSearchParams()
    if (params.status) qs.set("status", params.status)
    if (params.search) qs.set("search", params.search)
    qs.set("limit", String(params.limit ?? 50))
    return request<PageResponse<WorkflowJob>>(`/api/v1/jobs?${qs.toString()}`)
  },
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

  listPolicies: (limit = 50) =>
    request<PageResponse<ModerationPolicy>>(`/api/v1/policies?limit=${limit}`),
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

  listInstallations: () => request<PageResponse<Installation>>("/api/v1/installations?limit=50"),
  syncInstallations: () =>
    request<{ synced: number }>("/api/v1/installations/sync", { method: "POST" }),
  listDeliveries: () =>
    request<PageResponse<WebhookDelivery>>("/api/v1/webhooks/deliveries?limit=50"),
  listAuditLogs: () => request<PageResponse<AuditLog>>("/api/v1/audit-logs?limit=50"),
  settingsStatus: () => request<GitHubAppSettingsStatus>("/api/v1/settings/github-app"),
}
