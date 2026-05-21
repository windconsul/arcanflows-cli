package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

var agentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Manage AI agents",
	Long:  `List, create, update, and delete AI agents.`,
}

var agentsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all agents",
	RunE:  runAgentsList,
}

var agentsGetCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get agent details",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentsGet,
}

var agentsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new agent",
	RunE:  runAgentsCreate,
}

var agentsDeleteCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete an agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentsDelete,
}

func init() {
	agentsCreateCmd.Flags().String("name", "", "agent name (required)")
	agentsCreateCmd.Flags().String("model", "gpt-4o", "primary model")
	agentsCreateCmd.Flags().String("system-prompt", "", "system prompt text")
	agentsCreateCmd.Flags().String("system-prompt-file", "", "read system prompt from file")
	agentsCreateCmd.Flags().Float64("temperature", 0.7, "temperature (0.0-2.0)")
	agentsCreateCmd.Flags().Int("max-tokens", 2048, "max output tokens")
	agentsCreateCmd.Flags().String("language", "en", "language code (en, es, pt)")
	agentsCreateCmd.MarkFlagRequired("name")

	agentsCmd.AddCommand(agentsListCmd)
	agentsCmd.AddCommand(agentsGetCmd)
	agentsCmd.AddCommand(agentsCreateCmd)
	agentsCmd.AddCommand(agentsDeleteCmd)
	rootCmd.AddCommand(agentsCmd)
}

func runAgentsList(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	spinner := render.NewSpinner("Fetching agents...")
	spinner.Start()
	agents, err := client.ListAgents()
	spinner.Stop()

	if err != nil {
		return err
	}

	if len(agents) == 0 {
		render.Info("No agents found. Create one with 'arcanflows agents create'")
		return nil
	}

	// Check output format
	format, _ := rootCmd.Flags().GetString("format")
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(agents)
	}

	headers := []string{"ID", "Name", "Model", "Status"}
	var rows [][]string
	for _, a := range agents {
		id := a.ID
		if len(id) > 8 {
			id = id[:8]
		}
		status := a.Status
		if status == "active" {
			status = render.Green + status + render.Reset
		} else {
			status = render.Dim + status + render.Reset
		}
		rows = append(rows, []string{id, a.Name, a.Model, status})
	}

	render.Table(headers, rows)
	fmt.Fprintf(os.Stderr, "\n%s%d agents%s\n", render.Dim, len(agents), render.Reset)
	return nil
}

func runAgentsGet(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	agent, err := client.GetAgent(args[0])
	if err != nil {
		return err
	}

	format, _ := rootCmd.Flags().GetString("format")
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(agent)
	}

	fmt.Printf("\n  %sName:%s        %s\n", render.Dim, render.Reset, agent.Name)
	fmt.Printf("  %sID:%s          %s\n", render.Dim, render.Reset, agent.ID)
	fmt.Printf("  %sSlug:%s        %s\n", render.Dim, render.Reset, agent.Slug)
	fmt.Printf("  %sModel:%s       %s\n", render.Dim, render.Reset, agent.Model)
	fmt.Printf("  %sStatus:%s      %s\n", render.Dim, render.Reset, agent.Status)
	if agent.Description != "" {
		fmt.Printf("  %sDescription:%s %s\n", render.Dim, render.Reset, agent.Description)
	}
	fmt.Println()
	return nil
}

func runAgentsCreate(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	name, _ := cmd.Flags().GetString("name")
	model, _ := cmd.Flags().GetString("model")
	systemPrompt, _ := cmd.Flags().GetString("system-prompt")
	promptFile, _ := cmd.Flags().GetString("system-prompt-file")
	temperature, _ := cmd.Flags().GetFloat64("temperature")
	maxTokens, _ := cmd.Flags().GetInt("max-tokens")
	language, _ := cmd.Flags().GetString("language")

	// Read system prompt from file if specified
	if promptFile != "" {
		data, err := os.ReadFile(promptFile)
		if err != nil {
			return fmt.Errorf("reading prompt file: %w", err)
		}
		systemPrompt = string(data)
	}

	req := map[string]interface{}{
		"name":          name,
		"primary_model": model,
		"temperature":   temperature,
		"max_tokens":    maxTokens,
		"language":      language,
	}
	if systemPrompt != "" {
		req["system_prompt"] = systemPrompt
	}

	spinner := render.NewSpinner("Creating agent...")
	spinner.Start()
	agent, err := client.CreateAgent(req)
	spinner.Stop()

	if err != nil {
		return err
	}

	render.Success(fmt.Sprintf("Agent created: %s (id: %s)", agent.Name, agent.ID))
	return nil
}

func runAgentsDelete(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	spinner := render.NewSpinner("Deleting agent...")
	spinner.Start()
	err := client.Delete("/api/v1/agents/" + args[0])
	spinner.Stop()

	if err != nil {
		return err
	}

	render.Success("Agent deleted")
	return nil
}
