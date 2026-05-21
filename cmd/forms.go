package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

var formsCmd = &cobra.Command{
	Use:   "forms",
	Short: "Manage forms and submissions",
	Long:  `List forms, view submissions, and submit form data.`,
}

var formsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all forms",
	RunE:  runFormsList,
}

var formsGetCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get form details",
	Args:  cobra.ExactArgs(1),
	RunE:  runFormsGet,
}

var formsSubmissionsCmd = &cobra.Command{
	Use:   "submissions [form-id]",
	Short: "View form submissions",
	Args:  cobra.ExactArgs(1),
	RunE:  runFormsSubmissions,
}

var formsSubmitCmd = &cobra.Command{
	Use:   "submit",
	Short: "Submit a form (public)",
	Long: `Submit data to a published form by slug.

Examples:
  arcanflows forms submit --slug contact-form --data '{"name":"John","email":"john@example.com"}'`,
	RunE: runFormsSubmit,
}

func init() {
	formsSubmitCmd.Flags().String("slug", "", "form slug (required)")
	formsSubmitCmd.Flags().String("data", "", "JSON form data (required)")
	formsSubmitCmd.MarkFlagRequired("slug")
	formsSubmitCmd.MarkFlagRequired("data")
	formsSubmissionsCmd.Flags().Int("last", 10, "number of recent submissions")

	formsCmd.AddCommand(formsListCmd)
	formsCmd.AddCommand(formsGetCmd)
	formsCmd.AddCommand(formsSubmissionsCmd)
	formsCmd.AddCommand(formsSubmitCmd)
	rootCmd.AddCommand(formsCmd)
}

func runFormsList(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	spinner := render.NewSpinner("Fetching forms...")
	spinner.Start()
	var result struct {
		Forms []struct {
			ID              string `json:"id"`
			Name            string `json:"name"`
			Slug            string `json:"slug"`
			Status          string `json:"status"`
			IntegrationType string `json:"integration_type"`
			SubmissionCount int    `json:"submission_count"`
		} `json:"forms"`
		Total int `json:"total"`
	}
	err := client.Get("/api/v1/forms", &result)
	spinner.Stop()

	if err != nil {
		return err
	}

	if len(result.Forms) == 0 {
		render.Info("No forms found.")
		return nil
	}

	format, _ := rootCmd.Flags().GetString("format")
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result.Forms)
	}

	headers := []string{"ID", "Name", "Status", "Type", "Submissions"}
	var rows [][]string
	for _, f := range result.Forms {
		id := f.ID
		if len(id) > 8 {
			id = id[:8]
		}
		status := f.Status
		switch status {
		case "published":
			status = render.Green + status + render.Reset
		case "draft":
			status = render.Yellow + status + render.Reset
		}
		rows = append(rows, []string{id, f.Name, status, f.IntegrationType, fmt.Sprintf("%d", f.SubmissionCount)})
	}

	render.Table(headers, rows)
	fmt.Fprintf(os.Stderr, "\n%s%d forms%s\n", render.Dim, len(result.Forms), render.Reset)
	return nil
}

func runFormsGet(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	var form struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		Slug            string `json:"slug"`
		Description     string `json:"description"`
		Status          string `json:"status"`
		IntegrationType string `json:"integration_type"`
		SubmissionCount int    `json:"submission_count"`
		ViewCount       int    `json:"view_count"`
	}
	if err := client.Get("/api/v1/forms/"+args[0], &form); err != nil {
		return err
	}

	format, _ := rootCmd.Flags().GetString("format")
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(form)
	}

	fmt.Printf("\n  %sName:%s         %s\n", render.Dim, render.Reset, form.Name)
	fmt.Printf("  %sID:%s           %s\n", render.Dim, render.Reset, form.ID)
	fmt.Printf("  %sSlug:%s         %s\n", render.Dim, render.Reset, form.Slug)
	fmt.Printf("  %sStatus:%s       %s\n", render.Dim, render.Reset, form.Status)
	fmt.Printf("  %sType:%s         %s\n", render.Dim, render.Reset, form.IntegrationType)
	fmt.Printf("  %sSubmissions:%s  %d\n", render.Dim, render.Reset, form.SubmissionCount)
	fmt.Printf("  %sViews:%s        %d\n", render.Dim, render.Reset, form.ViewCount)
	if form.Description != "" {
		fmt.Printf("  %sDescription:%s  %s\n", render.Dim, render.Reset, form.Description)
	}
	fmt.Println()
	return nil
}

func runFormsSubmissions(cmd *cobra.Command, args []string) error {
	client := requireAuth()
	last, _ := cmd.Flags().GetInt("last")

	var result struct {
		Submissions []struct {
			ID        string                 `json:"id"`
			Data      map[string]interface{} `json:"data"`
			Status    string                 `json:"status"`
			CreatedAt string                 `json:"created_at"`
		} `json:"submissions"`
	}
	path := fmt.Sprintf("/api/v1/forms/%s/submissions?limit=%d", args[0], last)
	if err := client.Get(path, &result); err != nil {
		return err
	}

	format, _ := rootCmd.Flags().GetString("format")
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result.Submissions)
	}

	if len(result.Submissions) == 0 {
		render.Info("No submissions found.")
		return nil
	}

	headers := []string{"ID", "Status", "Submitted", "Fields"}
	var rows [][]string
	for _, s := range result.Submissions {
		id := s.ID
		if len(id) > 8 {
			id = id[:8]
		}
		submitted := s.CreatedAt
		if len(submitted) > 19 {
			submitted = submitted[:19]
		}
		rows = append(rows, []string{id, s.Status, submitted, fmt.Sprintf("%d", len(s.Data))})
	}

	render.Table(headers, rows)
	return nil
}

func runFormsSubmit(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	slug, _ := cmd.Flags().GetString("slug")
	dataStr, _ := cmd.Flags().GetString("data")

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return fmt.Errorf("invalid JSON data: %w", err)
	}

	body := map[string]interface{}{"data": data}

	spinner := render.NewSpinner("Submitting form...")
	spinner.Start()
	var result struct {
		Submission struct {
			ID string `json:"id"`
		} `json:"submission"`
	}
	err := client.Post("/api/v1/public/forms/"+slug+"/submit", body, &result)
	spinner.Stop()

	if err != nil {
		return err
	}

	render.Success(fmt.Sprintf("Form submitted (id: %s)", result.Submission.ID))
	return nil
}
