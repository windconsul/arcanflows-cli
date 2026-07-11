package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arcanflows/cli/internal/config"
)

// A 401 with a valid refresh token should transparently refresh and retry, and
// persist the new tokens via OnRefresh (PROPOSAL-010 C15).
func TestRefreshOn401(t *testing.T) {
	var refreshCalls, agentCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			refreshCalls++
			w.WriteHeader(200)
			w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh"}`))
		case "/api/v1/agents":
			agentCalls++
			if r.Header.Get("Authorization") != "Bearer new-access" {
				w.WriteHeader(401)
				return
			}
			w.WriteHeader(200)
			w.Write([]byte(`{"agents":[{"id":"1","name":"A"}],"total":1}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	var savedAccess, savedRefresh string
	c := &Client{
		BaseURL:      srv.URL,
		HTTPClient:   &http.Client{Timeout: 5 * time.Second},
		streamClient: &http.Client{},
		Token:        "old-access",
		RefreshToken: "old-refresh",
		OnRefresh: func(a, r string) {
			savedAccess, savedRefresh = a, r
		},
	}

	agents, err := c.ListAgents()
	if err != nil {
		t.Fatalf("expected success after refresh, got %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if refreshCalls != 1 {
		t.Errorf("expected exactly 1 refresh call, got %d", refreshCalls)
	}
	if agentCalls != 2 {
		t.Errorf("expected 2 agent calls (401 then retry), got %d", agentCalls)
	}
	if savedAccess != "new-access" || savedRefresh != "new-refresh" {
		t.Errorf("OnRefresh not persisted correctly: %q / %q", savedAccess, savedRefresh)
	}
	if c.Token != "new-access" {
		t.Errorf("client token not updated: %q", c.Token)
	}
}

// With API-key auth there is no refresh token — a 401 must fail, not loop.
func TestNoRefreshWithAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTPClient: &http.Client{}, streamClient: &http.Client{}, APIKey: "k", RefreshToken: "r"}
	if err := c.Get("/api/v1/agents", nil); err == nil {
		t.Error("expected unauthorized error with API key")
	}
}

// NewClient must honor the configured request timeout (C14).
func TestNewClientHonorsTimeout(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Settings.Timeout = "7s"
	c := NewClient(cfg, &config.Credentials{Profiles: map[string]config.ProfileCredentials{}})
	if c.HTTPClient.Timeout != 7*time.Second {
		t.Errorf("expected 7s timeout, got %v", c.HTTPClient.Timeout)
	}
	if c.streamClient.Timeout != 0 {
		t.Errorf("streaming client must have no timeout, got %v", c.streamClient.Timeout)
	}
}
