package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

var workflowsCmd = &cobra.Command{
	Use:     "workflows",
	Aliases: []string{"wf"},
	Short:   "Manage workflows",
	Long:    `List, view, trigger, and manage visual workflows.`,
}

var wfListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all workflows",
	RunE:  runWfList,
}

var wfGetCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get workflow details",
	Args:  cobra.ExactArgs(1),
	RunE:  runWfGet,
}

var wfRunCmd = &cobra.Command{
	Use:   "run [id]",
	Short: "Trigger a workflow execution",
	Long: `Trigger a workflow manually with optional input data.

Examples:
  arcanflows workflows run 45e0f665
  arcanflows workflows run 45e0f665 --data '{"email":"test@example.com"}'
  arcanflows workflows run 45e0f665 --data-file input.json`,
	Args: cobra.ExactArgs(1),
	RunE: runWfRun,
}

var wfExecutionsCmd = &cobra.Command{
	Use:   "executions [workflow-id]",
	Short: "View workflow executions",
	Args:  cobra.ExactArgs(1),
	RunE:  runWfExecutions,
}

var wfPublishCmd = &cobra.Command{
	Use:   "publish [id]",
	Short: "Publish a workflow",
	Args:  cobra.ExactArgs(1),
	RunE:  runWfPublish,
}

func init() {
	wfRunCmd.Flags().String("data", "", "JSON input data")
	wfRunCmd.Flags().String("data-file", "", "read input data from JSON file")
	wfListCmd.Flags().String("status", "", "filter by status: draft, published, archived")
	wfExecutionsCmd.Flags().Int("last", 10, "number of recent executions")

	workflowsCmd.AddCommand(wfListCmd)
	workflowsCmd.AddCommand(wfGetCmd)
	workflowsCmd.AddCommand(wfRunCmd)
	workflowsCmd.AddCommand(wfExecutionsCmd)
	workflowsCmd.AddCommand(wfPublishCmd)
	rootCmd.AddCommand(workflowsCmd)
}

func runWfList(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	spinner := render.NewSpinner("Fetching workflows...")
	spinner.Start()
	var result struct {
		Workflows []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			Slug           string `json:"slug"`
			Status         string `json:"status"`
			TriggerType    string `json:"trigger_type"`
			ExecutionCount int    `json:"execution_count"`
		} `json:"workflows"`
		Total int `json:"total"`
	}
	err := client.Get("/api/v1/workflows", &result)
	spinner.Stop()

	if err != nil {
		return err
	}

	if len(result.Workflows) == 0 {
		render.Info("No workflows found. Create one at your ArcanFlows console.")
		return nil
	}

	format, _ := rootCmd.Flags().GetString("format")
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result.Workflows)
	}

	headers := []string{"ID", "Name", "Trigger", "Status", "Executions"}
	var rows [][]string
	for _, w := range result.Workflows {
		id := w.ID
		if len(id) > 8 {
			id = id[:8]
		}
		status := w.Status
		switch status {
		case "published":
			status = render.Green + status + render.Reset
		case "draft":
			status = render.Yellow + status + render.Reset
		default:
			status = render.Dim + status + render.Reset
		}
		rows = append(rows, []string{id, w.Name, w.TriggerType, status, fmt.Sprintf("%d", w.ExecutionCount)})
	}

	render.Table(headers, rows)
	fmt.Fprintf(os.Stderr, "\n%s%d workflows%s\n", render.Dim, len(result.Workflows), render.Reset)
	return nil
}

func runWfGet(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	var wf struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Slug           string `json:"slug"`
		Description    string `json:"description"`
		Status         string `json:"status"`
		TriggerType    string `json:"trigger_type"`
		ExecutionCount int    `json:"execution_count"`
		SuccessCount   int    `json:"success_count"`
		FailureCount   int    `json:"failure_count"`
		Version        int    `json:"version"`
	}
	if err := client.Get("/api/v1/workflows/"+args[0], &wf); err != nil {
		return err
	}

	format, _ := rootCmd.Flags().GetString("format")
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(wf)
	}

	fmt.Printf("\n  %sName:%s         %s\n", render.Dim, render.Reset, wf.Name)
	fmt.Printf("  %sID:%s           %s\n", render.Dim, render.Reset, wf.ID)
	fmt.Printf("  %sSlug:%s         %s\n", render.Dim, render.Reset, wf.Slug)
	fmt.Printf("  %sStatus:%s       %s\n", render.Dim, render.Reset, wf.Status)
	fmt.Printf("  %sTrigger:%s      %s\n", render.Dim, render.Reset, wf.TriggerType)
	fmt.Printf("  %sVersion:%s      %d\n", render.Dim, render.Reset, wf.Version)
	fmt.Printf("  %sExecutions:%s   %d (success: %d, failed: %d)\n", render.Dim, render.Reset, wf.ExecutionCount, wf.SuccessCount, wf.FailureCount)
	if wf.Description != "" {
		fmt.Printf("  %sDescription:%s  %s\n", render.Dim, render.Reset, wf.Description)
	}
	fmt.Println()
	return nil
}

func runWfRun(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	dataStr, _ := cmd.Flags().GetString("data")
	dataFile, _ := cmd.Flags().GetString("data-file")

	var inputData map[string]interface{}

	if dataFile != "" {
		content, err := os.ReadFile(dataFile)
		if err != nil {
			return fmt.Errorf("reading data file: %w", err)
		}
		if err := json.Unmarshal(content, &inputData); err != nil {
			return fmt.Errorf("parsing data file: %w", err)
		}
	} else if dataStr != "" {
		if err := json.Unmarshal([]byte(dataStr), &inputData); err != nil {
			return fmt.Errorf("parsing data: %w", err)
		}
	}

	body := map[string]interface{}{}
	if inputData != nil {
		body["input_data"] = inputData
	}

	spinner := render.NewSpinner("Triggering workflow...")
	spinner.Start()
	var result struct {
		ExecutionID string `json:"execution_id"`
		Status      string `json:"status"`
		Message     string `json:"message"`
	}
	err := client.Post("/api/v1/workflows/"+args[0]+"/trigger", body, &result)
	spinner.Stop()

	if err != nil {
		return err
	}

	render.Success(fmt.Sprintf("Workflow triggered (execution: %s)", result.ExecutionID))
	if result.Status != "" {
		fmt.Fprintf(os.Stderr, "  %sStatus:%s %s\n", render.Dim, render.Reset, result.Status)
	}
	return nil
}

func runWfExecutions(cmd *cobra.Command, args []string) error {
	client := requireAuth()
	last, _ := cmd.Flags().GetInt("last")

	var result struct {
		Executions []struct {
			ID        string `json:"id"`
			Status    string `json:"status"`
			StartedAt string `json:"started_at"`
			Duration  int    `json:"duration_ms"`
			Error     string `json:"error,omitempty"`
		} `json:"executions"`
	}
	path := fmt.Sprintf("/api/v1/workflow-executions?workflow_id=%s&limit=%d", args[0], last)
	if err := client.Get(path, &result); err != nil {
		return err
	}

	if len(result.Executions) == 0 {
		render.Info("No executions found.")
		return nil
	}

	headers := []string{"ID", "Status", "Started", "Duration"}
	var rows [][]string
	for _, e := range result.Executions {
		id := e.ID
		if len(id) > 8 {
			id = id[:8]
		}
		status := e.Status
		switch {
		case strings.Contains(status, "completed") || strings.Contains(status, "success"):
			status = render.Green + status + render.Reset
		case strings.Contains(status, "failed") || strings.Contains(status, "error"):
			status = render.Red + status + render.Reset
		case strings.Contains(status, "running"):
			status = render.Cyan + status + render.Reset
		}
		started := e.StartedAt
		if len(started) > 19 {
			started = started[:19]
		}
		duration := fmt.Sprintf("%dms", e.Duration)
		rows = append(rows, []string{id, status, started, duration})
	}

	render.Table(headers, rows)
	return nil
}

func runWfPublish(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	spinner := render.NewSpinner("Publishing workflow...")
	spinner.Start()
	err := client.Post("/api/v1/workflows/"+args[0]+"/publish", nil, nil)
	spinner.Stop()

	if err != nil {
		return err
	}

	render.Success("Workflow published")
	return nil
}
