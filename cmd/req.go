package cmd

// arcanflows req — manage REQUIREMENTS.md (PROPOSAL-010 §6.2, D4: a visible
// root file, for humans first). The file is injected as chat context, and the
// agent may PROPOSE edits to it through the normal diff+confirm path — the
// human stays the editor of record.

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

const reqFile = "REQUIREMENTS.md"

var reqCmd = &cobra.Command{
	Use:   "req",
	Short: "Manage project requirements (REQUIREMENTS.md)",
}

var reqAddCmd = &cobra.Command{
	Use:   "add <requirement...>",
	Short: "Add a requirement",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		text := strings.Join(args, " ")
		f, err := os.OpenFile(reqFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		defer f.Close()
		if fi, _ := f.Stat(); fi != nil && fi.Size() == 0 {
			fmt.Fprintln(f, "# Requirements")
			fmt.Fprintln(f)
		}
		fmt.Fprintf(f, "- [ ] %s\n", text)
		render.Success(fmt.Sprintf("Added: %s", text))
		return nil
	},
}

var reqListCmd = &cobra.Command{
	Use:   "list",
	Short: "List requirements with their numbers",
	RunE: func(cmd *cobra.Command, args []string) error {
		items, err := readRequirements()
		if err != nil {
			return err
		}
		if len(items) == 0 {
			render.Info("No requirements yet — add one with: arcanflows req add <text>")
			return nil
		}
		for i, it := range items {
			mark := " "
			if it.done {
				mark = "x"
			}
			fmt.Printf("  %2d. [%s] %s\n", i+1, mark, it.text)
		}
		return nil
	},
}

var reqDoneCmd = &cobra.Command{
	Use:   "done <number>",
	Short: "Mark a requirement done",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 {
			return fmt.Errorf("give the requirement number from 'arcanflows req list'")
		}
		data, err := os.ReadFile(reqFile)
		if err != nil {
			return fmt.Errorf("no %s found", reqFile)
		}
		lines := strings.Split(string(data), "\n")
		count := 0
		for i, line := range lines {
			t := strings.TrimSpace(line)
			if strings.HasPrefix(t, "- [ ]") || strings.HasPrefix(t, "- [x]") {
				count++
				if count == n {
					lines[i] = strings.Replace(line, "- [ ]", "- [x]", 1)
					if err := os.WriteFile(reqFile, []byte(strings.Join(lines, "\n")), 0644); err != nil {
						return err
					}
					render.Success(fmt.Sprintf("Done: %s", strings.TrimPrefix(strings.TrimPrefix(t, "- [ ] "), "- [x] ")))
					return nil
				}
			}
		}
		return fmt.Errorf("requirement %d not found (%d total)", n, count)
	},
}

type reqItem struct {
	text string
	done bool
}

func readRequirements() ([]reqItem, error) {
	data, err := os.ReadFile(reqFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var items []reqItem
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(t, "- [ ] "); ok {
			items = append(items, reqItem{text: v})
		} else if v, ok := strings.CutPrefix(t, "- [x] "); ok {
			items = append(items, reqItem{text: v, done: true})
		}
	}
	return items, nil
}

func init() {
	reqCmd.AddCommand(reqAddCmd, reqListCmd, reqDoneCmd)
	rootCmd.AddCommand(reqCmd)
}
