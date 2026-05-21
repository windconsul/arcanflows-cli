package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/arcanflows/cli/internal/api"
	"github.com/arcanflows/cli/internal/fileops"
	"github.com/arcanflows/cli/internal/hooks"
	"github.com/arcanflows/cli/internal/memory"
	"github.com/arcanflows/cli/internal/render"
	"github.com/arcanflows/cli/internal/repo"
	"github.com/arcanflows/cli/internal/session"
	"github.com/arcanflows/cli/internal/shell"
	"github.com/spf13/cobra"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Interactive chat with an AI agent",
	Long: `Start an interactive chat session with an ArcanFlows agent.
The agent streams responses in real-time with markdown rendering.

Examples:
  arcanflows chat
  arcanflows chat --agent code-reviewer
  arcanflows chat --agent-id 3bdb789c-b0d3-4f28-aed7-13ad817d09fc
  arcanflows chat --model gpt-4o`,
	RunE: runChat,
}

var askCmd = &cobra.Command{
	Use:   "ask [question]",
	Short: "Ask a single question (non-interactive)",
	Long: `Send a single question to an agent and print the response.
Useful for piping and scripting.

Examples:
  arcanflows ask "What is the time complexity of quicksort?"
  echo "Explain this error" | arcanflows ask
  git diff | arcanflows ask "Review this diff"`,
	RunE: runAsk,
}

func init() {
	// Chat flags
	chatCmd.Flags().String("agent", "", "agent slug or name")
	chatCmd.Flags().String("agent-id", "", "agent ID (UUID)")
	chatCmd.Flags().String("model", "", "override model for this session")
	chatCmd.Flags().String("conversation-id", "", "continue existing conversation")
	chatCmd.Flags().Bool("continue", false, "continue last conversation")

	// Ask flags
	askCmd.Flags().String("agent", "", "agent slug or name")
	askCmd.Flags().String("agent-id", "", "agent ID (UUID)")
	askCmd.Flags().String("model", "", "override model")
	askCmd.Flags().String("format", "", "output format: raw, json")

	rootCmd.AddCommand(chatCmd)
	rootCmd.AddCommand(askCmd)
}

func resolveAgentID(client *api.Client, agentFlag, agentIDFlag string) (string, string, error) {
	// Explicit ID takes priority
	if agentIDFlag != "" {
		agent, err := client.GetAgent(agentIDFlag)
		if err != nil {
			return "", "", fmt.Errorf("agent not found: %w", err)
		}
		return agent.ID, agent.Name, nil
	}

	// Search by slug/name (exact then partial)
	if agentFlag != "" {
		agents, err := client.ListAgents()
		if err != nil {
			return "", "", err
		}
		// Exact match first
		for _, a := range agents {
			if strings.EqualFold(a.Slug, agentFlag) || strings.EqualFold(a.Name, agentFlag) {
				return a.ID, a.Name, nil
			}
		}
		// Partial match
		for _, a := range agents {
			if strings.Contains(strings.ToLower(a.Name), strings.ToLower(agentFlag)) ||
				strings.Contains(strings.ToLower(a.Slug), strings.ToLower(agentFlag)) {
				return a.ID, a.Name, nil
			}
		}
		return "", "", fmt.Errorf("agent '%s' not found (run: arcanflows agents list)", agentFlag)
	}

	// Use default from config
	if p, ok := cfg.Profiles[cfg.ActiveProfile]; ok && p.DefaultAgent != "" {
		return resolveAgentID(client, p.DefaultAgent, "")
	}

	// No agent specified — show interactive picker
	agents, err := client.ListAgents()
	if err != nil {
		return "", "", err
	}

	var activeAgents []api.Agent
	for _, a := range agents {
		if a.Status == "active" {
			activeAgents = append(activeAgents, a)
		}
	}

	if len(activeAgents) == 0 {
		return "", "", fmt.Errorf("no active agents found; create one at your ArcanFlows console")
	}

	// If only one active agent, use it
	if len(activeAgents) == 1 {
		return activeAgents[0].ID, activeAgents[0].Name, nil
	}

	// Interactive picker
	fmt.Fprintf(os.Stderr, "\n%sSelect an agent:%s\n\n", render.Bold, render.Reset)
	for i, a := range activeAgents {
		model := a.Model
		if len(model) > 20 {
			model = model[:20]
		}
		fmt.Fprintf(os.Stderr, "  %s%d)%s %s %s(%s)%s\n",
			render.Cyan, i+1, render.Reset, a.Name, render.Dim, model, render.Reset)
	}
	fmt.Fprintf(os.Stderr, "\n")

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprintf(os.Stderr, "%sChoice [1-%d]:%s ", render.Bold, len(activeAgents), render.Reset)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		var choice int
		if _, err := fmt.Sscanf(input, "%d", &choice); err == nil && choice >= 1 && choice <= len(activeAgents) {
			selected := activeAgents[choice-1]
			// Save as default for next time
			if p, ok := cfg.Profiles[cfg.ActiveProfile]; ok {
				p.DefaultAgent = selected.Name
				cfg.Profiles[cfg.ActiveProfile] = p
				cfg.Save()
			}
			fmt.Fprintf(os.Stderr, "%s  (saved as default — change with: arcanflows config set default-agent \"...\")%s\n\n",
				render.Dim, render.Reset)
			return selected.ID, selected.Name, nil
		}

		// Try matching by text input
		for _, a := range activeAgents {
			if strings.Contains(strings.ToLower(a.Name), strings.ToLower(input)) {
				return a.ID, a.Name, nil
			}
		}

		fmt.Fprintf(os.Stderr, "  %sInvalid choice. Enter a number 1-%d or agent name.%s\n", render.Yellow, len(activeAgents), render.Reset)
	}
}

// activeSession holds the current chat session for in-session command access.
var activeSession *session.Session
var sessionStore *session.Store

func runChat(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	agentFlag, _ := cmd.Flags().GetString("agent")
	agentIDFlag, _ := cmd.Flags().GetString("agent-id")
	conversationID, _ := cmd.Flags().GetString("conversation-id")
	continueFlag, _ := cmd.Flags().GetBool("continue")

	// Support positional arg as agent name: arcanflows chat "My Agent"
	if agentFlag == "" && agentIDFlag == "" && len(args) > 0 {
		agentFlag = strings.Join(args, " ")
	}

	// Session history setup
	sessionEnabled := cfg.Settings.SessionHistory
	if sessionEnabled {
		sessionStore = session.NewStore(cfg.Settings.SessionMaxAgeDays)
		// Run cleanup on start
		if cleaned, _ := sessionStore.Cleanup(); cleaned > 0 {
			render.Info(fmt.Sprintf("Cleaned up %d expired sessions", cleaned))
		}
	}

	// Handle --continue: resume last session
	if continueFlag && sessionEnabled {
		latest, err := sessionStore.Latest()
		if err != nil || latest == nil {
			render.Warning("No previous session to resume")
		} else {
			agentIDFlag = latest.AgentID
			conversationID = latest.ConversationID
			activeSession = latest
			render.Info(fmt.Sprintf("Resuming: %s (%d messages with %s)",
				latest.Summary, latest.MessageCount, latest.AgentName))
		}
	}

	// Load project instructions (ARCANFLOWS.md)
	projectInstructions := loadProjectInstructions()

	// Load project memory
	memStore, _ := memory.Load()
	if memStore != nil && memStore.Count() > 0 {
		render.Info(fmt.Sprintf("Loaded %d memory entries", memStore.Count()))
		projectInstructions += memStore.BuildContext()
	}

	// Resolve agent
	spinner := render.NewSpinner("Connecting to agent...")
	spinner.Start()

	agentID, agentName, err := resolveAgentID(client, agentFlag, agentIDFlag)
	spinner.Stop()
	if err != nil {
		return err
	}

	// Initialize session if not resuming
	if activeSession == nil && sessionEnabled {
		activeSession = session.NewSession(agentID, agentName)
	}

	// Load hooks
	hooksMgr := hooks.Load()
	if hooksMgr.Count() > 0 {
		render.Info(fmt.Sprintf("Loaded %d hooks (%s)", hooksMgr.Count(), hooksMgr.Summary()))
	}

	// Build hook variables
	cwd, _ := os.Getwd()
	hookVars := hooks.Variables{
		ProjectRoot: cwd,
		Branch:      runGitCmd("rev-parse", "--abbrev-ref", "HEAD"),
		Agent:       agentName,
	}

	// Run pre_chat hooks — output is injected into the prompt context
	if hooksMgr.HasHooks(hooks.PreChat) {
		hookOutput, err := hooksMgr.Run(hooks.PreChat, hookVars)
		if err != nil {
			render.Warning(fmt.Sprintf("pre_chat hook failed: %v", err))
		} else if hookOutput != "" {
			projectInstructions += "\n\n## Pre-Chat Hook Output\n\n" + hookOutput + "\n\n---\n\n"
		}
	}

	profile := cfg.Profiles[cfg.ActiveProfile]

	render.Header(
		fmt.Sprintf("ArcanFlows Chat - %s", agentName),
		fmt.Sprintf("Type /help for commands, Ctrl+C to exit | %s", profile.Organization),
	)
	fmt.Fprintln(os.Stderr)

	reader := bufio.NewReader(os.Stdin)

	for {
		render.Prompt("You")
		input, err := reader.ReadString('\n')
		if err != nil {
			// EOF (Ctrl+D)
			fmt.Fprintln(os.Stderr)
			saveSessionOnExit()
			return nil
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// Handle in-session commands
		if strings.HasPrefix(input, "/") {
			if handleChatCommand(input) {
				continue
			}
			if input == "/exit" || input == "/quit" {
				saveSessionOnExit()
				return nil
			}
		}

		// Track user message in session
		if activeSession != nil {
			activeSession.AddMessage("user", input)
		}

		// Send message and stream response
		render.AgentLabel(agentName)

		start := time.Now()
		var promptTokens, completionTokens int
		filter := render.NewToolFilter()

		err = client.ChatStream(agentID, api.ChatRequest{
			Message:        func() string {
						if projectInstructions != "" && conversationID == "" {
							msg := projectInstructions + input
							projectInstructions = "" // only send once
							return msg
						}
						return input
					}(),
			ConversationID: conversationID,
		}, func(chunk api.StreamChunk) {
			// Handle metadata event
			if chunk.Type == "metadata" && chunk.ConversationID != "" {
				conversationID = chunk.ConversationID
				// Update session with server conversation ID
				if activeSession != nil {
					activeSession.ConversationID = conversationID
				}
				return
			}

			// Handle thinking
			if chunk.Thinking != "" {
				return
			}

			// Handle tool status from backend
			if chunk.ToolStatus != "" {
				fmt.Fprintf(os.Stderr, "\n%s  [Using tool: %s...]%s\n", render.Dim, chunk.ToolStatus, render.Reset)
				return
			}

			// Handle streaming content — filter USE_TOOL: directives
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

			// Capture token counts
			if chunk.TotalTokens > 0 {
				promptTokens = chunk.PromptTokens
				completionTokens = chunk.CompletionTokens
			}
		})

		elapsed := time.Since(start)
		fmt.Println() // newline after streamed response

		if err != nil {
			render.Error(fmt.Sprintf("Chat error: %v", err))
			continue
		}

		// Flush any remaining buffered content
		if flushed := filter.Flush(); flushed != "" {
			fmt.Print(flushed)
		}

		// Track assistant response in session
		fullResponse := filter.Output.String()
		if activeSession != nil && fullResponse != "" {
			activeSession.AddMessage("assistant", fullResponse)
		}

		// Process file operations from agent response
		ops := fileops.ParseOperations(fullResponse)
		if len(ops) > 0 {
			fmt.Fprintf(os.Stderr, "\n%s  %d file operation(s) detected%s\n", render.Dim, len(ops), render.Reset)
			for _, op := range ops {
				hookVars.File = op.Path
				switch op.Type {
				case "create":
					fileops.CreateFile(op.Path, op.Content, false)
					hooksMgr.Run(hooks.PostCreate, hookVars)
					if activeSession != nil {
						activeSession.AddFileModified(op.Path)
					}
				case "edit":
					fileops.EditFile(op.Path, op.Content, false)
					hooksMgr.Run(hooks.PostEdit, hookVars)
					if activeSession != nil {
						activeSession.AddFileModified(op.Path)
					}
				case "delete":
					fileops.DeleteFile(op.Path, false)
					hooksMgr.Run(hooks.PostDelete, hookVars)
					if activeSession != nil {
						activeSession.AddFileModified(op.Path)
					}
				case "rename":
					if err := os.Rename(op.Path, op.NewPath); err == nil {
						render.Success(fmt.Sprintf("Renamed %s -> %s", op.Path, op.NewPath))
						if activeSession != nil {
							activeSession.AddFileModified(op.NewPath)
						}
					}
				}
			}
		}

		// Process shell execution directives from agent response
		shellCmds := shell.ParseExecuteDirectives(fullResponse)
		if len(shellCmds) > 0 {
			shellCfg := shell.DefaultConfig()
			for _, shCmd := range shellCmds {
				shell.Execute(shCmd, shellCfg)
				if activeSession != nil {
					activeSession.AddCommandRun(shCmd)
				}
			}
		}

		// Auto-save session after each exchange
		if activeSession != nil && sessionStore != nil {
			if err := sessionStore.Save(activeSession); err != nil {
				// Silent save failure — don't interrupt chat
			}
		}

		// Print stats
		if promptTokens > 0 || completionTokens > 0 {
			fmt.Fprintf(os.Stderr, "%s  [%d in / %d out | %.1fs]%s\n",
				render.Dim, promptTokens, completionTokens, elapsed.Seconds(), render.Reset)
		}
		fmt.Fprintln(os.Stderr)
	}
}

// saveSessionOnExit saves the session and prints a message on exit.
func saveSessionOnExit() {
	if activeSession != nil && sessionStore != nil && activeSession.MessageCount > 0 {
		if err := sessionStore.Save(activeSession); err == nil {
			render.Info(fmt.Sprintf("Session saved (%d messages). Resume with: arcanflows chat --continue",
				activeSession.MessageCount))
		}
	} else {
		render.Info("Session ended")
	}
}

func runAsk(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	agentFlag, _ := cmd.Flags().GetString("agent")
	agentIDFlag, _ := cmd.Flags().GetString("agent-id")

	// Get question from args or stdin
	var question string
	if len(args) > 0 {
		question = strings.Join(args, " ")
	} else {
		// Read from stdin (for piping)
		scanner := bufio.NewScanner(os.Stdin)
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		question = strings.Join(lines, "\n")
	}

	if strings.TrimSpace(question) == "" {
		return fmt.Errorf("no question provided; usage: arcanflows ask \"your question\"")
	}

	// Resolve agent
	agentID, _, err := resolveAgentID(client, agentFlag, agentIDFlag)
	if err != nil {
		return err
	}

	// Stream response
	return client.ChatStream(agentID, api.ChatRequest{
		Message: question,
	}, func(chunk api.StreamChunk) {
		if chunk.Delta != "" {
			fmt.Print(chunk.Delta)
		} else if chunk.Content != "" && chunk.Done {
			fmt.Print(chunk.Content)
		}
	})
}

// handleChatCommand processes in-session / commands. Returns true if handled.
func handleChatCommand(input string) bool {
	// Commands with arguments
	if strings.HasPrefix(input, "/file ") {
		path := strings.TrimPrefix(input, "/file ")
		path = strings.TrimSpace(path)
		content, err := fileops.ReadFile(path)
		if err != nil {
			render.Error(err.Error())
		} else {
			fmt.Fprintf(os.Stderr, "%s  (file attached — mention it in your next message)%s\n", render.Dim, render.Reset)
			_ = content // will be sent with next message via context
		}
		return true
	}

	if strings.HasPrefix(input, "/run ") {
		command := strings.TrimPrefix(input, "/run ")
		command = strings.TrimSpace(command)
		shellCfg := shell.DefaultConfig()
		shellCfg.AutoConfirm = true // /run is explicit user intent
		shell.Execute(command, shellCfg)
		return true
	}

	if strings.HasPrefix(input, "/create ") {
		path := strings.TrimPrefix(input, "/create ")
		path = strings.TrimSpace(path)
		fmt.Fprintf(os.Stderr, "%s  Ask the agent to create %s in your next message%s\n", render.Dim, path, render.Reset)
		return true
	}

	switch input {
	case "/help":
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  Chat Commands:")
		fmt.Fprintln(os.Stderr, "    /help             Show this help")
		fmt.Fprintln(os.Stderr, "    /file <path>      Read and show a file")
		fmt.Fprintln(os.Stderr, "    /create <path>    Hint: ask agent to create a file")
		fmt.Fprintln(os.Stderr, "    /run <command>    Execute a shell command")
		fmt.Fprintln(os.Stderr, "    /diff             Show git diff")
		fmt.Fprintln(os.Stderr, "    /status           Show git status")
		fmt.Fprintln(os.Stderr, "    /tree             Show directory tree")
		fmt.Fprintln(os.Stderr, "    /undo             Undo last file operation")
		fmt.Fprintln(os.Stderr, "    /context          Show loaded context")
		fmt.Fprintln(os.Stderr, "    /sessions         Show current session info")
		fmt.Fprintln(os.Stderr, "    /history          Show files modified this session")
		fmt.Fprintln(os.Stderr, "    /hooks            Show configured hooks")
		fmt.Fprintln(os.Stderr, "    /clear            Clear conversation")
		fmt.Fprintln(os.Stderr, "    /exit             Exit chat")
		fmt.Fprintln(os.Stderr, "")
		return true
	case "/clear":
		render.Info("Conversation cleared")
		return true
	case "/sessions":
		if activeSession != nil {
			fmt.Fprintf(os.Stderr, "\n%sCurrent Session:%s\n", render.Bold, render.Reset)
			fmt.Fprintf(os.Stderr, "  ID:       %s\n", activeSession.ID)
			fmt.Fprintf(os.Stderr, "  Agent:    %s\n", activeSession.AgentName)
			fmt.Fprintf(os.Stderr, "  Messages: %d\n", activeSession.MessageCount)
			fmt.Fprintf(os.Stderr, "  Started:  %s (%s)\n",
				activeSession.StartedAt.Format("15:04:05"),
				session.TimeAgo(activeSession.StartedAt))
			if len(activeSession.FilesModified) > 0 {
				fmt.Fprintf(os.Stderr, "  Files:    %d modified\n", len(activeSession.FilesModified))
			}
			if len(activeSession.CommandsRun) > 0 {
				fmt.Fprintf(os.Stderr, "  Commands: %d run\n", len(activeSession.CommandsRun))
			}
			fmt.Fprintln(os.Stderr)
		} else {
			render.Info("Session history is disabled")
		}
		return true
	case "/history":
		if activeSession != nil {
			if len(activeSession.FilesModified) == 0 && len(activeSession.CommandsRun) == 0 {
				render.Info("No files modified or commands run this session")
			} else {
				fmt.Fprintln(os.Stderr)
				if len(activeSession.FilesModified) > 0 {
					fmt.Fprintf(os.Stderr, "  %sFiles modified:%s\n", render.Bold, render.Reset)
					for _, f := range activeSession.FilesModified {
						fmt.Fprintf(os.Stderr, "    %s%s%s %s\n", render.Green, "+", render.Reset, f)
					}
				}
				if len(activeSession.CommandsRun) > 0 {
					fmt.Fprintf(os.Stderr, "  %sCommands run:%s\n", render.Bold, render.Reset)
					for _, c := range activeSession.CommandsRun {
						fmt.Fprintf(os.Stderr, "    %s$%s %s\n", render.Cyan, render.Reset, c)
					}
				}
				fmt.Fprintln(os.Stderr)
			}
		} else {
			render.Info("Session history is disabled")
		}
		return true
	case "/hooks":
		mgr := hooks.Load()
		if mgr.Count() == 0 {
			render.Info("No hooks configured (create with: arcanflows hooks init)")
		} else {
			fmt.Fprintf(os.Stderr, "\n%sHooks:%s %s\n\n", render.Bold, render.Reset, mgr.Summary())
		}
		return true
	case "/context":
		instructions, path, _ := repo.LoadInstructions(".")
		if instructions != "" {
			fmt.Fprintf(os.Stderr, "\n%sARCANFLOWS.md:%s %s\n", render.Dim, render.Reset, path)
			// Show first 10 lines
			lines := strings.Split(instructions, "\n")
			for i, line := range lines {
				if i >= 10 {
					fmt.Fprintf(os.Stderr, "  %s... (%d more lines)%s\n", render.Dim, len(lines)-10, render.Reset)
					break
				}
				fmt.Fprintf(os.Stderr, "  %s\n", line)
			}
		} else {
			fmt.Fprintf(os.Stderr, "  %sNo ARCANFLOWS.md found (run: arcanflows init)%s\n", render.Dim, render.Reset)
		}
		memStore, _ := memory.Load()
		if memStore != nil && memStore.Count() > 0 {
			fmt.Fprintf(os.Stderr, "\n%sMemory (%d entries):%s\n", render.Dim, memStore.Count(), render.Reset)
			for _, e := range memStore.Entries {
				fmt.Fprintf(os.Stderr, "  %d) %s\n", e.ID, e.Content)
			}
		}
		fmt.Fprintln(os.Stderr)
		return true
	case "/undo":
		if err := fileops.UndoLast(); err != nil {
			render.Error(err.Error())
		}
		return true
	case "/diff":
		out := runGitCmd("diff", "--stat")
		if out == "" {
			render.Info("No changes")
		} else {
			fmt.Fprintln(os.Stderr, out)
		}
		return true
	case "/status":
		out := runGitCmd("status", "--short")
		if out == "" {
			render.Info("Working tree clean")
		} else {
			fmt.Fprintln(os.Stderr, out)
		}
		return true
	case "/tree":
		out := runGitCmd("ls-tree", "-r", "--name-only", "HEAD")
		if out == "" {
			// Fallback: just list files
			fmt.Fprintln(os.Stderr, "  (use: find . -maxdepth 2 -type f)")
		} else {
			lines := strings.Split(out, "\n")
			if len(lines) > 30 {
				lines = lines[:30]
				lines = append(lines, fmt.Sprintf("... (%d more files)", len(strings.Split(out, "\n"))-30))
			}
			fmt.Fprintln(os.Stderr, strings.Join(lines, "\n"))
		}
		return true
	default:
		render.Warning(fmt.Sprintf("Unknown command: %s (type /help)", input))
		return true
	}
}

func runGitCmd(args ...string) string {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
