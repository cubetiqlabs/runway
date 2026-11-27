package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cubetiqlabs/runway/internal/domain"
	"github.com/google/uuid"
)

type HeartbeatRequest struct {
	OS             string                       `json:"os"`
	Arch           string                       `json:"arch"`
	Runtime        string                       `json:"runtime"`
	SandboxRuntime string                       `json:"sandbox_runtime,omitempty"`
	Version        string                       `json:"version"`
	Hostname       string                       `json:"hostname"`
	CPUCount       int                          `json:"cpu_count"`
	MemoryMB       int                          `json:"memory_mb"`
	MaxConcurrency int                          `json:"max_concurrency"`
	ActiveJobs     int                          `json:"active_jobs"`
	Labels         []string                     `json:"labels,omitempty"`
	Metrics        *domain.AgentMetricsSnapshot `json:"metrics,omitempty"`
}

type HeartbeatResponse struct {
	Status        string      `json:"status"`
	CancelTaskIDs []uuid.UUID `json:"cancel_task_ids"`
}

type TaskPayload struct {
	TaskID           uuid.UUID `json:"task_id"`
	RunnerID         uuid.UUID `json:"runner_id"`
	GitHubJobID      int64     `json:"github_job_id"`
	RepoFullName     string    `json:"repo_full_name"`
	RunnerName       string    `json:"runner_name"`
	PoolSlug         string    `json:"pool_slug"`
	OS               string    `json:"os"`
	Arch             string    `json:"arch"`
	Labels           []string  `json:"labels"`
	ContainerImage   string    `json:"container_image"`
	VCPU             int       `json:"vcpu"`
	MemoryMB         int       `json:"memory_mb"`
	IsForkPR         bool      `json:"is_fork_pr"`
	BuildProfile     string    `json:"build_profile"`
	CacheEnabled     bool      `json:"cache_enabled"`
	CacheScope       string    `json:"cache_scope"`
	SandboxMode      string    `json:"sandbox_mode"`
	EncodedJITConfig string    `json:"encoded_jit_config"`
}

type Client struct {
	baseURL    string
	token      string
	userAgent  string
	httpClient *http.Client
}

func NewClient(baseURL, token, version string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:      strings.TrimSpace(token),
		userAgent:  "runway-agent/" + version,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Heartbeat(ctx context.Context, req HeartbeatRequest) (*HeartbeatResponse, error) {
	var out HeartbeatResponse
	if _, err := c.doJSON(ctx, http.MethodPost, "/api/v1/agent/heartbeat", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ClaimTask(ctx context.Context) (*TaskPayload, error) {
	var out TaskPayload
	status, err := c.doJSON(ctx, http.MethodPost, "/api/v1/agent/claim", nil, &out)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNoContent {
		return nil, nil
	}
	return &out, nil
}

func (c *Client) CompleteTask(ctx context.Context, taskID uuid.UUID, status, errMsg string) error {
	body := map[string]string{
		"status":        status,
		"error_message": errMsg,
	}
	_, err := c.doJSON(ctx, http.MethodPost, "/api/v1/agent/tasks/"+taskID.String()+"/complete", body, nil)
	return err
}

func (c *Client) ShipTaskLogs(ctx context.Context, taskID uuid.UUID, entries []domain.RunnerLogEntry) error {
	if len(entries) == 0 {
		return nil
	}
	body := map[string]any{
		"entries": entries,
	}
	_, err := c.doJSON(ctx, http.MethodPost, "/api/v1/agent/tasks/"+taskID.String()+"/logs", body, nil)
	return err
}

func (c *Client) doJSON(ctx context.Context, method, path string, reqBody, out any) (int, error) {
	var rawReq []byte
	if reqBody != nil {
		var err error
		rawReq, err = json.Marshal(reqBody)
		if err != nil {
			return 0, fmt.Errorf("marshal request: %w", err)
		}
	}

	var lastStatus int
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return lastStatus, ctx.Err()
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}

		var reader io.Reader
		if rawReq != nil {
			reader = bytes.NewReader(rawReq)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.userAgent)
		if rawReq != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		lastStatus = resp.StatusCode
		rawResp, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		if resp.StatusCode == http.StatusNoContent {
			return resp.StatusCode, nil
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("server transient status %d: %s", resp.StatusCode, strings.TrimSpace(string(rawResp)))
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return resp.StatusCode, fmt.Errorf("server error (%d): %s", resp.StatusCode, strings.TrimSpace(string(rawResp)))
		}
		if out != nil && len(rawResp) > 0 {
			if err := json.Unmarshal(rawResp, out); err != nil {
				return resp.StatusCode, fmt.Errorf("decode server response: %w", err)
			}
		}
		return resp.StatusCode, nil
	}
	return lastStatus, lastErr
}
