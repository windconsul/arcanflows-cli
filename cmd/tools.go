package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/arcanflows/cli/internal/api"
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
	Use:   "exec [tool-slug-or-id]",
	Short: "Execute a tool",
	Long: `Execute a tool directly. Provide its input either as a single JSON blob
(--input) or as one or more key=value pairs (--param). When called without any
input, the command prints the tool's input schema so you can see exactly which
fields are required.

Examples:
  # Single JSON blob (best for nested objects / arrays)
  arcanflows tools exec ssh-device-manager \
    --input '{"host":"10.0.0.1","username":"admin","password":"...","commands":["show version"]}'

  # Key=value pairs (best for flat / scalar inputs)
  arcanflows tools exec web-search --param query="ArcanFlows pricing"

  # Mix: scalar params plus a JSON array for one field
  arcanflows tools exec ssh-device-manager \
    --param host=10.0.0.1 \
    --param username=admin \
    --param password=secret \
    --param-json commands='["show version","show clock"]'

  # Show what the tool requires (no execution)
  arcanflows tools exec ssh-device-manager   # prints input schema and exits`,
	Args: cobra.ExactArgs(1),
	RunE: runToolsExec,
}

var toolsShowCmd = &cobra.Command{
	Use:   "show [tool-slug-or-id]",
	Short: "Show a tool's input schema and metadata",
	Args:  cobra.ExactArgs(1),
	RunE:  runToolsShow,
}

func init() {
	toolsExecCmd.Flags().String("input", "", "Full JSON input object (e.g. '{\"host\":\"1.2.3.4\"}')")
	toolsExecCmd.Flags().StringArray("param", nil, "Scalar param as key=value (repeatable). String values only.")
	toolsExecCmd.Flags().StringArray("param-json", nil, "Param with a JSON value as key=jsonValue (repeatable). Use for arrays/numbers/booleans.")

	toolsCmd.AddCommand(toolsListCmd)
	toolsCmd.AddCommand(toolsExecCmd)
	toolsCmd.AddCommand(toolsShowCmd)
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
	paramPairs, _ := cmd.Flags().GetStringArray("param")
	paramJSONPairs, _ := cmd.Flags().GetStringArray("param-json")

	// Look up the tool (so we can show its schema if input is missing and
	// resolve slug → id either way).
	tool, err := findTool(client, args[0])
	if err != nil {
		return err
	}

	// Build input map from either --input (JSON blob) or --param/--param-json
	// (key=value pairs). The two modes are merged: --input is the base, then
	// --param values override on top, then --param-json on top of that.
	input, err := buildToolInput(inputStr, paramPairs, paramJSONPairs)
	if err != nil {
		return err
	}

	// No input at all → print the schema and exit. This is the single most
	// common mistake (e.g. `arcanflows tools exec ssh-device-manager` with no
	// flags) and produces a useless 500 from the API. Show the user what to
	// pass instead.
	if len(input) == 0 {
		printToolUsage(tool)
		return fmt.Errorf("no input provided — pass --input or --param (see usage above)")
	}

	// The /tools/:id/execute handler binds the entire request body as the
	// input map (see ToolHandler.ExecuteTool), so we send the fields at the
	// top level — NOT wrapped in {"input": ...}.
	spinner := render.NewSpinner(fmt.Sprintf("Executing %s...", tool.Name))
	spinner.Start()
	var result map[string]interface{}
	err = client.Post("/api/v1/tools/"+tool.ID+"/execute", input, &result)
	spinner.Stop()

	if err != nil {
		return err
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func runToolsShow(cmd *cobra.Command, args []string) error {
	client := requireAuth()
	tool, err := findTool(client, args[0])
	if err != nil {
		return err
	}
	printToolUsage(tool)
	return nil
}

// toolDetail captures the fields we need from GET /api/v1/tools/:id.
type toolDetail struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Slug        string                 `json:"slug"`
	ToolType    string                 `json:"tool_type"`
	Category    string                 `json:"category"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// findTool resolves a slug-or-id to a fully-loaded tool detail.
func findTool(client *api.Client, ref string) (*toolDetail, error) {
	// Try direct GET by ID first (cheapest).
	var detail toolDetail
	if err := client.Get("/api/v1/tools/"+ref, &detail); err == nil && detail.ID != "" {
		return &detail, nil
	}

	// Otherwise list and find by slug.
	var list struct {
		Tools []struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := client.Get("/api/v1/tools", &list); err != nil {
		return nil, err
	}
	for _, t := range list.Tools {
		if t.Slug == ref || t.ID == ref {
			if err := client.Get("/api/v1/tools/"+t.ID, &detail); err != nil {
				return nil, err
			}
			return &detail, nil
		}
	}
	return nil, fmt.Errorf("tool '%s' not found", ref)
}

// buildToolInput merges --input JSON with --param k=v and --param-json k=jsonV.
func buildToolInput(inputJSON string, paramPairs, paramJSONPairs []string) (map[string]interface{}, error) {
	input := make(map[string]interface{})

	if strings.TrimSpace(inputJSON) != "" {
		if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
			return nil, fmt.Errorf("--input is not valid JSON: %w", err)
		}
	}

	for _, p := range paramPairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("--param '%s' must be key=value", p)
		}
		input[k] = v
	}

	for _, p := range paramJSONPairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("--param-json '%s' must be key=jsonValue", p)
		}
		var decoded interface{}
		if err := json.Unmarshal([]byte(v), &decoded); err != nil {
			return nil, fmt.Errorf("--param-json '%s' value is not valid JSON: %w", k, err)
		}
		input[k] = decoded
	}

	return input, nil
}

// printToolUsage prints the tool's input schema as a human-readable usage
// hint to stderr, including a copy-pasteable example invocation.
func printToolUsage(tool *toolDetail) {
	fmt.Fprintf(os.Stderr, "\n%s%s%s  (%s)\n",
		render.Bold, tool.Name, render.Reset, tool.Slug)
	if tool.Description != "" {
		fmt.Fprintf(os.Stderr, "  %s%s%s\n", render.Dim, tool.Description, render.Reset)
	}

	if tool.InputSchema == nil {
		fmt.Fprintf(os.Stderr, "\n  %sThis tool has no input schema published.%s\n", render.Dim, render.Reset)
		return
	}

	required := map[string]bool{}
	if reqList, ok := tool.InputSchema["required"].([]interface{}); ok {
		for _, r := range reqList {
			if rs, ok := r.(string); ok {
				required[rs] = true
			}
		}
	}

	props, _ := tool.InputSchema["properties"].(map[string]interface{})
	if len(props) == 0 {
		return
	}

	fmt.Fprintf(os.Stderr, "\n  %sInput parameters:%s\n", render.Bold, render.Reset)

	// Print required first, then optional, alphabetical within each.
	var reqNames, optNames []string
	for name := range props {
		if required[name] {
			reqNames = append(reqNames, name)
		} else {
			optNames = append(optNames, name)
		}
	}
	sortStrings(reqNames)
	sortStrings(optNames)

	exampleInput := map[string]interface{}{}
	for _, name := range append(reqNames, optNames...) {
		p, _ := props[name].(map[string]interface{})
		ptype, _ := p["type"].(string)
		desc, _ := p["description"].(string)
		flag := " "
		if required[name] {
			flag = "*"
		}
		fmt.Fprintf(os.Stderr, "    %s %s%-22s%s %s%s%s  %s\n",
			flag, render.Bold, name, render.Reset, render.Dim, ptype, render.Reset, desc)
		if required[name] {
			exampleInput[name] = placeholderForType(ptype, p)
		}
	}

	fmt.Fprintf(os.Stderr, "\n  %s* = required%s\n", render.Dim, render.Reset)

	// Example block. Disable HTML escaping so the placeholder "<string>" stays
	// as-is instead of becoming "<string>".
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("    ", "  ")
	if err := enc.Encode(exampleInput); err == nil {
		fmt.Fprintf(os.Stderr, "\n  %sExample:%s\n", render.Bold, render.Reset)
		fmt.Fprintf(os.Stderr, "    arcanflows tools exec %s \\\n      --input '%s'\n",
			tool.Slug, compactJSON([]byte(buf.String())))
	}
	fmt.Fprintln(os.Stderr)
}

func placeholderForType(t string, schema map[string]interface{}) interface{} {
	switch t {
	case "string":
		return "<string>"
	case "integer":
		if d, ok := schema["default"]; ok {
			return d
		}
		return 0
	case "number":
		return 0.0
	case "boolean":
		return false
	case "array":
		return []interface{}{"<item>"}
	case "object":
		return map[string]interface{}{}
	default:
		return "<value>"
	}
}

func compactJSON(b []byte) string {
	// Strip leading whitespace from indented JSON so the example fits on
	// fewer lines when used as a shell arg.
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c == '\n' || c == ' ' {
			continue
		}
		out = append(out, byte(c))
	}
	return string(out)
}

// sortStrings — tiny sort helper to avoid pulling sort just here.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

