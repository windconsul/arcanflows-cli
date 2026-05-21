package cmd

import (
	"fmt"
	"os"

	"github.com/arcanflows/cli/internal/config"
	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI configuration",
	Long:  `View and modify ArcanFlows CLI configuration, manage profiles.`,
}

var configViewCmd = &cobra.Command{
	Use:   "view",
	Short: "View current configuration",
	RunE:  runConfigView,
}

var configSetCmd = &cobra.Command{
	Use:   "set [key] [value]",
	Short: "Set a configuration value",
	Long: `Set a configuration value for the active profile.

Examples:
  arcanflows config set server https://api.arcanflows.com
  arcanflows config set default-agent code-assistant
  arcanflows config set default-model gpt-4o
  arcanflows config set output-format json`,
	Args: cobra.ExactArgs(2),
	RunE: runConfigSet,
}

var configProfileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage configuration profiles",
}

var configProfileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all profiles",
	RunE:  runConfigProfileList,
}

var configProfileCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfigProfileCreate,
}

var configProfileUseCmd = &cobra.Command{
	Use:   "use [name]",
	Short: "Switch to a profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfigProfileUse,
}

func init() {
	configProfileCreateCmd.Flags().String("server", "", "server URL for this profile")

	configProfileCmd.AddCommand(configProfileListCmd)
	configProfileCmd.AddCommand(configProfileCreateCmd)
	configProfileCmd.AddCommand(configProfileUseCmd)

	configCmd.AddCommand(configViewCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configProfileCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigView(cmd *cobra.Command, args []string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key, value := args[0], args[1]
	profile := cfg.Profiles[cfg.ActiveProfile]

	switch key {
	case "server":
		profile.Server = value
	case "default-agent":
		profile.DefaultAgent = value
	case "default-model":
		profile.DefaultModel = value
	case "output-format":
		profile.OutputFormat = value
	case "organization":
		profile.Organization = value
	default:
		return fmt.Errorf("unknown config key: %s (valid: server, default-agent, default-model, output-format, organization)", key)
	}

	cfg.Profiles[cfg.ActiveProfile] = profile
	if err := cfg.Save(); err != nil {
		return err
	}

	render.Success(fmt.Sprintf("Set %s = %s (profile: %s)", key, value, cfg.ActiveProfile))
	return nil
}

func runConfigProfileList(cmd *cobra.Command, args []string) error {
	headers := []string{"Name", "Server", "Active"}
	var rows [][]string

	for name, p := range cfg.Profiles {
		active := ""
		if name == cfg.ActiveProfile {
			active = render.Green + "  *" + render.Reset
		}
		rows = append(rows, []string{name, p.Server, active})
	}

	render.Table(headers, rows)
	return nil
}

func runConfigProfileCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	if _, exists := cfg.Profiles[name]; exists {
		return fmt.Errorf("profile '%s' already exists", name)
	}

	server, _ := cmd.Flags().GetString("server")
	if server == "" {
		server = "https://api.arcanflows.com"
	}

	cfg.Profiles[name] = config.Profile{
		Server:       server,
		OutputFormat: "table",
	}
	if err := cfg.Save(); err != nil {
		return err
	}

	render.Success(fmt.Sprintf("Profile '%s' created (%s)", name, server))

	// Also initialize empty credentials for this profile
	if _, exists := creds.Profiles[name]; !exists {
		creds.Profiles[name] = config.ProfileCredentials{}
		creds.Save()
	}

	fmt.Fprintf(os.Stderr, "  Run: arcanflows config profile use %s && arcanflows login\n", name)
	return nil
}

func runConfigProfileUse(cmd *cobra.Command, args []string) error {
	name := args[0]
	if _, exists := cfg.Profiles[name]; !exists {
		return fmt.Errorf("profile '%s' not found (run: arcanflows config profile list)", name)
	}

	cfg.ActiveProfile = name
	if err := cfg.Save(); err != nil {
		return err
	}

	profile := cfg.Profiles[name]
	render.Success(fmt.Sprintf("Switched to profile '%s' (%s)", name, profile.Server))
	return nil
}
