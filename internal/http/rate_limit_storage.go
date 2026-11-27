package http

import (
	"context"
	"strings"
	"time"

	"github.com/cubetiqlabs/runway/internal/cache"
)

// cacheFiberStorage adapts cache.Store to GoFiber v3's fiber.Storage interface
// so rate-limit state is shared across all control-plane replicas in a cluster.
type cacheFiberStorage struct {
	store  cache.Store
	prefix string
}

func newCacheFiberStorage(store cache.Store, prefix string) *cacheFiberStorage {
	if prefix != "" && !strings.HasSuffix(prefix, ":") {
		prefix += ":"
	}
	return &cacheFiberStorage{store: store, prefix: prefix}
}

func (s *cacheFiberStorage) fullKey(key string) string {
	return s.prefix + key
}

func (s *cacheFiberStorage) GetWithContext(ctx context.Context, key string) ([]byte, error) {
	if s.store == nil || key == "" {
		return nil, nil
	}
	val, ok, err := s.store.Get(ctx, s.fullKey(key))
	if err != nil || !ok {
		return nil, err
	}
	return val, nil
}

func (s *cacheFiberStorage) Get(key string) ([]byte, error) {
	return s.GetWithContext(context.Background(), key)
}

func (s *cacheFiberStorage) SetWithContext(ctx context.Context, key string, val []byte, exp time.Duration) error {
	if s.store == nil || key == "" || len(val) == 0 {
		return nil
	}
	return s.store.Set(ctx, s.fullKey(key), val, exp)
}

func (s *cacheFiberStorage) Set(key string, val []byte, exp time.Duration) error {
	return s.SetWithContext(context.Background(), key, val, exp)
}

func (s *cacheFiberStorage) DeleteWithContext(ctx context.Context, key string) error {
	if s.store == nil || key == "" {
		return nil
	}
	return s.store.Delete(ctx, s.fullKey(key))
}

func (s *cacheFiberStorage) Delete(key string) error {
	return s.DeleteWithContext(context.Background(), key)
}

func (s *cacheFiberStorage) ResetWithContext(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	return s.store.DeletePrefix(ctx, s.prefix)
}

func (s *cacheFiberStorage) Reset() error {
	return s.ResetWithContext(context.Background())
}

func (s *cacheFiberStorage) Close() error {
	// Shared cacheStore lifecycle is managed by cmd/server/main.go.
	return nil
}
