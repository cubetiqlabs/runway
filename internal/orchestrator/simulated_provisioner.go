package orchestrator

import (
	"context"
	"encoding/json"
	"sync"
)

// SimulatedProvisioner provides an in-memory ephemeral runner provisioner for dev, testing, and environments without Docker socket access.
type SimulatedProvisioner struct {
	mu     sync.RWMutex
	active map[string]bool
}

func NewSimulatedProvisioner() *SimulatedProvisioner {
	return &SimulatedProvisioner{active: make(map[string]bool)}
}

func (s *SimulatedProvisioner) Name() string { return "simulated" }

func (s *SimulatedProvisioner) Ping(context.Context) error { return nil }

func (s *SimulatedProvisioner) Provision(_ context.Context, req ProvisionRequest) (*ProvisionResult, error) {
	ref := "sim-" + req.RunnerID.String()[:12]
	s.mu.Lock()
	s.active[ref] = true
	s.mu.Unlock()

	hostInfo, _ := json.Marshal(map[string]any{
		"provider":  "simulated",
		"vcpu":      req.Pool.VCPU,
		"memory_mb": req.Pool.MemoryMB,
		"disk_gb":   req.Pool.DiskGB,
		"os":        req.Pool.OS,
		"arch":      req.Pool.Arch,
	})
	return &ProvisionResult{ProviderRef: ref, HostInfo: hostInfo}, nil
}

func (s *SimulatedProvisioner) Terminate(_ context.Context, providerRef string) error {
	s.mu.Lock()
	delete(s.active, providerRef)
	s.mu.Unlock()
	return nil
}

func (s *SimulatedProvisioner) Healthy(_ context.Context, providerRef string) (bool, error) {
	s.mu.RLock()
	ok := s.active[providerRef]
	s.mu.RUnlock()
	return ok, nil
}
