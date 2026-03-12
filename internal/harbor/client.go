package harbor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const pageSize = 100

// Client is a Harbor v2.0 API client.
type Client struct {
	endpoint   string
	httpClient *http.Client
	username   string
	password   string
}

// NewClient creates a new Harbor API client.
func NewClient(endpoint, username, password string) *Client {
	return &Client{
		endpoint: strings.TrimRight(endpoint, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		username: username,
		password: password,
	}
}

// Project represents a Harbor project.
type Project struct {
	ProjectID int    `json:"project_id"`
	Name      string `json:"name"`
	Public    bool   `json:"metadata.public,omitempty"`
	RepoCount int    `json:"repo_count"`
}

// Repository represents a Harbor repository within a project.
type Repository struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	ProjectID     int    `json:"project_id"`
	ArtifactCount int    `json:"artifact_count"`
	PullCount     int64  `json:"pull_count"`
	UpdateTime    string `json:"update_time"`
}

// Artifact represents a Harbor artifact.
type Artifact struct {
	ID                int                     `json:"id"`
	Digest            string                  `json:"digest"`
	Size              int64                   `json:"size"`
	MediaType         string                  `json:"media_type"`
	ManifestMediaType string                  `json:"manifest_media_type"`
	ArtifactType      string                  `json:"type"`
	PushTime          string                  `json:"push_time"`
	Tags              []Tag                   `json:"tags"`
	Labels            []Label                 `json:"labels"`
	Annotations       map[string]string       `json:"annotations"`
	AdditionLinks     map[string]AdditionLink `json:"addition_links"`
	Accessories       []Accessory             `json:"accessories"`
	ExtraAttrs        map[string]any          `json:"extra_attrs"`
}

// Tag represents a Harbor tag.
type Tag struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	PushTime string `json:"push_time"`
}

// Label represents a Harbor label.
type Label struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"` // "g" (global) or "p" (project)
	ProjectID   int    `json:"project_id"`
}

// AdditionLink is a Harbor addition link.
type AdditionLink struct {
	HREF     string `json:"href"`
	Absolute bool   `json:"absolute"`
}

// Accessory represents a Harbor accessory (signature, SBOM, etc.).
type Accessory struct {
	ID         int    `json:"id"`
	ArtifactID int    `json:"artifact_id"`
	Digest     string `json:"digest"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
}

// ListProjects lists all Harbor projects.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var all []Project
	page := 1
	for {
		url := fmt.Sprintf("%s/api/v2.0/projects?page=%d&page_size=%d", c.endpoint, page, pageSize)
		body, err := c.doGet(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("list projects: %w", err)
		}
		var batch []Project
		if err := json.Unmarshal(body, &batch); err != nil {
			return nil, fmt.Errorf("list projects decode: %w", err)
		}
		all = append(all, batch...)
		if len(batch) < pageSize {
			break
		}
		page++
	}
	return all, nil
}

// ListRepositories lists repositories in a project.
func (c *Client) ListRepositories(ctx context.Context, projectName string) ([]Repository, error) {
	var all []Repository
	page := 1
	for {
		url := fmt.Sprintf("%s/api/v2.0/projects/%s/repositories?page=%d&page_size=%d",
			c.endpoint, projectName, page, pageSize)
		body, err := c.doGet(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("list repositories: %w", err)
		}
		var batch []Repository
		if err := json.Unmarshal(body, &batch); err != nil {
			return nil, fmt.Errorf("list repositories decode: %w", err)
		}
		all = append(all, batch...)
		if len(batch) < pageSize {
			break
		}
		page++
	}
	return all, nil
}

// ListArtifacts lists artifacts in a repository.
func (c *Client) ListArtifacts(ctx context.Context, projectName, repoName string) ([]Artifact, error) {
	encodedRepo := strings.ReplaceAll(repoName, "/", "%2F")
	var all []Artifact
	page := 1
	for {
		url := fmt.Sprintf("%s/api/v2.0/projects/%s/repositories/%s/artifacts?page=%d&page_size=%d&with_tag=true&with_label=true",
			c.endpoint, projectName, encodedRepo, page, pageSize)
		body, err := c.doGet(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("list artifacts: %w", err)
		}
		var batch []Artifact
		if err := json.Unmarshal(body, &batch); err != nil {
			return nil, fmt.Errorf("list artifacts decode: %w", err)
		}
		all = append(all, batch...)
		if len(batch) < pageSize {
			break
		}
		page++
	}
	return all, nil
}

// ListLabels lists global or project labels.
func (c *Client) ListLabels(ctx context.Context, scope string, projectID int) ([]Label, error) {
	url := fmt.Sprintf("%s/api/v2.0/labels?scope=%s", c.endpoint, scope)
	if scope == "p" {
		url += fmt.Sprintf("&project_id=%d", projectID)
	}
	body, err := c.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}
	var labels []Label
	if err := json.Unmarshal(body, &labels); err != nil {
		return nil, fmt.Errorf("list labels decode: %w", err)
	}
	return labels, nil
}

// Health checks if Harbor is reachable.
func (c *Client) Health(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/v2.0/health", c.endpoint)
	_, err := c.doGet(ctx, url)
	return err
}

func (c *Client) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}
