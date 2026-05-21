package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/arcanflows/cli/internal/config"
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
	BaseURL    string
	HTTPClient *http.Client
	Token      string // JWT access token
	APIKey     string // API key (alternative auth)
}

// NewClient creates a new API client from config and credentials.
func NewClient(cfg *config.Config, creds *config.Credentials) *Client {
	active := creds.GetActive(cfg.ActiveProfile)
	return &Client{
		BaseURL: strings.TrimRight(cfg.ActiveServer(), "/"),
		HTTPClient: &http.Client{
			Timeout: 120 * time.Second,
		},
		Token:  active.AccessToken,
		APIKey: active.APIKey,
	}
}

// NewClientWithToken creates a client with explicit credentials.
func NewClientWithToken(server, token, apiKey string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(server, "/"),
		HTTPClient: &http.Client{Timeout: 120 * time.Second},
		Token:      token,
		APIKey:     apiKey,
	}
}

// request performs an authenticated HTTP request.
func (c *Client) request(method, path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	url := c.BaseURL + path
	req, err := http.NewRequest(method, url, bodyReader)
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

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode == 401 {
		resp.Body.Close()
		return nil, fmt.Errorf("unauthorized: please run 'arcanflows login'")
	}

	if resp.StatusCode == 429 {
		resp.Body.Close()
		return nil, fmt.Errorf("rate limited: too many requests, please wait")
	}

	return resp, nil
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
