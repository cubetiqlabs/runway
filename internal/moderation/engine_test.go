package moderation

import (
	"testing"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
)

func TestEngineEvaluatePriorityAndForkPR(t *testing.T) {
	eng := NewEngine("allow")
	now := time.Now()
	pHigh := domain.ModerationPolicy{
		ID:           uuid.New(),
		Name:         "Block Fork PRs",
		Priority:     10,
		Enabled:      true,
		Action:       domain.ActionAllow,
		RepoPatterns: []string{"cubetiqlabs/*"},
		BlockForkPRs: true,
		CreatedAt:    now,
	}
	pLow := domain.ModerationPolicy{
		ID:           uuid.New(),
		Name:         "Default Allow Org",
		Priority:     100,
		Enabled:      true,
		Action:       domain.ActionAllow,
		RepoPatterns: []string{"cubetiqlabs/*"},
		CreatedAt:    now.Add(time.Second),
	}

	decFork := eng.Evaluate([]domain.ModerationPolicy{pLow, pHigh}, JobContext{
		RepoFullName: "cubetiqlabs/api",
		HeadBranch:   "feat/fork",
		Actor:        "external-contributor",
		Labels:       []string{"runway-ubuntu-4vcpu"},
		IsForkPR:     true,
	})
	if decFork.Action != domain.ActionDeny {
		t.Fatalf("expected deny for fork PR, got %s", decFork.Action)
	}

	decTrusted := eng.Evaluate([]domain.ModerationPolicy{pLow, pHigh}, JobContext{
		RepoFullName: "cubetiqlabs/api",
		HeadBranch:   "main",
		Actor:        "octocat",
		Labels:       []string{"runway-ubuntu-4vcpu"},
		IsForkPR:     false,
	})
	if decTrusted.Action != domain.ActionAllow {
		t.Fatalf("expected allow for trusted branch, got %s", decTrusted.Action)
	}
}

func TestEngineConcurrencyCap(t *testing.T) {
	eng := NewEngine("allow")
	capVal := 2
	p := domain.ModerationPolicy{
		ID:                   uuid.New(),
		Name:                 "Cap Repo Concurrency",
		Priority:             20,
		Enabled:              true,
		Action:               domain.ActionAllow,
		MaxConcurrentPerRepo: &capVal,
	}
	dec := eng.Evaluate([]domain.ModerationPolicy{p}, JobContext{
		RepoFullName:       "cubetiq/web",
		ActiveRepoJobCount: 2,
	})
	if dec.Action != domain.ActionRequireApproval {
		t.Fatalf("expected require_approval on concurrency cap, got %s", dec.Action)
	}
}

func TestMatchPoolSpecificity(t *testing.T) {
	pools := []domain.RunnerPool{
		{ID: uuid.New(), Name: "small", Labels: []string{"self-hosted", "linux"}, Enabled: true},
		{ID: uuid.New(), Name: "large", Labels: []string{"self-hosted", "linux", "runway-8vcpu"}, Enabled: true},
	}
	matched := MatchPool(pools, nil, []string{"self-hosted", "linux", "runway-8vcpu"})
	if matched == nil || matched.Name != "large" {
		t.Fatalf("expected pool 'large', got %+v", matched)
	}
}
