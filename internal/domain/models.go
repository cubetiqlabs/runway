package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type UserRole string

const (
	RoleAdmin     UserRole = "admin"
	RoleModerator UserRole = "moderator"
	RoleViewer    UserRole = "viewer"
)

type User struct {
	ID           uuid.UUID `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	PasswordHash string    `json:"-"`
	Role         UserRole  `json:"role"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Session struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	TokenHash string    `json:"-"`
	CSRFToken string    `json:"csrf_token"`
	IPAddress string    `json:"ip_address"`
	UserAgent string    `json:"user_agent"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type APIToken struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	TokenHash  string     `json:"-"`
	Scopes     []string   `json:"scopes"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type Installation struct {
	ID                     int64           `json:"id"`
	AccountID              int64           `json:"account_id"`
	AccountLogin           string          `json:"account_login"`
	AccountType            string          `json:"account_type"`
	TargetType             string          `json:"target_type"`
	RepositoriesSelection  string          `json:"repositories_selection"`
	Permissions            json.RawMessage `json:"permissions"`
	SuspendedAt            *time.Time      `json:"suspended_at,omitempty"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

type RunnerPool struct {
	ID             uuid.UUID `json:"id"`
	InstallationID *int64    `json:"installation_id,omitempty"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	Description    string    `json:"description"`
	Labels         []string  `json:"labels"`
	OS             string    `json:"os"`
	Arch           string    `json:"arch"`
	VCPU           int       `json:"vcpu"`
	MemoryMB       int       `json:"memory_mb"`
	DiskGB         int       `json:"disk_gb"`
	MinIdle        int       `json:"min_idle"`
	MaxConcurrency int       `json:"max_concurrency"`
	IdleTimeoutSec int       `json:"idle_timeout_sec"`
	RunnerGroupID  int64     `json:"runner_group_id"`
	Provider       string    `json:"provider"`
	ContainerImage string    `json:"container_image"`
	BuildProfile   string    `json:"build_profile"`
	CacheEnabled   bool      `json:"cache_enabled"`
	CacheScope     string    `json:"cache_scope"`
	CacheMaxGB     int       `json:"cache_max_gb"`
	SandboxMode    string    `json:"sandbox_mode"`
	Enabled        bool      `json:"enabled"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type RunnerStatus string

const (
	RunnerProvisioning RunnerStatus = "provisioning"
	RunnerOnline       RunnerStatus = "online"
	RunnerBusy         RunnerStatus = "busy"
	RunnerDraining     RunnerStatus = "draining"
	RunnerOffline      RunnerStatus = "offline"
	RunnerTerminated   RunnerStatus = "terminated"
)

type Runner struct {
	ID              uuid.UUID       `json:"id"`
	PoolID          *uuid.UUID      `json:"pool_id,omitempty"`
	PoolName        string          `json:"pool_name,omitempty"`
	InstallationID  *int64          `json:"installation_id,omitempty"`
	GitHubRunnerID  *int64          `json:"github_runner_id,omitempty"`
	Name            string          `json:"name"`
	Provider        string          `json:"provider"`
	ProviderRef     string          `json:"provider_ref"`
	Labels          []string        `json:"labels"`
	Status          RunnerStatus    `json:"status"`
	Ephemeral       bool            `json:"ephemeral"`
	CurrentJobID    *int64          `json:"current_job_id,omitempty"`
	HostInfo        json.RawMessage `json:"host_info"`
	LastHeartbeatAt *time.Time      `json:"last_heartbeat_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type PolicyAction string

const (
	ActionAllow           PolicyAction = "allow"
	ActionDeny            PolicyAction = "deny"
	ActionRequireApproval PolicyAction = "require_approval"
)

type ModerationPolicy struct {
	ID                   uuid.UUID    `json:"id"`
	InstallationID       *int64       `json:"installation_id,omitempty"`
	Name                 string       `json:"name"`
	Description          string       `json:"description"`
	Priority             int          `json:"priority"`
	Enabled              bool         `json:"enabled"`
	Action               PolicyAction `json:"action"`
	RepoPatterns         []string     `json:"repo_patterns"`
	BranchPatterns       []string     `json:"branch_patterns"`
	ActorPatterns        []string     `json:"actor_patterns"`
	LabelPatterns        []string     `json:"label_patterns"`
	BlockForkPRs         bool         `json:"block_fork_prs"`
	MaxConcurrentPerRepo *int         `json:"max_concurrent_per_repo,omitempty"`
	MaxJobDurationMin    *int         `json:"max_job_duration_min,omitempty"`
	CreatedAt            time.Time    `json:"created_at"`
	UpdatedAt            time.Time    `json:"updated_at"`
}

type JobStatus string

const (
	JobQueued           JobStatus = "queued"
	JobAwaitingApproval JobStatus = "awaiting_approval"
	JobProvisioning     JobStatus = "provisioning"
	JobInProgress       JobStatus = "in_progress"
	JobCompleted        JobStatus = "completed"
	JobRejected         JobStatus = "rejected"
	JobCancelled        JobStatus = "cancelled"
)

type WorkflowJob struct {
	ID                 uuid.UUID  `json:"id"`
	GitHubJobID        int64      `json:"github_job_id"`
	GitHubRunID        int64      `json:"github_run_id"`
	InstallationID     *int64     `json:"installation_id,omitempty"`
	RepoFullName       string     `json:"repo_full_name"`
	WorkflowName       string     `json:"workflow_name"`
	JobName            string     `json:"job_name"`
	HeadBranch         string     `json:"head_branch"`
	HeadSHA            string     `json:"head_sha"`
	Actor              string     `json:"actor"`
	IsForkPR           bool       `json:"is_fork_pr"`
	Labels             []string   `json:"labels"`
	Status             JobStatus  `json:"status"`
	Conclusion         string     `json:"conclusion"`
	MatchedPoolID      *uuid.UUID `json:"matched_pool_id,omitempty"`
	MatchedPoolName    string     `json:"matched_pool_name,omitempty"`
	MatchedPolicyID    *uuid.UUID `json:"matched_policy_id,omitempty"`
	MatchedPolicyName  string     `json:"matched_policy_name,omitempty"`
	ModerationDecision string     `json:"moderation_decision"`
	ModerationReason   string     `json:"moderation_reason"`
	ModeratedBy        *uuid.UUID `json:"moderated_by,omitempty"`
	AssignedRunnerID   *uuid.UUID `json:"assigned_runner_id,omitempty"`
	AssignedRunnerName string     `json:"assigned_runner_name,omitempty"`
	QueuedAt           time.Time  `json:"queued_at"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type WebhookDelivery struct {
	DeliveryID     string    `json:"delivery_id"`
	EventName      string    `json:"event_name"`
	Action         string    `json:"action"`
	InstallationID *int64    `json:"installation_id,omitempty"`
	RepoFullName   string    `json:"repo_full_name"`
	SignatureValid bool      `json:"signature_valid"`
	Status         string    `json:"status"`
	ErrorMessage   string    `json:"error_message"`
	PayloadHash    string    `json:"payload_hash"`
	ProcessingMS   int       `json:"processing_ms"`
	ReceivedAt     time.Time `json:"received_at"`
}

type AuditLog struct {
	ID           int64           `json:"id"`
	ActorID      *uuid.UUID      `json:"actor_id,omitempty"`
	ActorLabel   string          `json:"actor_label"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	Metadata     json.RawMessage `json:"metadata"`
	IPAddress    string          `json:"ip_address"`
	CreatedAt    time.Time       `json:"created_at"`
}

type PoolUtilization struct {
	PoolID         uuid.UUID `json:"pool_id"`
	Name           string    `json:"name"`
	OS             string    `json:"os"`
	Arch           string    `json:"arch"`
	VCPU           int       `json:"vcpu"`
	MemoryMB       int       `json:"memory_mb"`
	OnlineRunners  int       `json:"online_runners"`
	BusyRunners    int       `json:"busy_runners"`
	MaxConcurrency int       `json:"max_concurrency"`
	Enabled        bool      `json:"enabled"`
}

type DashboardOverview struct {
	TotalRunners      int               `json:"total_runners"`
	OnlineRunners     int               `json:"online_runners"`
	BusyRunners       int               `json:"busy_runners"`
	ProvisioningCount int               `json:"provisioning_runners"`
	ActiveJobs        int               `json:"active_jobs"`
	AwaitingApproval  int               `json:"awaiting_approval"`
	DeniedJobs24h     int               `json:"denied_jobs_24h"`
	CompletedJobs24h  int               `json:"completed_jobs_24h"`
	AvgQueueLatencyMS int               `json:"avg_queue_latency_ms"`
	Installations     int               `json:"installations"`
	PoolUtilization   []PoolUtilization `json:"pool_utilization"`
	RecentJobs        []WorkflowJob     `json:"recent_jobs"`
	RecentDeliveries  []WebhookDelivery `json:"recent_deliveries"`
}

type AgentStatus string

const (
	AgentOnline   AgentStatus = "online"
	AgentDraining AgentStatus = "draining"
	AgentDrained  AgentStatus = "drained"
	AgentOffline  AgentStatus = "offline"
)

type AgentMetricsSnapshot struct {
	CPUUsagePercent    float64   `json:"cpu_usage_percent"`
	MemoryTotalMB      int       `json:"memory_total_mb"`
	MemoryUsedMB       int       `json:"memory_used_mb"`
	MemoryUsagePercent float64   `json:"memory_usage_percent"`
	DiskTotalGB        float64   `json:"disk_total_gb"`
	DiskUsedGB         float64   `json:"disk_used_gb"`
	DiskFreeGB         float64   `json:"disk_free_gb"`
	DiskUsagePercent   float64   `json:"disk_usage_percent"`
	LoadAvg1           float64   `json:"load_avg_1"`
	LoadAvg5           float64   `json:"load_avg_5"`
	LoadAvg15          float64   `json:"load_avg_15"`
	UptimeSec          int64     `json:"uptime_sec"`
	HostUptimeSec      int64     `json:"host_uptime_sec"`
	SandboxRuntime     string    `json:"sandbox_runtime,omitempty"`
	IsolatedDinD       bool      `json:"isolated_dind"`
	CacheHitCount      int64     `json:"cache_hit_count"`
	CacheMissCount     int64     `json:"cache_miss_count"`
	CacheSizeMB        int64     `json:"cache_size_mb"`
	TasksCompleted     int64     `json:"tasks_completed"`
	TasksFailed        int64     `json:"tasks_failed"`
	TasksCancelled     int64     `json:"tasks_cancelled"`
	AvgTaskDurationMS  int64     `json:"avg_task_duration_ms"`
	CollectedAt        time.Time `json:"collected_at"`
}

type AgentTaskStats struct {
	TotalTasks     int64   `json:"total_tasks"`
	CompletedTasks int64   `json:"completed_tasks"`
	FailedTasks    int64   `json:"failed_tasks"`
	CancelledTasks int64   `json:"cancelled_tasks"`
	ActiveTasks    int64   `json:"active_tasks"`
	Tasks24h       int64   `json:"tasks_24h"`
	SuccessRate    float64 `json:"success_rate"`
	AvgDurationMS  int64   `json:"avg_duration_ms"`
	P95DurationMS  int64   `json:"p95_duration_ms"`
}

type AgentMetricPoint struct {
	Timestamp          time.Time `json:"timestamp"`
	CPUUsagePercent    float64   `json:"cpu_usage_percent"`
	MemoryUsagePercent float64   `json:"memory_usage_percent"`
	DiskUsagePercent   float64   `json:"disk_usage_percent"`
	MemoryUsedMB       int       `json:"memory_used_mb"`
	ActiveJobs         int       `json:"active_jobs"`
	LoadAvg1           float64   `json:"load_avg_1"`
	CacheHitCount      int64     `json:"cache_hit_count"`
	CacheMissCount     int64     `json:"cache_miss_count"`
}

type Agent struct {
	ID              uuid.UUID       `json:"id"`
	Name            string          `json:"name"`
	TokenPrefix     string          `json:"token_prefix"`
	TokenHash       string          `json:"-"`
	OS              string          `json:"os"`
	Arch            string          `json:"arch"`
	Runtime         string          `json:"runtime"`
	SandboxRuntime  string          `json:"sandbox_runtime,omitempty"`
	Labels          []string        `json:"labels"`
	PoolID          *uuid.UUID      `json:"pool_id,omitempty"`
	PoolName        string          `json:"pool_name,omitempty"`
	Version         string          `json:"version"`
	Hostname        string          `json:"hostname"`
	CPUCount        int             `json:"cpu_count"`
	MemoryMB        int             `json:"memory_mb"`
	MaxConcurrency  int             `json:"max_concurrency"`
	ActiveJobs      int             `json:"active_jobs"`
	Status          AgentStatus     `json:"status"`
	Metrics         json.RawMessage `json:"metrics"`
	LastHeartbeatAt *time.Time      `json:"last_heartbeat_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type AgentDetail struct {
	Agent         Agent              `json:"agent"`
	Stats         AgentTaskStats     `json:"stats"`
	MetricHistory []AgentMetricPoint `json:"metric_history"`
	RecentTasks   []AgentTask        `json:"recent_tasks"`
}

type AgentTaskStatus string

const (
	AgentTaskPending   AgentTaskStatus = "pending"
	AgentTaskClaimed   AgentTaskStatus = "claimed"
	AgentTaskRunning   AgentTaskStatus = "running"
	AgentTaskCompleted AgentTaskStatus = "completed"
	AgentTaskFailed    AgentTaskStatus = "failed"
	AgentTaskCancelled AgentTaskStatus = "cancelled"
)

type AgentTask struct {
	ID                        uuid.UUID       `json:"id"`
	AgentID                   *uuid.UUID      `json:"agent_id,omitempty"`
	RunnerID                  uuid.UUID       `json:"runner_id"`
	PoolID                    *uuid.UUID      `json:"pool_id,omitempty"`
	JobID                     uuid.UUID       `json:"job_id"`
	GitHubJobID               int64           `json:"github_job_id"`
	RepoFullName              string          `json:"repo_full_name"`
	RunnerName                string          `json:"runner_name"`
	PoolSlug                  string          `json:"pool_slug"`
	OS                        string          `json:"os"`
	Arch                      string          `json:"arch"`
	Labels                    []string        `json:"labels"`
	ContainerImage            string          `json:"container_image"`
	VCPU                      int             `json:"vcpu"`
	MemoryMB                  int             `json:"memory_mb"`
	IsForkPR                  bool            `json:"is_fork_pr"`
	BuildProfile              string          `json:"build_profile"`
	CacheEnabled              bool            `json:"cache_enabled"`
	CacheScope                string          `json:"cache_scope"`
	SandboxMode               string          `json:"sandbox_mode"`
	EncodedJITConfigEncrypted []byte          `json:"-"`
	Status                    AgentTaskStatus `json:"status"`
	ErrorMessage              string          `json:"error_message"`
	ClaimedAt                 *time.Time      `json:"claimed_at,omitempty"`
	CompletedAt               *time.Time      `json:"completed_at,omitempty"`
	CreatedAt                 time.Time       `json:"created_at"`
	UpdatedAt                 time.Time       `json:"updated_at"`
}

const (
	EventCategoryWebhook         = "webhook"
	EventCategoryAudit           = "audit"
	EventCategoryRunnerLog       = "runner_log"
	EventCategoryRunnerLifecycle = "runner_lifecycle"
	EventCategoryJobLifecycle    = "job_lifecycle"
	EventCategoryAgentLifecycle  = "agent_lifecycle"
	EventCategoryAgentMetric     = "agent_metric"
	EventCategorySystem          = "system"
)

type Event struct {
	ID            uuid.UUID       `json:"id"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Category      string          `json:"category"`
	EventType     string          `json:"event_type"`
	Severity      string          `json:"severity"`
	Source        string          `json:"source"`
	ActorType     string          `json:"actor_type"`
	ActorID       string          `json:"actor_id"`
	ActorLabel    string          `json:"actor_label"`
	ResourceType  string          `json:"resource_type"`
	ResourceID    string          `json:"resource_id"`
	CorrelationID string          `json:"correlation_id"`
	RepoFullName  string          `json:"repo_full_name"`
	DurationMS    int64           `json:"duration_ms"`
	Status        string          `json:"status"`
	Message       string          `json:"message"`
	Payload       json.RawMessage `json:"payload"`
	IPAddress     string          `json:"ip_address"`
	SchemaVersion int             `json:"schema_version"`
}

type EventIngestionStats struct {
	Published      int64 `json:"published"`
	Flushed        int64 `json:"flushed"`
	Dropped        int64 `json:"dropped"`
	BatchErrors    int64 `json:"batch_errors"`
	QueueDepth     int   `json:"queue_depth"`
	BufferCapacity int   `json:"buffer_capacity"`
}

type EventSummary struct {
	Driver      string              `json:"driver"`
	TotalEvents int64               `json:"total_events"`
	ByCategory  map[string]int64    `json:"by_category"`
	BySeverity  map[string]int64    `json:"by_severity"`
	Ingestion   EventIngestionStats `json:"ingestion"`
}

type RunnerLogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Stream    string    `json:"stream"`
	Level     string    `json:"level"`
	Line      string    `json:"line"`
}

type CacheStats struct {
	Driver         string  `json:"driver"`
	ClusterEnabled bool    `json:"cluster_enabled"`
	Hits           uint64  `json:"hits"`
	Misses         uint64  `json:"misses"`
	Sets           uint64  `json:"sets"`
	Deletes        uint64  `json:"deletes"`
	Errors         uint64  `json:"errors"`
	HitRate        float64 `json:"hit_rate"`
}

type ClusterNode struct {
	InstanceID string    `json:"instance_id"`
	Hostname   string    `json:"hostname"`
	Version    string    `json:"version"`
	Address    string    `json:"address"`
	StartedAt  time.Time `json:"started_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}
