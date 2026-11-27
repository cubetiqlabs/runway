package repository

import (
	"context"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type JobRepo struct {
	db *gorm.DB
}

func NewJobRepo(db *gorm.DB) *JobRepo {
	return &JobRepo{db: db}
}

const jobSelectCols = `
	j.id, j.github_job_id, j.github_run_id, j.installation_id, j.repo_full_name,
	j.workflow_name, j.job_name, j.head_branch, j.head_sha, j.actor, j.is_fork_pr,
	j.labels, j.status, j.conclusion, j.matched_pool_id, COALESCE(p.name, '') AS matched_pool_name,
	j.matched_policy_id, COALESCE(mp.name, '') AS matched_policy_name, j.moderation_decision, j.moderation_reason,
	j.moderated_by, j.assigned_runner_id, COALESCE(rn.name, '') AS assigned_runner_name,
	j.queued_at, j.started_at, j.completed_at, j.created_at, j.updated_at
`

func (r *JobRepo) jobBaseQuery(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).
		Table("workflow_jobs j").
		Select(jobSelectCols).
		Joins("LEFT JOIN runner_pools p ON p.id = j.matched_pool_id").
		Joins("LEFT JOIN moderation_policies mp ON mp.id = j.matched_policy_id").
		Joins("LEFT JOIN runners rn ON rn.id = j.assigned_runner_id")
}

func (r *JobRepo) UpsertFromWebhook(ctx context.Context, j *domain.WorkflowJob) error {
	m := WorkflowJobModel{
		GitHubJobID:        j.GitHubJobID,
		GitHubRunID:        j.GitHubRunID,
		InstallationID:     j.InstallationID,
		RepoFullName:       j.RepoFullName,
		WorkflowName:       j.WorkflowName,
		JobName:            j.JobName,
		HeadBranch:         j.HeadBranch,
		HeadSHA:            j.HeadSHA,
		Actor:              j.Actor,
		IsForkPR:           j.IsForkPR,
		Labels:             pq.StringArray(nonNilSlice(j.Labels)),
		Status:             string(j.Status),
		Conclusion:         j.Conclusion,
		MatchedPoolID:      j.MatchedPoolID,
		MatchedPolicyID:    j.MatchedPolicyID,
		ModerationDecision: j.ModerationDecision,
		ModerationReason:   j.ModerationReason,
		AssignedRunnerID:   j.AssignedRunnerID,
		QueuedAt:           j.QueuedAt,
		StartedAt:          j.StartedAt,
		CompletedAt:        j.CompletedAt,
	}

	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "github_job_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"workflow_name":       gorm.Expr("CASE WHEN EXCLUDED.workflow_name <> '' THEN EXCLUDED.workflow_name ELSE workflow_jobs.workflow_name END"),
			"head_branch":         gorm.Expr("CASE WHEN EXCLUDED.head_branch <> '' THEN EXCLUDED.head_branch ELSE workflow_jobs.head_branch END"),
			"actor":               gorm.Expr("CASE WHEN EXCLUDED.actor <> '' THEN EXCLUDED.actor ELSE workflow_jobs.actor END"),
			"labels":              gorm.Expr("CASE WHEN cardinality(EXCLUDED.labels) > 0 THEN EXCLUDED.labels ELSE workflow_jobs.labels END"),
			"status":              gorm.Expr("EXCLUDED.status"),
			"conclusion":          gorm.Expr("CASE WHEN EXCLUDED.conclusion <> '' THEN EXCLUDED.conclusion ELSE workflow_jobs.conclusion END"),
			"matched_pool_id":     gorm.Expr("COALESCE(EXCLUDED.matched_pool_id, workflow_jobs.matched_pool_id)"),
			"matched_policy_id":   gorm.Expr("COALESCE(EXCLUDED.matched_policy_id, workflow_jobs.matched_policy_id)"),
			"moderation_decision": gorm.Expr("EXCLUDED.moderation_decision"),
			"moderation_reason":   gorm.Expr("CASE WHEN EXCLUDED.moderation_reason <> '' THEN EXCLUDED.moderation_reason ELSE workflow_jobs.moderation_reason END"),
			"assigned_runner_id":  gorm.Expr("COALESCE(EXCLUDED.assigned_runner_id, workflow_jobs.assigned_runner_id)"),
			"started_at":          gorm.Expr("COALESCE(EXCLUDED.started_at, workflow_jobs.started_at)"),
			"completed_at":        gorm.Expr("COALESCE(EXCLUDED.completed_at, workflow_jobs.completed_at)"),
			"updated_at":          gorm.Expr("NOW()"),
		}),
	}).Create(&m).Error
	if err != nil {
		return mapGormError(err)
	}
	*j = m.toDomain()
	return nil
}

func (r *JobRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.WorkflowJob, error) {
	var m WorkflowJobModel
	if err := r.jobBaseQuery(ctx).Where("j.id = ?", id).Take(&m).Error; err != nil {
		return nil, mapGormError(err)
	}
	out := m.toDomain()
	return &out, nil
}

func (r *JobRepo) GetByGitHubJobID(ctx context.Context, githubJobID int64) (*domain.WorkflowJob, error) {
	var m WorkflowJobModel
	if err := r.jobBaseQuery(ctx).Where("j.github_job_id = ?", githubJobID).Take(&m).Error; err != nil {
		return nil, mapGormError(err)
	}
	out := m.toDomain()
	return &out, nil
}

func (r *JobRepo) List(ctx context.Context, filter domain.JobFilter) ([]domain.WorkflowJob, int, error) {
	q := r.db.WithContext(ctx).Table("workflow_jobs j")
	if filter.Status != "" {
		q = q.Where("j.status = ?", filter.Status)
	}
	if filter.Repo != "" {
		q = q.Where("j.repo_full_name = ?", filter.Repo)
	}
	if like := sanitizeLike(filter.Search); like != "" {
		q = q.Where(`(j.repo_full_name ILIKE ? ESCAPE '\' OR j.job_name ILIKE ? ESCAPE '\' OR j.actor ILIKE ? ESCAPE '\')`, like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, mapGormError(err)
	}

	var models []WorkflowJobModel
	sortCols := map[string]string{
		"repo_full_name":      "j.repo_full_name",
		"job_name":            "j.job_name",
		"actor":               "j.actor",
		"matched_pool_name":   "p.name",
		"moderation_decision": "j.moderation_decision",
		"status":              "j.status",
		"queued_at":           "j.queued_at",
	}
	err := q.Select(jobSelectCols).
		Joins("LEFT JOIN runner_pools p ON p.id = j.matched_pool_id").
		Joins("LEFT JOIN moderation_policies mp ON mp.id = j.matched_policy_id").
		Joins("LEFT JOIN runners rn ON rn.id = j.assigned_runner_id").
		Scopes(applySort(filter.PageParams, sortCols, "j.queued_at DESC"), paginate(filter.PageParams)).
		Scan(&models).Error
	if err != nil {
		return nil, 0, mapGormError(err)
	}

	items := make([]domain.WorkflowJob, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, int(total), nil
}

func clampQueryLimit(limit, def int) int {
	if limit <= 0 {
		return def
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func (r *JobRepo) ListDispatchableQueued(ctx context.Context, limit int) ([]domain.WorkflowJob, error) {
	var models []WorkflowJobModel
	err := r.jobBaseQuery(ctx).
		Where("j.status = ? AND j.moderation_decision IN ?", string(domain.JobQueued), []string{string(domain.ActionAllow), "approved"}).
		Order("j.queued_at ASC").
		Limit(clampQueryLimit(limit, 50)).
		Scan(&models).Error
	if err != nil {
		return nil, mapGormError(err)
	}
	items := make([]domain.WorkflowJob, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, nil
}

func (r *JobRepo) ClaimForProvisioning(ctx context.Context, id uuid.UUID, poolID *uuid.UUID, runnerID *uuid.UUID, reason string) (bool, error) {
	updates := map[string]any{
		"status":            string(domain.JobProvisioning),
		"moderation_reason": reason,
		"updated_at":        time.Now().UTC(),
	}
	if poolID != nil {
		updates["matched_pool_id"] = *poolID
	}
	if runnerID != nil {
		updates["assigned_runner_id"] = *runnerID
	}
	res := r.db.WithContext(ctx).Model(&WorkflowJobModel{}).
		Where("id = ? AND status = ?", id, string(domain.JobQueued)).
		Updates(updates)
	if res.Error != nil {
		return false, mapGormError(res.Error)
	}
	return res.RowsAffected > 0, nil
}

func (r *JobRepo) RequeueIfProvisioning(ctx context.Context, id uuid.UUID, reason string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&WorkflowJobModel{}).
		Where("id = ? AND status IN ?", id, []string{string(domain.JobQueued), string(domain.JobProvisioning)}).
		Updates(map[string]any{
			"status":             string(domain.JobQueued),
			"assigned_runner_id": nil,
			"moderation_reason":  reason,
			"updated_at":         time.Now().UTC(),
		})
	if res.Error != nil {
		return false, mapGormError(res.Error)
	}
	return res.RowsAffected > 0, nil
}

func (r *JobRepo) FindOrphanedProvisioningJobs(ctx context.Context, olderThan time.Time, limit int) ([]domain.WorkflowJob, error) {
	var models []WorkflowJobModel
	err := r.jobBaseQuery(ctx).
		Where("j.status = ? AND j.updated_at < ? AND (j.assigned_runner_id IS NULL OR rn.id IS NULL OR rn.status IN ?)",
			string(domain.JobProvisioning), olderThan, []string{string(domain.RunnerOffline), string(domain.RunnerTerminated)}).
		Order("j.queued_at ASC").
		Limit(clampQueryLimit(limit, 50)).
		Scan(&models).Error
	if err != nil {
		return nil, mapGormError(err)
	}
	items := make([]domain.WorkflowJob, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, nil
}

func (r *JobRepo) CountActiveForRepo(ctx context.Context, repoFullName string) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&WorkflowJobModel{}).
		Where("repo_full_name = ? AND status IN ?", repoFullName, []string{"queued", "provisioning", "in_progress"}).
		Count(&count).Error
	return int(count), mapGormError(err)
}

func (r *JobRepo) UpdateModerationAndRunner(ctx context.Context, id uuid.UUID, status domain.JobStatus, decision, reason string, moderatedBy, poolID, runnerID *uuid.UUID) error {
	updates := map[string]any{
		"status":              string(status),
		"moderation_decision": decision,
		"moderation_reason":   reason,
		"updated_at":          time.Now().UTC(),
	}
	if moderatedBy != nil {
		updates["moderated_by"] = *moderatedBy
	}
	if poolID != nil {
		updates["matched_pool_id"] = *poolID
	}
	if runnerID != nil {
		updates["assigned_runner_id"] = *runnerID
	}
	res := r.db.WithContext(ctx).Model(&WorkflowJobModel{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return mapGormError(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *JobRepo) UpdateLifecycle(ctx context.Context, githubJobID int64, status domain.JobStatus, conclusion string, startedAt, completedAt *time.Time) error {
	updates := map[string]any{
		"status":     string(status),
		"updated_at": time.Now().UTC(),
	}
	if conclusion != "" {
		updates["conclusion"] = conclusion
	}
	if startedAt != nil {
		updates["started_at"] = *startedAt
	}
	if completedAt != nil {
		updates["completed_at"] = *completedAt
	}
	res := r.db.WithContext(ctx).Model(&WorkflowJobModel{}).Where("github_job_id = ?", githubJobID).Updates(updates)
	if res.Error != nil {
		return mapGormError(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}
