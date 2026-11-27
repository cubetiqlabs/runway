package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client provides hardened GitHub App & Actions Runner REST calls.
type Client struct {
	auth       *AppAuth
	apiBaseURL string
	httpClient *http.Client
}

type JITConfigRequest struct {
	Name          string   `json:"name"`
	RunnerGroupID int64    `json:"runner_group_id"`
	Labels        []string `json:"labels"`
	WorkFolder    string   `json:"work_folder,omitempty"`
}

type GitHubRunner struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	OS     string `json:"os"`
	Status string `json:"status"`
	Busy   bool   `json:"busy"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

type JITConfigResponse struct {
	Runner           GitHubRunner `json:"runner"`
	EncodedJITConfig string       `json:"encoded_jit_config"`
}

type RemoteInstallation struct {
	ID      int64 `json:"id"`
	Account struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"account"`
	TargetType            string          `json:"target_type"`
	RepositorySelection   string          `json:"repository_selection"`
	Permissions           json.RawMessage `json:"permissions"`
	SuspendedAt           *time.Time      `json:"suspended_at"`
}

func NewClient(auth *AppAuth, apiBaseURL string) *Client {
	base := strings.TrimRight(strings.TrimSpace(apiBaseURL), "/")
	if base == "" {
		base = "https://api.github.com"
	}
	return &Client{
		auth:       auth,
		apiBaseURL: base,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// IsConfigured reports whether GitHub App credentials are loaded.
func (c *Client) IsConfigured() bool {
	return c != nil && c.auth != nil
}

func (c *Client) doJSON(ctx context.Context, method, path, bearerToken string, reqBody, out any) error {
	var bodyBytes []byte
	if reqBody != nil {
		var err error
		bodyBytes, err = json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt*250) * time.Millisecond):
			}
		}

		var reader io.Reader
		if bodyBytes != nil {
			reader = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.apiBaseURL+path, reader)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+bearerToken)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("User-Agent", "runway/1.0")
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("github api transient status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("github api error (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		}

		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("decode github response: %w", err)
			}
		}
		return nil
	}
	return lastErr
}

func (c *Client) ListInstallations(ctx context.Context) ([]RemoteInstallation, error) {
	if !c.IsConfigured() {
		return nil, fmt.Errorf("github app credentials are not configured")
	}
	appJWT, err := c.auth.MintAppJWT(time.Now().UTC())
	if err != nil {
		return nil, err
	}
	var res []RemoteInstallation
	if err := c.doJSON(ctx, http.MethodGet, "/app/installations?per_page=100", appJWT, nil, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func (c *Client) GenerateOrgJITConfig(ctx context.Context, installationID int64, org string, req JITConfigRequest) (*JITConfigResponse, error) {
	tok, err := c.auth.InstallationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	var out JITConfigResponse
	path := fmt.Sprintf("/orgs/%s/actions/runners/generate-jitconfig", org)
	if err := c.doJSON(ctx, http.MethodPost, path, tok, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GenerateRepoJITConfig(ctx context.Context, installationID int64, owner, repo string, req JITConfigRequest) (*JITConfigResponse, error) {
	tok, err := c.auth.InstallationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	var out JITConfigResponse
	path := fmt.Sprintf("/repos/%s/%s/actions/runners/generate-jitconfig", owner, repo)
	if err := c.doJSON(ctx, http.MethodPost, path, tok, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListOrgRunners(ctx context.Context, installationID int64, org string) ([]GitHubRunner, error) {
	tok, err := c.auth.InstallationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	var out struct {
		Runners []GitHubRunner `json:"runners"`
	}
	path := fmt.Sprintf("/orgs/%s/actions/runners?per_page=100", org)
	if err := c.doJSON(ctx, http.MethodGet, path, tok, nil, &out); err != nil {
		return nil, err
	}
	return out.Runners, nil
}

func (c *Client) DeleteOrgRunner(ctx context.Context, installationID int64, org string, runnerID int64) error {
	tok, err := c.auth.InstallationToken(ctx, installationID)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/orgs/%s/actions/runners/%d", org, runnerID)
	return c.doJSON(ctx, http.MethodDelete, path, tok, nil, nil)
}

func (c *Client) DeleteRepoRunner(ctx context.Context, installationID int64, owner, repo string, runnerID int64) error {
	tok, err := c.auth.InstallationToken(ctx, installationID)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runners/%d", owner, repo, runnerID)
	return c.doJSON(ctx, http.MethodDelete, path, tok, nil, nil)
}

type WorkflowJobRemote struct {
	ID          int64      `json:"id"`
	RunID       int64      `json:"run_id"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	RunnerName  string     `json:"runner_name"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

func (c *Client) GetWorkflowJob(ctx context.Context, installationID int64, owner, repo string, jobID int64) (*WorkflowJobRemote, error) {
	tok, err := c.auth.InstallationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	var out WorkflowJobRemote
	path := fmt.Sprintf("/repos/%s/%s/actions/jobs/%d", owner, repo, jobID)
	if err := c.doJSON(ctx, http.MethodGet, path, tok, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
