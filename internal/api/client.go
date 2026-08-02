package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/arcanflows/cli/internal/config"
)

// Sentinel errors so the retry logic can tell a retryable transport failure
// from a definitive auth/rate error (PROPOSAL-010 C14).
var (
	errUnauthorized = errors.New("unauthorized: please run 'arcanflows login'")
	errRateLimited  = errors.New("rate limited: too many requests, please wait")
)

// UserAgent identifies the CLI to the API server. The server uses the
// "ArcanFlows-CLI/" prefix to skip browser-only protections such as the
// Cloudflare Turnstile CAPTCHA on /auth/login and /auth/register — the CLI
// has no way to obtain a Turnstile token, and the API's per-IP rate limiter
// already covers credential-stuffing protection for non-browser clients.
// Set by cmd.init() so the version suffix tracks the current build.
var UserAgent = "ArcanFlows-CLI/dev"

// Client is the ArcanFlows API client.
type Client struct {
	BaseURL      string
	HTTPClient   *http.Client // non-streaming requests (has an overall timeout)
	streamClient *http.Client // streaming requests (no overall timeout; cancel via ctx)
	Token        string       // JWT access token
	RefreshToken string       // used to transparently refresh on 401 (C15)
	APIKey       string       // API key (alternative auth)
	RetryCount   int          // retry attempts for idempotent GETs on transport errors (C14)
	// OnRefresh is called with new tokens after a successful refresh, so the
	// caller can persist them (wired by cmd to write credentials.yaml).
	OnRefresh func(accessToken, refreshToken string)
}

// NewClient creates a new API client from config and credentials.
func NewClient(cfg *config.Config, creds *config.Credentials) *Client {
	active := creds.GetActive(cfg.ActiveProfile)
	timeout := 120 * time.Second
	if d, err := time.ParseDuration(cfg.Settings.Timeout); err == nil && d > 0 {
		timeout = d
	}
	return &Client{
		BaseURL:      strings.TrimRight(cfg.ActiveServer(), "/"),
		HTTPClient:   &http.Client{Timeout: timeout},
		streamClient: &http.Client{Timeout: 0},
		Token:        active.AccessToken,
		RefreshToken: active.RefreshToken,
		APIKey:       active.APIKey,
		RetryCount:   cfg.Settings.RetryCount,
	}
}

// NewClientWithToken creates a client with explicit credentials.
func NewClientWithToken(server, token, apiKey string) *Client {
	return &Client{
		BaseURL:      strings.TrimRight(server, "/"),
		HTTPClient:   &http.Client{Timeout: 120 * time.Second},
		streamClient: &http.Client{Timeout: 0},
		Token:        token,
		APIKey:       apiKey,
	}
}

// request performs an authenticated non-streaming HTTP request. Idempotent GETs
// are retried on transport errors up to RetryCount times (C14); auth/rate errors
// are never retried.
func (c *Client) request(method, path string, body interface{}) (*http.Response, error) {
	attempts := 1
	if method == http.MethodGet && c.RetryCount > 0 {
		attempts = c.RetryCount + 1
	}
	var resp *http.Response
	var err error
	for i := 0; i < attempts; i++ {
		resp, err = c.requestContext(context.Background(), method, path, body)
		if err == nil || errors.Is(err, errUnauthorized) || errors.Is(err, errRateLimited) {
			return resp, err
		}
		if i < attempts-1 {
			time.Sleep(time.Duration(i+1) * 300 * time.Millisecond)
		}
	}
	return resp, err
}

// requestContext performs an authenticated non-streaming request bound to ctx.
func (c *Client) requestContext(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	return c.do(ctx, c.HTTPClient, method, path, body, true)
}

// buildRequest constructs an authenticated *http.Request. The auth header is
// read from c.Token at build time, so a token refreshed mid-call is picked up
// on the retry.
func (c *Client) buildRequest(ctx context.Context, method, path string, body interface{}) (*http.Request, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	if c.APIKey != "" {
		req.Header.Set("X-API-Key", c.APIKey)
	} else if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return req, nil
}

// do issues the request on the given http.Client. On a 401, if a refresh token
// is available and allowRefresh is set, it refreshes once and retries (C15).
func (c *Client) do(ctx context.Context, httpClient *http.Client, method, path string, body interface{}, allowRefresh bool) (*http.Response, error) {
	req, err := c.buildRequest(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if resp.StatusCode == 401 {
		resp.Body.Close()
		if allowRefresh && c.RefreshToken != "" && c.APIKey == "" {
			if rerr := c.Refresh(); rerr == nil {
				return c.do(ctx, httpClient, method, path, body, false)
			}
		}
		return nil, errUnauthorized
	}
	if resp.StatusCode == 429 {
		resp.Body.Close()
		return nil, errRateLimited
	}
	return resp, nil
}

// Refresh exchanges the stored refresh token for a fresh access token and
// persists the result via OnRefresh (C15). It does NOT go through do() to avoid
// a refresh loop.
func (c *Client) Refresh() error {
	if c.RefreshToken == "" {
		return errors.New("no refresh token")
	}
	data, _ := json.Marshal(map[string]string{"refresh_token": c.RefreshToken})
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/api/v1/auth/refresh", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token refresh failed: HTTP %d", resp.StatusCode)
	}
	var tp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tp); err != nil {
		return err
	}
	if tp.AccessToken == "" {
		return errors.New("token refresh returned no access token")
	}
	c.Token = tp.AccessToken
	if tp.RefreshToken != "" {
		c.RefreshToken = tp.RefreshToken
	}
	if c.OnRefresh != nil {
		c.OnRefresh(c.Token, c.RefreshToken)
	}
	return nil
}

// Get performs an authenticated GET request and decodes JSON response.
func (c *Client) Get(path string, result interface{}) error {
	resp, err := c.request("GET", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// Post performs an authenticated POST request and decodes JSON response.
func (c *Client) Post(path string, body interface{}, result interface{}) error {
	resp, err := c.request("POST", path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// Put performs an authenticated PUT request.
func (c *Client) Put(path string, body interface{}, result interface{}) error {
	resp, err := c.request("PUT", path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// Delete performs an authenticated DELETE request.
func (c *Client) Delete(path string) error {
	resp, err := c.request("DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// StreamPost performs a POST request and returns the raw response for SSE streaming.
func (c *Client) StreamPost(path string, body interface{}) (*http.Response, error) {
	return c.request("POST", path, body)
}

// StreamPostContext is StreamPost bound to a cancellable context. It uses the
// no-timeout streaming client so a long chat isn't cut off by the request
// timeout — cancellation is via ctx instead.
func (c *Client) StreamPostContext(ctx context.Context, path string, body interface{}) (*http.Response, error) {
	return c.do(ctx, c.streamClient, "POST", path, body, true)
}

// Login authenticates and returns tokens.
func (c *Client) Login(email, password string) (*LoginResponse, error) {
	var result LoginResponse
	err := c.Post("/api/v1/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, &result)
	if err != nil {
		return nil, err
	}
	c.Token = result.AccessToken
	return &result, nil
}

// LoginResponse represents the login API response.
type LoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	User         struct {
		ID        string `json:"id"`
		Email     string `json:"email"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Role      string `json:"role"`
	} `json:"user"`
	Tenant struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"tenant"`
}

// Agent represents an agent from the API.
type Agent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	Model       string `json:"primary_model"`
	Status      string `json:"status"`
}

// AgentListResponse is the response for listing agents.
type AgentListResponse struct {
	Agents []Agent `json:"agents"`
	Total  int     `json:"total"`
}

// ListAgents returns all agents for the current tenant.
func (c *Client) ListAgents() ([]Agent, error) {
	var result AgentListResponse
	if err := c.Get("/api/v1/agents", &result); err != nil {
		return nil, err
	}
	return result.Agents, nil
}

// GetAgent returns a single agent by ID.
func (c *Client) GetAgent(id string) (*Agent, error) {
	var agent Agent
	if err := c.Get("/api/v1/agents/"+id, &agent); err != nil {
		return nil, err
	}
	return &agent, nil
}

// CreateAgent creates a new agent.
func (c *Client) CreateAgent(req map[string]interface{}) (*Agent, error) {
	var agent Agent
	if err := c.Post("/api/v1/agents", req, &agent); err != nil {
		return nil, err
	}
	return &agent, nil
}

// DownloadRaw fetches an absolute URL with the client's auth token and
// returns the body bytes — the artifact bridge (PROPOSAL-010 §6.6). Only
// call with URLs pointing at the configured platform host.
func (c *Client) DownloadRaw(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	const maxArtifact = 100 << 20 // 100MB cap — no OOM from a runaway file
	return io.ReadAll(io.LimitReader(resp.Body, maxArtifact))
}
