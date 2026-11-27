package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/orchestrator"
)

type DockerExecutor struct {
	prov *orchestrator.DockerProvisioner
}

func NewDockerExecutor(dockerHost string, opts orchestrator.DockerOptions) *DockerExecutor {
	return &DockerExecutor{
		prov: orchestrator.NewDockerProvisionerWithOptions(dockerHost, opts),
	}
}

func (e *DockerExecutor) Ping(ctx context.Context) error {
	return e.prov.Ping(ctx)
}

func (e *DockerExecutor) Execute(ctx context.Context, task *TaskPayload) error {
	provCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	res, err := e.prov.Provision(provCtx, orchestrator.ProvisionRequest{
		RunnerID:         task.RunnerID,
		RunnerName:       task.RunnerName,
		EncodedJITConfig: task.EncodedJITConfig,
		GitHubJobID:      task.GitHubJobID,
		RepoFullName:     task.RepoFullName,
		Pool: domain.RunnerPool{
			Slug:           task.PoolSlug,
			ContainerImage: task.ContainerImage,
			VCPU:           task.VCPU,
			MemoryMB:       task.MemoryMB,
		},
	})
	cancel()
	if err != nil {
		return err
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			termCtx, termCancel := context.WithTimeout(context.Background(), 15*time.Second)
			_ = e.prov.Terminate(termCtx, res.ProviderRef)
			termCancel()
			return fmt.Errorf("runner cancelled: %w", ctx.Err())
		case <-ticker.C:
			healthy, healthErr := e.prov.Healthy(ctx, res.ProviderRef)
			if healthErr != nil {
				continue
			}
			if !healthy {
				return nil
			}
		}
	}
}
