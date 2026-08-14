package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

// ---- Domain types ----

// SystemArtifactItem represents a single system artifact event in an API response.
type SystemArtifactItem struct {
	Key           string    `json:"key"`
	Operation     string    `json:"operation"`
	AgentID       string    `json:"agent_id"`
	SessionID     string    `json:"session_id"`
	TurnID        string    `json:"turn_id"`
	CorrelationID string    `json:"correlation_id"`
	OccurredAt    time.Time `json:"occurred_at"`
	ToolName      string    `json:"tool_name"`
	SHA           string    `json:"sha"`
}

// SystemArtifactPage is the paginated list response for system artifacts.
type SystemArtifactPage struct {
	TotalCount int
	Page       int
	PerPage    int
	Items      []SystemArtifactItem
}

// SystemArtifactFilter specifies filters for listing system artifacts.
type SystemArtifactFilter struct {
	Q              string
	AgentIDs       []string
	SessionIDs     []string
	TurnIDs        []string
	CorrelationIDs []string
	Operation      string
	Since          *time.Time
	Until          *time.Time
	IncludeDeleted bool
	Page           int
	PerPage        int
	Sort           string
	Order          string
}

// UserArtifactItem represents a user artifact in an API response.
type UserArtifactItem struct {
	Source    string    `json:"source"`
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	Filename  string    `json:"filename"`
	Size      int64     `json:"size"`
	MIMEType  string    `json:"mime_type"`
	SHA       string    `json:"sha"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UserArtifactPage is the paginated list response for user artifacts.
type UserArtifactPage struct {
	TotalCount int
	Page       int
	PerPage    int
	Items      []UserArtifactItem
}

// UserArtifactFilter specifies filters for listing user artifacts.
type UserArtifactFilter struct {
	Q       string
	Page    int
	PerPage int
	Sort    string
	Order   string
}

// PutResponse is the result of a PUT user artifact operation.
type PutResponse struct {
	Source string `json:"source"`
	Key    string `json:"key"`
	Status string `json:"status"`
	SHA    string `json:"sha"`
	Size   int64  `json:"size"`
}

// ArchiveRequest specifies which artifacts to include in an archive download.
type ArchiveRequest struct {
	Keys      []string `json:"keys,omitempty"`
	Q         string   `json:"q,omitempty"`
	SessionID []string `json:"session_id,omitempty"`
}

// ---- SystemArtifactClient ----

// SystemArtifactClient provides access to system artifact operations.
type SystemArtifactClient struct {
	c *Client
}

// SystemArtifacts returns a SystemArtifactClient.
func (c *Client) SystemArtifacts() *SystemArtifactClient {
	return &SystemArtifactClient{c: c}
}

// List returns a paginated list of system artifacts matching the filter.
func (sc *SystemArtifactClient) List(ctx context.Context, f SystemArtifactFilter) (*SystemArtifactPage, error) {
	q := url.Values{}
	if f.Q != "" {
		q.Set("q", f.Q)
	}
	for _, id := range f.AgentIDs {
		q.Add("agent_id", id)
	}
	for _, id := range f.SessionIDs {
		q.Add("session_id", id)
	}
	for _, id := range f.TurnIDs {
		q.Add("turn_id", id)
	}
	for _, id := range f.CorrelationIDs {
		q.Add("correlation_id", id)
	}
	if f.Operation != "" {
		q.Set("operation", f.Operation)
	}
	if f.IncludeDeleted {
		q.Set("include_deleted", "true")
	}
	if f.Page > 0 {
		q.Set("page", strconv.Itoa(f.Page))
	}
	if f.PerPage > 0 {
		q.Set("per_page", strconv.Itoa(f.PerPage))
	}
	if f.Sort != "" {
		q.Set("sort", f.Sort)
	}
	if f.Order != "" {
		q.Set("order", f.Order)
	}
	if f.Since != nil {
		q.Set("since", f.Since.UTC().Format(time.RFC3339))
	}
	if f.Until != nil {
		q.Set("until", f.Until.UTC().Format(time.RFC3339))
	}

	rawURL := sc.c.baseURL + "/api/v1/artifacts/system"
	if len(q) > 0 {
		rawURL += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := sc.c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list system artifacts: HTTP %d", resp.StatusCode)
	}

	var raw struct {
		TotalCount int                  `json:"total_count"`
		Page       int                  `json:"page"`
		PerPage    int                  `json:"per_page"`
		Items      []SystemArtifactItem `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return &SystemArtifactPage{
		TotalCount: raw.TotalCount,
		Page:       raw.Page,
		PerPage:    raw.PerPage,
		Items:      raw.Items,
	}, nil
}

// ListAll collects all matching system artifact events by walking pages.
// f.Page is ignored. When f.PerPage <= 0, each request omits per_page so the
// server default (100) applies.
func (sc *SystemArtifactClient) ListAll(ctx context.Context, f SystemArtifactFilter) ([]SystemArtifactItem, error) {
	var all []SystemArtifactItem
	pageNum := 1
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cf := f
		cf.Page = pageNum
		page, err := sc.List(ctx, cf)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if len(page.Items) == 0 || len(all) >= page.TotalCount {
			break
		}
		if pageNum > 100000 {
			return all, fmt.Errorf("list all system artifacts: exceeded page safety limit")
		}
		pageNum++
	}
	return all, nil
}

// Download streams the content of the system artifact at key.
// The caller must close the returned ReadCloser.
func (sc *SystemArtifactClient) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	u := sc.c.baseURL + "/api/v1/artifacts/system/" + key + "/content"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := sc.c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download system artifact %q: HTTP %d", key, resp.StatusCode)
	}
	return resp.Body, nil
}

// Archive requests a ZIP archive of the specified artifacts.
// The caller must close the returned ReadCloser.
func (sc *SystemArtifactClient) Archive(ctx context.Context, r ArchiveRequest) (io.ReadCloser, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		sc.c.baseURL+"/api/v1/artifacts/system/archive", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := sc.c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("archive system artifacts: HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// ---- UserArtifactClient ----

// UserArtifactClient provides access to user artifact operations.
type UserArtifactClient struct {
	c *Client
}

// UserArtifacts returns a UserArtifactClient.
func (c *Client) UserArtifacts() *UserArtifactClient {
	return &UserArtifactClient{c: c}
}

// Put uploads content from r and stores it at the logical key.
// Returns the server response with status (created | updated).
func (uc *UserArtifactClient) Put(ctx context.Context, key string, r io.Reader) (*PutResponse, error) {
	return uc.put(ctx, key, r)
}

// PutFile reads the file at localPath and stores it at the logical key.
func (uc *UserArtifactClient) PutFile(ctx context.Context, key, localPath string) (*PutResponse, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("open file %q: %w", localPath, err)
	}
	defer f.Close()
	return uc.put(ctx, key, f)
}

func (uc *UserArtifactClient) put(ctx context.Context, key string, r io.Reader) (*PutResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		uc.c.baseURL+"/api/v1/artifacts/user/"+key, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := uc.c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("put user artifact %q: HTTP %d", key, resp.StatusCode)
	}

	var pr PutResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// Download streams the content of the user artifact at key.
// The caller must close the returned ReadCloser.
func (uc *UserArtifactClient) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	u := uc.c.baseURL + "/api/v1/artifacts/user/" + key + "/content"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := uc.c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download user artifact %q: HTTP %d", key, resp.StatusCode)
	}
	return resp.Body, nil
}

// Delete removes the user artifact at key.
func (uc *UserArtifactClient) Delete(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		uc.c.baseURL+"/api/v1/artifacts/user/"+key, nil)
	if err != nil {
		return err
	}
	resp, err := uc.c.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("delete user artifact %q: HTTP %d", key, resp.StatusCode)
	}
	return nil
}

// List returns a paginated list of user artifacts matching the filter.
func (uc *UserArtifactClient) List(ctx context.Context, f UserArtifactFilter) (*UserArtifactPage, error) {
	q := url.Values{}
	if f.Q != "" {
		q.Set("q", f.Q)
	}
	if f.Page > 0 {
		q.Set("page", strconv.Itoa(f.Page))
	}
	if f.PerPage > 0 {
		q.Set("per_page", strconv.Itoa(f.PerPage))
	}
	if f.Sort != "" {
		q.Set("sort", f.Sort)
	}
	if f.Order != "" {
		q.Set("order", f.Order)
	}

	rawURL := uc.c.baseURL + "/api/v1/artifacts/user"
	if len(q) > 0 {
		rawURL += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := uc.c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list user artifacts: HTTP %d", resp.StatusCode)
	}

	var raw struct {
		TotalCount int                `json:"total_count"`
		Page       int                `json:"page"`
		PerPage    int                `json:"per_page"`
		Items      []UserArtifactItem `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return &UserArtifactPage{
		TotalCount: raw.TotalCount,
		Page:       raw.Page,
		PerPage:    raw.PerPage,
		Items:      raw.Items,
	}, nil
}

// ListAll collects all matching user artifacts by walking pages.
// f.Page is ignored. When f.PerPage <= 0, each request omits per_page so the
// server default (100) applies.
func (uc *UserArtifactClient) ListAll(ctx context.Context, f UserArtifactFilter) ([]UserArtifactItem, error) {
	var all []UserArtifactItem
	pageNum := 1
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cf := f
		cf.Page = pageNum
		page, err := uc.List(ctx, cf)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if len(page.Items) == 0 || len(all) >= page.TotalCount {
			break
		}
		if pageNum > 100000 {
			return all, fmt.Errorf("list all user artifacts: exceeded page safety limit")
		}
		pageNum++
	}
	return all, nil
}

// Archive requests a ZIP archive of user artifacts.
// The caller must close the returned ReadCloser.
func (uc *UserArtifactClient) Archive(ctx context.Context, r ArchiveRequest) (io.ReadCloser, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		uc.c.baseURL+"/api/v1/artifacts/user/archive", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := uc.c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("archive user artifacts: HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}
