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
		{ID: uuid.New(), Name: "small", Slug: "small", Labels: []string{"self-hosted", "linux"}, Enabled: true},
		{ID: uuid.New(), Name: "large", Slug: "large", Labels: []string{"self-hosted", "linux", "runway-8vcpu"}, Enabled: true},
	}
	matched := MatchPool(pools, nil, []string{"self-hosted", "linux", "runway-8vcpu"})
	if matched == nil || matched.Name != "large" {
		t.Fatalf("expected pool 'large', got %+v", matched)
	}
}

func TestMatchPoolGitHubSemantics(t *testing.T) {
	inst1 := int64(101)
	inst2 := int64(202)
	pools := []domain.RunnerPool{
		{
			ID:      uuid.New(),
			Name:    "Ubuntu 4vCPU",
			Slug:    "runway-ubuntu-4vcpu",
			Labels:  []string{"self-hosted", "linux", "x64", "runway-ubuntu-4vcpu"},
			OS:      "linux",
			Arch:    "x64",
			VCPU:    4,
			Enabled: true,
		},
		{
			ID:      uuid.New(),
			Name:    "Ubuntu 8vCPU",
			Slug:    "runway-ubuntu-8vcpu",
			Labels:  []string{"self-hosted", "linux", "x64", "runway-ubuntu-8vcpu"},
			OS:      "linux",
			Arch:    "x64",
			VCPU:    8,
			Enabled: true,
		},
		{
			ID:      uuid.New(),
			Name:    "Disabled GPU",
			Slug:    "runway-gpu",
			Labels:  []string{"self-hosted", "linux", "x64", "gpu"},
			OS:      "linux",
			Arch:    "x64",
			VCPU:    8,
			Enabled: false,
		},
		{
			ID:             uuid.New(),
			InstallationID: &inst1,
			Name:           "Org Scoped 4vCPU",
			Slug:           "runway-org-4vcpu",
			Labels:         []string{"self-hosted", "linux", "x64", "runway-ubuntu-4vcpu"},
			OS:             "linux",
			Arch:           "x64",
			VCPU:           4,
			Enabled:        true,
		},
	}

	// Single label runs-on: runway-ubuntu-4vcpu
	if got := MatchPool(pools, nil, []string{"runway-ubuntu-4vcpu"}); got == nil || got.Slug != "runway-ubuntu-4vcpu" {
		t.Fatalf("single label: expected runway-ubuntu-4vcpu, got %+v", got)
	}

	// Pair runs-on: [self-hosted, runway-ubuntu-8vcpu]
	if got := MatchPool(pools, nil, []string{"self-hosted", "runway-ubuntu-8vcpu"}); got == nil || got.Slug != "runway-ubuntu-8vcpu" {
		t.Fatalf("pair label: expected runway-ubuntu-8vcpu, got %+v", got)
	}

	// Generic runs-on: self-hosted prefers smallest VCPU pool
	if got := MatchPool(pools, nil, []string{"self-hosted"}); got == nil || got.Slug != "runway-ubuntu-4vcpu" {
		t.Fatalf("generic self-hosted: expected smallest vcpu pool runway-ubuntu-4vcpu, got %+v", got)
	}
	if got := MatchPool(pools, nil, []string{"self-hosted", "linux"}); got == nil || got.Slug != "runway-ubuntu-4vcpu" {
		t.Fatalf("generic self-hosted linux: expected runway-ubuntu-4vcpu, got %+v", got)
	}

	// Case-insensitive and whitespace normalization
	if got := MatchPool(pools, nil, []string{" Runway-Ubuntu-4vCPU "}); got == nil || got.Slug != "runway-ubuntu-4vcpu" {
		t.Fatalf("case/whitespace: expected runway-ubuntu-4vcpu, got %+v", got)
	}

	// GitHub-hosted runners must not match
	for _, labels := range [][]string{{"ubuntu-latest"}, {"macos-latest"}, {"self-hosted", "gpu"}, {}} {
		if got := MatchPool(pools, &inst1, labels); got != nil {
			t.Fatalf("labels %v: expected nil, got %+v", labels, got)
		}
	}

	// Installation-scoped pool skipped for nil or mismatched installation, preferred for matching installation
	if got := MatchPool(pools, &inst2, []string{"runway-ubuntu-4vcpu"}); got == nil || got.Slug != "runway-ubuntu-4vcpu" {
		t.Fatalf("mismatched installation: expected global runway-ubuntu-4vcpu, got %+v", got)
	}
	if got := MatchPool(pools, &inst1, []string{"runway-ubuntu-4vcpu"}); got == nil || got.Slug != "runway-org-4vcpu" {
		t.Fatalf("matching installation: expected runway-org-4vcpu, got %+v", got)
	}
}
