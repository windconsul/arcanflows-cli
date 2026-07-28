package cmd

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/arcanflows/cli/internal/api"
	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

var builderCmd = &cobra.Command{
	Use:   "builder [request]",
	Short: "Design and create no-code components by conversation",
	Long: `Talk to the ArcanFlows Builder to design data tables, forms, agents,
workflows, tools, analytics dashboards, and whole apps — then create them with /build.

The Builder proposes a plan; you review it and type /build to create everything,
cross-wired. Refine by just talking ("make the form shorter", "add a status field").

Examples:
  arcanflows builder
  arcanflows builder "a contacts table and an intake form that saves to it"
  arcanflows builder "a daily sales report dashboard" --yes   # plan + build, non-interactive`,
	RunE: runBuilder,
}

func init() {
	builderCmd.Flags().BoolP("yes", "y", false, "auto-build the first plan and exit (non-interactive)")
	rootCmd.AddCommand(builderCmd)
}

// newConversationID returns a random UUID-v4-shaped id for Builder server-side
// memory (no external uuid dependency).
func newConversationID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("builder-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// builderState holds the live plan across turns of one Builder session.
type builderState struct {
	conversationID string
	history        []api.BuilderTurn
	pending        []json.RawMessage    // current (un-built) proposals — raw for verbatim round-trip
	pendingMeta    []api.ProposalMeta   // display metadata for the pending proposals
	existing       []json.RawMessage    // components already created this session (for edit/wiring context)
	firstUserMsg   string
}

func runBuilder(cmd *cobra.Command, args []string) error {
	client := requireAuth()

	reader := bufio.NewReader(os.Stdin)
	render.SetInputReader(reader)

	autoBuild, _ := cmd.Flags().GetBool("yes")
	seed := strings.TrimSpace(strings.Join(args, " "))

	profile := cfg.Profiles[cfg.ActiveProfile]
	st := &builderState{conversationID: newConversationID()}

	// One-shot mode: plan the seed request, build it, and exit — for scripting.
	if seed != "" && autoBuild {
		if err := builderPlanTurn(context.Background(), client, st, seed); err != nil {
			return err
		}
		if len(st.pending) == 0 {
			render.Warning("The Builder didn't produce anything to build.")
			return nil
		}
		return builderBuild(client, st)
	}

	render.Header("ArcanFlows Builder",
		fmt.Sprintf("Describe what to build · /build to create · /help · Ctrl+C to exit | %s", profile.Organization))
	fmt.Fprintln(os.Stderr)

	// Ctrl+C: cancel the current turn; at idle, exit cleanly.
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
			fmt.Fprintln(os.Stderr)
			os.Exit(0)
		}
	}()

	pendingSeed := seed
	for {
		var input string
		if pendingSeed != "" {
			input = pendingSeed
			pendingSeed = ""
			fmt.Fprintf(os.Stderr, "%sYou:%s %s\n", render.Bold, render.Reset, input)
		} else {
			render.Prompt("You")
			line, err := reader.ReadString('\n')
			if err != nil { // EOF (Ctrl+D)
				fmt.Fprintln(os.Stderr)
				return nil
			}
			input = strings.TrimSpace(line)
		}
		if input == "" {
			continue
		}

		// In-session commands
		if strings.HasPrefix(input, "/") {
			switch {
			case input == "/exit" || input == "/quit":
				return nil
			case input == "/help":
				printBuilderHelp()
				continue
			case input == "/new" || input == "/clear":
				st = &builderState{conversationID: newConversationID()}
				render.Info("Started a new Builder project")
				continue
			case input == "/plan":
				renderPending(st.pendingMeta)
				continue
			case input == "/build":
				if len(st.pending) == 0 {
					render.Warning("Nothing to build yet — describe what you want first.")
					continue
				}
				if err := builderBuild(client, st); err != nil {
					render.Error(err.Error())
				}
				continue
			default:
				render.Warning(fmt.Sprintf("Unknown command: %s (type /help)", input))
				continue
			}
		}

		// Planning turn (streamed), cancellable via the persistent Ctrl+C handler.
		turnCtx, cancelTurn := context.WithCancel(context.Background())
		turnMu.Lock()
		turnCancel = cancelTurn
		turnInterrupted = false
		turnMu.Unlock()

		err := builderPlanTurn(turnCtx, client, st, input)

		turnMu.Lock()
		turnCancel = nil
		interrupted := turnInterrupted
		turnMu.Unlock()
		cancelTurn()

		if interrupted {
			render.Warning("Interrupted — returning to prompt")
			continue
		}
		if err != nil {
			render.Error(err.Error())
			continue
		}
	}
}

// builderPlanTurn streams one Builder turn: prints the plan message, collects the
// proposals into st.pending (replacing the prior plan on a refinement), and shows
// a build hint. Pure-conversation turns (no proposals) leave the pending plan.
func builderPlanTurn(ctx context.Context, client *api.Client, st *builderState, message string) error {
	if st.firstUserMsg == "" {
		st.firstUserMsg = message
	}
	req := api.BuilderChatRequest{
		Message:            message,
		History:            st.history,
		CurrentProposals:   st.pending,
		ConversationID:     st.conversationID,
		ExistingComponents: st.existing,
	}

	fmt.Fprintf(os.Stderr, "\n%sBuilder%s\n", render.Cyan, render.Reset)
	spinner := render.NewSpinner("Designing…")
	spinner.Start()
	spinnerStopped := false
	stopSpinner := func() {
		if !spinnerStopped {
			spinner.Stop()
			spinnerStopped = true
		}
	}

	var newPending []json.RawMessage
	var newMeta []api.ProposalMeta
	var assistantMsg string

	err := client.BuilderChatStream(ctx, req, func(ev api.BuilderEvent) {
		switch ev.Type {
		case "plan":
			stopSpinner()
			if m := strings.TrimSpace(ev.Message); m != "" {
				fmt.Println(m)
				assistantMsg = m
			}
			if len(ev.Planned) > 0 {
				fmt.Fprintf(os.Stderr, "%s  drafting %d component(s)…%s\n", render.Dim, len(ev.Planned), render.Reset)
			}
		case "proposal":
			stopSpinner()
			var m api.ProposalMeta
			_ = json.Unmarshal(ev.Proposal, &m)
			newPending = append(newPending, ev.Proposal)
			newMeta = append(newMeta, m)
			printProposalLine(m)
		case "failure":
			stopSpinner()
			label := ev.CompType
			if label == "" {
				label = "component"
			}
			render.Warning(fmt.Sprintf("couldn't draft the %s — %s", label, ev.Reason))
		case "done":
			if m := strings.TrimSpace(ev.Message); m != "" {
				assistantMsg = m
			}
		case "error":
			stopSpinner()
			render.Error(ev.Message)
		}
	})
	stopSpinner()
	if err != nil {
		return err
	}

	// Record the turn for resume/history.
	st.history = append(st.history, api.BuilderTurn{Role: "user", Content: message})
	if assistantMsg != "" {
		st.history = append(st.history, api.BuilderTurn{Role: "assistant", Content: assistantMsg})
	}

	// A refinement replaces the whole plan; a pure-conversation turn keeps it.
	if len(newMeta) > 0 {
		st.pending = newPending
		st.pendingMeta = newMeta
		fmt.Fprintf(os.Stderr, "\n%s  %d component(s) planned — type %s/build%s%s to create them.%s\n",
			render.Dim, len(newMeta), render.Bold, render.Reset, render.Dim, render.Reset)
	}
	fmt.Fprintln(os.Stderr)
	return nil
}

// builderBuild executes the pending proposals and prints the result + health.
func builderBuild(client *api.Client, st *builderState) error {
	title := st.firstUserMsg
	if len(title) > 120 {
		title = title[:120]
	}
	spinner := render.NewSpinner("Building…")
	spinner.Start()
	res, err := client.BuilderExecute(api.BuilderExecuteRequest{
		Proposals:      st.pending,
		History:        st.history,
		Title:          title,
		ConversationID: st.conversationID,
	})
	spinner.Stop()
	if err != nil {
		return err
	}

	if res.Summary != "" {
		fmt.Println(res.Summary)
	}
	if res.Partial && res.Error != "" {
		render.Warning("Partial build: " + res.Error)
	}
	healthByID := map[string]api.ComponentHealth{}
	for _, h := range res.Health {
		healthByID[h.ID] = h
	}
	for _, c := range res.Created {
		h, ok := healthByID[c.ID]
		warn := ok && h.Status == "warning" && len(h.Issues) > 0
		mark, color := "✓", render.Green
		if warn {
			mark, color = "!", render.Yellow
		}
		fmt.Fprintf(os.Stderr, "  %s%s%s %s %s(%s)%s %s%s%s\n",
			color, mark, render.Reset, c.Name, render.Dim, typeLabel(c.Type), render.Reset,
			render.Dim, c.URL, render.Reset)
		if warn {
			for _, iss := range h.Issues {
				fmt.Fprintf(os.Stderr, "      %s- %s%s\n", render.Yellow, iss, render.Reset)
			}
		}
	}
	render.Success(fmt.Sprintf("Created %d component(s).", len(res.Created)))

	// Track created components so follow-up turns can edit/wire to them, and clear
	// the pending plan.
	for _, c := range res.Created {
		if b, e := json.Marshal(c); e == nil {
			st.existing = append(st.existing, b)
		}
	}
	st.pending = nil
	st.pendingMeta = nil
	fmt.Fprintln(os.Stderr)
	return nil
}

func printProposalLine(m api.ProposalMeta) {
	verb := "create"
	if m.UpdateOf != "" {
		verb = "update"
	}
	summary := m.Summary
	if summary != "" {
		summary = " — " + summary
	}
	fmt.Fprintf(os.Stderr, "  %s•%s %s%s%s %s[%s %s]%s%s\n",
		render.Cyan, render.Reset, render.Bold, m.Name, render.Reset,
		render.Dim, verb, typeLabel(m.Type), render.Reset, summary)
}

func renderPending(meta []api.ProposalMeta) {
	if len(meta) == 0 {
		render.Info("No plan yet — describe what you want to build.")
		return
	}
	fmt.Fprintf(os.Stderr, "\n%sCurrent plan (%d component(s)):%s\n", render.Bold, len(meta), render.Reset)
	for _, m := range meta {
		printProposalLine(m)
	}
	fmt.Fprintf(os.Stderr, "%s  type /build to create them.%s\n\n", render.Dim, render.Reset)
}

func typeLabel(t string) string {
	switch t {
	case "data_table":
		return "data table"
	default:
		return strings.ReplaceAll(t, "_", " ")
	}
}

func printBuilderHelp() {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Builder Commands:")
	fmt.Fprintln(os.Stderr, "    /build            Create the current plan (cross-wired)")
	fmt.Fprintln(os.Stderr, "    /plan             Show the current (un-built) plan")
	fmt.Fprintln(os.Stderr, "    /new              Start a fresh Builder project")
	fmt.Fprintln(os.Stderr, "    /help             Show this help")
	fmt.Fprintln(os.Stderr, "    /exit             Exit the Builder")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Just type to design or refine — e.g. \"add a priority field\", \"make it simpler\".")
	fmt.Fprintln(os.Stderr, "")
}
