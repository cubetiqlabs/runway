package repository

import (
	"encoding/json"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

func nonEmptyJSON(raw []byte) []byte {
	if len(raw) == 0 {
		return []byte(`{}`)
	}
	return raw
}

// Models returns all GORM-managed database models in dependency order for AutoMigrate.
func Models() []any {
	return []any{
		&UserModel{},
		&SessionModel{},
		&APITokenModel{},
		&InstallationModel{},
		&RunnerPoolModel{},
		&ModerationPolicyModel{},
		&RunnerModel{},
		&WorkflowJobModel{},
		&WebhookDeliveryModel{},
		&AuditLogModel{},
		&AgentModel{},
		&AgentTaskModel{},
	}
}

type UserModel struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Email        string    `gorm:"type:text;uniqueIndex;not null"`
	Name         string    `gorm:"type:text;not null"`
	PasswordHash string    `gorm:"type:text;not null"`
	Role         string    `gorm:"type:text;not null;default:'moderator'"`
	IsActive     bool      `gorm:"not null;default:true"`
	CreatedAt    time.Time `gorm:"not null;default:now()"`
	UpdatedAt    time.Time `gorm:"not null;default:now()"`
}

func (UserModel) TableName() string { return "users" }

func (m UserModel) toDomain() domain.User {
	return domain.User{
		ID:           m.ID,
		Email:        m.Email,
		Name:         m.Name,
		PasswordHash: m.PasswordHash,
		Role:         domain.UserRole(m.Role),
		IsActive:     m.IsActive,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
}

type SessionModel struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID    uuid.UUID `gorm:"type:uuid;index;not null"`
	User      UserModel `gorm:"foreignKey:UserID;references:ID;constraint:OnDelete:CASCADE"`
	TokenHash string    `gorm:"type:text;uniqueIndex;not null"`
	CSRFToken string    `gorm:"column:csrf_token;type:text;not null"`
	IPAddress string    `gorm:"type:text;not null;default:''"`
	UserAgent string    `gorm:"type:text;not null;default:''"`
	ExpiresAt time.Time `gorm:"index;not null"`
	CreatedAt time.Time `gorm:"not null;default:now()"`
}

func (SessionModel) TableName() string { return "sessions" }

func (m SessionModel) toDomain() domain.Session {
	return domain.Session{
		ID:        m.ID,
		UserID:    m.UserID,
		TokenHash: m.TokenHash,
		CSRFToken: m.CSRFToken,
		IPAddress: m.IPAddress,
		UserAgent: m.UserAgent,
		ExpiresAt: m.ExpiresAt,
		CreatedAt: m.CreatedAt,
	}
}

type APITokenModel struct {
	ID         uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID     uuid.UUID      `gorm:"type:uuid;index;not null"`
	User       UserModel      `gorm:"foreignKey:UserID;references:ID;constraint:OnDelete:CASCADE"`
	Name       string         `gorm:"type:text;not null"`
	Prefix     string         `gorm:"type:text;not null"`
	TokenHash  string         `gorm:"type:text;uniqueIndex;not null"`
	Scopes     pq.StringArray `gorm:"type:text[];not null;default:'{read}'"`
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	CreatedAt  time.Time `gorm:"not null;default:now()"`
}

func (APITokenModel) TableName() string { return "api_tokens" }

func (m APITokenModel) toDomain() domain.APIToken {
	return domain.APIToken{
		ID:         m.ID,
		UserID:     m.UserID,
		Name:       m.Name,
		Prefix:     m.Prefix,
		TokenHash:  m.TokenHash,
		Scopes:     nonNilSlice([]string(m.Scopes)),
		LastUsedAt: m.LastUsedAt,
		ExpiresAt:  m.ExpiresAt,
		CreatedAt:  m.CreatedAt,
	}
}

type InstallationModel struct {
	ID                    int64           `gorm:"type:bigint;primaryKey;autoIncrement:false"`
	AccountID             int64           `gorm:"type:bigint;not null"`
	AccountLogin          string          `gorm:"type:text;index;not null"`
	AccountType           string          `gorm:"type:text;not null;default:'Organization'"`
	TargetType            string          `gorm:"type:text;not null;default:'Organization'"`
	RepositoriesSelection string          `gorm:"type:text;not null;default:'all'"`
	Permissions           json.RawMessage `gorm:"type:jsonb;not null;default:'{}'"`
	SuspendedAt           *time.Time
	CreatedAt             time.Time `gorm:"not null;default:now()"`
	UpdatedAt             time.Time `gorm:"not null;default:now()"`
}

func (InstallationModel) TableName() string { return "installations" }

func (m InstallationModel) toDomain() domain.Installation {
	return domain.Installation{
		ID:                    m.ID,
		AccountID:             m.AccountID,
		AccountLogin:          m.AccountLogin,
		AccountType:           m.AccountType,
		TargetType:            m.TargetType,
		RepositoriesSelection: m.RepositoriesSelection,
		Permissions:           nonEmptyJSON(m.Permissions),
		SuspendedAt:           m.SuspendedAt,
		CreatedAt:             m.CreatedAt,
		UpdatedAt:             m.UpdatedAt,
	}
}

type RunnerPoolModel struct {
	ID             uuid.UUID          `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	InstallationID *int64             `gorm:"type:bigint"`
	Installation   *InstallationModel `gorm:"foreignKey:InstallationID;references:ID;constraint:OnDelete:SET NULL"`
	Name           string             `gorm:"type:text;uniqueIndex;not null"`
	Slug           string             `gorm:"type:text;uniqueIndex;not null"`
	Description    string             `gorm:"type:text;not null;default:''"`
	Labels         pq.StringArray     `gorm:"type:text[];not null;default:'{}'"`
	OS             string             `gorm:"column:os;type:text;not null;default:'linux'"`
	Arch           string             `gorm:"column:arch;type:text;not null;default:'x64'"`
	VCPU           int                `gorm:"column:vcpu;not null;default:4"`
	MemoryMB       int                `gorm:"column:memory_mb;not null;default:8192"`
	DiskGB         int                `gorm:"column:disk_gb;not null;default:40"`
	MinIdle        int                `gorm:"not null;default:0"`
	MaxConcurrency int                `gorm:"not null;default:10"`
	IdleTimeoutSec int                `gorm:"not null;default:300"`
	RunnerGroupID  int64              `gorm:"type:bigint;not null;default:1"`
	Provider       string             `gorm:"type:text;not null;default:'simulated'"`
	ContainerImage string             `gorm:"type:text;not null;default:'ghcr.io/cubetiqlabs/runway-runner:latest'"`
	Enabled        bool               `gorm:"index;not null;default:true"`
	CreatedAt      time.Time          `gorm:"not null;default:now()"`
	UpdatedAt      time.Time          `gorm:"not null;default:now()"`
}

func (RunnerPoolModel) TableName() string { return "runner_pools" }

func (m RunnerPoolModel) toDomain() domain.RunnerPool {
	return domain.RunnerPool{
		ID:             m.ID,
		InstallationID: m.InstallationID,
		Name:           m.Name,
		Slug:           m.Slug,
		Description:    m.Description,
		Labels:         nonNilSlice([]string(m.Labels)),
		OS:             m.OS,
		Arch:           m.Arch,
		VCPU:           m.VCPU,
		MemoryMB:       m.MemoryMB,
		DiskGB:         m.DiskGB,
		MinIdle:        m.MinIdle,
		MaxConcurrency: m.MaxConcurrency,
		IdleTimeoutSec: m.IdleTimeoutSec,
		RunnerGroupID:  m.RunnerGroupID,
		Provider:       m.Provider,
		ContainerImage: m.ContainerImage,
		Enabled:        m.Enabled,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}

type ModerationPolicyModel struct {
	ID                   uuid.UUID          `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	InstallationID       *int64             `gorm:"type:bigint"`
	Installation         *InstallationModel `gorm:"foreignKey:InstallationID;references:ID;constraint:OnDelete:CASCADE"`
	Name                 string             `gorm:"type:text;not null"`
	Description          string             `gorm:"type:text;not null;default:''"`
	Priority             int                `gorm:"index:idx_policy_eval,priority:2;not null;default:100"`
	Enabled              bool               `gorm:"index:idx_policy_eval,priority:1;not null;default:true"`
	Action               string             `gorm:"type:text;not null"`
	RepoPatterns         pq.StringArray     `gorm:"type:text[];not null;default:'{}'"`
	BranchPatterns       pq.StringArray     `gorm:"type:text[];not null;default:'{}'"`
	ActorPatterns        pq.StringArray     `gorm:"type:text[];not null;default:'{}'"`
	LabelPatterns        pq.StringArray     `gorm:"type:text[];not null;default:'{}'"`
	BlockForkPRs         bool               `gorm:"column:block_fork_prs;not null;default:false"`
	MaxConcurrentPerRepo *int
	MaxJobDurationMin    *int
	CreatedAt            time.Time `gorm:"index:idx_policy_eval,priority:3;not null;default:now()"`
	UpdatedAt            time.Time `gorm:"not null;default:now()"`
}

func (ModerationPolicyModel) TableName() string { return "moderation_policies" }

func (m ModerationPolicyModel) toDomain() domain.ModerationPolicy {
	return domain.ModerationPolicy{
		ID:                   m.ID,
		InstallationID:       m.InstallationID,
		Name:                 m.Name,
		Description:          m.Description,
		Priority:             m.Priority,
		Enabled:              m.Enabled,
		Action:               domain.PolicyAction(m.Action),
		RepoPatterns:         nonNilSlice([]string(m.RepoPatterns)),
		BranchPatterns:       nonNilSlice([]string(m.BranchPatterns)),
		ActorPatterns:        nonNilSlice([]string(m.ActorPatterns)),
		LabelPatterns:        nonNilSlice([]string(m.LabelPatterns)),
		BlockForkPRs:         m.BlockForkPRs,
		MaxConcurrentPerRepo: m.MaxConcurrentPerRepo,
		MaxJobDurationMin:    m.MaxJobDurationMin,
		CreatedAt:            m.CreatedAt,
		UpdatedAt:            m.UpdatedAt,
	}
}

type RunnerModel struct {
	ID              uuid.UUID          `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	PoolID          *uuid.UUID         `gorm:"type:uuid;index"`
	Pool            *RunnerPoolModel   `gorm:"foreignKey:PoolID;references:ID;constraint:OnDelete:SET NULL"`
	PoolName        string             `gorm:"->;-:migration"`
	InstallationID  *int64             `gorm:"type:bigint"`
	Installation    *InstallationModel `gorm:"foreignKey:InstallationID;references:ID;constraint:OnDelete:SET NULL"`
	GitHubRunnerID  *int64             `gorm:"column:github_runner_id;type:bigint"`
	Name            string             `gorm:"type:text;uniqueIndex;not null"`
	Provider        string             `gorm:"type:text;not null;default:'simulated'"`
	ProviderRef     string             `gorm:"type:text;not null;default:''"`
	Labels          pq.StringArray     `gorm:"type:text[];not null;default:'{}'"`
	Status          string             `gorm:"type:text;index;not null;default:'provisioning'"`
	Ephemeral       bool               `gorm:"not null;default:true"`
	CurrentJobID    *int64             `gorm:"column:current_job_id;type:bigint"`
	HostInfo        json.RawMessage    `gorm:"type:jsonb;not null;default:'{}'"`
	LastHeartbeatAt *time.Time
	CreatedAt       time.Time `gorm:"not null;default:now()"`
	UpdatedAt       time.Time `gorm:"not null;default:now()"`
}

func (RunnerModel) TableName() string { return "runners" }

func (m RunnerModel) toDomain() domain.Runner {
	return domain.Runner{
		ID:              m.ID,
		PoolID:          m.PoolID,
		PoolName:        m.PoolName,
		InstallationID:  m.InstallationID,
		GitHubRunnerID:  m.GitHubRunnerID,
		Name:            m.Name,
		Provider:        m.Provider,
		ProviderRef:     m.ProviderRef,
		Labels:          nonNilSlice([]string(m.Labels)),
		Status:          domain.RunnerStatus(m.Status),
		Ephemeral:       m.Ephemeral,
		CurrentJobID:    m.CurrentJobID,
		HostInfo:        nonEmptyJSON(m.HostInfo),
		LastHeartbeatAt: m.LastHeartbeatAt,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}

type WorkflowJobModel struct {
	ID                 uuid.UUID              `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	GitHubJobID        int64                  `gorm:"column:github_job_id;type:bigint;uniqueIndex;not null"`
	GitHubRunID        int64                  `gorm:"column:github_run_id;type:bigint;not null"`
	InstallationID     *int64                 `gorm:"type:bigint"`
	Installation       *InstallationModel     `gorm:"foreignKey:InstallationID;references:ID;constraint:OnDelete:SET NULL"`
	RepoFullName       string                 `gorm:"type:text;index:idx_job_repo_status,priority:1;not null"`
	WorkflowName       string                 `gorm:"type:text;not null;default:''"`
	JobName            string                 `gorm:"type:text;not null"`
	HeadBranch         string                 `gorm:"type:text;not null;default:''"`
	HeadSHA            string                 `gorm:"column:head_sha;type:text;not null;default:''"`
	Actor              string                 `gorm:"type:text;not null;default:''"`
	IsForkPR           bool                   `gorm:"column:is_fork_pr;not null;default:false"`
	Labels             pq.StringArray         `gorm:"type:text[];not null;default:'{}'"`
	Status             string                 `gorm:"type:text;index:idx_job_status_queued,priority:1;index:idx_job_repo_status,priority:2;not null;default:'queued'"`
	Conclusion         string                 `gorm:"type:text;not null;default:''"`
	MatchedPoolID      *uuid.UUID             `gorm:"type:uuid"`
	MatchedPool        *RunnerPoolModel       `gorm:"foreignKey:MatchedPoolID;references:ID;constraint:OnDelete:SET NULL"`
	MatchedPoolName    string                 `gorm:"->;-:migration"`
	MatchedPolicyID    *uuid.UUID             `gorm:"type:uuid"`
	MatchedPolicy      *ModerationPolicyModel `gorm:"foreignKey:MatchedPolicyID;references:ID;constraint:OnDelete:SET NULL"`
	MatchedPolicyName  string                 `gorm:"->;-:migration"`
	ModerationDecision string                 `gorm:"type:text;not null;default:'allow'"`
	ModerationReason   string                 `gorm:"type:text;not null;default:''"`
	ModeratedBy        *uuid.UUID             `gorm:"type:uuid"`
	Moderator          *UserModel             `gorm:"foreignKey:ModeratedBy;references:ID;constraint:OnDelete:SET NULL"`
	AssignedRunnerID   *uuid.UUID             `gorm:"type:uuid"`
	AssignedRunner     *RunnerModel           `gorm:"foreignKey:AssignedRunnerID;references:ID;constraint:OnDelete:SET NULL"`
	AssignedRunnerName string                 `gorm:"->;-:migration"`
	QueuedAt           time.Time              `gorm:"index:idx_job_status_queued,priority:2;not null;default:now()"`
	StartedAt          *time.Time
	CompletedAt        *time.Time
	CreatedAt          time.Time `gorm:"not null;default:now()"`
	UpdatedAt          time.Time `gorm:"not null;default:now()"`
}

func (WorkflowJobModel) TableName() string { return "workflow_jobs" }

func (m WorkflowJobModel) toDomain() domain.WorkflowJob {
	return domain.WorkflowJob{
		ID:                 m.ID,
		GitHubJobID:        m.GitHubJobID,
		GitHubRunID:        m.GitHubRunID,
		InstallationID:     m.InstallationID,
		RepoFullName:       m.RepoFullName,
		WorkflowName:       m.WorkflowName,
		JobName:            m.JobName,
		HeadBranch:         m.HeadBranch,
		HeadSHA:            m.HeadSHA,
		Actor:              m.Actor,
		IsForkPR:           m.IsForkPR,
		Labels:             nonNilSlice([]string(m.Labels)),
		Status:             domain.JobStatus(m.Status),
		Conclusion:         m.Conclusion,
		MatchedPoolID:      m.MatchedPoolID,
		MatchedPoolName:    m.MatchedPoolName,
		MatchedPolicyID:    m.MatchedPolicyID,
		MatchedPolicyName:  m.MatchedPolicyName,
		ModerationDecision: m.ModerationDecision,
		ModerationReason:   m.ModerationReason,
		ModeratedBy:        m.ModeratedBy,
		AssignedRunnerID:   m.AssignedRunnerID,
		AssignedRunnerName: m.AssignedRunnerName,
		QueuedAt:           m.QueuedAt,
		StartedAt:          m.StartedAt,
		CompletedAt:        m.CompletedAt,
		CreatedAt:          m.CreatedAt,
		UpdatedAt:          m.UpdatedAt,
	}
}

type WebhookDeliveryModel struct {
	DeliveryID     string    `gorm:"type:text;primaryKey"`
	EventName      string    `gorm:"type:text;not null"`
	Action         string    `gorm:"type:text;not null;default:''"`
	InstallationID *int64    `gorm:"type:bigint"`
	RepoFullName   string    `gorm:"type:text;not null;default:''"`
	SignatureValid bool      `gorm:"not null"`
	Status         string    `gorm:"type:text;not null;default:'received'"`
	ErrorMessage   string    `gorm:"type:text;not null;default:''"`
	PayloadHash    string    `gorm:"type:text;not null;default:''"`
	ProcessingMS   int       `gorm:"column:processing_ms;not null;default:0"`
	ReceivedAt     time.Time `gorm:"index;not null;default:now()"`
}

func (WebhookDeliveryModel) TableName() string { return "webhook_deliveries" }

func (m WebhookDeliveryModel) toDomain() domain.WebhookDelivery {
	return domain.WebhookDelivery{
		DeliveryID:     m.DeliveryID,
		EventName:      m.EventName,
		Action:         m.Action,
		InstallationID: m.InstallationID,
		RepoFullName:   m.RepoFullName,
		SignatureValid: m.SignatureValid,
		Status:         m.Status,
		ErrorMessage:   m.ErrorMessage,
		PayloadHash:    m.PayloadHash,
		ProcessingMS:   m.ProcessingMS,
		ReceivedAt:     m.ReceivedAt,
	}
}

type AuditLogModel struct {
	ID           int64           `gorm:"primaryKey;autoIncrement"`
	ActorID      *uuid.UUID      `gorm:"type:uuid"`
	ActorLabel   string          `gorm:"type:text;not null"`
	Action       string          `gorm:"type:text;not null"`
	ResourceType string          `gorm:"type:text;index:idx_audit_resource,priority:1;not null"`
	ResourceID   string          `gorm:"type:text;index:idx_audit_resource,priority:2;not null"`
	Metadata     json.RawMessage `gorm:"type:jsonb;not null;default:'{}'"`
	IPAddress    string          `gorm:"type:text;not null;default:''"`
	CreatedAt    time.Time       `gorm:"index;not null;default:now()"`
}

func (AuditLogModel) TableName() string { return "audit_logs" }

func (m AuditLogModel) toDomain() domain.AuditLog {
	return domain.AuditLog{
		ID:           m.ID,
		ActorID:      m.ActorID,
		ActorLabel:   m.ActorLabel,
		Action:       m.Action,
		ResourceType: m.ResourceType,
		ResourceID:   m.ResourceID,
		Metadata:     nonEmptyJSON(m.Metadata),
		IPAddress:    m.IPAddress,
		CreatedAt:    m.CreatedAt,
	}
}

type AgentModel struct {
	ID              uuid.UUID        `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Name            string           `gorm:"type:text;uniqueIndex;not null"`
	TokenPrefix     string           `gorm:"type:text;not null"`
	TokenHash       string           `gorm:"type:text;uniqueIndex;not null"`
	OS              string           `gorm:"column:os;type:text;not null;default:'linux'"`
	Arch            string           `gorm:"column:arch;type:text;not null;default:'x64'"`
	Runtime         string           `gorm:"type:text;not null;default:'native'"`
	Labels          pq.StringArray   `gorm:"type:text[];not null;default:'{}'"`
	PoolID          *uuid.UUID       `gorm:"type:uuid;index"`
	Pool            *RunnerPoolModel `gorm:"foreignKey:PoolID;references:ID;constraint:OnDelete:SET NULL"`
	PoolName        string           `gorm:"->;-:migration"`
	Version         string           `gorm:"type:text;not null;default:'dev'"`
	Hostname        string           `gorm:"type:text;not null;default:''"`
	CPUCount        int              `gorm:"column:cpu_count;not null;default:0"`
	MemoryMB        int              `gorm:"column:memory_mb;not null;default:0"`
	MaxConcurrency  int              `gorm:"not null;default:2"`
	ActiveJobs      int              `gorm:"not null;default:0"`
	Status          string           `gorm:"type:text;index;not null;default:'offline'"`
	LastHeartbeatAt *time.Time
	CreatedAt       time.Time `gorm:"not null;default:now()"`
	UpdatedAt       time.Time `gorm:"not null;default:now()"`
}

func (AgentModel) TableName() string { return "agents" }

func (m AgentModel) toDomain() domain.Agent {
	return domain.Agent{
		ID:              m.ID,
		Name:            m.Name,
		TokenPrefix:     m.TokenPrefix,
		TokenHash:       m.TokenHash,
		OS:              m.OS,
		Arch:            m.Arch,
		Runtime:         m.Runtime,
		Labels:          nonNilSlice([]string(m.Labels)),
		PoolID:          m.PoolID,
		PoolName:        m.PoolName,
		Version:         m.Version,
		Hostname:        m.Hostname,
		CPUCount:        m.CPUCount,
		MemoryMB:        m.MemoryMB,
		MaxConcurrency:  m.MaxConcurrency,
		ActiveJobs:      m.ActiveJobs,
		Status:          domain.AgentStatus(m.Status),
		LastHeartbeatAt: m.LastHeartbeatAt,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}

type AgentTaskModel struct {
	ID                        uuid.UUID         `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	AgentID                   *uuid.UUID        `gorm:"type:uuid;index"`
	Agent                     *AgentModel       `gorm:"foreignKey:AgentID;references:ID;constraint:OnDelete:SET NULL"`
	RunnerID                  uuid.UUID         `gorm:"type:uuid;index;not null"`
	PoolID                    *uuid.UUID        `gorm:"type:uuid;index"`
	JobID                     uuid.UUID         `gorm:"type:uuid;index;not null"`
	GitHubJobID               int64             `gorm:"column:github_job_id;type:bigint;not null"`
	RepoFullName              string            `gorm:"type:text;not null;default:''"`
	RunnerName                string            `gorm:"type:text;not null"`
	PoolSlug                  string            `gorm:"type:text;not null;default:''"`
	OS                        string            `gorm:"column:os;type:text;not null;default:'linux'"`
	Arch                      string            `gorm:"column:arch;type:text;not null;default:'x64'"`
	Labels                    pq.StringArray    `gorm:"type:text[];not null;default:'{}'"`
	ContainerImage            string            `gorm:"type:text;not null;default:''"`
	VCPU                      int               `gorm:"column:vcpu;not null;default:4"`
	MemoryMB                  int               `gorm:"column:memory_mb;not null;default:8192"`
	EncodedJITConfigEncrypted []byte            `gorm:"column:encoded_jit_config_encrypted;type:bytea"`
	Status                    string            `gorm:"type:text;index;not null;default:'pending'"`
	ErrorMessage              string            `gorm:"type:text;not null;default:''"`
	ClaimedAt                 *time.Time
	CompletedAt               *time.Time
	CreatedAt                 time.Time `gorm:"index;not null;default:now()"`
	UpdatedAt                 time.Time `gorm:"not null;default:now()"`
}

func (AgentTaskModel) TableName() string { return "agent_tasks" }

func (m AgentTaskModel) toDomain() domain.AgentTask {
	return domain.AgentTask{
		ID:                        m.ID,
		AgentID:                   m.AgentID,
		RunnerID:                  m.RunnerID,
		PoolID:                    m.PoolID,
		JobID:                     m.JobID,
		GitHubJobID:               m.GitHubJobID,
		RepoFullName:              m.RepoFullName,
		RunnerName:                m.RunnerName,
		PoolSlug:                  m.PoolSlug,
		OS:                        m.OS,
		Arch:                      m.Arch,
		Labels:                    nonNilSlice([]string(m.Labels)),
		ContainerImage:            m.ContainerImage,
		VCPU:                      m.VCPU,
		MemoryMB:                  m.MemoryMB,
		EncodedJITConfigEncrypted: m.EncodedJITConfigEncrypted,
		Status:                    domain.AgentTaskStatus(m.Status),
		ErrorMessage:              m.ErrorMessage,
		ClaimedAt:                 m.ClaimedAt,
		CompletedAt:               m.CompletedAt,
		CreatedAt:                 m.CreatedAt,
		UpdatedAt:                 m.UpdatedAt,
	}
}
