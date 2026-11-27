package cache

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
)

func TestMemoryStoreKVExpiryAndPrefixDelete(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore("test:", "node-1")
	defer store.Close()

	if err := SetJSON(ctx, store, "pools:enabled", []string{"p1", "p2"}, 100*time.Millisecond); err != nil {
		t.Fatalf("SetJSON failed: %v", err)
	}
	got, ok, err := GetJSON[[]string](ctx, store, "pools:enabled")
	if err != nil || !ok || len(got) != 2 {
		t.Fatalf("expected cache hit with 2 items, got %v ok=%v err=%v", got, ok, err)
	}

	time.Sleep(130 * time.Millisecond)
	_, ok, err = GetJSON[[]string](ctx, store, "pools:enabled")
	if err != nil || ok {
		t.Fatalf("expected expired key miss, got ok=%v err=%v", ok, err)
	}

	_ = store.Set(ctx, "agent:detail:a:1h", []byte("1"), time.Minute)
	_ = store.Set(ctx, "agent:detail:a:24h", []byte("2"), time.Minute)
	_ = store.Set(ctx, "agent:detail:b:1h", []byte("3"), time.Minute)

	if err := store.DeletePrefix(ctx, "agent:detail:a:"); err != nil {
		t.Fatalf("DeletePrefix failed: %v", err)
	}
	if _, ok, _ := store.Get(ctx, "agent:detail:a:1h"); ok {
		t.Fatalf("expected prefixed key to be deleted")
	}
	if _, ok, _ := store.Get(ctx, "agent:detail:b:1h"); !ok {
		t.Fatalf("expected unrelated key to remain")
	}
}

func TestGetOrLoadSingleflight(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore("sf:", "node-1")
	defer store.Close()

	var loads atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := GetOrLoad(ctx, store, "overview", time.Second, func(context.Context) (int, error) {
				time.Sleep(25 * time.Millisecond)
				return int(loads.Add(1)), nil
			})
			if err != nil || v != 1 {
				t.Errorf("expected singleflight loaded value 1, got %d err=%v", v, err)
			}
		}()
	}
	wg.Wait()

	if loads.Load() != 1 {
		t.Fatalf("expected loader to execute exactly once, ran %d times", loads.Load())
	}
}

func TestDistributedLockAndSetNX(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore("lock:", "node-1")
	defer store.Close()

	ok, err := store.SetNX(ctx, "webhook:dedup:d1", []byte("1"), time.Minute)
	if err != nil || !ok {
		t.Fatalf("first SetNX should succeed, ok=%v err=%v", ok, err)
	}
	ok, err = store.SetNX(ctx, "webhook:dedup:d1", []byte("1"), time.Minute)
	if err != nil || ok {
		t.Fatalf("second SetNX should return false, ok=%v err=%v", ok, err)
	}

	lock1, acquired, err := store.AcquireLock(ctx, "reconcile", 200*time.Millisecond)
	if err != nil || !acquired || lock1 == nil {
		t.Fatalf("expected lock1 to acquire, got acquired=%v err=%v", acquired, err)
	}
	if _, acquired2, _ := store.AcquireLock(ctx, "reconcile", 200*time.Millisecond); acquired2 {
		t.Fatalf("expected lock2 contention while lock1 is held")
	}

	if extended, err := lock1.Extend(ctx, time.Second); err != nil || !extended {
		t.Fatalf("expected lock extend to succeed, extended=%v err=%v", extended, err)
	}
	if err := lock1.Release(ctx); err != nil {
		t.Fatalf("release failed: %v", err)
	}

	lock3, acquired3, err := AcquireLockWithWait(ctx, store, "reconcile", time.Second, 200*time.Millisecond)
	if err != nil || !acquired3 || lock3 == nil {
		t.Fatalf("expected lock3 to acquire after release, got acquired=%v err=%v", acquired3, err)
	}
	_ = lock3.Release(ctx)
}

func TestPubSubAndClusterNodeRegistry(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore("ps:", "node-a")
	defer store.Close()

	sub, err := store.Subscribe(ctx, "cluster:slot_freed")
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	defer sub.Close()

	if err := store.Publish(ctx, "cluster:slot_freed", "node-b"); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	select {
	case msg := <-sub.Channel():
		if msg != "node-b" {
			t.Fatalf("expected node-b payload, got %q", msg)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for pubsub message")
	}

	now := time.Now().UTC()
	_ = store.RegisterNode(ctx, domain.ClusterNode{
		InstanceID: "node-a",
		Hostname:   "api-1",
		Version:    "1.0.0",
		Address:    "0.0.0.0:8080",
		StartedAt:  now.Add(-time.Minute),
		LastSeenAt: now,
	}, time.Minute)
	_ = store.RegisterNode(ctx, domain.ClusterNode{
		InstanceID: "node-b",
		Hostname:   "api-2",
		Version:    "1.0.0",
		Address:    "0.0.0.0:8080",
		StartedAt:  now,
		LastSeenAt: now,
	}, time.Minute)

	nodes, err := store.ListNodes(ctx)
	if err != nil || len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d err=%v", len(nodes), err)
	}
	if nodes[0].InstanceID != "node-a" {
		t.Fatalf("expected node-a first, got %s", nodes[0].InstanceID)
	}

	_ = store.UnregisterNode(ctx, "node-a")
	nodes, _ = store.ListNodes(ctx)
	if len(nodes) != 1 || nodes[0].InstanceID != "node-b" {
		t.Fatalf("expected only node-b remaining, got %+v", nodes)
	}
}

func TestDisabledStoreStats(t *testing.T) {
	ctx := context.Background()
	store := NewDisabledStore("disabled-node")
	defer store.Close()

	_ = store.Set(ctx, "k", []byte("v"), time.Minute)
	if _, ok, _ := store.Get(ctx, "k"); ok {
		t.Fatalf("disabled store should always miss")
	}
	st := store.Stats()
	if st.Driver != "disabled" || st.ClusterEnabled {
		t.Fatalf("unexpected disabled stats: %+v", st)
	}
}
