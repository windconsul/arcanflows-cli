package cmd

// PROPOSAL-121 S1 — user-defined commands: Markdown prompt files in
// .arcanflows/commands/ (project) and ~/.arcanflows/commands/ (user), run as
// /name inside `arcanflows chat` or as `arcanflows run name`.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/arcanflows/cli/internal/api"
	"github.com/arcanflows/cli/internal/commands"
	"github.com/arcanflows/cli/internal/config"
	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

var commandsCmd = &cobra.Command{
	Use:   "commands",
	Short: "Your own commands: prompt files you run as /name",
	Long: `Commands are Markdown prompt files you run as /name inside "arcanflows chat"
or as "arcanflows run name" from the shell.

Where they live (a project command wins over a user command with the same name):
  .arcanflows/commands/<name>.md     this project
  ~/.arcanflows/commands/<name>.md   every project

A file is a prompt with optional frontmatter:
  ---
  description: Weekly sales report
  argument-hint: "[week]"
  agent: sales-analyst      # optional: send to this agent
  ---
  Build the weekly sales report for $ARGUMENTS.

$ARGUMENTS is everything after the command name; $1, $2 … are single words.
Commands are text: nothing in them runs on your machine.

Examples:
  arcanflows commands init             Add the starter set to this project
  arcanflows commands new standup      Create .arcanflows/commands/standup.md
  arcanflows commands list
  arcanflows run review internal/api/client.go`,
}

func projectDir() string {
	cwd, _ := os.Getwd()
	return cwd
}

func loadCommands() map[string]*commands.Command {
	cmds, warns := commands.Load(projectDir(), config.Dir())
	for _, w := range warns {
		render.Warning(w)
	}
	return cmds
}

func commandTarget(c *commands.Command) string {
	switch {
	case c.Skill != "":
		return "skill " + c.Skill
	case c.Agent != "":
		return "agent " + c.Agent
	default:
		return "current agent"
	}
}

var commandsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List your commands",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmds := loadCommands()
		if len(cmds) == 0 {
			render.Info("No commands yet. Add the starter set with: arcanflows commands init")
			return nil
		}
		rows := [][]string{}
		for _, c := range commands.Sorted(cmds) {
			rows = append(rows, []string{"/" + c.Name, c.Scope, commandTarget(c), c.Description})
		}
		render.Table([]string{"COMMAND", "SCOPE", "SENDS TO", "DESCRIPTION"}, rows)
		return nil
	},
}

var commandsShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show a command's file and prompt",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := findCommand(args[0])
		if err != nil {
			return err
		}
		fmt.Printf("/%s  (%s)\n  file:     %s\n  sends to: %s\n", c.Name, c.Scope, c.Path, commandTarget(c))
		if c.Description != "" {
			fmt.Printf("  about:    %s\n", c.Description)
		}
		if c.ArgumentHint != "" {
			fmt.Printf("  usage:    /%s %s\n", c.Name, c.ArgumentHint)
		}
		warnUnsupported(c)
		fmt.Printf("\n%s\n", c.Body)
		return nil
	},
}

var commandsNewCmd = &cobra.Command{
	Use:   "new <name>",
	Short: "Create a command file from a scaffold",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := strings.ToLower(strings.TrimPrefix(args[0], "/"))
		if _, err := commands.Parse(name, []byte(commands.Scaffold(name))); err != nil {
			return err
		}
		if commands.Builtins[name] {
			return fmt.Errorf("/%s is a built-in chat command; pick another name", name)
		}
		dir := commandDir(cmd)
		path := filepath.Join(dir, name+".md")
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists", path)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(commands.Scaffold(name)), 0o644); err != nil {
			return err
		}
		if _, warns := commands.Load(projectDir(), config.Dir()); len(warns) > 0 {
			for _, w := range warns {
				render.Warning(w)
			}
		}
		render.Success(fmt.Sprintf("Created %s — edit the prompt, then run /%s in chat", path, name))
		return nil
	},
}

var commandsInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Add the starter commands (plan, review, research, weekly-report …)",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := commandDir(cmd)
		force, _ := cmd.Flags().GetBool("force")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		names := make([]string, 0, len(commands.Starter))
		for n := range commands.Starter {
			names = append(names, n)
		}
		sort.Strings(names)
		var added, kept []string
		for _, n := range names {
			path := filepath.Join(dir, n+".md")
			if _, err := os.Stat(path); err == nil && !force {
				kept = append(kept, "/"+n)
				continue
			}
			if err := os.WriteFile(path, []byte(commands.Starter[n]), 0o644); err != nil {
				return err
			}
			added = append(added, "/"+n)
		}
		if len(added) > 0 {
			render.Success(fmt.Sprintf("Added %s in %s", strings.Join(added, " "), dir))
		}
		if len(kept) > 0 {
			render.Info(fmt.Sprintf("Kept your existing %s (use --force to replace)", strings.Join(kept, " ")))
		}
		return nil
	},
}

var runCmd = &cobra.Command{
	Use:   "run <command> [arguments...]",
	Short: "Run one of your commands once and print the answer",
	Long: `Run a command from .arcanflows/commands/ or ~/.arcanflows/commands/ once,
like "ask", and print the answer. See "arcanflows commands --help".

Examples:
  arcanflows run weekly-report 2026-W46
  arcanflows run review internal/api/client.go --agent code-reviewer
  git diff | arcanflows run review`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := findCommand(args[0])
		if err != nil {
			return err
		}
		input := strings.Join(args[1:], " ")
		waitStdin, _ := cmd.Flags().GetBool("stdin")
		if stdin := readPipedStdin(waitStdin); stdin != "" {
			input = strings.TrimSpace(input + "\n\n" + stdin)
		}
		warnUnsupported(c)
		client := requireAuth()
		agentFlag, _ := cmd.Flags().GetString("agent")
		agentIDFlag, _ := cmd.Flags().GetString("agent-id")
		if agentFlag == "" && agentIDFlag == "" {
			agentFlag = c.Agent
		}
		if c.Skill != "" {
			// name=value words are the skill's inputs; the rendered prompt is
			// its source material.
			inputs, text := splitSkillArgs(input)
			return runSkill(client, c.Skill, agentFlag, agentIDFlag, inputs, c.Render(text), false, false)
		}
		agentID, _, err := resolveAgentID(client, agentFlag, agentIDFlag)
		if err != nil {
			return err
		}
		err = client.ChatStream(agentID, api.ChatRequest{Message: c.Render(input)}, printAnswer())
		fmt.Println()
		return err
	},
}

func findCommand(name string) (*commands.Command, error) {
	name = strings.ToLower(strings.TrimPrefix(name, "/"))
	cmds := loadCommands()
	if c, ok := cmds[name]; ok {
		return c, nil
	}
	return nil, fmt.Errorf("no command /%s (see: arcanflows commands list)", name)
}

func warnUnsupported(c *commands.Command) {
	if c.Model != "" {
		render.Warning(fmt.Sprintf("/%s: model %q is ignored — the agent always answers with its own model", c.Name, c.Model))
	}
	if len(c.Ignored) > 0 {
		render.Warning(fmt.Sprintf("/%s: frontmatter %s has no meaning here and is ignored", c.Name, strings.Join(c.Ignored, ", ")))
	}
}

func commandDir(cmd *cobra.Command) string {
	if user, _ := cmd.Flags().GetBool("user"); user {
		return filepath.Join(config.Dir(), "commands")
	}
	return filepath.Join(projectDir(), ".arcanflows", "commands")
}

// readPipedStdin returns what is piped in, else "". stdin that is not a
// terminal but never delivers anything (cron, CI, a parent that keeps the
// pipe open) must not hang the command: unless wait is set, input that has
// not started arriving within half a second is treated as absent.
func readPipedStdin(wait bool) string {
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice != 0 {
		return ""
	}
	type result struct {
		b   []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		b, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		ch <- result{b, err}
	}()
	if wait {
		r := <-ch
		return strings.TrimSpace(string(r.b))
	}
	select {
	case r := <-ch:
		return strings.TrimSpace(string(r.b))
	case <-time.After(500 * time.Millisecond):
		return ""
	}
}

func init() {
	commandsNewCmd.Flags().Bool("user", false, "create it in ~/.arcanflows/commands (every project)")
	commandsInitCmd.Flags().Bool("user", false, "add them to ~/.arcanflows/commands (every project)")
	commandsInitCmd.Flags().Bool("force", false, "replace starter files you already have")
	runCmd.Flags().String("agent", "", "agent slug or name (overrides the command's agent)")
	runCmd.Flags().String("agent-id", "", "agent ID (UUID)")
	runCmd.Flags().Bool("stdin", false, "wait for piped input however long it takes (default: skip stdin that sends nothing within 0.5 s)")
	commandsCmd.AddCommand(commandsListCmd, commandsShowCmd, commandsNewCmd, commandsInitCmd)
	rootCmd.AddCommand(commandsCmd, runCmd)
}

// expandCustomCommand resolves "/name args" against the command files. ok is
// false when name is not a custom command (the built-in handling continues).
// rendered is "" when the command was fully handled here (sent to another
// agent, or refused) and nothing should be sent to the chat's agent.
func expandCustomCommand(client *api.Client, currentAgentID, input string) (rendered string, ok bool) {
	name, args := commands.SplitInvocation(input)
	if commands.Builtins[name] {
		return "", false
	}
	cmds, _ := commands.Load(projectDir(), config.Dir())
	c, found := cmds[name]
	if !found {
		return "", false
	}
	warnUnsupported(c)
	if c.Skill != "" {
		inputs, text := splitSkillArgs(args)
		agentFlag, agentIDFlag := c.Agent, ""
		if agentFlag == "" {
			agentIDFlag = currentAgentID
		}
		if err := runSkill(client, c.Skill, agentFlag, agentIDFlag, inputs, c.Render(text), false, false); err != nil {
			render.Error(fmt.Sprintf("/%s: %v", c.Name, err))
		}
		return "", true
	}
	prompt := c.Render(args)
	if c.Agent != "" {
		targetID, targetName, err := resolveAgentID(client, c.Agent, "")
		if err != nil {
			render.Error(fmt.Sprintf("/%s: %v", c.Name, err))
			return "", true
		}
		if targetID != currentAgentID {
			// Another agent answers this one: a one-off question, outside
			// this conversation.
			render.Info(fmt.Sprintf("/%s → %s", c.Name, targetName))
			err := client.ChatStream(targetID, api.ChatRequest{Message: prompt}, printAnswer())
			fmt.Println()
			if err != nil {
				render.Error(err.Error())
			}
			return "", true
		}
	}
	render.Info(fmt.Sprintf("/%s", c.Name))
	return prompt, true
}

func printCustomCommandsHelp() {
	cmds, _ := commands.Load(projectDir(), config.Dir())
	if len(cmds) == 0 {
		fmt.Fprintln(os.Stderr, "\n  Your commands: none yet (arcanflows commands init)")
		return
	}
	fmt.Fprintln(os.Stderr, "\n  Your commands:")
	for _, c := range commands.Sorted(cmds) {
		label := "/" + c.Name
		if c.ArgumentHint != "" {
			label += " " + c.ArgumentHint
		}
		fmt.Fprintf(os.Stderr, "    %-28s %s\n", label, c.Description)
	}
}

// printAnswer prints a streamed answer once: the deltas as they arrive, and
// the final content only when nothing was streamed. The done event repeats
// the whole answer, so printing both doubled every reply.
func printAnswer() func(api.StreamChunk) {
	streamed := false
	return func(chunk api.StreamChunk) {
		if chunk.Delta != "" {
			streamed = true
			fmt.Print(chunk.Delta)
		} else if chunk.Done && chunk.Content != "" && !streamed {
			fmt.Print(chunk.Content)
		}
	}
}
