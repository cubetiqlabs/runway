package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cubetiqlabs/runway/internal/crypto"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
)

const agentTaskRefPrefix = "agent-task:"

// AgentProvisioner queues encrypted ephemeral JIT runner tasks for distributed runway-agent hosts.
type AgentProvisioner struct {
	agentRepo domain.AgentRepository
	encKey    []byte
}

func NewAgentProvisioner(agentRepo domain.AgentRepository, encKey []byte) *AgentProvisioner {
	return &AgentProvisioner{
		agentRepo: agentRepo,
		encKey:    encKey,
	}
}

func (p *AgentProvisioner) Name() string { return "agent" }

func (p *AgentProvisioner) Ping(ctx context.Context) error {
	if p.agentRepo == nil {
		return fmt.Errorf("agent repository is not initialized")
	}
	count, err := p.agentRepo.CountOnline(ctx)
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("no online runway-agent workers connected; register and start at least one agent")
	}
	return nil
}

func (p *AgentProvisioner) Provision(ctx context.Context, req ProvisionRequest) (*ProvisionResult, error) {
	if p.agentRepo == nil {
		return nil, fmt.Errorf("agent repository is not initialized")
	}
	if len(p.encKey) != 32 {
		return nil, fmt.Errorf("agent provisioner requires a valid 32-byte encryption key")
	}
	if strings.TrimSpace(req.EncodedJITConfig) == "" {
		return nil, fmt.Errorf("agent provisioner requires encoded_jit_config (ensure GitHub App credentials and installation permissions are configured)")
	}

	ciphertext, err := crypto.EncryptAESGCM(p.encKey, []byte(req.EncodedJITConfig))
	if err != nil {
		return nil, fmt.Errorf("encrypt jit config: %w", err)
	}

	poolID := req.Pool.ID
	task := &domain.AgentTask{
		RunnerID:                  req.RunnerID,
		PoolID:                    &poolID,
		JobID:                     req.JobID,
		GitHubJobID:               req.GitHubJobID,
		RepoFullName:              req.RepoFullName,
		RunnerName:                req.RunnerName,
		PoolSlug:                  req.Pool.Slug,
		OS:                        req.Pool.OS,
		Arch:                      req.Pool.Arch,
		Labels:                    req.Pool.Labels,
		ContainerImage:            req.Pool.ContainerImage,
		VCPU:                      req.Pool.VCPU,
		MemoryMB:                  req.Pool.MemoryMB,
		IsForkPR:                  req.IsForkPR,
		BuildProfile:              req.Pool.BuildProfile,
		CacheEnabled:              req.Pool.CacheEnabled,
		CacheScope:                req.Pool.CacheScope,
		SandboxMode:               req.Pool.SandboxMode,
		EncodedJITConfigEncrypted: ciphertext,
		Status:                    domain.AgentTaskPending,
	}
	if err := p.agentRepo.CreateTask(ctx, task); err != nil {
		return nil, fmt.Errorf("enqueue agent task: %w", err)
	}

	hostInfo, _ := json.Marshal(map[string]any{
		"provider":  "agent",
		"task_id":   task.ID.String(),
		"os":        req.Pool.OS,
		"arch":      req.Pool.Arch,
		"vcpu":      req.Pool.VCPU,
		"memory_mb": req.Pool.MemoryMB,
	})
	return &ProvisionResult{
		ProviderRef:   agentTaskRefPrefix + task.ID.String(),
		InitialStatus: domain.RunnerProvisioning,
		HostInfo:      hostInfo,
	}, nil
}

func (p *AgentProvisioner) Terminate(ctx context.Context, providerRef string) error {
	if p.agentRepo == nil {
		return nil
	}
	taskID, ok := parseAgentTaskRef(providerRef)
	if !ok {
		return nil
	}
	return p.agentRepo.CancelTaskByID(ctx, taskID)
}

func (p *AgentProvisioner) Healthy(ctx context.Context, providerRef string) (bool, error) {
	if p.agentRepo == nil {
		return false, nil
	}
	taskID, ok := parseAgentTaskRef(providerRef)
	if !ok {
		return false, nil
	}
	task, err := p.agentRepo.GetTaskByID(ctx, taskID)
	if err != nil {
		return false, nil
	}
	switch task.Status {
	case domain.AgentTaskPending:
		return true, nil
	case domain.AgentTaskClaimed, domain.AgentTaskRunning:
		if task.AgentID != nil {
			ag, agErr := p.agentRepo.GetByID(ctx, *task.AgentID)
			if agErr != nil || ag.Status == domain.AgentOffline {
				return false, nil
			}
		}
		return true, nil
	default:
		return false, nil
	}
}

func parseAgentTaskRef(ref string) (uuid.UUID, bool) {
	raw := strings.TrimPrefix(strings.TrimSpace(ref), agentTaskRefPrefix)
	if raw == "" || raw == strings.TrimSpace(ref) {
		// Also support "agent:<agentID>:<taskID>" set when an agent claims a task.
		parts := strings.Split(strings.TrimSpace(ref), ":")
		if len(parts) == 3 && parts[0] == "agent" {
			raw = parts[2]
		} else {
			return uuid.Nil, false
		}
	}
	id, err := uuid.Parse(raw)
	return id, err == nil
}
