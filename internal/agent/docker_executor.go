package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/cubetiqlabs/runway/internal/orchestrator"
)

type DockerExecutor struct {
	prov         *orchestrator.DockerProvisioner
	cacheEnabled bool
}

func NewDockerExecutor(dockerHost string, opts orchestrator.DockerOptions) *DockerExecutor {
	return &DockerExecutor{
		prov:         orchestrator.NewDockerProvisionerWithOptions(dockerHost, opts),
		cacheEnabled: opts.CacheEnabled,
	}
}

func (e *DockerExecutor) Ping(ctx context.Context) error {
	return e.prov.Ping(ctx)
}

func (e *DockerExecutor) CacheStats() (hits, misses int64) {
	return e.prov.CacheStats()
}

func (e *DockerExecutor) PrewarmImages(ctx context.Context, images []string) {
	e.prov.PrewarmImages(ctx, images)
}

func (e *DockerExecutor) Execute(ctx context.Context, task *TaskPayload) error {
	return e.ExecuteWithLogs(ctx, task, nil)
}

func (e *DockerExecutor) ExecuteWithLogs(ctx context.Context, task *TaskPayload, logSink func([]domain.RunnerLogEntry)) error {
	provCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	cacheEnabled := e.cacheEnabled && task.CacheEnabled
	res, err := e.prov.Provision(provCtx, orchestrator.ProvisionRequest{
		RunnerID:         task.RunnerID,
		RunnerName:       task.RunnerName,
		EncodedJITConfig: task.EncodedJITConfig,
		GitHubJobID:      task.GitHubJobID,
		RepoFullName:     task.RepoFullName,
		IsForkPR:         task.IsForkPR,
		Pool: domain.RunnerPool{
			Slug:           task.PoolSlug,
			ContainerImage: task.ContainerImage,
			VCPU:           task.VCPU,
			MemoryMB:       task.MemoryMB,
			BuildProfile:   task.BuildProfile,
			CacheEnabled:   cacheEnabled,
			CacheScope:     task.CacheScope,
			SandboxMode:    task.SandboxMode,
		},
	})
	cancel()
	if err != nil {
		return err
	}

	defer func() {
		if logSink != nil {
			logCtx, logCancel := context.WithTimeout(context.Background(), 5*time.Second)
			if entries, logErr := e.prov.FetchLogs(logCtx, res.ProviderRef, 500); logErr == nil && len(entries) > 0 {
				for i := range entries {
					if task.EncodedJITConfig != "" {
						entries[i].Line = strings.ReplaceAll(entries[i].Line, task.EncodedJITConfig, "[REDACTED]")
					}
				}
				logSink(entries)
			}
			logCancel()
		}
		termCtx, termCancel := context.WithTimeout(context.Background(), 20*time.Second)
		_ = e.prov.Terminate(termCtx, res.ProviderRef)
		termCancel()
	}()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
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
