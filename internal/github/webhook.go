package github

import (
	"encoding/json"
	"time"
)

type WebhookInstallation struct {
	ID                  int64           `json:"id"`
	RepositorySelection string          `json:"repository_selection"`
	Permissions         json.RawMessage `json:"permissions"`
	SuspendedAt         *time.Time      `json:"suspended_at"`
	Account             struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"account"`
	TargetType string `json:"target_type"`
}

type InstallationEvent struct {
	Action       string              `json:"action"`
	Installation WebhookInstallation `json:"installation"`
}

type WorkflowJobEvent struct {
	Action      string `json:"action"` // queued | in_progress | completed | waiting
	WorkflowJob struct {
		ID           int64      `json:"id"`
		RunID        int64      `json:"run_id"`
		WorkflowName string     `json:"workflow_name"`
		Name         string     `json:"name"`
		HeadBranch   string     `json:"head_branch"`
		HeadSHA      string     `json:"head_sha"`
		Status       string     `json:"status"`
		Conclusion   string     `json:"conclusion"`
		Labels       []string   `json:"labels"`
		RunnerID     *int64     `json:"runner_id"`
		RunnerName   string     `json:"runner_name"`
		CreatedAt    time.Time  `json:"created_at"`
		StartedAt    *time.Time `json:"started_at"`
		CompletedAt  *time.Time `json:"completed_at"`
	} `json:"workflow_job"`
	Repository struct {
		FullName string `json:"full_name"`
		Fork     bool   `json:"fork"`
		Owner    struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"owner"`
		Name string `json:"name"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	PullRequests []struct {
		Head struct {
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"base"`
	} `json:"pull_requests"`
}
