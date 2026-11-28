package cache

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cubetiqlabs/runway/internal/config"
	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

var (
	releaseLockScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
end
return 0
`)
	extendLockScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("pexpire", KEYS[1], ARGV[2])
end
return 0
`)
)

// RedisStore implements Store backed by Valkey or Redis (standalone, Sentinel, or Cluster).
type RedisStore struct {
	driver     string
	client     redis.UniversalClient
	prefix     string
	defaultTTL time.Duration
	instanceID string
	log        *zap.Logger

	hits    atomic.Uint64
	misses  atomic.Uint64
	sets    atomic.Uint64
	deletes atomic.Uint64
	errors  atomic.Uint64
}

func newRedisStore(ctx context.Context, cfg config.CacheConfig, instanceID string, log *zap.Logger) (*RedisStore, error) {
	opts, err := buildRedisOptions(cfg)
	if err != nil {
		return nil, err
	}
	client := redis.NewUniversalClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.DialTimeout)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect to %s: %w", cfg.Driver, err)
	}

	prefix := strings.TrimSpace(cfg.KeyPrefix)
	if prefix == "" {
		prefix = "runway:"
	}
	if !strings.HasSuffix(prefix, ":") {
		prefix += ":"
	}
	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if driver == "" {
		driver = "valkey"
	}

	log.Info("connected to cache store",
		zap.String("driver", driver),
		zap.String("instance_id", instanceID),
		zap.Bool("cluster_client", len(opts.Addrs) > 1),
	)

	return &RedisStore{
		driver:     driver,
		client:     client,
		prefix:     prefix,
		defaultTTL: cfg.DefaultTTL,
		instanceID: instanceID,
		log:        log,
	}, nil
}

func buildRedisOptions(cfg config.CacheConfig) (*redis.UniversalOptions, error) {
	uOpts := &redis.UniversalOptions{
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	if len(cfg.Addrs) > 0 {
		uOpts.Addrs = append([]string(nil), cfg.Addrs...)
	} else if strings.TrimSpace(cfg.URL) != "" {
		parsed, err := redis.ParseURL(strings.TrimSpace(cfg.URL))
		if err != nil {
			return nil, fmt.Errorf("parse cache.url: %w", err)
		}
		uOpts.Addrs = []string{parsed.Addr}
		if parsed.Username != "" {
			uOpts.Username = parsed.Username
		}
		if uOpts.Password == "" && parsed.Password != "" {
			uOpts.Password = parsed.Password
		}
		if uOpts.DB == 0 && parsed.DB != 0 {
			uOpts.DB = parsed.DB
		}
		if parsed.TLSConfig != nil {
			uOpts.TLSConfig = parsed.TLSConfig
		}
	} else {
		return nil, fmt.Errorf("cache.url or cache.addrs is required for %s driver", cfg.Driver)
	}

	if cfg.TLSEnabled && uOpts.TLSConfig == nil {
		uOpts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return uOpts, nil
}

func (s *RedisStore) key(k string) string {
	return s.prefix + strings.TrimPrefix(k, s.prefix)
}

func (s *RedisStore) Driver() string       { return s.driver }
func (s *RedisStore) ClusterEnabled() bool { return true }
func (s *RedisStore) InstanceID() string   { return s.instanceID }

func (s *RedisStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *RedisStore) Close() error {
	return s.client.Close()
}

func (s *RedisStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if strings.TrimSpace(key) == "" {
		return nil, false, nil
	}
	val, err := s.client.Get(ctx, s.key(key)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			s.misses.Add(1)
			return nil, false, nil
		}
		s.errors.Add(1)
		return nil, false, err
	}
	s.hits.Add(1)
	return val, true, nil
}

func (s *RedisStore) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	if ttl < 0 {
		ttl = s.defaultTTL
	}
	if err := s.client.Set(ctx, s.key(key), val, ttl).Err(); err != nil {
		s.errors.Add(1)
		return err
	}
	s.sets.Add(1)
	return nil
}

func (s *RedisStore) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	full := make([]string, 0, len(keys))
	for _, k := range keys {
		if strings.TrimSpace(k) != "" {
			full = append(full, s.key(k))
		}
	}
	if len(full) == 0 {
		return nil
	}
	if err := s.client.Unlink(ctx, full...).Err(); err != nil {
		s.errors.Add(1)
		return err
	}
	s.deletes.Add(uint64(len(full)))
	return nil
}

func (s *RedisStore) DeletePrefix(ctx context.Context, prefix string) error {
	keys, err := s.scanKeys(ctx, s.key(prefix)+"*")
	if err != nil {
		s.errors.Add(1)
		return err
	}
	const batchSize = 200
	for i := 0; i < len(keys); i += batchSize {
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		if err := s.client.Unlink(ctx, keys[i:end]...).Err(); err != nil {
			s.errors.Add(1)
			return err
		}
		s.deletes.Add(uint64(end - i))
	}
	return nil
}

func (s *RedisStore) SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error) {
	if strings.TrimSpace(key) == "" {
		return false, nil
	}
	ok, err := s.client.SetNX(ctx, s.key(key), val, ttl).Result()
	if err != nil {
		s.errors.Add(1)
		return false, err
	}
	if ok {
		s.sets.Add(1)
	}
	return ok, nil
}

func (s *RedisStore) AcquireLock(ctx context.Context, name string, ttl time.Duration) (Lock, bool, error) {
	if ttl <= 0 {
		ttl = 10 * time.Second
	}
	fullKey := s.key("lock:" + name)
	token := uuid.NewString()
	ok, err := s.client.SetNX(ctx, fullKey, token, ttl).Result()
	if err != nil {
		s.errors.Add(1)
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	return &redisLock{
		store: s,
		key:   fullKey,
		token: token,
	}, true, nil
}

type redisLock struct {
	store *RedisStore
	key   string
	token string
}

func (l *redisLock) Extend(ctx context.Context, ttl time.Duration) (bool, error) {
	ms := ttl.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	res, err := extendLockScript.Run(ctx, l.store.client, []string{l.key}, l.token, ms).Int64()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

func (l *redisLock) Release(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
	}
	_, err := releaseLockScript.Run(ctx, l.store.client, []string{l.key}, l.token).Result()
	return err
}

func (s *RedisStore) Publish(ctx context.Context, channel string, payload string) error {
	if strings.TrimSpace(channel) == "" {
		return nil
	}
	if err := s.client.Publish(ctx, s.key("pubsub:"+channel), payload).Err(); err != nil {
		s.errors.Add(1)
		return err
	}
	return nil
}

func (s *RedisStore) Subscribe(ctx context.Context, channels ...string) (Subscription, error) {
	prefixed := make([]string, 0, len(channels))
	for _, ch := range channels {
		if strings.TrimSpace(ch) != "" {
			prefixed = append(prefixed, s.key("pubsub:"+ch))
		}
	}
	ps := s.client.Subscribe(ctx, prefixed...)
	// Wait for subscription ACK before returning so immediate Publish hits the subscriber.
	if _, err := ps.Receive(ctx); err != nil {
		_ = ps.Close()
		return nil, err
	}
	out := make(chan string, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(out)
		ch := ps.Channel()
		for msg := range ch {
			select {
			case out <- msg.Payload:
			default:
				// Coalesce burst wakeups rather than blocking pubsub reader
			}
		}
	}()
	return &redisSubscription{ps: ps, ch: out, done: done}, nil
}

type redisSubscription struct {
	ps   *redis.PubSub
	ch   chan string
	done chan struct{}
}

func (s *redisSubscription) Channel() <-chan string { return s.ch }

func (s *redisSubscription) Close() error {
	err := s.ps.Close()
	<-s.done
	return err
}

func (s *RedisStore) RegisterNode(ctx context.Context, node domain.ClusterNode, ttl time.Duration) error {
	if strings.TrimSpace(node.InstanceID) == "" {
		node.InstanceID = s.instanceID
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	raw, err := json.Marshal(node)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.key("cluster:node:"+node.InstanceID), raw, ttl).Err()
}

func (s *RedisStore) UnregisterNode(ctx context.Context, instanceID string) error {
	if strings.TrimSpace(instanceID) == "" {
		instanceID = s.instanceID
	}
	return s.client.Unlink(ctx, s.key("cluster:node:"+instanceID)).Err()
}

func (s *RedisStore) ListNodes(ctx context.Context) ([]domain.ClusterNode, error) {
	keys, err := s.scanKeys(ctx, s.key("cluster:node:*"))
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return []domain.ClusterNode{}, nil
	}

	pipe := s.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(keys))
	for i, k := range keys {
		cmds[i] = pipe.Get(ctx, k)
	}
	_, _ = pipe.Exec(ctx)

	nodes := make([]domain.ClusterNode, 0, len(keys))
	for _, cmd := range cmds {
		raw, gErr := cmd.Bytes()
		if gErr != nil {
			continue
		}
		var n domain.ClusterNode
		if json.Unmarshal(raw, &n) == nil && n.InstanceID != "" {
			nodes = append(nodes, n)
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].StartedAt.Equal(nodes[j].StartedAt) {
			return nodes[i].InstanceID < nodes[j].InstanceID
		}
		return nodes[i].StartedAt.Before(nodes[j].StartedAt)
	})
	return nodes, nil
}

func (s *RedisStore) Stats() domain.CacheStats {
	hits := s.hits.Load()
	misses := s.misses.Load()
	return domain.CacheStats{
		Driver:         s.driver,
		ClusterEnabled: true,
		Hits:           hits,
		Misses:         misses,
		Sets:           s.sets.Load(),
		Deletes:        s.deletes.Load(),
		Errors:         s.errors.Load(),
		HitRate:        computeHitRate(hits, misses),
	}
}

func (s *RedisStore) scanKeys(ctx context.Context, pattern string) ([]string, error) {
	var all []string
	scanNode := func(c *redis.Client) error {
		var cursor uint64
		for {
			keys, next, err := c.Scan(ctx, cursor, pattern, 200).Result()
			if err != nil {
				return err
			}
			all = append(all, keys...)
			cursor = next
			if cursor == 0 {
				return nil
			}
		}
	}

	if cc, ok := s.client.(*redis.ClusterClient); ok {
		err := cc.ForEachMaster(ctx, func(_ context.Context, shard *redis.Client) error {
			return scanNode(shard)
		})
		return all, err
	}

	var cursor uint64
	for {
		keys, next, err := s.client.Scan(ctx, cursor, pattern, 200).Result()
		if err != nil {
			return nil, err
		}
		all = append(all, keys...)
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return all, nil
}
