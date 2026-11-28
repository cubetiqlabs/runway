package service

import (
	"context"
	"time"

	"github.com/cubetiqlabs/runway/internal/cache"
	"github.com/cubetiqlabs/runway/internal/domain"
)

const (
	cacheKeyOverview       = "dashboard:overview"
	cacheKeyEnabledPools   = "pools:enabled"
	cacheKeyEnabledPolicy  = "policies:enabled"
	cacheClusterSlotFreed  = "cluster:slot_freed"
	enabledConfigCacheTTL  = 30 * time.Second
	installationCacheTTL   = 5 * time.Minute
	webhookDedupTTL        = 24 * time.Hour
)

func cachedEnabledPools(ctx context.Context, c cache.Store, repo domain.PoolRepository) ([]domain.RunnerPool, error) {
	return cache.GetOrLoad(ctx, c, cacheKeyEnabledPools, enabledConfigCacheTTL, func(loadCtx context.Context) ([]domain.RunnerPool, error) {
		return repo.ListEnabled(loadCtx)
	})
}

func cachedEnabledPolicies(ctx context.Context, c cache.Store, repo domain.PolicyRepository) ([]domain.ModerationPolicy, error) {
	return cache.GetOrLoad(ctx, c, cacheKeyEnabledPolicy, enabledConfigCacheTTL, func(loadCtx context.Context) ([]domain.ModerationPolicy, error) {
		return repo.ListEnabledOrdered(loadCtx)
	})
}

func invalidateCacheKeys(ctx context.Context, c cache.Store, keys ...string) {
	if c != nil && len(keys) > 0 {
		_ = c.Delete(ctx, keys...)
	}
}
