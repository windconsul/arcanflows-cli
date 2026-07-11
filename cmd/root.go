package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/arcanflows/cli/internal/api"
	"github.com/arcanflows/cli/internal/config"
	"github.com/arcanflows/cli/internal/render"
	"github.com/arcanflows/cli/internal/repo"
	"github.com/spf13/cobra"
)

var (
	// Version is set at build time.
	Version = "dev"

	cfg   *config.Config
	creds *config.Credentials
)

func init() {
	// Sync the API client's User-Agent with the current build version so the
	// server can identify CLI traffic (used to bypass browser-only CAPTCHA
	// on /auth/login and /auth/register).
	api.UserAgent = "ArcanFlows-CLI/" + Version
}

var rootCmd = &cobra.Command{
	Use:   "arcanflows",
	Short: "ArcanFlows CLI - AI Automation from your terminal",
	Long: `ArcanFlows CLI lets you interact with AI agents, manage workflows,
query data tables, and analyze repositories — all from the command line.

Get started:
  arcanflows login          Log in to your ArcanFlows account
  arcanflows chat           Chat with an AI agent
  arcanflows agents list    List your agents
  arcanflows repo analyze   Analyze a code repository`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		render.Error(err.Error())
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringP("profile", "p", "", "config profile to use")
	rootCmd.PersistentFlags().StringP("format", "o", "", "output format: table, json, yaml, csv")
}

func initConfig() {
	var err error
	cfg, err = config.Load()
	if err != nil {
		render.Error(fmt.Sprintf("Failed to load config: %v", err))
		os.Exit(1)
	}
	creds, err = config.LoadCredentials()
	if err != nil {
		render.Error(fmt.Sprintf("Failed to load credentials: %v", err))
		os.Exit(1)
	}
	// Override profile from flag
	if p, _ := rootCmd.Flags().GetString("profile"); p != "" {
		cfg.ActiveProfile = p
	}
}

// loadProjectInstructions loads ARCANFLOWS.md if present and returns formatted context.
func loadProjectInstructions() string {
	content, path, err := repo.LoadInstructions(".")
	if err != nil || content == "" {
		return ""
	}
	relPath, _ := filepath.Rel(".", path)
	if relPath == "" {
		relPath = path
	}
	render.Info(fmt.Sprintf("Loaded %s", relPath))
	return "## Project Instructions (from ARCANFLOWS.md)\n\n" + content + "\n\n---\n\n"
}

// newAPIClient creates an API client from current config/creds, wiring token
// refresh so a 401 transparently refreshes and persists the new tokens (C15).
func newAPIClient() *api.Client {
	client := api.NewClient(cfg, creds)
	client.OnRefresh = func(accessToken, refreshToken string) {
		if creds.Profiles == nil {
			creds.Profiles = make(map[string]config.ProfileCredentials)
		}
		pc := creds.Profiles[cfg.ActiveProfile]
		pc.AccessToken = accessToken
		if refreshToken != "" {
			pc.RefreshToken = refreshToken
		}
		creds.Profiles[cfg.ActiveProfile] = pc
		_ = creds.Save()
	}
	return client
}

// requireAuth checks that credentials exist and returns a client.
func requireAuth() *api.Client {
	active := creds.GetActive(cfg.ActiveProfile)
	if active.AccessToken == "" && active.APIKey == "" {
		render.Error("Not logged in. Run 'arcanflows login' first.")
		os.Exit(1)
	}
	return newAPIClient()
}
