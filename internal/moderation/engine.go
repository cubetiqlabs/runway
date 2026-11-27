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

// MatchPool selects the first enabled RunnerPool whose labels are a subset of the job's requested labels
// (ignoring generic 'self-hosted' if pool defines more specific labels, or matching all pool labels).
func MatchPool(pools []domain.RunnerPool, installationID *int64, jobLabels []string) *domain.RunnerPool {
	if len(jobLabels) == 0 {
		return nil
	}
	labelSet := make(map[string]struct{}, len(jobLabels))
	for _, l := range jobLabels {
		labelSet[strings.ToLower(strings.TrimSpace(l))] = struct{}{}
	}

	var best *domain.RunnerPool
	bestScore := -1
	for i := range pools {
		p := &pools[i]
		if !p.Enabled || len(p.Labels) == 0 {
			continue
		}
		if p.InstallationID != nil && installationID != nil && *p.InstallationID != *installationID {
			continue
		}
		matchedAll := true
		for _, pl := range p.Labels {
			if _, ok := labelSet[strings.ToLower(strings.TrimSpace(pl))]; !ok {
				matchedAll = false
				break
			}
		}
		if matchedAll && len(p.Labels) > bestScore {
			bestScore = len(p.Labels)
			best = p
		}
	}
	return best
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
