package cmd

// arcanflows commit — agent-drafted commit messages from the REAL diff
// (PROPOSAL-010 §6.5). The agent drafts; the human confirms; git commits.
// Fires the pre_commit/post_commit hooks that have existed since Phase 2
// without ever having a command to serve.

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/arcanflows/cli/internal/api"
	"github.com/arcanflows/cli/internal/gitctx"
	"github.com/arcanflows/cli/internal/hooks"
	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

var commitAgent string

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Draft a commit message from the diff with an agent, confirm, commit",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !gitctx.IsRepo() {
			return fmt.Errorf("not a git repository")
		}
		diff, staged := gitctx.Diff()
		if strings.TrimSpace(diff) == "" {
			render.Info("Nothing to commit — working tree clean.")
			return nil
		}
		scope := "staged changes"
		if !staged {
			scope = "unstaged changes (nothing staged — will commit with -a)"
		}
		render.Info(fmt.Sprintf("Drafting commit message from %s (%d bytes of diff)…", scope, len(diff)))

		client := newAPIClient()
		agentRef := commitAgent
		if agentRef == "" {
			if p, ok := cfg.Profiles[cfg.ActiveProfile]; ok {
				agentRef = p.DefaultAgent
			}
		}
		if agentRef == "" {
			return fmt.Errorf("no agent: pass --agent or set a default with 'arcanflows chat'")
		}
		agentID, _, err := resolveAgentID(client, agentRef, "")
		if err != nil {
			return err
		}

		prompt := "Write a conventional-commit message for this diff. Reply with ONLY the commit message (subject line, blank line, short body). No preamble, no code fences.\n\n" + diff
		var message strings.Builder
		err = client.ChatStream(agentID, api.ChatRequest{Message: prompt}, func(chunk api.StreamChunk) {
			if chunk.Delta != "" {
				message.WriteString(chunk.Delta)
			} else if chunk.Content != "" && chunk.Done && message.Len() == 0 {
				message.WriteString(chunk.Content)
			}
		})
		if err != nil {
			return fmt.Errorf("drafting message: %w", err)
		}
		msg := strings.TrimSpace(strings.Trim(strings.TrimSpace(message.String()), "`"))
		if msg == "" {
			return fmt.Errorf("the agent returned an empty message")
		}

		fmt.Printf("\n%sProposed commit message:%s\n%s\n\n", render.Bold, render.Reset, msg)
		if !render.Confirm("Commit with this message?") {
			render.Info("Aborted — nothing committed.")
			return nil
		}

		hooksMgr := hooks.Load()
		vars := hooks.Variables{}
		if hooksMgr != nil {
			if _, herr := hooksMgr.Run(hooks.PreCommit, vars); herr != nil {
				return fmt.Errorf("blocked by pre_commit hook: %w", herr)
			}
		}
		gitArgs := []string{"commit", "-m", msg}
		if !staged {
			gitArgs = []string{"commit", "-a", "-m", msg}
		}
		out, err := exec.Command("git", gitArgs...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("git commit failed: %s", strings.TrimSpace(string(out)))
		}
		fmt.Print(string(out))
		if hooksMgr != nil {
			hooksMgr.Run(hooks.PostCommit, vars)
		}
		render.Success("Committed.")
		return nil
	},
}

func init() {
	commitCmd.Flags().StringVar(&commitAgent, "agent", "", "Agent to draft the message (default: profile default agent)")
	rootCmd.AddCommand(commitCmd)
}
