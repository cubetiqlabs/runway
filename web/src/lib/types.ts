export type UserRole = "admin" | "moderator" | "viewer"

export interface User {
  id: string
  email: string
  name: string
  role: UserRole
  is_active: boolean
  created_at: string
  updated_at: string
}

export interface Installation {
  id: number
  account_id: number
  account_login: string
  account_type: string
  target_type: string
  repositories_selection: string
  permissions: Record<string, string>
  suspended_at?: string | null
  created_at: string
  updated_at: string
}

export type BuildProfile = "standard" | "fast" | "ultra"
export type CacheScope = "repo" | "pool" | "none"

export interface RunnerPool {
  id: string
  installation_id?: number | null
  name: string
  slug: string
  description: string
  labels: string[]
  os: string
  arch: string
  vcpu: number
  memory_mb: number
  disk_gb: number
  min_idle: number
  max_concurrency: number
  idle_timeout_sec: number
  runner_group_id: number
  provider: "docker" | "simulated" | "agent"
  container_image: string
  build_profile: BuildProfile
  cache_enabled: boolean
  cache_scope: CacheScope
  cache_max_gb: number
  sandbox_mode: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export type RunnerStatus =
  | "provisioning"
  | "online"
  | "busy"
  | "draining"
  | "offline"
  | "terminated"

export interface Runner {
  id: string
  pool_id?: string | null
  pool_name?: string
  installation_id?: number | null
  github_runner_id?: number | null
  name: string
  provider: string
  provider_ref: string
  labels: string[]
  status: RunnerStatus
  ephemeral: boolean
  current_job_id?: number | null
  last_heartbeat_at?: string | null
  created_at: string
  updated_at: string
}

export type PolicyAction = "allow" | "deny" | "require_approval"

export interface ModerationPolicy {
  id: string
  installation_id?: number | null
  name: string
  description: string
  priority: number
  enabled: boolean
  action: PolicyAction
  repo_patterns: string[]
  branch_patterns: string[]
  actor_patterns: string[]
  label_patterns: string[]
  block_fork_prs: boolean
  max_concurrent_per_repo?: number | null
  max_job_duration_min?: number | null
  created_at: string
  updated_at: string
}

export type JobStatus =
  | "queued"
  | "awaiting_approval"
  | "provisioning"
  | "in_progress"
  | "completed"
  | "rejected"
  | "cancelled"

export interface WorkflowJob {
  id: string
  github_job_id: number
  github_run_id: number
  installation_id?: number | null
  repo_full_name: string
  workflow_name: string
  job_name: string
  head_branch: string
  head_sha: string
  actor: string
  is_fork_pr: boolean
  labels: string[]
  status: JobStatus
  conclusion: string
  matched_pool_id?: string | null
  matched_pool_name?: string
  matched_policy_id?: string | null
  matched_policy_name?: string
  moderation_decision: string
  moderation_reason: string
  assigned_runner_id?: string | null
  assigned_runner_name?: string
  queued_at: string
  started_at?: string | null
  completed_at?: string | null
}

export interface WebhookDelivery {
  delivery_id: string
  event_name: string
  action: string
  installation_id?: number | null
  repo_full_name: string
  signature_valid: boolean
  status: string
  error_message: string
  processing_ms: number
  received_at: string
}

export interface AuditLog {
  id: number
  actor_id?: string | null
  actor_label: string
  action: string
  resource_type: string
  resource_id: string
  metadata: Record<string, unknown>
  ip_address: string
  created_at: string
}

export interface PoolUtilization {
  pool_id: string
  name: string
  os: string
  arch: string
  vcpu: number
  memory_mb: number
  online_runners: number
  busy_runners: number
  max_concurrency: number
  enabled: boolean
}

export interface DashboardOverview {
  total_runners: number
  online_runners: number
  busy_runners: number
  provisioning_runners: number
  active_jobs: number
  awaiting_approval: number
  denied_jobs_24h: number
  completed_jobs_24h: number
  avg_queue_latency_ms: number
  installations: number
  pool_utilization: PoolUtilization[]
  recent_jobs: WorkflowJob[]
  recent_deliveries: WebhookDelivery[]
}

export interface EventIngestionStats {
  published: number
  flushed: number
  dropped: number
  batch_errors: number
  queue_depth: number
  buffer_capacity: number
}

export interface GitHubAppSettingsStatus {
  app_id: number
  app_slug: string
  api_base_url: string
  webhook_secret_set: boolean
  private_key_configured: boolean
  orchestrator_provider: string
  orchestrator_healthy: boolean
  orchestrator_error?: string
  moderation_default_action: string
  eventstore_driver: string
  eventstore_healthy: boolean
  eventstore_error?: string
  eventstore_stats?: EventIngestionStats
}

export interface PageResponse<T> {
  items: T[]
  total: number
  limit: number
  offset: number
  page: number
  total_pages: number
  has_more: boolean
}

export type AgentStatus = "online" | "draining" | "drained" | "offline"

export interface AgentMetricsSnapshot {
  cpu_usage_percent: number
  memory_total_mb: number
  memory_used_mb: number
  memory_usage_percent: number
  disk_total_gb: number
  disk_used_gb: number
  disk_free_gb: number
  disk_usage_percent: number
  load_avg_1: number
  load_avg_5: number
  load_avg_15: number
  uptime_sec: number
  host_uptime_sec: number
  sandbox_runtime?: string
  isolated_dind?: boolean
  cache_hit_count: number
  cache_miss_count: number
  cache_size_mb: number
  tasks_completed: number
  tasks_failed: number
  tasks_cancelled: number
  avg_task_duration_ms: number
  collected_at: string
}

export interface AgentTaskStats {
  total_tasks: number
  completed_tasks: number
  failed_tasks: number
  cancelled_tasks: number
  active_tasks: number
  tasks_24h: number
  success_rate: number
  avg_duration_ms: number
  p95_duration_ms: number
}

export interface AgentMetricPoint {
  timestamp: string
  cpu_usage_percent: number
  memory_usage_percent: number
  disk_usage_percent: number
  memory_used_mb: number
  active_jobs: number
  load_avg_1: number
  cache_hit_count: number
  cache_miss_count: number
}

export interface AgentTask {
  id: string
  agent_id?: string | null
  runner_id: string
  pool_id?: string | null
  job_id: string
  github_job_id: number
  repo_full_name: string
  runner_name: string
  pool_slug: string
  os: string
  arch: string
  labels: string[]
  container_image: string
  vcpu: number
  memory_mb: number
  is_fork_pr: boolean
  build_profile: string
  cache_enabled: boolean
  cache_scope: string
  sandbox_mode: string
  status: string
  error_message: string
  claimed_at?: string | null
  completed_at?: string | null
  created_at: string
  updated_at: string
}

export interface Agent {
  id: string
  name: string
  token_prefix: string
  os: string
  arch: string
  runtime: string
  sandbox_runtime?: string
  labels: string[]
  pool_id?: string | null
  pool_name?: string
  version: string
  hostname: string
  cpu_count: number
  memory_mb: number
  max_concurrency: number
  active_jobs: number
  status: AgentStatus
  metrics?: Partial<AgentMetricsSnapshot>
  last_heartbeat_at?: string | null
  created_at: string
  updated_at: string
}

export interface AgentDetail {
  agent: Agent
  stats: AgentTaskStats
  metric_history: AgentMetricPoint[]
  recent_tasks: AgentTask[]
}

export interface RegisterAgentResponse {
  agent: Agent
  token: string
}

export interface EventRecord {
  id: string
  occurred_at: string
  category: string
  event_type: string
  severity: string
  source: string
  actor_type: string
  actor_id: string
  actor_label: string
  resource_type: string
  resource_id: string
  correlation_id: string
  repo_full_name: string
  duration_ms: number
  status: string
  message: string
  payload: Record<string, unknown>
  ip_address: string
  schema_version: number
}

export interface EventSummary {
  driver: string
  total_events: number
  by_category: Record<string, number>
  by_severity: Record<string, number>
  ingestion: EventIngestionStats
}
