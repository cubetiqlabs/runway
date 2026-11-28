package cache

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
)

type memItem struct {
	val       []byte
	expiresAt time.Time // zero means no expiry
}

type memLockEntry struct {
	token     string
	expiresAt time.Time
}

// MemoryStore is an in-process Store used in standalone/dev mode and tests.
type MemoryStore struct {
	driver         string
	disabledKV     bool
	clusterEnabled atomic.Bool
	prefix         string
	instanceID     string

	mu    sync.RWMutex
	items map[string]memItem

	lockMu sync.Mutex
	locks  map[string]memLockEntry

	subMu sync.RWMutex
	subs  map[string]map[*memSubscription]struct{}

	nodeMu sync.RWMutex
	nodes  map[string]memNodeEntry

	hits    atomic.Uint64
	misses  atomic.Uint64
	sets    atomic.Uint64
	deletes atomic.Uint64
	errors  atomic.Uint64
}

type memNodeEntry struct {
	node      domain.ClusterNode
	expiresAt time.Time
}

// NewMemoryStore constructs an in-process cache store with TTL support.
func NewMemoryStore(prefix, instanceID string) *MemoryStore {
	if strings.TrimSpace(prefix) == "" {
		prefix = "runway:"
	}
	if !strings.HasSuffix(prefix, ":") {
		prefix += ":"
	}
	if strings.TrimSpace(instanceID) == "" {
		instanceID = resolveInstanceID("")
	}
	return &MemoryStore{
		driver:     "memory",
		prefix:     prefix,
		instanceID: instanceID,
		items:      make(map[string]memItem),
		locks:      make(map[string]memLockEntry),
		subs:       make(map[string]map[*memSubscription]struct{}),
		nodes:      make(map[string]memNodeEntry),
	}
}

// NewDisabledStore constructs a no-op KV cache store while preserving locks and node status.
func NewDisabledStore(instanceID string) *MemoryStore {
	m := NewMemoryStore("runway:", instanceID)
	m.driver = "disabled"
	m.disabledKV = true
	return m
}

// SetClusterEnabled toggles cluster mode flag (intended for multi-replica unit tests).
func (m *MemoryStore) SetClusterEnabled(enabled bool) {
	m.clusterEnabled.Store(enabled)
}

func (m *MemoryStore) key(k string) string {
	return m.prefix + strings.TrimPrefix(k, m.prefix)
}

func (m *MemoryStore) Driver() string       { return m.driver }
func (m *MemoryStore) ClusterEnabled() bool { return m.clusterEnabled.Load() }
func (m *MemoryStore) InstanceID() string   { return m.instanceID }
func (m *MemoryStore) Ping(context.Context) error {
	return nil
}

func (m *MemoryStore) Close() error {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	for _, set := range m.subs {
		for sub := range set {
			sub.closeOnce()
		}
	}
	m.subs = make(map[string]map[*memSubscription]struct{})
	return nil
}

func (m *MemoryStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	if m.disabledKV || strings.TrimSpace(key) == "" {
		m.misses.Add(1)
		return nil, false, nil
	}
	full := m.key(key)
	now := time.Now()

	m.mu.RLock()
	it, ok := m.items[full]
	m.mu.RUnlock()
	if !ok {
		m.misses.Add(1)
		return nil, false, nil
	}
	if !it.expiresAt.IsZero() && now.After(it.expiresAt) {
		m.mu.Lock()
		if cur, exists := m.items[full]; exists && !cur.expiresAt.IsZero() && time.Now().After(cur.expiresAt) {
			delete(m.items, full)
		}
		m.mu.Unlock()
		m.misses.Add(1)
		return nil, false, nil
	}
	m.hits.Add(1)
	cp := make([]byte, len(it.val))
	copy(cp, it.val)
	return cp, true, nil
}

func (m *MemoryStore) Set(_ context.Context, key string, val []byte, ttl time.Duration) error {
	if m.disabledKV || strings.TrimSpace(key) == "" {
		return nil
	}
	full := m.key(key)
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	cp := make([]byte, len(val))
	copy(cp, val)
	m.mu.Lock()
	m.items[full] = memItem{val: cp, expiresAt: exp}
	m.mu.Unlock()
	m.sets.Add(1)
	return nil
}

func (m *MemoryStore) Delete(_ context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	m.mu.Lock()
	var n uint64
	for _, k := range keys {
		if strings.TrimSpace(k) == "" {
			continue
		}
		delete(m.items, m.key(k))
		n++
	}
	m.mu.Unlock()
	if n > 0 {
		m.deletes.Add(n)
	}
	return nil
}

func (m *MemoryStore) DeletePrefix(_ context.Context, prefix string) error {
	fullPrefix := m.key(prefix)
	m.mu.Lock()
	var n uint64
	for k := range m.items {
		if strings.HasPrefix(k, fullPrefix) {
			delete(m.items, k)
			n++
		}
	}
	m.mu.Unlock()
	if n > 0 {
		m.deletes.Add(n)
	}
	return nil
}

func (m *MemoryStore) SetNX(_ context.Context, key string, val []byte, ttl time.Duration) (bool, error) {
	if strings.TrimSpace(key) == "" {
		return false, nil
	}
	full := m.key(key)
	now := time.Now()
	var exp time.Time
	if ttl > 0 {
		exp = now.Add(ttl)
	}
	cp := make([]byte, len(val))
	copy(cp, val)

	m.mu.Lock()
	defer m.mu.Unlock()
	if cur, exists := m.items[full]; exists {
		if cur.expiresAt.IsZero() || now.Before(cur.expiresAt) {
			return false, nil
		}
	}
	m.items[full] = memItem{val: cp, expiresAt: exp}
	m.sets.Add(1)
	return true, nil
}

func (m *MemoryStore) AcquireLock(_ context.Context, name string, ttl time.Duration) (Lock, bool, error) {
	if ttl <= 0 {
		ttl = 10 * time.Second
	}
	full := m.key("lock:" + name)
	now := time.Now()
	token := uuid.NewString()

	m.lockMu.Lock()
	defer m.lockMu.Unlock()
	if cur, exists := m.locks[full]; exists && now.Before(cur.expiresAt) {
		return nil, false, nil
	}
	m.locks[full] = memLockEntry{
		token:     token,
		expiresAt: now.Add(ttl),
	}
	return &memLock{store: m, key: full, token: token}, true, nil
}

type memLock struct {
	store *MemoryStore
	key   string
	token string
}

func (l *memLock) Extend(_ context.Context, ttl time.Duration) (bool, error) {
	l.store.lockMu.Lock()
	defer l.store.lockMu.Unlock()
	cur, ok := l.store.locks[l.key]
	if !ok || cur.token != l.token || time.Now().After(cur.expiresAt) {
		return false, nil
	}
	cur.expiresAt = time.Now().Add(ttl)
	l.store.locks[l.key] = cur
	return true, nil
}

func (l *memLock) Release(_ context.Context) error {
	l.store.lockMu.Lock()
	defer l.store.lockMu.Unlock()
	if cur, ok := l.store.locks[l.key]; ok && cur.token == l.token {
		delete(l.store.locks, l.key)
	}
	return nil
}

func (m *MemoryStore) Publish(_ context.Context, channel string, payload string) error {
	if strings.TrimSpace(channel) == "" {
		return nil
	}
	full := m.key("pubsub:" + channel)
	m.subMu.RLock()
	targets := make([]*memSubscription, 0, len(m.subs[full]))
	for sub := range m.subs[full] {
		targets = append(targets, sub)
	}
	m.subMu.RUnlock()

	for _, sub := range targets {
		sub.send(payload)
	}
	return nil
}

func (m *MemoryStore) Subscribe(_ context.Context, channels ...string) (Subscription, error) {
	fullChs := make([]string, 0, len(channels))
	for _, ch := range channels {
		if strings.TrimSpace(ch) != "" {
			fullChs = append(fullChs, m.key("pubsub:"+ch))
		}
	}
	sub := &memSubscription{
		store:    m,
		channels: fullChs,
		ch:       make(chan string, 64),
	}
	m.subMu.Lock()
	for _, c := range fullChs {
		if m.subs[c] == nil {
			m.subs[c] = make(map[*memSubscription]struct{})
		}
		m.subs[c][sub] = struct{}{}
	}
	m.subMu.Unlock()
	return sub, nil
}

type memSubscription struct {
	store    *MemoryStore
	channels []string
	ch       chan string
	mu       sync.Mutex
	closed   bool
}

func (s *memSubscription) Channel() <-chan string { return s.ch }

func (s *memSubscription) send(payload string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- payload:
	default:
	}
}

func (s *memSubscription) closeOnce() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
	s.mu.Unlock()
}

func (s *memSubscription) Close() error {
	s.store.subMu.Lock()
	for _, c := range s.channels {
		if set := s.store.subs[c]; set != nil {
			delete(set, s)
			if len(set) == 0 {
				delete(s.store.subs, c)
			}
		}
	}
	s.store.subMu.Unlock()
	s.closeOnce()
	return nil
}

func (m *MemoryStore) RegisterNode(_ context.Context, node domain.ClusterNode, ttl time.Duration) error {
	if strings.TrimSpace(node.InstanceID) == "" {
		node.InstanceID = m.instanceID
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	raw, _ := json.Marshal(node)
	var copied domain.ClusterNode
	_ = json.Unmarshal(raw, &copied)

	m.nodeMu.Lock()
	m.nodes[node.InstanceID] = memNodeEntry{
		node:      copied,
		expiresAt: time.Now().Add(ttl),
	}
	m.nodeMu.Unlock()
	return nil
}

func (m *MemoryStore) UnregisterNode(_ context.Context, instanceID string) error {
	if strings.TrimSpace(instanceID) == "" {
		instanceID = m.instanceID
	}
	m.nodeMu.Lock()
	delete(m.nodes, instanceID)
	m.nodeMu.Unlock()
	return nil
}

func (m *MemoryStore) ListNodes(_ context.Context) ([]domain.ClusterNode, error) {
	now := time.Now()
	m.nodeMu.Lock()
	out := make([]domain.ClusterNode, 0, len(m.nodes))
	for id, entry := range m.nodes {
		if now.After(entry.expiresAt) {
			delete(m.nodes, id)
			continue
		}
		out = append(out, entry.node)
	}
	m.nodeMu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].InstanceID < out[j].InstanceID
		}
		return out[i].StartedAt.Before(out[j].StartedAt)
	})
	return out, nil
}

func (m *MemoryStore) Stats() domain.CacheStats {
	hits := m.hits.Load()
	misses := m.misses.Load()
	return domain.CacheStats{
		Driver:         m.driver,
		ClusterEnabled: m.clusterEnabled.Load(),
		Hits:           hits,
		Misses:         misses,
		Sets:           m.sets.Load(),
		Deletes:        m.deletes.Load(),
		Errors:         m.errors.Load(),
		HitRate:        computeHitRate(hits, misses),
	}
}
