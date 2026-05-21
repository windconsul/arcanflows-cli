package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/arcanflows/cli/internal/hooks"
	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Manage pre/post command hooks",
	Long: `Configure hooks that run automatically before or after CLI operations.
Hooks are defined in .arcanflows/hooks.yaml in your project directory.

Examples:
  arcanflows hooks init        Create a starter hooks.yaml
  arcanflows hooks list        Show all configured hooks
  arcanflows hooks test <event> Run hooks for a specific event`,
}

var hooksInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a starter hooks.yaml",
	RunE:  runHooksInit,
}

var hooksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured hooks",
	RunE:  runHooksList,
}

var hooksTestCmd = &cobra.Command{
	Use:   "test [event]",
	Short: "Test-run hooks for a specific event",
	Long: `Run hooks for a specific event to verify they work.

Available events:
  pre_chat, post_chat, pre_edit, post_edit,
  pre_create, post_create, pre_delete, post_delete,
  pre_run, post_run, pre_commit, post_commit`,
	Args: cobra.ExactArgs(1),
	RunE: runHooksTest,
}

func init() {
	hooksCmd.AddCommand(hooksInitCmd)
	hooksCmd.AddCommand(hooksListCmd)
	hooksCmd.AddCommand(hooksTestCmd)

	hooksInitCmd.Flags().Bool("force", false, "overwrite existing hooks.yaml")

	rootCmd.AddCommand(hooksCmd)
}

func runHooksInit(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	path := hooks.DefaultConfigPath()

	if _, err := os.Stat(path); err == nil && !force {
		render.Warning(fmt.Sprintf("%s already exists. Use --force to overwrite.", path))
		return nil
	}

	// Create directory
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	content := hooks.GenerateTemplate()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing hooks.yaml: %w", err)
	}

	render.Success(fmt.Sprintf("Created %s", path))
	fmt.Fprintf(os.Stderr, "  %sEdit this file to add your project hooks.%s\n\n", render.Dim, render.Reset)
	return nil
}

func runHooksList(cmd *cobra.Command, args []string) error {
	mgr := hooks.Load()

	if mgr.Count() == 0 {
		render.Info("No hooks configured. Create hooks with: arcanflows hooks init")
		return nil
	}

	fmt.Fprintf(os.Stderr, "\n%sConfigured Hooks:%s %s\n\n", render.Bold, render.Reset, mgr.Summary())

	// Load raw config to display details
	path := hooks.DefaultConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	// Display the YAML content (non-comment lines only)
	lines := splitLines(string(data))
	for _, line := range lines {
		if line == "" || line[0] == '#' {
			continue
		}
		fmt.Fprintf(os.Stderr, "  %s\n", line)
	}
	fmt.Fprintln(os.Stderr)
	return nil
}

func runHooksTest(cmd *cobra.Command, args []string) error {
	event := hooks.HookEvent(args[0])

	// Validate event name
	validEvents := []hooks.HookEvent{
		hooks.PreChat, hooks.PostChat,
		hooks.PreEdit, hooks.PostEdit,
		hooks.PreCreate, hooks.PostCreate,
		hooks.PreDelete, hooks.PostDelete,
		hooks.PreRun, hooks.PostRun,
		hooks.PreCommit, hooks.PostCommit,
	}
	valid := false
	for _, e := range validEvents {
		if event == e {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("unknown event: %s\nValid events: pre_chat, post_chat, pre_edit, post_edit, pre_create, post_create, pre_delete, post_delete, pre_run, post_run, pre_commit, post_commit", args[0])
	}

	mgr := hooks.Load()

	if !mgr.HasHooks(event) {
		render.Info(fmt.Sprintf("No hooks configured for '%s'", event))
		return nil
	}

	// Get current branch for test vars
	branch := runGitCmd("rev-parse", "--abbrev-ref", "HEAD")
	cwd, _ := os.Getwd()

	vars := hooks.Variables{
		File:        "test-file.txt",
		ProjectRoot: cwd,
		Branch:      branch,
		Agent:       "test-agent",
	}

	render.Info(fmt.Sprintf("Testing hooks for '%s'...", event))
	output, err := mgr.Run(event, vars)
	if err != nil {
		render.Error(fmt.Sprintf("Hook failed: %v", err))
		return nil
	}

	if output != "" {
		fmt.Fprintf(os.Stderr, "\n%sHook output:%s\n%s\n", render.Bold, render.Reset, output)
	}

	render.Success("All hooks passed")
	return nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
