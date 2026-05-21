package cmd

import (
	"fmt"
	"os"

	"github.com/arcanflows/cli/internal/render"
	"github.com/arcanflows/cli/internal/repo"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize ARCANFLOWS.md for the current project",
	Long: `Scan the current project and generate a starter ARCANFLOWS.md file.
This file tells the ArcanFlows CLI about your project conventions,
architecture, and rules — like CLAUDE.md for Claude Code.

The generated file is a template. Edit it to add your specific
conventions, current focus, and rules.`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().Bool("force", false, "overwrite existing ARCANFLOWS.md")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")

	// Check if file already exists
	for _, name := range []string{"ARCANFLOWS.md", ".arcanflows.md"} {
		if _, err := os.Stat(name); err == nil && !force {
			render.Warning(fmt.Sprintf("%s already exists. Use --force to overwrite.", name))
			return nil
		}
	}

	// Scan project
	spinner := render.NewSpinner("Scanning project...")
	spinner.Start()

	scanner := repo.NewScanner(".",
		cfg.Settings.RepoMaxFiles,
		int64(cfg.Settings.RepoMaxFileSize),
		cfg.Settings.RepoExclude,
	)
	scan, err := scanner.Scan()
	spinner.Stop()

	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	// Generate template
	content := repo.GenerateTemplate(scan)

	// Write file
	if err := os.WriteFile("ARCANFLOWS.md", []byte(content), 0644); err != nil {
		return fmt.Errorf("writing ARCANFLOWS.md: %w", err)
	}

	render.Success("Created ARCANFLOWS.md")
	fmt.Fprintf(os.Stderr, "\n  %sDetected:%s %d files, %d languages", render.Dim, render.Reset, scan.TotalFiles, len(scan.Languages))
	if len(scan.Frameworks) > 0 {
		fmt.Fprintf(os.Stderr, ", %s", fmt.Sprintf("%v", scan.Frameworks))
	}
	fmt.Fprintf(os.Stderr, "\n\n  Edit ARCANFLOWS.md to add your conventions, rules, and current focus.\n")
	fmt.Fprintf(os.Stderr, "  The CLI will auto-load it on every command.\n\n")
	return nil
}
