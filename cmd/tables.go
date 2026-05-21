package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

var tablesCmd = &cobra.Command{
	Use:     "tables",
	Aliases: []string{"dt", "data-tables"},
	Short:   "Manage data tables and records",
	Long:    `List tables, query records, insert/update data, and import/export CSV.`,
}

var tblListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all data tables",
	RunE:  runTblList,
}

var tblSchemaCmd = &cobra.Command{
	Use:   "schema [table-id]",
	Short: "Show table fields/schema",
	Args:  cobra.ExactArgs(1),
	RunE:  runTblSchema,
}

var tblQueryCmd = &cobra.Command{
	Use:   "query [table-id]",
	Short: "Query records from a table",
	Long: `Query records with optional filters and output formats.

Examples:
  arcanflows tables query 62ffa7e3
  arcanflows tables query 62ffa7e3 --limit 10
  arcanflows tables query 62ffa7e3 --format csv > export.csv
  arcanflows tables query 62ffa7e3 --format json | jq '.records[].name'`,
	Args: cobra.ExactArgs(1),
	RunE: runTblQuery,
}

var tblInsertCmd = &cobra.Command{
	Use:   "insert [table-id]",
	Short: "Insert a record into a table",
	Long: `Insert a new record with JSON data.

Examples:
  arcanflows tables insert 62ffa7e3 --data '{"name":"John","email":"john@example.com"}'`,
	Args: cobra.ExactArgs(1),
	RunE: runTblInsert,
}

var tblExportCmd = &cobra.Command{
	Use:   "export [table-id]",
	Short: "Export all records as CSV or JSON",
	Args:  cobra.ExactArgs(1),
	RunE:  runTblExport,
}

func init() {
	tblQueryCmd.Flags().Int("limit", 50, "max records to return")
	tblQueryCmd.Flags().Int("offset", 0, "skip N records")
	tblQueryCmd.Flags().String("filter", "", "filter expression: field=value")
	tblInsertCmd.Flags().String("data", "", "JSON record data (required)")
	tblInsertCmd.MarkFlagRequired("data")
	tblExportCmd.Flags().String("format", "csv", "export format: csv, json")

	tablesCmd.AddCommand(tblListCmd)
	tablesCmd.AddCommand(tblSchemaCmd)
	tablesCmd.AddCommand(tblQueryCmd)
	tablesCmd.AddCommand(tblInsertCmd)
	tablesCmd.AddCommand(tblExportCmd)
	rootCmd.AddCommand(tablesCmd)
}

func runTblList(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	spinner := render.NewSpinner("Fetching data tables...")
	spinner.Start()
	var result struct {
		Tables []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Slug        string `json:"slug"`
			RecordCount int    `json:"record_count"`
			Status      string `json:"status"`
			Icon        string `json:"icon"`
		} `json:"tables"`
		Total int `json:"total"`
	}
	err := client.Get("/api/v1/data-tables", &result)
	spinner.Stop()

	if err != nil {
		return err
	}

	if len(result.Tables) == 0 {
		render.Info("No data tables found.")
		return nil
	}

	format, _ := rootCmd.Flags().GetString("format")
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result.Tables)
	}

	headers := []string{"ID", "Name", "Records", "Status"}
	var rows [][]string
	for _, t := range result.Tables {
		id := t.ID
		if len(id) > 8 {
			id = id[:8]
		}
		rows = append(rows, []string{id, t.Name, fmt.Sprintf("%d", t.RecordCount), t.Status})
	}

	render.Table(headers, rows)
	fmt.Fprintf(os.Stderr, "\n%s%d tables%s\n", render.Dim, len(result.Tables), render.Reset)
	return nil
}

func runTblSchema(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	var table struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Fields []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Type     string `json:"type"`
			Label    string `json:"label"`
			Required bool   `json:"required"`
		} `json:"fields"`
	}
	if err := client.Get("/api/v1/data-tables/"+args[0], &table); err != nil {
		return err
	}

	format, _ := rootCmd.Flags().GetString("format")
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(table)
	}

	fmt.Printf("\n  %sTable:%s %s (%s)\n\n", render.Bold, render.Reset, table.Name, table.ID)

	headers := []string{"Field Name", "Type", "Label", "Required"}
	var rows [][]string
	for _, f := range table.Fields {
		req := ""
		if f.Required {
			req = render.Red + "yes" + render.Reset
		}
		rows = append(rows, []string{f.Name, f.Type, f.Label, req})
	}
	render.Table(headers, rows)
	fmt.Println()
	return nil
}

func runTblQuery(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	limit, _ := cmd.Flags().GetInt("limit")
	offset, _ := cmd.Flags().GetInt("offset")

	path := fmt.Sprintf("/api/v1/data-tables/%s/records?limit=%d&offset=%d", args[0], limit, offset)

	spinner := render.NewSpinner("Querying records...")
	spinner.Start()
	var result struct {
		Records []struct {
			ID   string                 `json:"id"`
			Data map[string]interface{} `json:"data"`
		} `json:"records"`
		Total int `json:"total"`
	}
	err := client.Get(path, &result)
	spinner.Stop()

	if err != nil {
		return err
	}

	format, _ := rootCmd.Flags().GetString("format")

	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	if format == "csv" {
		return writeRecordsCSV(result.Records)
	}

	if len(result.Records) == 0 {
		render.Info(fmt.Sprintf("No records found (total: %d)", result.Total))
		return nil
	}

	// Collect all unique field names for columns
	fieldSet := make(map[string]bool)
	for _, r := range result.Records {
		for k := range r.Data {
			if !strings.HasPrefix(k, "_") {
				fieldSet[k] = true
			}
		}
	}
	var fieldNames []string
	for k := range fieldSet {
		fieldNames = append(fieldNames, k)
	}

	// Build table with ID + field columns
	headers := append([]string{"ID"}, fieldNames...)
	var rows [][]string
	for _, r := range result.Records {
		id := r.ID
		if len(id) > 8 {
			id = id[:8]
		}
		row := []string{id}
		for _, f := range fieldNames {
			val := r.Data[f]
			if val == nil {
				row = append(row, "")
			} else {
				s := fmt.Sprintf("%v", val)
				if len(s) > 40 {
					s = s[:37] + "..."
				}
				row = append(row, s)
			}
		}
		rows = append(rows, row)
	}

	render.Table(headers, rows)
	fmt.Fprintf(os.Stderr, "\n%sShowing %d of %d records%s\n", render.Dim, len(result.Records), result.Total, render.Reset)
	return nil
}

func runTblInsert(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	dataStr, _ := cmd.Flags().GetString("data")
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return fmt.Errorf("invalid JSON data: %w", err)
	}

	body := map[string]interface{}{"data": data}

	spinner := render.NewSpinner("Inserting record...")
	spinner.Start()
	var result struct {
		ID string `json:"id"`
	}
	err := client.Post("/api/v1/data-tables/"+args[0]+"/records", body, &result)
	spinner.Stop()

	if err != nil {
		return err
	}

	render.Success(fmt.Sprintf("Record created (id: %s)", result.ID))
	return nil
}

func runTblExport(cmd *cobra.Command, args []string) error {
	client := requireAuth()
	format, _ := cmd.Flags().GetString("format")

	// Fetch all records
	spinner := render.NewSpinner("Exporting records...")
	spinner.Start()
	var result struct {
		Records []struct {
			ID   string                 `json:"id"`
			Data map[string]interface{} `json:"data"`
		} `json:"records"`
		Total int `json:"total"`
	}
	err := client.Get(fmt.Sprintf("/api/v1/data-tables/%s/records?limit=10000", args[0]), &result)
	spinner.Stop()

	if err != nil {
		return err
	}

	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result.Records)
	}

	// CSV
	return writeRecordsCSV(result.Records)
}

func writeRecordsCSV(records []struct {
	ID   string                 `json:"id"`
	Data map[string]interface{} `json:"data"`
}) error {
	if len(records) == 0 {
		return nil
	}

	// Collect fields
	fieldSet := make(map[string]bool)
	for _, r := range records {
		for k := range r.Data {
			fieldSet[k] = true
		}
	}
	fields := []string{"id"}
	for k := range fieldSet {
		fields = append(fields, k)
	}

	w := csv.NewWriter(os.Stdout)
	w.Write(fields)

	for _, r := range records {
		row := make([]string, len(fields))
		row[0] = r.ID
		for i, f := range fields[1:] {
			if v, ok := r.Data[f]; ok && v != nil {
				row[i+1] = fmt.Sprintf("%v", v)
			}
		}
		w.Write(row)
	}
	w.Flush()
	return w.Error()
}
