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
