package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Client is a generic OCI Distribution API client.
type Client struct {
	endpoint   string
	httpClient *http.Client
	username   string
	password   string
	token      string
	authMode   string
	logger     zerolog.Logger

	// Cached bearer tokens per scope
	bearerTokens map[string]string
}

// NewClient creates a new OCI registry client.
func NewClient(endpoint, authMode, username, password, token string, logger zerolog.Logger) *Client {
	return &Client{
		endpoint: strings.TrimRight(endpoint, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		username:     username,
		password:     password,
		token:        token,
		authMode:     authMode,
		logger:       logger,
		bearerTokens: make(map[string]string),
	}
}

// Ping checks /v2/ endpoint.
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.doRequest(ctx, "GET", "/v2/", nil)
	if err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("ping: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// ListTags returns all tags for a repository.
func (c *Client) ListTags(ctx context.Context, repo string) ([]string, error) {
	var allTags []string
	url := fmt.Sprintf("/v2/%s/tags/list", repo)
	for url != "" {
		resp, err := c.doRequest(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("list tags: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("list tags: status %d: %s", resp.StatusCode, string(body))
		}

		var result struct {
			Tags []string `json:"tags"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("list tags decode: %w", err)
		}
		allTags = append(allTags, result.Tags...)

		url = getNextLink(resp)
	}
	return allTags, nil
}

// ManifestResponse holds the result of a manifest fetch.
type ManifestResponse struct {
	MediaType string
	Digest    string
	Body      []byte
	Size      int64
}

// GetManifest fetches a manifest by reference (tag or digest).
func (c *Client) GetManifest(ctx context.Context, repo, reference string) (*ManifestResponse, error) {
	path := fmt.Sprintf("/v2/%s/manifests/%s", repo, reference)
	req, err := http.NewRequestWithContext(ctx, "GET", c.endpoint+path, nil)
	if err != nil {
		return nil, err
	}
	// Accept both OCI and Docker manifest types
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.docker.distribution.manifest.v1+json",
	}, ", "))
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get manifest: %w", err)
	}
	defer resp.Body.Close()

	// Handle auth challenge
	if resp.StatusCode == http.StatusUnauthorized {
		if err := c.handleAuthChallenge(ctx, resp, repo); err != nil {
			return nil, err
		}
		// Retry
		return c.GetManifest(ctx, repo, reference)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get manifest: status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("get manifest: read body: %w", err)
	}

	return &ManifestResponse{
		MediaType: resp.Header.Get("Content-Type"),
		Digest:    resp.Header.Get("Docker-Content-Digest"),
		Body:      body,
		Size:      int64(len(body)),
	}, nil
}

// GetBlob fetches a blob by digest.
func (c *Client) GetBlob(ctx context.Context, repo, digest string) ([]byte, error) {
	path := fmt.Sprintf("/v2/%s/blobs/%s", repo, digest)
	resp, err := c.doRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("get blob: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		if err := c.handleAuthChallenge(ctx, resp, repo); err != nil {
			return nil, err
		}
		return c.GetBlob(ctx, repo, digest)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get blob: status %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

// ListReferrers lists referrers for a digest. Falls back to tag schema if API returns 404.
func (c *Client) ListReferrers(ctx context.Context, repo, digest string) ([]Descriptor, error) {
	path := fmt.Sprintf("/v2/%s/referrers/%s", repo, digest)
	resp, err := c.doRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("list referrers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Fall back to tag schema
		return c.listReferrersByTagSchema(ctx, repo, digest)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, nil // Referrers not supported
	}

	var index ImageIndex
	if err := json.NewDecoder(resp.Body).Decode(&index); err != nil {
		return nil, fmt.Errorf("list referrers decode: %w", err)
	}
	return index.Manifests, nil
}

func (c *Client) listReferrersByTagSchema(ctx context.Context, repo, digest string) ([]Descriptor, error) {
	// Tag schema: sha256-<hex> as tag
	tag := strings.Replace(digest, ":", "-", 1)
	manifest, err := c.GetManifest(ctx, repo, tag)
	if err != nil {
		return nil, nil // Tag schema not available
	}

	var index ImageIndex
	if err := json.Unmarshal(manifest.Body, &index); err != nil {
		return nil, nil
	}
	return index.Manifests, nil
}

// Catalog lists repositories via /v2/_catalog (non-standard, but widely supported).
func (c *Client) Catalog(ctx context.Context) ([]string, error) {
	var allRepos []string
	url := "/v2/_catalog"
	for url != "" {
		resp, err := c.doRequest(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("catalog endpoint not supported")
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("catalog: status %d: %s", resp.StatusCode, string(body))
		}

		var result struct {
			Repositories []string `json:"repositories"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("catalog decode: %w", err)
		}
		allRepos = append(allRepos, result.Repositories...)
		url = getNextLink(resp)
	}
	return allRepos, nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := path
	if !strings.HasPrefix(path, "http") {
		url = c.endpoint + path
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	// Handle auth challenge
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		scope := extractScope(path)
		if err := c.handleAuthChallenge(ctx, resp, scope); err != nil {
			return nil, err
		}
		// Retry with new auth
		req2, err := http.NewRequestWithContext(ctx, method, url, body)
		if err != nil {
			return nil, err
		}
		c.setAuth(req2)
		return c.httpClient.Do(req2)
	}

	return resp, nil
}

func (c *Client) setAuth(req *http.Request) {
	switch c.authMode {
	case "basic":
		req.SetBasicAuth(c.username, c.password)
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+c.token)
	case "github_pat":
		req.SetBasicAuth(c.username, c.token)
	}
	// If we have a cached bearer token for this scope, use it
	if token, ok := c.bearerTokens["default"]; ok {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func (c *Client) handleAuthChallenge(ctx context.Context, resp *http.Response, scope string) error {
	challenge := resp.Header.Get("WWW-Authenticate")
	if challenge == "" {
		return fmt.Errorf("401 with no WWW-Authenticate header")
	}

	realm, service, challengeScope := parseWWWAuthenticate(challenge)
	if realm == "" {
		return fmt.Errorf("could not parse WWW-Authenticate: %s", challenge)
	}

	// Request bearer token
	tokenURL := fmt.Sprintf("%s?service=%s", realm, service)
	if challengeScope != "" {
		tokenURL += "&scope=" + challengeScope
	}

	tokenReq, err := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
	if err != nil {
		return err
	}
	if c.authMode == "basic" {
		tokenReq.SetBasicAuth(c.username, c.password)
	} else if c.authMode == "github_pat" {
		tokenReq.SetBasicAuth(c.username, c.token)
	}

	tokenResp, err := c.httpClient.Do(tokenReq)
	if err != nil {
		return fmt.Errorf("token request: %w", err)
	}
	defer tokenResp.Body.Close()

	if tokenResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(tokenResp.Body)
		return fmt.Errorf("token request: status %d: %s", tokenResp.StatusCode, string(body))
	}

	var tokenResult struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenResult); err != nil {
		return fmt.Errorf("token decode: %w", err)
	}

	tok := tokenResult.Token
	if tok == "" {
		tok = tokenResult.AccessToken
	}
	c.bearerTokens["default"] = tok
	return nil
}

func parseWWWAuthenticate(header string) (realm, service, scope string) {
	// Parse: Bearer realm="...",service="...",scope="..."
	header = strings.TrimPrefix(header, "Bearer ")
	parts := strings.Split(header, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "realm=") {
			realm = strings.Trim(strings.TrimPrefix(part, "realm="), "\"")
		} else if strings.HasPrefix(part, "service=") {
			service = strings.Trim(strings.TrimPrefix(part, "service="), "\"")
		} else if strings.HasPrefix(part, "scope=") {
			scope = strings.Trim(strings.TrimPrefix(part, "scope="), "\"")
		}
	}
	return
}

func extractScope(path string) string {
	// Extract repo from path like /v2/<repo>/tags/list
	path = strings.TrimPrefix(path, "/v2/")
	parts := strings.Split(path, "/")
	if len(parts) >= 2 {
		return strings.Join(parts[:len(parts)-1], "/")
	}
	return ""
}

func getNextLink(resp *http.Response) string {
	link := resp.Header.Get("Link")
	if link == "" {
		return ""
	}
	// Parse: <url>; rel="next"
	if idx := strings.Index(link, "<"); idx >= 0 {
		end := strings.Index(link[idx:], ">")
		if end >= 0 {
			return link[idx+1 : idx+end]
		}
	}
	return ""
}
