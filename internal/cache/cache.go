package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

// Lock represents a held distributed (or local fallback) mutual-exclusion lease.
type Lock interface {
	Extend(ctx context.Context, ttl time.Duration) (bool, error)
	Release(ctx context.Context) error
}

// Subscription receives payloads broadcast over a cluster Pub/Sub channel.
type Subscription interface {
	Channel() <-chan string
	Close() error
}

// Store abstracts Valkey/Redis caching, distributed locking, Pub/Sub messaging,
// and live control-plane cluster node registration.
type Store interface {
	Driver() string
	ClusterEnabled() bool
	InstanceID() string
	Ping(ctx context.Context) error
	Close() error

	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	DeletePrefix(ctx context.Context, prefix string) error
	SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error)

	AcquireLock(ctx context.Context, name string, ttl time.Duration) (Lock, bool, error)
	Publish(ctx context.Context, channel string, payload string) error
	Subscribe(ctx context.Context, channels ...string) (Subscription, error)

	RegisterNode(ctx context.Context, node domain.ClusterNode, ttl time.Duration) error
	UnregisterNode(ctx context.Context, instanceID string) error
	ListNodes(ctx context.Context) ([]domain.ClusterNode, error)

	Stats() domain.CacheStats
}

var loadGroup singleflight.Group

// New constructs the configured cache store (`valkey`, `redis`, `memory`, or `disabled`).
func New(ctx context.Context, cfg config.CacheConfig, log *zap.Logger) (Store, error) {
	if log == nil {
		log = zap.NewNop()
	}
	instanceID := resolveInstanceID(cfg.InstanceID)
	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	switch driver {
	case "valkey", "redis":
		return newRedisStore(ctx, cfg, instanceID, log)
	case "disabled":
		return NewDisabledStore(instanceID), nil
	case "", "memory":
		return NewMemoryStore(cfg.KeyPrefix, instanceID), nil
	default:
		return nil, fmt.Errorf("unsupported cache driver %q", cfg.Driver)
	}
}

func resolveInstanceID(explicit string) string {
	if trimmed := strings.TrimSpace(explicit); trimmed != "" {
		return trimmed
	}
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "runway"
	}
	short := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	return host + "-" + short
}

// GetJSON reads and unmarshals a JSON-encoded cache entry.
func GetJSON[T any](ctx context.Context, s Store, key string) (T, bool, error) {
	var zero T
	if s == nil {
		return zero, false, nil
	}
	raw, ok, err := s.Get(ctx, key)
	if err != nil || !ok || len(raw) == 0 {
		return zero, false, err
	}
	var out T
	if uErr := json.Unmarshal(raw, &out); uErr != nil {
		_ = s.Delete(ctx, key)
		return zero, false, nil
	}
	return out, true, nil
}

// SetJSON marshals val as JSON and writes it to the cache store.
func SetJSON(ctx context.Context, s Store, key string, val any, ttl time.Duration) error {
	if s == nil {
		return nil
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return s.Set(ctx, key, raw, ttl)
}

// GetOrLoad returns cached JSON or invokes loader once per concurrent key miss (singleflight).
// Cache backend failures never block requests; loader errors are propagated directly.
func GetOrLoad[T any](ctx context.Context, s Store, key string, ttl time.Duration, loader func(context.Context) (T, error)) (T, error) {
	if s == nil {
		return loader(ctx)
	}
	if val, ok, err := GetJSON[T](ctx, s, key); err == nil && ok {
		return val, nil
	}

	sfKey := fmt.Sprintf("%p:%s", s, key)
	res, err, _ := loadGroup.Do(sfKey, func() (any, error) {
		if val, ok, gErr := GetJSON[T](ctx, s, key); gErr == nil && ok {
			return val, nil
		}
		loaded, loadErr := loader(ctx)
		if loadErr != nil {
			return nil, loadErr
		}
		_ = SetJSON(ctx, s, key, loaded, ttl)
		return loaded, nil
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return res.(T), nil
}

// AcquireLockWithWait retries acquiring a distributed lock with short backoff until waitTimeout or ctx cancellation.
func AcquireLockWithWait(ctx context.Context, s Store, name string, ttl, waitTimeout time.Duration) (Lock, bool, error) {
	if s == nil {
		return nil, false, nil
	}
	if waitTimeout <= 0 {
		return s.AcquireLock(ctx, name, ttl)
	}

	deadline := time.Now().Add(waitTimeout)
	delay := 15 * time.Millisecond
	for {
		lock, ok, err := s.AcquireLock(ctx, name, ttl)
		if err != nil || ok {
			return lock, ok, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, false, nil
		}
		sleep := delay
		if sleep > remaining {
			sleep = remaining
		}
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, false, ctx.Err()
		case <-timer.C:
		}
		if delay < 50*time.Millisecond {
			delay += 10 * time.Millisecond
		}
	}
}

func computeHitRate(hits, misses uint64) float64 {
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) * 100.0 / float64(total)
}
