package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/arcanflows/cli/internal/api"
	"github.com/arcanflows/cli/internal/config"
	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to your ArcanFlows account",
	Long:  `Authenticate with email/password or API key. Credentials are stored securely in ~/.arcanflows/credentials.yaml.`,
	RunE:  runLogin,
}

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Log out and clear stored credentials",
	RunE:  runLogout,
}

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Display current user and organization",
	RunE:  runWhoami,
}

func init() {
	loginCmd.Flags().String("server", "", "ArcanFlows API server URL")
	loginCmd.Flags().String("api-key", "", "authenticate with API key instead of email/password")
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(logoutCmd)
	rootCmd.AddCommand(whoamiCmd)
}

func runLogin(cmd *cobra.Command, args []string) error {
	server, _ := cmd.Flags().GetString("server")
	apiKey, _ := cmd.Flags().GetString("api-key")

	if server == "" {
		server = cfg.ActiveServer()
	}

	render.Header("ArcanFlows Login", server)

	if apiKey != "" {
		// API key auth
		client := api.NewClientWithToken(server, "", apiKey)
		// Verify the key works by making a test request
		agents, err := client.ListAgents()
		if err != nil {
			return fmt.Errorf("API key validation failed: %w", err)
		}

		// Save credentials
		creds.Profiles[cfg.ActiveProfile] = config.ProfileCredentials{
			APIKey: apiKey,
		}
		if err := creds.Save(); err != nil {
			return fmt.Errorf("saving credentials: %w", err)
		}

		// Update server in config
		if p, ok := cfg.Profiles[cfg.ActiveProfile]; ok {
			p.Server = server
			cfg.Profiles[cfg.ActiveProfile] = p
		}
		cfg.Save()

		render.Success(fmt.Sprintf("Logged in with API key (%d agents accessible)", len(agents)))
		return nil
	}

	// Interactive email/password login
	reader := bufio.NewReader(os.Stdin)

	fmt.Fprint(os.Stderr, "  Email: ")
	email, _ := reader.ReadString('\n')
	email = strings.TrimSpace(email)

	fmt.Fprint(os.Stderr, "  Password: ")
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr) // newline after hidden password
	if err != nil {
		return fmt.Errorf("reading password: %w", err)
	}
	password := string(passwordBytes)

	spinner := render.NewSpinner("Authenticating...")
	spinner.Start()

	client := api.NewClientWithToken(server, "", "")
	result, err := client.Login(email, password)
	spinner.Stop()

	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	// Save credentials
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

	// Update config
	if p, ok := cfg.Profiles[cfg.ActiveProfile]; ok {
		p.Server = server
		p.Organization = result.Tenant.Name
		cfg.Profiles[cfg.ActiveProfile] = p
	}
	cfg.Save()

	name := result.User.FirstName
	if name == "" {
		name = result.User.Email
	}
	render.Success(fmt.Sprintf("Logged in as %s (%s)", name, result.Tenant.Name))
	return nil
}

func runLogout(cmd *cobra.Command, args []string) error {
	delete(creds.Profiles, cfg.ActiveProfile)
	if err := creds.Save(); err != nil {
		return err
	}
	render.Success("Logged out")
	return nil
}

func runWhoami(cmd *cobra.Command, args []string) error {
	active := creds.GetActive(cfg.ActiveProfile)
	if active.AccessToken == "" && active.APIKey == "" {
		render.Error("Not logged in. Run 'arcanflows login' first.")
		return nil
	}

	profile := cfg.Profiles[cfg.ActiveProfile]

	fmt.Println()
	if active.Email != "" {
		fmt.Printf("  %sUser:%s      %s\n", render.Dim, render.Reset, active.Email)
	}
	fmt.Printf("  %sRole:%s      %s\n", render.Dim, render.Reset, active.Role)
	if profile.Organization != "" {
		fmt.Printf("  %sOrg:%s       %s\n", render.Dim, render.Reset, profile.Organization)
	}
	fmt.Printf("  %sServer:%s    %s\n", render.Dim, render.Reset, profile.Server)
	fmt.Printf("  %sProfile:%s   %s\n", render.Dim, render.Reset, cfg.ActiveProfile)
	if active.APIKey != "" {
		fmt.Printf("  %sAuth:%s      API Key\n", render.Dim, render.Reset)
	} else {
		fmt.Printf("  %sAuth:%s      JWT Token\n", render.Dim, render.Reset)
	}
	fmt.Println()
	return nil
}
