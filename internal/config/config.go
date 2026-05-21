package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Profile represents a server connection profile.
type Profile struct {
	Server       string `yaml:"server"`
	Organization string `yaml:"organization,omitempty"`
	DefaultAgent string `yaml:"default_agent,omitempty"`
	DefaultModel string `yaml:"default_model,omitempty"`
	OutputFormat string `yaml:"output_format,omitempty"` // table, json, yaml, csv
}

// Settings holds global CLI settings.
type Settings struct {
	ChatStreaming      bool     `yaml:"chat_streaming"`
	ChatMarkdown       bool     `yaml:"chat_markdown"`
	ChatSyntaxHL       bool     `yaml:"chat_syntax_highlight"`
	ChatMaxHistory     int      `yaml:"chat_max_history"`
	RepoMaxFileSize    int      `yaml:"repo_max_file_size"`
	RepoMaxFiles       int      `yaml:"repo_max_files"`
	RepoExclude        []string `yaml:"repo_exclude_patterns,omitempty"`
	Color              string   `yaml:"color"`  // auto, always, never
	Timeout            string   `yaml:"timeout"`
	RetryCount         int      `yaml:"retry_count"`
	SessionHistory     bool     `yaml:"session_history"`
	SessionMaxAgeDays  int      `yaml:"session_max_age_days"`
}

// Config is the top-level CLI configuration.
type Config struct {
	Version        int                `yaml:"version"`
	ActiveProfile  string             `yaml:"active_profile"`
	Profiles       map[string]Profile `yaml:"profiles"`
	Settings       Settings           `yaml:"settings"`
}

// Credentials stores authentication tokens per profile.
type Credentials struct {
	Profiles map[string]ProfileCredentials `yaml:"profiles"`
}

// ProfileCredentials stores tokens for a single profile.
type ProfileCredentials struct {
	AccessToken  string `yaml:"access_token,omitempty"`
	RefreshToken string `yaml:"refresh_token,omitempty"`
	APIKey       string `yaml:"api_key,omitempty"`
	Email        string `yaml:"email,omitempty"`
	UserID       string `yaml:"user_id,omitempty"`
	Role         string `yaml:"role,omitempty"`
}

// Dir returns the ArcanFlows config directory (~/.arcanflows).
func Dir() string {
	if d := os.Getenv("ARCANFLOWS_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".arcanflows")
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Version:       1,
		ActiveProfile: "default",
		Profiles: map[string]Profile{
			"default": {
				Server:       "https://api.arcanflows.com",
				OutputFormat: "table",
			},
		},
		Settings: Settings{
			ChatStreaming:   true,
			ChatMarkdown:    true,
			ChatSyntaxHL:    true,
			ChatMaxHistory:  50,
			RepoMaxFileSize: 100000,
			RepoMaxFiles:    200,
			RepoExclude: []string{
				"node_modules", ".git", "vendor", "dist", "__pycache__",
				"*.min.js", "*.lock", ".next", "target", "build",
			},
			Color:            "auto",
			Timeout:          "30s",
			RetryCount:       3,
			SessionHistory:   true,
			SessionMaxAgeDays: 30,
		},
	}
}

// Load reads the config from disk, or returns default if not found.
func Load() (*Config, error) {
	path := filepath.Join(Dir(), "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

// Save writes the config to disk.
func (c *Config) Save() error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0600)
}

// ActiveServer returns the server URL for the active profile, with env override.
func (c *Config) ActiveServer() string {
	if s := os.Getenv("ARCANFLOWS_SERVER"); s != "" {
		return s
	}
	if p, ok := c.Profiles[c.ActiveProfile]; ok {
		return p.Server
	}
	return "https://api.arcanflows.com"
}

// ActiveOutputFormat returns the output format for the active profile.
func (c *Config) ActiveOutputFormat() string {
	if p, ok := c.Profiles[c.ActiveProfile]; ok && p.OutputFormat != "" {
		return p.OutputFormat
	}
	return "table"
}

// LoadCredentials reads credentials from disk.
func LoadCredentials() (*Credentials, error) {
	path := filepath.Join(Dir(), "credentials.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Credentials{Profiles: make(map[string]ProfileCredentials)}, nil
		}
		return nil, err
	}
	var creds Credentials
	if err := yaml.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	if creds.Profiles == nil {
		creds.Profiles = make(map[string]ProfileCredentials)
	}
	return &creds, nil
}

// SaveCredentials writes credentials to disk with restricted permissions.
func (c *Credentials) Save() error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "credentials.yaml"), data, 0600)
}

// GetActive returns credentials for the given profile, with env override.
func (c *Credentials) GetActive(profile string) ProfileCredentials {
	// Environment variable takes priority
	if key := os.Getenv("ARCANFLOWS_API_KEY"); key != "" {
		return ProfileCredentials{APIKey: key}
	}
	return c.Profiles[profile]
}
