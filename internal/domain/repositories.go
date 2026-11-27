package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type PageParams struct {
	Limit   int
	Offset  int
	SortBy  string
	SortDir string
}

type RunnerFilter struct {
	Status   string
	Provider string
	PoolID   *uuid.UUID
	Search   string
	PageParams
}

type JobFilter struct {
	Status string
	Repo   string
	Search string
	PageParams
}

type AgentFilter struct {
	Status  string
	OS      string
	Runtime string
	PoolID  *uuid.UUID
	Search  string
	PageParams
}

type AgentTaskFilter struct {
	Status string
	Search string
	PageParams
}

type PoolFilter struct {
	Provider string
	OS       string
	Enabled  *bool
	Search   string
	PageParams
}

type PolicyFilter struct {
	Action  string
	Enabled *bool
	Search  string
	PageParams
}

type InstallationFilter struct {
	Search string
	PageParams
}

type DeliveryFilter struct {
	Status    string
	EventName string
	Search    string
	PageParams
}

type AuditFilter struct {
	Action       string
	ResourceType string
	Search       string
	PageParams
}

type EventFilter struct {
	Category      string
	EventType     string
	Severity      string
	ResourceType  string
	ResourceID    string
	CorrelationID string
	Repo          string
	Search        string
	Since         *time.Time
	Until         *time.Time
	PageParams
}

type AuthRepository interface {
	CountUsers(ctx context.Context) (int, error)
	CreateUser(ctx context.Context, user *User) error
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (*User, error)
	CreateSession(ctx context.Context, session *Session) error
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, *User, error)
	DeleteSessionByTokenHash(ctx context.Context, tokenHash string) error
	DeleteExpiredSessions(ctx context.Context) error
	CreateAPIToken(ctx context.Context, token *APIToken) error
	ListAPITokensByUser(ctx context.Context, userID uuid.UUID) ([]APIToken, error)
	GetAPITokenByHash(ctx context.Context, tokenHash string) (*APIToken, *User, error)
	TouchAPIToken(ctx context.Context, id uuid.UUID) error
	DeleteAPIToken(ctx context.Context, id, userID uuid.UUID) error
}

type InstallationRepository interface {
	Upsert(ctx context.Context, inst *Installation) error
	List(ctx context.Context, filter InstallationFilter) ([]Installation, int, error)
	GetByID(ctx context.Context, id int64) (*Installation, error)
	DeleteByID(ctx context.Context, id int64) error
}

type PoolRepository interface {
	Count(ctx context.Context) (int, error)
	Create(ctx context.Context, pool *RunnerPool) error
	Update(ctx context.Context, pool *RunnerPool) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*RunnerPool, error)
	List(ctx context.Context, filter PoolFilter) ([]RunnerPool, int, error)
	ListEnabled(ctx context.Context) ([]RunnerPool, error)
}

type RunnerRepository interface {
	Create(ctx context.Context, runner *Runner) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status RunnerStatus, githubRunnerID *int64, providerRef string, currentJobID *int64) error
	SetProvisioned(ctx context.Context, id uuid.UUID, status RunnerStatus, githubRunnerID *int64, providerRef string, currentJobID *int64) error
	Heartbeat(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*Runner, error)
	GetByName(ctx context.Context, name string) (*Runner, error)
	List(ctx context.Context, filter RunnerFilter) ([]Runner, int, error)
	ListByStatus(ctx context.Context, status RunnerStatus) ([]Runner, error)
	ListActiveByProvider(ctx context.Context, provider string) ([]Runner, error)
	CountActiveInPool(ctx context.Context, poolID uuid.UUID) (int, error)
	FindStaleRunners(ctx context.Context, provisioningBefore, heartbeatBefore time.Time) ([]Runner, error)
}

type JobRepository interface {
	UpsertFromWebhook(ctx context.Context, job *WorkflowJob) error
	GetByID(ctx context.Context, id uuid.UUID) (*WorkflowJob, error)
	GetByGitHubJobID(ctx context.Context, githubJobID int64) (*WorkflowJob, error)
	List(ctx context.Context, filter JobFilter) ([]WorkflowJob, int, error)
	ListDispatchableQueued(ctx context.Context, limit int) ([]WorkflowJob, error)
	CountActiveForRepo(ctx context.Context, repoFullName string) (int, error)
	ClaimForProvisioning(ctx context.Context, id uuid.UUID, poolID *uuid.UUID, runnerID *uuid.UUID, reason string) (bool, error)
	RequeueIfProvisioning(ctx context.Context, id uuid.UUID, reason string) (bool, error)
	FindOrphanedProvisioningJobs(ctx context.Context, olderThan time.Time, limit int) ([]WorkflowJob, error)
	UpdateModerationAndRunner(ctx context.Context, id uuid.UUID, status JobStatus, decision, reason string, moderatedBy, poolID, runnerID *uuid.UUID) error
	UpdateLifecycle(ctx context.Context, githubJobID int64, status JobStatus, conclusion string, startedAt, completedAt *time.Time) error
}

type PolicyRepository interface {
	Count(ctx context.Context) (int, error)
	Create(ctx context.Context, policy *ModerationPolicy) error
	Update(ctx context.Context, policy *ModerationPolicy) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*ModerationPolicy, error)
	List(ctx context.Context, filter PolicyFilter) ([]ModerationPolicy, int, error)
	ListEnabledOrdered(ctx context.Context) ([]ModerationPolicy, error)
}

type AuditRepository interface {
	InsertDeliveryIfAbsent(ctx context.Context, d *WebhookDelivery) (bool, error)
	UpdateDeliveryStatus(ctx context.Context, deliveryID, status, errMsg string, processingMS int) error
	ListDeliveries(ctx context.Context, filter DeliveryFilter) ([]WebhookDelivery, int, error)
	AppendAuditLog(ctx context.Context, log *AuditLog) error
	ListAuditLogs(ctx context.Context, filter AuditFilter) ([]AuditLog, int, error)
	GetDashboardOverview(ctx context.Context) (*DashboardOverview, error)
}

type AgentRepository interface {
	Create(ctx context.Context, agent *Agent) error
	GetByID(ctx context.Context, id uuid.UUID) (*Agent, error)
	GetByTokenHash(ctx context.Context, tokenHash string) (*Agent, error)
	List(ctx context.Context, filter AgentFilter) ([]Agent, int, error)
	ListByStatus(ctx context.Context, status AgentStatus) ([]Agent, error)
	CountOnline(ctx context.Context) (int, error)
	UpdateHeartbeat(ctx context.Context, agent *Agent) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status AgentStatus) error
	UpdateToken(ctx context.Context, id uuid.UUID, prefix, tokenHash string) error
	Delete(ctx context.Context, id uuid.UUID) error
	MarkStaleOffline(ctx context.Context, before time.Time) (int64, error)
	MarkDrainingCompleteIfIdle(ctx context.Context, agentID uuid.UUID) (bool, error)
	CountActiveTasksByAgent(ctx context.Context, agentID uuid.UUID) (int, error)
	ListTasksByAgent(ctx context.Context, agentID uuid.UUID, filter AgentTaskFilter) ([]AgentTask, int, error)
	GetAgentTaskStats(ctx context.Context, agentID uuid.UUID) (*AgentTaskStats, error)
	CreateTask(ctx context.Context, task *AgentTask) error
	GetTaskByID(ctx context.Context, id uuid.UUID) (*AgentTask, error)
	ClaimNextTask(ctx context.Context, agent *Agent) (*AgentTask, error)
	CompleteTask(ctx context.Context, taskID, agentID uuid.UUID, status AgentTaskStatus, errMsg string) (*AgentTask, error)
	CancelTasksByRunner(ctx context.Context, runnerID uuid.UUID) error
	CancelTaskByID(ctx context.Context, taskID uuid.UUID) error
	ListCancelledTaskIDsForAgent(ctx context.Context, agentID uuid.UUID) ([]uuid.UUID, error)
	ExpirePendingTasks(ctx context.Context, before time.Time) ([]AgentTask, error)
	CountAvailableSlotsForPool(ctx context.Context, pool RunnerPool) (freeSlots int, onlineAgents int, err error)
	FailTasksForOfflineAgents(ctx context.Context) ([]AgentTask, error)
}
