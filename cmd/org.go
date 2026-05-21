package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/arcanflows/cli/internal/config"
	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

var orgCmd = &cobra.Command{
	Use:     "org",
	Aliases: []string{"tenant", "organization"},
	Short:   "Manage organization/tenant context",
	Long: `List available organizations and switch between them.
Your user account may have access to multiple organizations (tenants).

Examples:
  arcanflows org list                     # See all organizations you can access
  arcanflows org switch "Client Corp"     # Switch to a different organization
  arcanflows org switch abc123-uuid       # Switch by tenant ID`,
}

var orgListCmd = &cobra.Command{
	Use:   "list",
	Short: "List organizations you have access to",
	RunE:  runOrgList,
}

var orgSwitchCmd = &cobra.Command{
	Use:   "switch [name-or-id]",
	Short: "Switch to a different organization",
	Args:  cobra.ExactArgs(1),
	RunE:  runOrgSwitch,
}

var orgCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show current organization",
	RunE:  runOrgCurrent,
}

func init() {
	orgCmd.AddCommand(orgListCmd)
	orgCmd.AddCommand(orgSwitchCmd)
	orgCmd.AddCommand(orgCurrentCmd)
	rootCmd.AddCommand(orgCmd)
}

type tenantInfo struct {
	TenantID   string `json:"tenant_id"`
	TenantName string `json:"tenant_name"`
	TenantSlug string `json:"tenant_slug"`
	UserID     string `json:"user_id"`
	Role       string `json:"role"`
	Status     string `json:"status"`
	IsCurrent  bool   `json:"is_current"`
}

func runOrgList(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	spinner := render.NewSpinner("Fetching organizations...")
	spinner.Start()
	var tenantsResp struct {
		Tenants []tenantInfo `json:"tenants"`
	}
	// Try object response first, fall back to array
	err := client.Get("/api/v1/auth/tenants", &tenantsResp)
	spinner.Stop()

	if err != nil {
		return err
	}

	tenants := tenantsResp.Tenants
	if len(tenants) == 0 {
		render.Info("No organizations found.")
		return nil
	}

	format, _ := rootCmd.Flags().GetString("format")
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(tenants)
	}

	headers := []string{"Name", "ID", "Role", "Active"}
	var rows [][]string
	for _, t := range tenants {
		active := ""
		if t.IsCurrent {
			active = render.Green + "  *" + render.Reset
		}
		id := t.TenantID
		if len(id) > 12 {
			id = id[:12] + "..."
		}
		rows = append(rows, []string{t.TenantName, id, t.Role, active})
	}

	render.Table(headers, rows)
	fmt.Fprintf(os.Stderr, "\n%s%d organizations%s\n", render.Dim, len(tenants), render.Reset)
	return nil
}

func runOrgSwitch(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	target := args[0]

	// First get list of tenants to resolve name -> ID
	spinner := render.NewSpinner("Fetching organizations...")
	spinner.Start()
	var tenantsResp struct {
		Tenants []tenantInfo `json:"tenants"`
	}
	err := client.Get("/api/v1/auth/tenants", &tenantsResp)
	spinner.Stop()

	if err != nil {
		return err
	}

	tenants := tenantsResp.Tenants
	var targetID, targetName string

	// Try exact ID match first
	for _, t := range tenants {
		if t.TenantID == target {
			targetID = t.TenantID
			targetName = t.TenantName
			break
		}
	}

	// Try name match (case-insensitive)
	if targetID == "" {
		for _, t := range tenants {
			if strings.EqualFold(t.TenantName, target) {
				targetID = t.TenantID
				targetName = t.TenantName
				break
			}
		}
	}

	// Try partial name match
	if targetID == "" {
		for _, t := range tenants {
			if strings.Contains(strings.ToLower(t.TenantName), strings.ToLower(target)) {
				targetID = t.TenantID
				targetName = t.TenantName
				break
			}
		}
	}

	if targetID == "" {
		render.Error(fmt.Sprintf("Organization '%s' not found. Available:", target))
		for _, t := range tenants {
			fmt.Fprintf(os.Stderr, "  - %s (%s)\n", t.TenantName, t.TenantID)
		}
		return fmt.Errorf("organization not found")
	}

	// Call switch-tenant API
	spinner = render.NewSpinner(fmt.Sprintf("Switching to %s...", targetName))
	spinner.Start()

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		User         struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"user"`
		Tenant struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"tenant"`
	}

	err = client.Post("/api/v1/auth/switch-tenant", map[string]string{
		"tenant_id": targetID,
	}, &result)
	spinner.Stop()

	if err != nil {
		return fmt.Errorf("switch failed: %w", err)
	}

	// Update credentials with new JWT
	creds.Profiles[cfg.ActiveProfile] = config.ProfileCredentials{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		Email:        result.User.Email,
		UserID:       result.User.ID,
		Role:         result.User.Role,
	}
	if err := creds.Save(); err != nil {
		return fmt.Errorf("saving credentials: %w", err)
	}

	// Update org name in config
	profile := cfg.Profiles[cfg.ActiveProfile]
	profile.Organization = result.Tenant.Name
	cfg.Profiles[cfg.ActiveProfile] = profile
	cfg.Save()

	render.Success(fmt.Sprintf("Switched to %s (role: %s)", result.Tenant.Name, result.User.Role))
	return nil
}

func runOrgCurrent(cmd *cobra.Command, args []string) error {
	profile := cfg.Profiles[cfg.ActiveProfile]
	active := creds.GetActive(cfg.ActiveProfile)

	if profile.Organization == "" && active.AccessToken == "" {
		render.Error("Not logged in. Run 'arcanflows login' first.")
		return nil
	}

	fmt.Printf("\n  %sOrganization:%s %s\n", render.Dim, render.Reset, profile.Organization)
	fmt.Printf("  %sUser:%s         %s\n", render.Dim, render.Reset, active.Email)
	fmt.Printf("  %sRole:%s         %s\n", render.Dim, render.Reset, active.Role)
	fmt.Printf("  %sServer:%s       %s\n", render.Dim, render.Reset, profile.Server)
	fmt.Println()
	return nil
}
