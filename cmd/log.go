package cmd

// arcanflows log — the unified "what did the agent do here, and what did it
// cost" view (PROPOSAL-010 §6.3): joins the local usage ledger's per-turn
// records into a readable development log.

import (
	"fmt"
	"strings"
	"time"

	"github.com/arcanflows/cli/internal/ledger"
	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

var logSince string

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Show local agent usage: turns, tokens, files touched, commands run",
	RunE: func(cmd *cobra.Command, args []string) error {
		var cutoff time.Time
		if logSince != "" {
			d, err := parseSince(logSince)
			if err != nil {
				return err
			}
			cutoff = time.Now().Add(-d)
		}
		entries, err := ledger.ReadSince(cutoff)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			render.Info("No usage recorded here yet — the ledger fills as you chat.")
			return nil
		}

		var totalPrompt, totalCompletion int
		files := map[string]bool{}
		cmds := 0
		for _, e := range entries {
			totalPrompt += e.PromptTokens
			totalCompletion += e.CompletionTokens
			for _, f := range e.FilesTouched {
				files[f] = true
			}
			cmds += len(e.CommandsRun)
			line := fmt.Sprintf("%s  %-18s %6d in / %-6d out", e.Time.Format("Jan 02 15:04"), truncate(e.Agent, 18), e.PromptTokens, e.CompletionTokens)
			if len(e.FilesTouched) > 0 {
				line += fmt.Sprintf("  files: %s", truncate(strings.Join(e.FilesTouched, ", "), 60))
			}
			if len(e.CommandsRun) > 0 {
				line += fmt.Sprintf("  cmds: %d", len(e.CommandsRun))
			}
			fmt.Println(line)
		}
		fmt.Printf("\n%s%d turn(s) · %d prompt + %d completion tokens · %d unique file(s) · %d command(s)%s\n",
			render.Bold, len(entries), totalPrompt, totalCompletion, len(files), cmds, render.Reset)
		return nil
	},
}

// parseSince accepts 7d / 24h / 90m.
func parseSince(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err == nil {
			return time.Duration(days) * 24 * time.Hour, nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	return 0, fmt.Errorf("invalid --since %q (use 7d, 24h, 90m)", s)
}

func truncate(s string, n int) string {
	r := []rune(s) // by character: a byte cut can split "é" in two
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func init() {
	logCmd.Flags().StringVar(&logSince, "since", "", "Only show entries newer than this (7d, 24h, 90m)")
	rootCmd.AddCommand(logCmd)
}
