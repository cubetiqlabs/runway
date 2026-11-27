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
  provider: "docker" | "simulated"
  container_image: string
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

export interface GitHubAppSettingsStatus {
  app_id: number
  app_slug: string
  api_base_url: string
  webhook_secret_set: boolean
  private_key_configured: boolean
  orchestrator_provider: string
  moderation_default_action: string
}

export interface PageResponse<T> {
  items: T[]
  total: number
}
