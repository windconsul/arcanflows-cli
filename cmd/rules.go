package cmd

// arcanflows rules — show the effective layered rule set (PROPOSAL-010 §6.1):
// which files contributed, the prompt-guidance prose, and the mechanically
// enforced [deny] entries, plus the hash sessions record for drift visibility.

import (
	"fmt"

	"github.com/arcanflows/cli/internal/render"
	"github.com/arcanflows/cli/internal/rules"
	"github.com/spf13/cobra"
)

var rulesCmd = &cobra.Command{
	Use:   "rules",
	Short: "Show the effective user rules (global + project) and enforced [deny] entries",
	RunE: func(cmd *cobra.Command, args []string) error {
		r := rules.Load(".")
		if len(r.Sources) == 0 {
			render.Info("No rules found. Create ~/.arcanflows/RULES.md (global) or ARCANFLOWS.md (project).")
			return nil
		}
		fmt.Printf("%sEffective rules%s (hash %s)\n\n", render.Bold, render.Reset, r.Hash)
		for _, src := range r.Sources {
			fmt.Printf("  source: %s\n", src)
		}
		if r.GlobalText != "" {
			fmt.Printf("\n%sGlobal guidance (sent to the agent):%s\n%s\n", render.Bold, render.Reset, r.GlobalText)
		}
		if len(r.DenyPaths)+len(r.DenyCommands) > 0 {
			fmt.Printf("\n%sEnforced [deny] rules (checked by the CLI at apply time):%s\n", render.Bold, render.Reset)
			for _, p := range r.DenyPaths {
				fmt.Printf("  path:    %s\n", p)
			}
			for _, c := range r.DenyCommands {
				fmt.Printf("  command: %s\n", c)
			}
			fmt.Printf("%s\n(override for one session with: arcanflows chat --allow-denied)%s\n", render.Dim, render.Reset)
		} else {
			fmt.Println("\nNo [deny] entries — all enforcement comes from the built-in protected paths.")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(rulesCmd)
}
