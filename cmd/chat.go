package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
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
	chatCmd.Flags().Bool("allow-outside", false, "allow file operations outside the project directory (unsafe)")

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

	for {
		fmt.Fprintf(os.Stderr, "%sChoice [1-%d]:%s ", render.Bold, len(activeAgents), render.Reset)
		input, _ := render.ReadLine()

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

	// Single shared stdin reader for the whole session — chat input, the agent
	// picker, and every y/n confirmation read from THIS reader (PROPOSAL-010 C11).
	reader := bufio.NewReader(os.Stdin)
	render.SetInputReader(reader)

	agentFlag, _ := cmd.Flags().GetString("agent")
	agentIDFlag, _ := cmd.Flags().GetString("agent-id")
	conversationID, _ := cmd.Flags().GetString("conversation-id")
	continueFlag, _ := cmd.Flags().GetBool("continue")

	// Confine file operations to the project directory unless explicitly allowed.
	if allowOutside, _ := cmd.Flags().GetBool("allow-outside"); allowOutside {
		fileops.SetAllowOutside(true)
		render.Warning("File operations are NOT confined to the project directory (--allow-outside)")
	}

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
		fmt.Sprintf("Type /help for commands, Ctrl+C to cancel/exit | %s", profile.Organization),
	)
	fmt.Fprintln(os.Stderr)

	// Content queued by /file, prepended to the next message (C5).
	var pendingFileContext strings.Builder

	// One persistent Ctrl+C handler for the whole session: during a turn it
	// cancels that turn (1.3); at the idle prompt it saves the session and
	// exits (C19) instead of the process being killed with nothing saved.
	var turnMu sync.Mutex
	var turnCancel context.CancelFunc
	var turnInterrupted bool
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	go func() {
		for range sigCh {
			turnMu.Lock()
			cancel := turnCancel
			if cancel != nil {
				turnInterrupted = true
				turnMu.Unlock()
				cancel()
				continue
			}
			turnMu.Unlock()
			// Idle: save and exit cleanly.
			fmt.Fprintln(os.Stderr)
			saveSessionOnExit()
			os.Exit(0)
		}
	}()

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
			// State-mutating commands need runChat scope (conversationID, pending).
			switch {
			case input == "/clear":
				// Actually start a fresh conversation (C9) — was a no-op before.
				conversationID = ""
				if sessionEnabled {
					activeSession = session.NewSession(agentID, agentName)
				}
				pendingFileContext.Reset()
				render.Info("Conversation cleared — starting fresh")
				continue
			case strings.HasPrefix(input, "/file "):
				// Actually attach the file to the next message (C5) — was discarded.
				p := strings.TrimSpace(strings.TrimPrefix(input, "/file "))
				content, ferr := fileops.ReadFile(p)
				if ferr != nil {
					render.Error(ferr.Error())
				} else {
					pendingFileContext.WriteString(fmt.Sprintf("## Attached file: %s\n```\n%s\n```\n\n", p, content))
					render.Info(fmt.Sprintf("Attached %s — it will be included with your next message", p))
				}
				continue
			}
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

		// --- Agentic loop (PROPOSAL-010 Phase 1) --------------------------------
		// Send the message, apply any local file/shell actions the agent asks for,
		// feed the outcomes back into the SAME conversation, and let the agent
		// continue — up to a bounded number of iterations. The backend rehydrates
		// history from conversation_id, so only the new message is sent each turn.
		maxIters := cfg.Settings.ChatMaxToolIters
		if maxIters <= 0 {
			maxIters = 8
		}
		originalRequest := input
		nextMessage := input
		// Attach any /file content queued for this message (C5).
		if pendingFileContext.Len() > 0 {
			nextMessage = pendingFileContext.String() + "\n" + nextMessage
			pendingFileContext.Reset()
		}
		// Send project instructions + memory on the FIRST message of the session,
		// including on --continue (C10): the server has prior chat history but not
		// this client-side context.
		if projectInstructions != "" {
			nextMessage = projectInstructions + nextMessage
			projectInstructions = "" // only send once
		}

		for iter := 0; iter < maxIters; iter++ {
			// Turn cancellation is handled by the persistent Ctrl+C handler above;
			// register this turn's cancel func so an interrupt aborts the request
			// (1.3) rather than killing the CLI.
			turnCtx, cancelTurn := context.WithCancel(context.Background())
			turnMu.Lock()
			turnCancel = cancelTurn
			turnInterrupted = false
			turnMu.Unlock()

			render.AgentLabel(agentName)
			start := time.Now()
			var promptTokens, completionTokens int
			filter := render.NewToolFilter()

			err = client.ChatStreamContext(turnCtx, agentID, api.ChatRequest{
				Message:        nextMessage,
				ConversationID: conversationID,
			}, func(chunk api.StreamChunk) {
				if chunk.Type == "metadata" && chunk.ConversationID != "" {
					conversationID = chunk.ConversationID
					if activeSession != nil {
						activeSession.ConversationID = conversationID
					}
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

			turnMu.Lock()
			turnCancel = nil
			interrupted := turnInterrupted
			turnMu.Unlock()
			cancelTurn()
			elapsed := time.Since(start)
			fmt.Println() // newline after streamed response

			if interrupted {
				render.Warning("Interrupted — returning to prompt")
				break
			}
			if err != nil {
				render.Error(fmt.Sprintf("Chat error: %v", err))
				break
			}

			if flushed := filter.Flush(); flushed != "" {
				fmt.Print(flushed)
			}
			fullResponse := filter.Output.String()
			if activeSession != nil && fullResponse != "" {
				activeSession.AddMessage("assistant", fullResponse)
			}

			if promptTokens > 0 || completionTokens > 0 {
				fmt.Fprintf(os.Stderr, "%s  [%d in / %d out | %.1fs]%s\n",
					render.Dim, promptTokens, completionTokens, elapsed.Seconds(), render.Reset)
			}

			// Apply the agent's local actions and collect their outcomes.
			summary, meaningful := applyAgentActions(fullResponse, hooksMgr, &hookVars)

			if activeSession != nil && sessionStore != nil {
				_ = sessionStore.Save(activeSession)
			}

			if meaningful == 0 {
				// No local actions to report — the agent's turn is complete.
				break
			}
			if iter == maxIters-1 {
				render.Warning(fmt.Sprintf("Reached the %d-step tool limit; stopping (raise chat_max_tool_iterations to allow more)", maxIters))
				break
			}

			// Feed the outcomes back and let the agent continue.
			fmt.Fprintf(os.Stderr, "%s  [sending results back to the agent…]%s\n", render.Dim, render.Reset)
			nextMessage = buildToolResultMessage(originalRequest, summary)
		}

		fmt.Fprintln(os.Stderr)
	}
}

// applyAgentActions executes the file/shell directives found in an agent
// response and returns a human-readable summary plus the count of "meaningful"
// outcomes — applied or failed. User-skipped actions don't count, so a decline
// doesn't keep the agent loop nagging (PROPOSAL-010 Phase 1).
func applyAgentActions(response string, hooksMgr *hooks.Manager, hookVars *hooks.Variables) (string, int) {
	var lines []string
	meaningful := 0

	ops := fileops.ParseOperations(response)
	if len(ops) > 0 {
		fmt.Fprintf(os.Stderr, "\n%s  %d file operation(s) detected%s\n", render.Dim, len(ops), render.Reset)
		for _, op := range ops {
			hookVars.File = op.Path
			target := op.Path
			if op.Type == "rename" {
				target = fmt.Sprintf("%s -> %s", op.Path, op.NewPath)
			}

			// pre_* hooks now fire (C8): a hard failure blocks the operation.
			preEvent := map[string]hooks.HookEvent{"create": hooks.PreCreate, "edit": hooks.PreEdit, "replace": hooks.PreEdit, "delete": hooks.PreDelete}[op.Type]
			if preEvent != "" {
				if _, herr := hooksMgr.Run(preEvent, *hookVars); herr != nil {
					render.Warning(fmt.Sprintf("%s blocked by %s hook: %v", op.Type, preEvent, herr))
					lines = append(lines, fmt.Sprintf("- %s %s: blocked by %s hook", op.Type, target, preEvent))
					meaningful++
					continue
				}
			}

			var opErr error
			switch op.Type {
			case "create":
				opErr = fileops.CreateFile(op.Path, op.Content, false)
				if opErr == nil {
					hooksMgr.Run(hooks.PostCreate, *hookVars)
					sessionAddFile(op.Path)
				}
			case "edit":
				opErr = fileops.EditFile(op.Path, op.Content, false)
				if opErr == nil {
					hooksMgr.Run(hooks.PostEdit, *hookVars)
					sessionAddFile(op.Path)
				}
			case "replace":
				opErr = fileops.ReplaceInFile(op.Path, op.Replacements, false)
				if opErr == nil {
					hooksMgr.Run(hooks.PostEdit, *hookVars)
					sessionAddFile(op.Path)
				}
			case "delete":
				opErr = fileops.DeleteFile(op.Path, false)
				if opErr == nil {
					hooksMgr.Run(hooks.PostDelete, *hookVars)
					sessionAddFile(op.Path)
				}
			case "rename":
				opErr = fileops.MoveFile(op.Path, op.NewPath, false)
				if opErr == nil {
					sessionAddFile(op.NewPath)
				}
			}
			if opErr != nil {
				render.Error(opErr.Error())
				lines = append(lines, fmt.Sprintf("- %s %s: FAILED — %v", op.Type, target, opErr))
			} else {
				lines = append(lines, fmt.Sprintf("- %s %s: ok", op.Type, target))
			}
			meaningful++
		}
	}

	shellCmds := shell.ParseExecuteDirectives(response)
	if len(shellCmds) > 0 {
		shellCfg := shell.DefaultConfig()
		for _, shCmd := range shellCmds {
			// pre_run/post_run hooks now fire (C8).
			hooksMgr.Run(hooks.PreRun, *hookVars)
			out, code, execErr := shell.Execute(shCmd, shellCfg)
			hooksMgr.Run(hooks.PostRun, *hookVars)
			if activeSession != nil {
				activeSession.AddCommandRun(shCmd)
			}
			switch {
			case code == -1 && execErr == nil:
				// user declined at the confirmation prompt — not meaningful
				lines = append(lines, fmt.Sprintf("- $ %s: skipped by user", shCmd))
			case code == -1 && execErr != nil:
				lines = append(lines, fmt.Sprintf("- $ %s: not run (%v)", shCmd, execErr))
				meaningful++
			default:
				snippet := out
				if len(snippet) > 2000 {
					snippet = snippet[:2000] + "\n... (truncated)"
				}
				lines = append(lines, fmt.Sprintf("- $ %s: exit %d\n```\n%s\n```", shCmd, code, snippet))
				meaningful++
			}
		}
	}

	return strings.Join(lines, "\n"), meaningful
}

// sessionAddFile records a modified file in the active session, if any.
func sessionAddFile(path string) {
	if activeSession != nil {
		activeSession.AddFileModified(path)
	}
}

// buildToolResultMessage formats the outcome of local actions as a follow-up
// message for the agent, and restates the original goal so it survives the
// server's ~10-message rehydration window during a longer loop.
func buildToolResultMessage(originalRequest, summary string) string {
	var b strings.Builder
	b.WriteString("[SYSTEM — automated results of the actions you just requested]\n")
	b.WriteString(summary)
	b.WriteString("\n\nIf more steps are needed, provide the next actions (file blocks / EXECUTE:). " +
		"If the task is complete, give a short final summary and stop — do not repeat actions that already succeeded.")
	if r := strings.TrimSpace(originalRequest); r != "" {
		if len(r) > 300 {
			r = r[:300] + "…"
		}
		b.WriteString("\n\n(Original request, for reference: " + r + ")")
	}
	return b.String()
}

// saveSessionOnExit saves the session, fires post_chat hooks (C8), and prints a
// message on exit.
func saveSessionOnExit() {
	if mgr := hooks.Load(); mgr.HasHooks(hooks.PostChat) {
		cwd, _ := os.Getwd()
		vars := hooks.Variables{ProjectRoot: cwd, Branch: runGitCmd("rev-parse", "--abbrev-ref", "HEAD")}
		if activeSession != nil {
			vars.Agent = activeSession.AgentName
		}
		mgr.Run(hooks.PostChat, vars)
	}
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
// Note: /file and /clear are handled in runChat (they mutate session state).
func handleChatCommand(input string) bool {
	// Commands with arguments
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
