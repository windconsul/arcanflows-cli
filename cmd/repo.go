package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/arcanflows/cli/internal/api"
	"github.com/arcanflows/cli/internal/render"
	"github.com/arcanflows/cli/internal/repo"
	"github.com/spf13/cobra"
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "AI-powered repository analysis",
	Long: `Analyze, review, and generate insights for code repositories
using ArcanFlows AI agents.

Examples:
  arcanflows repo analyze                 # Analyze current directory
  arcanflows repo analyze ./services      # Analyze specific path
  arcanflows repo review                  # Review uncommitted changes
  arcanflows repo explain ./src/auth.go   # Explain a file
  arcanflows repo audit                   # Security audit
  arcanflows repo test ./utils.ts         # Generate tests`,
}

var repoAnalyzeCmd = &cobra.Command{
	Use:   "analyze [path]",
	Short: "Analyze a repository with AI",
	Long: `Scan the repository, detect languages and frameworks, then send
the structured context to an AI agent for comprehensive analysis.`,
	RunE: runRepoAnalyze,
}

var repoReviewCmd = &cobra.Command{
	Use:   "review [file]",
	Short: "AI code review of changes or a file",
	Long: `Review uncommitted git changes, or a specific file, against
best practices using an AI agent.

Examples:
  arcanflows repo review                  # Review git diff
  arcanflows repo review ./src/auth.go    # Review specific file`,
	RunE: runRepoReview,
}

var repoExplainCmd = &cobra.Command{
	Use:   "explain [file]",
	Short: "Explain what a file or function does",
	Args:  cobra.ExactArgs(1),
	RunE:  runRepoExplain,
}

var repoAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Security audit of the repository",
	Long: `Scan the repository for security issues, vulnerabilities,
hardcoded secrets, and OWASP top 10 risks.`,
	RunE: runRepoAudit,
}

var repoTestCmd = &cobra.Command{
	Use:   "test [file]",
	Short: "Generate unit tests for a file",
	Args:  cobra.ExactArgs(1),
	RunE:  runRepoTest,
}

var repoDocsCmd = &cobra.Command{
	Use:   "docs [path]",
	Short: "Generate documentation for code",
	RunE:  runRepoDocs,
}

func init() {
	repoAnalyzeCmd.Flags().String("focus", "", "analysis focus: security, performance, architecture, tests")
	repoAnalyzeCmd.Flags().String("agent", "", "agent to use for analysis")
	repoAnalyzeCmd.Flags().String("agent-id", "", "agent ID to use")

	repoReviewCmd.Flags().String("agent", "", "agent to use")
	repoReviewCmd.Flags().String("agent-id", "", "agent ID")

	repoExplainCmd.Flags().String("agent", "", "agent to use")
	repoExplainCmd.Flags().String("agent-id", "", "agent ID")

	repoAuditCmd.Flags().String("focus", "", "audit focus: sql-injection, xss, secrets, dependencies")
	repoAuditCmd.Flags().String("agent", "", "agent to use")
	repoAuditCmd.Flags().String("agent-id", "", "agent ID")

	repoTestCmd.Flags().String("agent", "", "agent to use")
	repoTestCmd.Flags().String("agent-id", "", "agent ID")
	repoTestCmd.Flags().String("framework", "", "test framework: jest, vitest, go-test, pytest")

	repoDocsCmd.Flags().String("agent", "", "agent to use")
	repoDocsCmd.Flags().String("agent-id", "", "agent ID")

	repoCmd.AddCommand(repoAnalyzeCmd)
	repoCmd.AddCommand(repoReviewCmd)
	repoCmd.AddCommand(repoExplainCmd)
	repoCmd.AddCommand(repoAuditCmd)
	repoCmd.AddCommand(repoTestCmd)
	repoCmd.AddCommand(repoDocsCmd)
	rootCmd.AddCommand(repoCmd)
}

func scanRepo(path string) (*repo.ScanResult, error) {
	if path == "" {
		path = "."
	}

	spinner := render.NewSpinner("Scanning repository...")
	spinner.Start()

	scanner := repo.NewScanner(path,
		cfg.Settings.RepoMaxFiles,
		int64(cfg.Settings.RepoMaxFileSize),
		cfg.Settings.RepoExclude,
	)
	scan, err := scanner.Scan()
	spinner.Stop()

	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	// Print scan summary
	fmt.Fprintf(os.Stderr, "  %sScanned:%s %d files across %d languages\n",
		render.Dim, render.Reset, scan.TotalFiles, len(scan.Languages))

	for _, l := range sortedLangs(scan.Languages) {
		pct := float64(l.stat.Lines) / float64(scan.TotalLines) * 100
		if scan.TotalLines == 0 {
			pct = 0
		}
		fmt.Fprintf(os.Stderr, "  %s|%s %s: %d files, %d lines (%.0f%%)\n",
			render.Dim, render.Reset, l.name, l.stat.Files, l.stat.Lines, pct)
	}

	if len(scan.Frameworks) > 0 {
		fmt.Fprintf(os.Stderr, "  %sFrameworks:%s %s\n",
			render.Dim, render.Reset, strings.Join(scan.Frameworks, ", "))
	}
	fmt.Fprintln(os.Stderr)

	return scan, nil
}

func resolveRepoAgent(cmd *cobra.Command, client *api.Client) (string, string, error) {
	agentFlag, _ := cmd.Flags().GetString("agent")
	agentIDFlag, _ := cmd.Flags().GetString("agent-id")
	return resolveAgentID(client, agentFlag, agentIDFlag)
}

func streamAgentResponse(client *api.Client, agentID, agentName, prompt string) error {
	render.AgentLabel(agentName)

	start := time.Now()
	var promptTokens, completionTokens int
	filter := render.NewToolFilter()

	err := client.ChatStream(agentID, api.ChatRequest{
		Message: prompt,
	}, func(chunk api.StreamChunk) {
		if chunk.Type == "metadata" {
			return
		}
		if chunk.Thinking != "" {
			return
		}
		if chunk.ToolStatus != "" {
			fmt.Fprintf(os.Stderr, "\n%s  [Using tool: %s...]%s\n", render.Dim, chunk.ToolStatus, render.Reset)
			return
		}
		if chunk.Delta != "" {
			if text := filter.ProcessDelta(chunk.Delta); text != "" {
				fmt.Print(text)
			}
		} else if chunk.Content != "" && chunk.Done {
			content := filter.ProcessFinalContent(chunk.Content)
			if filter.Output.Len() == 0 && content != "" {
				fmt.Print(content)
			}
		}
		if chunk.TotalTokens > 0 {
			promptTokens = chunk.PromptTokens
			completionTokens = chunk.CompletionTokens
		}
	})

	elapsed := time.Since(start)
	fmt.Println()

	if err != nil {
		return err
	}

	if promptTokens > 0 {
		fmt.Fprintf(os.Stderr, "\n%s  [%d in / %d out | %.1fs]%s\n",
			render.Dim, promptTokens, completionTokens, elapsed.Seconds(), render.Reset)
	}
	return nil
}

// repoPromptPrefix prevents the agent from using tools (diagrams, web search, etc.)
// when analyzing repository context — the context is already provided in the prompt.
const repoPromptPrefix = `IMPORTANT INSTRUCTIONS FOR THIS REQUEST:
- Do NOT use any tools (no diagrams, no web search, no file readers, no code execution).
- All the information you need is provided below in the repository context.
- Respond with plain text and markdown only.
- Do NOT generate or render any diagrams, images, or code blocks meant for execution.
- NEVER use triple backticks with "diagrams" as the language (e.g. never write ` + "```diagrams" + `). Use "python" or "text" instead for code examples.
- For architecture descriptions, use bullet points or ASCII text, NOT diagram code.

`

func runRepoAnalyze(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	// Load project instructions
	projectInstructions := loadProjectInstructions()
	focus, _ := cmd.Flags().GetString("focus")

	scan, err := scanRepo(path)
	if err != nil {
		return err
	}

	agentID, agentName, err := resolveRepoAgent(cmd, client)
	if err != nil {
		return err
	}

	context := repo.BuildContext(scan, focus)
	prompt := repoPromptPrefix + projectInstructions + context + "\n\nPlease provide a comprehensive analysis of this repository. Include:\n1. Architecture overview\n2. Code quality assessment\n3. Top 5 strengths\n4. Top 5 areas for improvement\n5. Specific actionable recommendations"

	if focus != "" {
		prompt = repoPromptPrefix + context + fmt.Sprintf("\n\nFocus your analysis specifically on **%s**. Provide detailed, actionable findings.", focus)
	}

	return streamAgentResponse(client, agentID, agentName, prompt)
}

func runRepoReview(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	agentID, agentName, err := resolveRepoAgent(cmd, client)
	if err != nil {
		return err
	}

	if len(args) > 0 {
		// Review specific file
		scan, err := scanRepo(".")
		if err != nil {
			return err
		}

		fileContext := repo.BuildFileContext(args[0], scan)
		prompt := repoPromptPrefix + fileContext + "\n\nPlease review this code for:\n1. Bugs and potential issues\n2. Performance concerns\n3. Security vulnerabilities\n4. Code quality and readability\n5. Suggested improvements with specific code examples"

		return streamAgentResponse(client, agentID, agentName, prompt)
	}

	// Review git diff
	cwd, _ := os.Getwd()
	diffContext := repo.BuildDiffContext(cwd)

	if strings.Contains(diffContext, "No uncommitted changes") {
		render.Info("No uncommitted changes to review.")
		return nil
	}

	prompt := repoPromptPrefix + diffContext + "\n\nPlease review these code changes for:\n1. Bugs introduced\n2. Security issues\n3. Performance impact\n4. Code quality\n5. Missing edge cases or error handling\nProvide specific line-level feedback where applicable."

	return streamAgentResponse(client, agentID, agentName, prompt)
}

func runRepoExplain(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	agentID, agentName, err := resolveRepoAgent(cmd, client)
	if err != nil {
		return err
	}

	scan, err := scanRepo(".")
	if err != nil {
		return err
	}

	fileContext := repo.BuildFileContext(args[0], scan)
	prompt := repoPromptPrefix + fileContext + "\n\nPlease explain this code in detail:\n1. What does this file do? (high-level purpose)\n2. Key functions/classes and their responsibilities\n3. How it fits into the broader project\n4. Notable patterns or techniques used\n5. Any complexity that deserves special attention"

	return streamAgentResponse(client, agentID, agentName, prompt)
}

func runRepoAudit(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	agentID, agentName, err := resolveRepoAgent(cmd, client)
	if err != nil {
		return err
	}

	focus, _ := cmd.Flags().GetString("focus")

	scan, err := scanRepo(".")
	if err != nil {
		return err
	}

	context := repo.BuildContext(scan, "security")
	prompt := repoPromptPrefix + context + "\n\nPerform a thorough security audit of this repository. Check for:\n1. OWASP Top 10 vulnerabilities\n2. Hardcoded secrets, API keys, or credentials\n3. SQL injection, XSS, command injection risks\n4. Authentication and authorization issues\n5. Insecure dependencies\n6. Missing input validation\n7. Insecure cryptographic practices\n8. Information disclosure risks\n\nFor each finding, provide: severity (Critical/High/Medium/Low), location, description, and remediation."

	if focus != "" {
		prompt = repoPromptPrefix + context + fmt.Sprintf("\n\nPerform a focused security audit on **%s**. Provide detailed findings with severity, location, and remediation steps.", focus)
	}

	return streamAgentResponse(client, agentID, agentName, prompt)
}

func runRepoTest(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	agentID, agentName, err := resolveRepoAgent(cmd, client)
	if err != nil {
		return err
	}

	framework, _ := cmd.Flags().GetString("framework")

	scan, err := scanRepo(".")
	if err != nil {
		return err
	}

	fileContext := repo.BuildFileContext(args[0], scan)
	prompt := repoPromptPrefix + fileContext + "\n\nGenerate comprehensive unit tests for this file. Include:\n1. Tests for all exported functions/methods\n2. Edge cases and boundary conditions\n3. Error handling paths\n4. Mocking of dependencies where needed\n5. Clear test descriptions"

	if framework != "" {
		prompt += fmt.Sprintf("\n\nUse the **%s** testing framework.", framework)
	} else {
		// Auto-detect
		lang := repo.DetectLanguageForFile(args[0])
		switch lang {
		case "TypeScript", "JavaScript":
			prompt += "\n\nUse vitest or jest as the testing framework."
		case "Go":
			prompt += "\n\nUse Go's standard testing package with testify assertions."
		case "Python":
			prompt += "\n\nUse pytest as the testing framework."
		}
	}

	prompt += "\n\nOutput ONLY the test code, ready to save to a file."

	return streamAgentResponse(client, agentID, agentName, prompt)
}

func runRepoDocs(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	agentID, agentName, err := resolveRepoAgent(cmd, client)
	if err != nil {
		return err
	}

	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	scan, err := scanRepo(path)
	if err != nil {
		return err
	}

	context := repo.BuildContext(scan, "")
	prompt := repoPromptPrefix + context + "\n\nGenerate comprehensive documentation for this codebase. Include:\n1. Project overview and purpose\n2. Architecture description with component diagram (Mermaid)\n3. Setup and installation instructions\n4. API reference (if applicable)\n5. Configuration options\n6. Key modules and their responsibilities\n\nFormat as Markdown suitable for a README or docs site."

	return streamAgentResponse(client, agentID, agentName, prompt)
}

// Helper to sort languages by line count
type langEntry struct {
	name string
	stat repo.LangStat
}

func sortedLangs(langs map[string]repo.LangStat) []langEntry {
	var entries []langEntry
	for name, stat := range langs {
		entries = append(entries, langEntry{name, stat})
	}
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].stat.Lines > entries[i].stat.Lines {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
	return entries
}
