import { QueryClient, queryOptions } from "@tanstack/react-query"
import { ApiError, api, type ListQueryParams } from "./api"

export function createQueryClient(): QueryClient {
  const isServer = typeof window === "undefined"
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 10_000,
        gcTime: isServer ? Infinity : 5 * 60_000,
        refetchOnWindowFocus: !isServer,
        retry: (failureCount, error) => {
          if (error instanceof ApiError && error.status >= 400 && error.status < 500) {
            return false
          }
          return failureCount < 1
        },
      },
      mutations: {
        retry: 0,
      },
    },
  })
}

export const queryKeys = {
  authMe: ["auth", "me"] as const,
  overview: ["dashboard", "overview"] as const,
  pools: (params: ListQueryParams = {}) => ["pools", params] as const,
  runners: (params: ListQueryParams = {}) => ["runners", params] as const,
  runnerLogs: (id: string, params: ListQueryParams = {}) => ["runners", id, "logs", params] as const,
  jobs: (params: ListQueryParams = {}) => ["jobs", params] as const,
  policies: (params: ListQueryParams = {}) => ["policies", params] as const,
  installations: (params: ListQueryParams = {}) => ["installations", params] as const,
  deliveries: (params: ListQueryParams = {}) => ["deliveries", params] as const,
  auditLogs: (params: ListQueryParams = {}) => ["audit-logs", params] as const,
  events: (params: ListQueryParams = {}) => ["events", params] as const,
  eventSummary: (window: string) => ["events", "summary", window] as const,
  settingsStatus: ["settings", "status"] as const,
  agents: (params: ListQueryParams = {}) => ["agents", params] as const,
  agentDetail: (id: string, window: string) => ["agents", id, "detail", window] as const,
  agentTasks: (id: string, params: ListQueryParams = {}) => ["agents", id, "tasks", params] as const,
}

export const authMeQueryOptions = () =>
  queryOptions({
    queryKey: queryKeys.authMe,
    queryFn: ({ signal }) => api.me(signal),
    staleTime: 60_000,
    retry: false,
  })

export const overviewQueryOptions = () =>
  queryOptions({
    queryKey: queryKeys.overview,
    queryFn: ({ signal }) => api.overview(signal),
  })

export const poolsQueryOptions = (params: ListQueryParams = {}) =>
  queryOptions({
    queryKey: queryKeys.pools(params),
    queryFn: ({ signal }) => api.listPools(params, signal),
    staleTime: 15_000,
  })

export const runnersQueryOptions = (params: ListQueryParams = {}) =>
  queryOptions({
    queryKey: queryKeys.runners(params),
    queryFn: ({ signal }) => api.listRunners(params, signal),
  })

export const runnerLogsQueryOptions = (id: string, params: ListQueryParams = {}) =>
  queryOptions({
    queryKey: queryKeys.runnerLogs(id, params),
    queryFn: ({ signal }) => api.getRunnerLogs(id, params, signal),
    staleTime: 3_000,
  })

export const jobsQueryOptions = (params: ListQueryParams = {}) =>
  queryOptions({
    queryKey: queryKeys.jobs(params),
    queryFn: ({ signal }) => api.listJobs(params, signal),
  })

export const policiesQueryOptions = (params: ListQueryParams = {}) =>
  queryOptions({
    queryKey: queryKeys.policies(params),
    queryFn: ({ signal }) => api.listPolicies(params, signal),
    staleTime: 15_000,
  })

export const installationsQueryOptions = (params: ListQueryParams = {}) =>
  queryOptions({
    queryKey: queryKeys.installations(params),
    queryFn: ({ signal }) => api.listInstallations(params, signal),
    staleTime: 30_000,
  })

export const deliveriesQueryOptions = (params: ListQueryParams = {}) =>
  queryOptions({
    queryKey: queryKeys.deliveries(params),
    queryFn: ({ signal }) => api.listDeliveries(params, signal),
  })

export const auditLogsQueryOptions = (params: ListQueryParams = {}) =>
  queryOptions({
    queryKey: queryKeys.auditLogs(params),
    queryFn: ({ signal }) => api.listAuditLogs(params, signal),
  })

export const eventsQueryOptions = (params: ListQueryParams = {}) =>
  queryOptions({
    queryKey: queryKeys.events(params),
    queryFn: ({ signal }) => api.listEvents(params, signal),
  })

export const eventSummaryQueryOptions = (window = "24h") =>
  queryOptions({
    queryKey: queryKeys.eventSummary(window),
    queryFn: ({ signal }) => api.eventSummary(window, signal),
  })

export const settingsStatusQueryOptions = () =>
  queryOptions({
    queryKey: queryKeys.settingsStatus,
    queryFn: ({ signal }) => api.settingsStatus(signal),
    staleTime: 10_000,
  })

export const agentsQueryOptions = (params: ListQueryParams = {}) =>
  queryOptions({
    queryKey: queryKeys.agents(params),
    queryFn: ({ signal }) => api.listAgents(params, signal),
  })

export const agentDetailQueryOptions = (id: string, window = "1h") =>
  queryOptions({
    queryKey: queryKeys.agentDetail(id, window),
    queryFn: ({ signal }) => api.getAgent(id, window, signal),
    staleTime: 3_000,
  })

export const agentTasksQueryOptions = (id: string, params: ListQueryParams = {}) =>
  queryOptions({
    queryKey: queryKeys.agentTasks(id, params),
    queryFn: ({ signal }) => api.listAgentTasks(id, params, signal),
  })

export async function prefetchRouteData(queryClient: QueryClient, pathname: string): Promise<void> {
  switch (pathname) {
    case "/":
      await queryClient.prefetchQuery(overviewQueryOptions())
      break
    case "/runners":
      await queryClient.prefetchQuery(
        runnersQueryOptions({ limit: 25, page: 1, sort_by: "created_at", sort_dir: "desc" })
      )
      break
    case "/agents":
      await Promise.all([
        queryClient.prefetchQuery(
          agentsQueryOptions({ limit: 25, page: 1, sort_by: "created_at", sort_dir: "desc" })
        ),
        queryClient.prefetchQuery(poolsQueryOptions({ limit: 100 })),
      ])
      break
    case "/pools":
      await queryClient.prefetchQuery(poolsQueryOptions({ limit: 20, page: 1 }))
      break
    case "/jobs":
      await queryClient.prefetchQuery(
        jobsQueryOptions({ limit: 25, page: 1, sort_by: "queued_at", sort_dir: "desc" })
      )
      break
    case "/policies":
      await queryClient.prefetchQuery(
        policiesQueryOptions({ limit: 25, page: 1, sort_by: "priority", sort_dir: "asc" })
      )
      break
    case "/events":
      await Promise.all([
        queryClient.prefetchQuery(
          eventsQueryOptions({
            limit: 25,
            page: 1,
            window: "24h",
            sort_by: "occurred_at",
            sort_dir: "desc",
          })
        ),
        queryClient.prefetchQuery(eventSummaryQueryOptions("24h")),
      ])
      break
    case "/installations":
      await Promise.all([
        queryClient.prefetchQuery(settingsStatusQueryOptions()),
        queryClient.prefetchQuery(
          installationsQueryOptions({ limit: 25, page: 1, sort_by: "updated_at", sort_dir: "desc" })
        ),
      ])
      break
  }
}
