package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/arcanflows/cli/internal/memory"
	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

var contextCmd = &cobra.Command{
	Use:     "context",
	Aliases: []string{"ctx", "memory"},
	Short:   "Manage persistent project context/memory",
	Long: `Add, list, and remove persistent facts about your project.
These are automatically included in every prompt to give the agent
project-specific knowledge across sessions.

Examples:
  arcanflows context add "Redis is on port 6380, not default 6379"
  arcanflows context add "We use pytest with async fixtures"
  arcanflows context list
  arcanflows context remove 1
  arcanflows context clear`,
}

var ctxAddCmd = &cobra.Command{
	Use:   "add [text]",
	Short: "Add a memory entry",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runCtxAdd,
}

var ctxListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all memory entries",
	RunE:  runCtxList,
}

var ctxRemoveCmd = &cobra.Command{
	Use:   "remove [id]",
	Short: "Remove a memory entry by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runCtxRemove,
}

var ctxClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear all memory entries",
	RunE:  runCtxClear,
}

func init() {
	contextCmd.AddCommand(ctxAddCmd)
	contextCmd.AddCommand(ctxListCmd)
	contextCmd.AddCommand(ctxRemoveCmd)
	contextCmd.AddCommand(ctxClearCmd)
	rootCmd.AddCommand(contextCmd)
}

func runCtxAdd(cmd *cobra.Command, args []string) error {
	store, err := memory.Load()
	if err != nil {
		return err
	}

	content := strings.Join(args, " ")
	id := store.Add(content, "user")

	if err := store.Save(); err != nil {
		return err
	}

	render.Success(fmt.Sprintf("Memory #%d added (%d total)", id, store.Count()))
	return nil
}

func runCtxList(cmd *cobra.Command, args []string) error {
	store, err := memory.Load()
	if err != nil {
		return err
	}

	if store.Count() == 0 {
		render.Info("No memory entries. Add one with: arcanflows context add \"...\"")
		return nil
	}

	fmt.Fprintf(os.Stderr, "\n%sProject Memory (%d entries):%s\n\n", render.Bold, store.Count(), render.Reset)
	for _, e := range store.Entries {
		source := ""
		if e.Source == "auto" {
			source = render.Dim + " (auto)" + render.Reset
		}
		fmt.Fprintf(os.Stderr, "  %s%d)%s %s%s\n", render.Cyan, e.ID, render.Reset, e.Content, source)
	}
	fmt.Fprintln(os.Stderr)
	return nil
}

func runCtxRemove(cmd *cobra.Command, args []string) error {
	store, err := memory.Load()
	if err != nil {
		return err
	}

	var id int
	if _, err := fmt.Sscanf(args[0], "%d", &id); err != nil {
		return fmt.Errorf("invalid ID: %s", args[0])
	}

	if !store.Remove(id) {
		return fmt.Errorf("memory #%d not found", id)
	}

	if err := store.Save(); err != nil {
		return err
	}

	render.Success(fmt.Sprintf("Memory #%d removed (%d remaining)", id, store.Count()))
	return nil
}

func runCtxClear(cmd *cobra.Command, args []string) error {
	store, err := memory.Load()
	if err != nil {
		return err
	}

	count := store.Count()
	store.Clear()

	if err := store.Save(); err != nil {
		return err
	}

	render.Success(fmt.Sprintf("Cleared %d memory entries", count))
	return nil
}
