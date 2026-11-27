package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type PageParams struct {
	Limit  int
	Offset int
}

type RunnerFilter struct {
	Status string
	PoolID *uuid.UUID
	Search string
	PageParams
}

type JobFilter struct {
	Status string
	Repo   string
	Search string
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
	List(ctx context.Context, page PageParams) ([]Installation, int, error)
	GetByID(ctx context.Context, id int64) (*Installation, error)
	DeleteByID(ctx context.Context, id int64) error
}

type PoolRepository interface {
	Count(ctx context.Context) (int, error)
	Create(ctx context.Context, pool *RunnerPool) error
	Update(ctx context.Context, pool *RunnerPool) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*RunnerPool, error)
	List(ctx context.Context, page PageParams) ([]RunnerPool, int, error)
	ListEnabled(ctx context.Context) ([]RunnerPool, error)
}

type RunnerRepository interface {
	Create(ctx context.Context, runner *Runner) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status RunnerStatus, githubRunnerID *int64, providerRef string, currentJobID *int64) error
	Heartbeat(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*Runner, error)
	GetByName(ctx context.Context, name string) (*Runner, error)
	List(ctx context.Context, filter RunnerFilter) ([]Runner, int, error)
	CountActiveInPool(ctx context.Context, poolID uuid.UUID) (int, error)
	FindStaleRunners(ctx context.Context, provisioningBefore, heartbeatBefore time.Time) ([]Runner, error)
}

type JobRepository interface {
	UpsertFromWebhook(ctx context.Context, job *WorkflowJob) error
	GetByID(ctx context.Context, id uuid.UUID) (*WorkflowJob, error)
	GetByGitHubJobID(ctx context.Context, githubJobID int64) (*WorkflowJob, error)
	List(ctx context.Context, filter JobFilter) ([]WorkflowJob, int, error)
	CountActiveForRepo(ctx context.Context, repoFullName string) (int, error)
	UpdateModerationAndRunner(ctx context.Context, id uuid.UUID, status JobStatus, decision, reason string, moderatedBy, poolID, runnerID *uuid.UUID) error
	UpdateLifecycle(ctx context.Context, githubJobID int64, status JobStatus, conclusion string, startedAt, completedAt *time.Time) error
}

type PolicyRepository interface {
	Count(ctx context.Context) (int, error)
	Create(ctx context.Context, policy *ModerationPolicy) error
	Update(ctx context.Context, policy *ModerationPolicy) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*ModerationPolicy, error)
	List(ctx context.Context, page PageParams) ([]ModerationPolicy, int, error)
	ListEnabledOrdered(ctx context.Context) ([]ModerationPolicy, error)
}

type AuditRepository interface {
	InsertDeliveryIfAbsent(ctx context.Context, d *WebhookDelivery) (bool, error)
	UpdateDeliveryStatus(ctx context.Context, deliveryID, status, errMsg string, processingMS int) error
	ListDeliveries(ctx context.Context, page PageParams) ([]WebhookDelivery, int, error)
	AppendAuditLog(ctx context.Context, log *AuditLog) error
	ListAuditLogs(ctx context.Context, page PageParams) ([]AuditLog, int, error)
	GetDashboardOverview(ctx context.Context) (*DashboardOverview, error)
}
