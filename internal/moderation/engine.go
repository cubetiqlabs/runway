package moderation

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
)

type JobContext struct {
	InstallationID      *int64
	RepoFullName        string
	HeadBranch          string
	Actor               string
	Labels              []string
	IsForkPR            bool
	ActiveRepoJobCount  int
}

type Decision struct {
	Action     domain.PolicyAction `json:"action"`
	PolicyID   *uuid.UUID          `json:"policy_id,omitempty"`
	PolicyName string              `json:"policy_name"`
	Reason     string              `json:"reason"`
}

// Engine evaluates moderation policies deterministically in priority order.
type Engine struct {
	defaultAction domain.PolicyAction
}

func NewEngine(defaultAction string) *Engine {
	act := domain.PolicyAction(defaultAction)
	switch act {
	case domain.ActionAllow, domain.ActionDeny, domain.ActionRequireApproval:
	default:
		act = domain.ActionAllow
	}
	return &Engine{defaultAction: act}
}

// Evaluate checks enabled policies sorted by Priority ASC, CreatedAt ASC.
// The first policy whose selectors match determines the decision.
func (e *Engine) Evaluate(policies []domain.ModerationPolicy, job JobContext) Decision {
	ordered := make([]domain.ModerationPolicy, 0, len(policies))
	for _, p := range policies {
		if !p.Enabled {
			continue
		}
		if p.InstallationID != nil && job.InstallationID != nil && *p.InstallationID != *job.InstallationID {
			continue
		}
		if p.InstallationID != nil && job.InstallationID == nil {
			continue
		}
		ordered = append(ordered, p)
	}

	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Priority != ordered[j].Priority {
			return ordered[i].Priority < ordered[j].Priority
		}
		return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
	})

	for _, p := range ordered {
		if !matchPatterns(p.RepoPatterns, job.RepoFullName) {
			continue
		}
		if !matchPatterns(p.BranchPatterns, job.HeadBranch) {
			continue
		}
		if !matchPatterns(p.ActorPatterns, job.Actor) {
			continue
		}
		if !matchAnyLabel(p.LabelPatterns, job.Labels) {
			continue
		}

		// Policy matched selectors; evaluate safety/quota guards.
		if p.BlockForkPRs && job.IsForkPR {
			id := p.ID
			return Decision{
				Action:     domain.ActionDeny,
				PolicyID:   &id,
				PolicyName: p.Name,
				Reason:     fmt.Sprintf("Policy %q blocks pull request runs from forks", p.Name),
			}
		}

		if p.MaxConcurrentPerRepo != nil && job.ActiveRepoJobCount >= *p.MaxConcurrentPerRepo {
			id := p.ID
			return Decision{
				Action:     domain.ActionRequireApproval,
				PolicyID:   &id,
				PolicyName: p.Name,
				Reason:     fmt.Sprintf("Repository %s reached concurrency cap (%d/%d) under policy %q", job.RepoFullName, job.ActiveRepoJobCount, *p.MaxConcurrentPerRepo, p.Name),
			}
		}

		id := p.ID
		return Decision{
			Action:     p.Action,
			PolicyID:   &id,
			PolicyName: p.Name,
			Reason:     fmt.Sprintf("Matched policy %q (priority %d)", p.Name, p.Priority),
		}
	}

	return Decision{
		Action:     e.defaultAction,
		PolicyName: "default",
		Reason:     fmt.Sprintf("No custom policy matched; applied default action %q", e.defaultAction),
	}
}

// MatchPool selects the best enabled RunnerPool whose advertised labels satisfy all of the job's requested labels.
// GitHub sends only the exact `runs-on` labels in workflow_job.labels, so every requested job label
// must be present in the pool's label set (pool.Labels plus slug, os, arch, and "self-hosted").
func MatchPool(pools []domain.RunnerPool, installationID *int64, jobLabels []string) *domain.RunnerPool {
	normJobLabels := make([]string, 0, len(jobLabels))
	seenJob := make(map[string]struct{}, len(jobLabels))
	for _, raw := range jobLabels {
		l := strings.ToLower(strings.TrimSpace(raw))
		if l == "" {
			continue
		}
		if _, exists := seenJob[l]; !exists {
			seenJob[l] = struct{}{}
			normJobLabels = append(normJobLabels, l)
		}
	}
	if len(normJobLabels) == 0 {
		return nil
	}

	var best *domain.RunnerPool
	bestExtra := 0
	bestScoped := false
	bestSlugHit := false

	for i := range pools {
		p := &pools[i]
		if !p.Enabled {
			continue
		}
		if p.InstallationID != nil {
			if installationID == nil || *p.InstallationID != *installationID {
				continue
			}
		}

		poolSet := poolLabelSet(p)
		if len(poolSet) == 0 {
			continue
		}

		matchedAll := true
		for _, jl := range normJobLabels {
			if _, ok := poolSet[jl]; !ok {
				matchedAll = false
				break
			}
		}
		if !matchedAll {
			continue
		}

		scoped := p.InstallationID != nil
		slugLower := strings.ToLower(strings.TrimSpace(p.Slug))
		_, slugHit := seenJob[slugLower]
		if slugLower == "" {
			slugHit = false
		}
		extra := len(poolSet) - len(normJobLabels)
		if extra < 0 {
			extra = 0
		}

		if best == nil || isBetterPoolMatch(p, scoped, slugHit, extra, best, bestScoped, bestSlugHit, bestExtra) {
			best = p
			bestScoped = scoped
			bestSlugHit = slugHit
			bestExtra = extra
		}
	}
	return best
}

func poolLabelSet(p *domain.RunnerPool) map[string]struct{} {
	set := make(map[string]struct{}, len(p.Labels)+4)
	for _, raw := range p.Labels {
		if l := strings.ToLower(strings.TrimSpace(raw)); l != "" {
			set[l] = struct{}{}
		}
	}
	for _, extra := range []string{p.Slug, p.OS, p.Arch, "self-hosted"} {
		if l := strings.ToLower(strings.TrimSpace(extra)); l != "" {
			set[l] = struct{}{}
		}
	}
	return set
}

func isBetterPoolMatch(
	cand *domain.RunnerPool, candScoped, candSlugHit bool, candExtra int,
	best *domain.RunnerPool, bestScoped, bestSlugHit bool, bestExtra int,
) bool {
	if candScoped != bestScoped {
		return candScoped
	}
	if candSlugHit != bestSlugHit {
		return candSlugHit
	}
	if candExtra != bestExtra {
		return candExtra < bestExtra
	}
	if cand.VCPU != best.VCPU {
		return cand.VCPU < best.VCPU
	}
	return strings.ToLower(cand.Slug) < strings.ToLower(best.Slug)
}

func matchPatterns(patterns []string, value string) bool {
	if len(patterns) == 0 {
		return true
	}
	val := strings.ToLower(strings.TrimSpace(value))
	for _, raw := range patterns {
		pat := strings.ToLower(strings.TrimSpace(raw))
		if pat == "" || pat == "*" {
			return true
		}
		if matched, err := path.Match(pat, val); err == nil && matched {
			return true
		}
		if strings.HasSuffix(pat, "/*") {
			prefix := strings.TrimSuffix(pat, "*")
			if strings.HasPrefix(val, prefix) {
				return true
			}
		}
	}
	return false
}

func matchAnyLabel(patterns []string, labels []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, label := range labels {
		if matchPatterns(patterns, label) {
			return true
		}
	}
	return false
}
