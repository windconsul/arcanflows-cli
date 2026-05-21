package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

var toolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "Manage and execute tools",
	Long:  `List available tools and execute them directly.`,
}

var toolsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available tools",
	RunE:  runToolsList,
}

var toolsExecCmd = &cobra.Command{
	Use:   "exec [tool-slug] ",
	Short: "Execute a tool",
	Long: `Execute a tool directly with JSON input.

Examples:
  arcanflows tools exec web-search --input '{"query":"ArcanFlows pricing"}'
  arcanflows tools exec calendar --input '{"action":"list_events"}'`,
	Args: cobra.ExactArgs(1),
	RunE: runToolsExec,
}

func init() {
	toolsExecCmd.Flags().String("input", "{}", "JSON input for the tool")

	toolsCmd.AddCommand(toolsListCmd)
	toolsCmd.AddCommand(toolsExecCmd)
	rootCmd.AddCommand(toolsCmd)
}

func runToolsList(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	spinner := render.NewSpinner("Fetching tools...")
	spinner.Start()
	var result struct {
		Tools []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Slug     string `json:"slug"`
			ToolType string `json:"tool_type"`
			Category string `json:"category"`
			Enabled  bool   `json:"is_enabled"`
		} `json:"tools"`
		Total int `json:"total"`
	}
	err := client.Get("/api/v1/tools", &result)
	spinner.Stop()

	if err != nil {
		return err
	}

	if len(result.Tools) == 0 {
		render.Info("No tools found.")
		return nil
	}

	format, _ := rootCmd.Flags().GetString("format")
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result.Tools)
	}

	headers := []string{"ID", "Name", "Slug", "Type", "Category"}
	var rows [][]string
	for _, t := range result.Tools {
		id := t.ID
		if len(id) > 8 {
			id = id[:8]
		}
		rows = append(rows, []string{id, t.Name, t.Slug, t.ToolType, t.Category})
	}

	render.Table(headers, rows)
	fmt.Fprintf(os.Stderr, "\n%s%d tools%s\n", render.Dim, len(result.Tools), render.Reset)
	return nil
}

func runToolsExec(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	inputStr, _ := cmd.Flags().GetString("input")
	var input map[string]interface{}
	if err := json.Unmarshal([]byte(inputStr), &input); err != nil {
		return fmt.Errorf("invalid JSON input: %w", err)
	}

	// First find tool by slug
	var toolsList struct {
		Tools []struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := client.Get("/api/v1/tools", &toolsList); err != nil {
		return err
	}

	var toolID, toolName string
	for _, t := range toolsList.Tools {
		if t.Slug == args[0] || t.ID == args[0] {
			toolID = t.ID
			toolName = t.Name
			break
		}
	}
	if toolID == "" {
		return fmt.Errorf("tool '%s' not found", args[0])
	}

	body := map[string]interface{}{"input": input}

	spinner := render.NewSpinner(fmt.Sprintf("Executing %s...", toolName))
	spinner.Start()
	var result map[string]interface{}
	err := client.Post("/api/v1/tools/"+toolID+"/execute", body, &result)
	spinner.Stop()

	if err != nil {
		return err
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}
