package cmd

// PROPOSAL-121 S2 — platform skills from the terminal: list, show, run (the
// same background job an agent starts with run-skill), follow, stop.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"time"

	"github.com/arcanflows/cli/internal/api"
	"github.com/arcanflows/cli/internal/render"
	"github.com/spf13/cobra"
)

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "List, inspect and run your agents' skills",
	Long: `Skills are the procedures and know-how attached to your agents (Skills in the console).
A procedure skill runs a workflow as a background job; "skills run" starts it
from the terminal exactly as the agent would in a chat, with the same limits.

Examples:
  arcanflows skills list
  arcanflows skills list --agent sales-assistant
  arcanflows skills show research-brief
  arcanflows skills run research-brief --agent analyst --input topic="EV batteries"
  git log -20 | arcanflows skills run meeting-follow-up --agent pm`,
}

var skillsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the workspace's skills, or the ones attached to an agent",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := requireAuth()
		agentFlag, _ := cmd.Flags().GetString("agent")
		if agentFlag != "" {
			agentID, agentName, err := resolveAgentID(client, agentFlag, "")
			if err != nil {
				return err
			}
			list, err := client.ListAgentSkills(agentID)
			if err != nil {
				return fmt.Errorf("%s", api.ErrorMessage(err))
			}
			if len(list) == 0 {
				render.Info(agentName + " has no skills attached (attach them in the console: Skills)")
				return nil
			}
			rows := [][]string{}
			for _, a := range list {
				if a.Skill == nil {
					continue
				}
				state := "enabled"
				if !a.IsEnabled {
					state = "off"
				}
				if a.UpdateAvailable {
					state += ", update available"
				}
				rows = append(rows, []string{a.Skill.Slug, a.Skill.Name, a.Skill.Kind(), fmt.Sprintf("v%d", a.SkillVersion), state})
			}
			render.Table([]string{"SKILL", "NAME", "KIND", "VERSION", "STATE"}, rows)
			return nil
		}
		list, err := client.ListSkills()
		if err != nil {
			return fmt.Errorf("%s", api.ErrorMessage(err))
		}
		rows := [][]string{}
		for _, s := range list {
			owner := "workspace"
			if s.IsSystem {
				owner = "system"
			}
			rows = append(rows, []string{s.Slug, s.Name, s.Kind(), owner, fmt.Sprintf("%d", s.AttachedAgents), truncate(s.Description, 60)})
		}
		render.Table([]string{"SKILL", "NAME", "KIND", "FROM", "AGENTS", "DESCRIPTION"}, rows)
		return nil
	},
}

var skillsShowCmd = &cobra.Command{
	Use:   "show <skill>",
	Short: "Show a skill: what it does, its inputs, its contract",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := requireAuth()
		list, err := client.ListSkills()
		if err != nil {
			return fmt.Errorf("%s", api.ErrorMessage(err))
		}
		var sk *api.Skill
		for i := range list {
			if api.SkillMatches(&list[i], args[0]) {
				sk = &list[i]
				break
			}
		}
		if sk == nil {
			return fmt.Errorf("no skill %q (see: arcanflows skills list)", args[0])
		}
		fmt.Printf("%s  (%s, v%d)\n  slug: %s\n", sk.Name, sk.Kind(), sk.Version, sk.Slug)
		if sk.Description != "" {
			fmt.Printf("  %s\n", sk.Description)
		}
		if sk.EntryWorkflow != "" {
			fmt.Printf("  runs: %s\n", sk.EntryWorkflow)
		}
		if sk.TriggerPhrase != "" {
			fmt.Printf("  trigger phrase: %q\n", sk.TriggerPhrase)
		}
		if len(sk.Inputs) > 0 {
			fmt.Println("  inputs:")
			for _, in := range sk.Inputs {
				req := ""
				if in.Required {
					req = " (required)"
				}
				fmt.Printf("    %s%s  %s\n", in.Name, req, in.Description)
			}
		}
		for _, d := range sk.Degraded {
			render.Warning(d)
		}
		if agentFlag, _ := cmd.Flags().GetString("agent"); agentFlag != "" && sk.Kind() == "procedure" {
			agentID, _, err := resolveAgentID(client, agentFlag, "")
			if err != nil {
				return err
			}
			if ri, err := client.GetSkillRunInputs(agentID, sk.ID); err == nil {
				printRunInputs(ri)
			} else {
				render.Warning(api.ErrorMessage(err))
			}
		}
		if sk.Instruction != "" {
			fmt.Printf("\n%s\n", sk.Instruction)
		}
		return nil
	},
}

// printRunInputs lists what a direct run of the skill takes.
func printRunInputs(ri *api.SkillRunInputs) {
	fmt.Fprintf(os.Stderr, "  run inputs (%s):\n", ri.Workflow)
	for _, in := range ri.Inputs {
		tag := ""
		switch {
		case in.Required:
			tag = " (required)"
		case in.Default != "":
			tag = fmt.Sprintf(" (default %s)", in.Default)
		}
		fmt.Fprintf(os.Stderr, "    --input %s=…%s  %s\n", in.Name, tag, in.Description)
	}
}

var skillsRunCmd = &cobra.Command{
	Use:   "run <skill> [text…]",
	Short: "Run a procedure skill on an agent and follow it to the result",
	Long: `Start a procedure skill attached to an agent — the same background job, queue,
cost limits and ledger as when the agent starts it in a chat — and follow it
until it finishes. Text after the skill name (and piped input) is handed to
the skill as its source material; --input sets a named input.

Examples:
  arcanflows skills run research-brief --agent analyst --input topic="EV batteries"
  cat notes.md | arcanflows skills run meeting-follow-up --agent pm
  arcanflows skills run data-report --agent ops --detach`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := requireAuth()
		agentFlag, _ := cmd.Flags().GetString("agent")
		agentIDFlag, _ := cmd.Flags().GetString("agent-id")
		inputFlags, _ := cmd.Flags().GetStringArray("input")
		detach, _ := cmd.Flags().GetBool("detach")
		waitStdin, _ := cmd.Flags().GetBool("stdin")
		asJSON, _ := cmd.Flags().GetBool("json")

		inputs := map[string]any{}
		for _, kv := range inputFlags {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || strings.TrimSpace(k) == "" {
				return fmt.Errorf("--input takes name=value, got %q", kv)
			}
			inputs[strings.TrimSpace(k)] = v
		}
		source := strings.Join(args[1:], " ")
		if stdin := readPipedStdin(waitStdin); stdin != "" {
			source = strings.TrimSpace(source + "\n\n" + stdin)
		}
		return runSkill(client, args[0], agentFlag, agentIDFlag, inputs, source, detach, asJSON)
	},
}

// runSkill resolves the agent and its attached skill, starts the run and —
// unless detached — follows it to the end.
func runSkill(client *api.Client, ref, agentFlag, agentIDFlag string, inputs map[string]any, source string, detach, asJSON bool) error {
	agentID, agentName, err := resolveAgentID(client, agentFlag, agentIDFlag)
	if err != nil {
		return err
	}
	attached, err := client.ListAgentSkills(agentID)
	if err != nil {
		return fmt.Errorf("%s", api.ErrorMessage(err))
	}
	var sk *api.Skill
	for _, a := range attached {
		if api.SkillMatches(a.Skill, ref) {
			sk = a.Skill
			break
		}
	}
	if sk == nil {
		names := []string{}
		for _, a := range attached {
			if a.Skill != nil && a.Skill.Kind() == "procedure" {
				names = append(names, a.Skill.Slug)
			}
		}
		if len(names) == 0 {
			return fmt.Errorf("%s has no procedure skills attached (attach one in the console: Skills)", agentName)
		}
		return fmt.Errorf("%s has no skill %q; it can run: %s", agentName, ref, strings.Join(names, ", "))
	}
	run, err := client.StartSkillRun(agentID, sk.ID, inputs, source)
	if err != nil {
		msg := api.ErrorMessage(err)
		if strings.Contains(msg, "missing required inputs") {
			if ri, ierr := client.GetSkillRunInputs(agentID, sk.ID); ierr == nil {
				printRunInputs(ri)
				return fmt.Errorf("missing required inputs — pass them with --input name=value")
			}
		}
		return fmt.Errorf("%s", msg)
	}
	if asJSON && detach {
		return printJSON(run)
	}
	steps := []string{}
	for _, p := range run.Plan {
		steps = append(steps, p.Name)
	}
	if !asJSON {
		render.Info(fmt.Sprintf("%s on %s", sk.Name, agentName))
		if len(steps) > 0 {
			fmt.Fprintf(os.Stderr, "  plan: %s\n", strings.Join(steps, " → "))
		}
		if run.EstimateUSD != nil {
			fmt.Fprintf(os.Stderr, "  estimate: about $%.2f\n", *run.EstimateUSD)
		}
	}
	execID := run.ExecutionID
	if run.Queued && run.QueueID != nil {
		if !asJSON {
			fmt.Fprintf(os.Stderr, "  queued (position %d) — the workspace is at its limit of concurrent runs\n", run.Position)
		}
		if detach {
			fmt.Fprintf(os.Stderr, "  queue id: %s\n", *run.QueueID)
			return nil
		}
		for {
			time.Sleep(3 * time.Second)
			q, err := client.GetSkillQueueItem(*run.QueueID)
			if err != nil {
				return fmt.Errorf("%s", api.ErrorMessage(err))
			}
			if q.ExecutionID != nil {
				execID = *q.ExecutionID
				break
			}
			if q.Status == "cancelled" || q.Status == "failed" {
				return fmt.Errorf("the queued run %s: %s", q.Status, q.Error)
			}
		}
	}
	if detach {
		fmt.Printf("%s\n", execID)
		fmt.Fprintf(os.Stderr, "  follow: arcanflows skills status %s %s --agent-id %s\n", sk.Slug, execID, agentID)
		return nil
	}
	return followSkillRun(client, agentID, sk, execID, asJSON)
}

func followSkillRun(client *api.Client, agentID string, sk *api.Skill, execID string, asJSON bool) error {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		st, err := client.GetSkillRun(agentID, sk.ID, execID)
		if err != nil {
			return fmt.Errorf("%s", api.ErrorMessage(err))
		}
		if st.Done() {
			if !asJSON {
				fmt.Fprint(os.Stderr, "\r\033[K")
			}
			return reportSkillRun(st, asJSON)
		}
		if !asJSON {
			step := st.CurrentNode
			if step == "" {
				step = "starting"
			}
			fmt.Fprintf(os.Stderr, "\r\033[K  %s · %d steps done · %s · %ds", st.Status, st.StepsDone, step, st.ElapsedSec)
		}
		select {
		case <-interrupt:
			fmt.Fprintf(os.Stderr, "\n  still running in the background — follow: arcanflows skills status %s %s --agent-id %s\n  stop: arcanflows skills stop %s %s --agent-id %s\n",
				sk.Slug, execID, agentID, sk.Slug, execID, agentID)
			return nil
		case <-tick.C:
		}
	}
}

func reportSkillRun(st *api.SkillRunStatus, asJSON bool) error {
	if asJSON {
		return printJSON(st)
	}
	switch st.Status {
	case "completed":
		render.Success(fmt.Sprintf("completed in %ds · %d steps · $%.4f", st.ElapsedSec, st.StepsDone, st.CostUSD))
	default:
		render.Error(fmt.Sprintf("%s after %ds: %s", st.Status, st.ElapsedSec, st.Error))
	}
	if st.Summary != "" {
		fmt.Printf("\n%s\n", st.Summary)
	}
	for _, l := range st.Links {
		fmt.Printf("  %s\n", l)
	}
	if st.Status != "completed" {
		return fmt.Errorf("the skill run %s", st.Status)
	}
	return nil
}

var skillsStatusCmd = &cobra.Command{
	Use:   "status <skill> <run-id>",
	Short: "Show (and follow with --follow) a skill run",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := requireAuth()
		agentID, sk, err := agentAndSkill(cmd, client, args[0])
		if err != nil {
			return err
		}
		follow, _ := cmd.Flags().GetBool("follow")
		asJSON, _ := cmd.Flags().GetBool("json")
		if follow {
			return followSkillRun(client, agentID, sk, args[1], asJSON)
		}
		st, err := client.GetSkillRun(agentID, sk.ID, args[1])
		if err != nil {
			return fmt.Errorf("%s", api.ErrorMessage(err))
		}
		if !st.Done() && !asJSON {
			fmt.Printf("%s · %d steps done · %s · %ds\n", st.Status, st.StepsDone, st.CurrentNode, st.ElapsedSec)
			return nil
		}
		return reportSkillRun(st, asJSON)
	},
}

var skillsStopCmd = &cobra.Command{
	Use:   "stop <skill> <run-id>",
	Short: "Stop a running skill",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := requireAuth()
		agentID, sk, err := agentAndSkill(cmd, client, args[0])
		if err != nil {
			return err
		}
		if err := client.StopSkillRun(agentID, sk.ID, args[1]); err != nil {
			return fmt.Errorf("%s", api.ErrorMessage(err))
		}
		render.Success("stopping " + args[1])
		return nil
	},
}

func agentAndSkill(cmd *cobra.Command, client *api.Client, ref string) (string, *api.Skill, error) {
	agentFlag, _ := cmd.Flags().GetString("agent")
	agentIDFlag, _ := cmd.Flags().GetString("agent-id")
	agentID, agentName, err := resolveAgentID(client, agentFlag, agentIDFlag)
	if err != nil {
		return "", nil, err
	}
	attached, err := client.ListAgentSkills(agentID)
	if err != nil {
		return "", nil, fmt.Errorf("%s", api.ErrorMessage(err))
	}
	for _, a := range attached {
		if api.SkillMatches(a.Skill, ref) {
			return agentID, a.Skill, nil
		}
	}
	return "", nil, fmt.Errorf("%s has no skill %q", agentName, ref)
}

var inputArg = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)

// splitSkillArgs separates name=value words (inputs) from the free text.
func splitSkillArgs(s string) (map[string]any, string) {
	inputs := map[string]any{}
	var rest []string
	for _, w := range splitWords(s) {
		if m := inputArg.FindStringSubmatch(w); m != nil {
			inputs[m[1]] = m[2]
			continue
		}
		rest = append(rest, w)
	}
	return inputs, strings.Join(rest, " ")
}

// splitWords splits on spaces, keeping "double-quoted phrases" together.
func splitWords(s string) []string {
	var out []string
	var cur strings.Builder
	inQ := false
	for _, r := range s {
		switch {
		case r == '"':
			inQ = !inQ
		case r == ' ' && !inQ:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func init() {
	for _, c := range []*cobra.Command{skillsListCmd, skillsShowCmd, skillsRunCmd, skillsStatusCmd, skillsStopCmd} {
		c.Flags().String("agent", "", "agent slug or name")
	}
	for _, c := range []*cobra.Command{skillsRunCmd, skillsStatusCmd, skillsStopCmd} {
		c.Flags().String("agent-id", "", "agent ID (UUID)")
	}
	skillsRunCmd.Flags().StringArray("input", nil, "a named input, name=value (repeatable)")
	skillsRunCmd.Flags().Bool("detach", false, "start the run and return its id instead of following it")
	skillsRunCmd.Flags().Bool("stdin", false, "wait for piped input however long it takes")
	skillsRunCmd.Flags().Bool("json", false, "print the result as JSON")
	skillsStatusCmd.Flags().Bool("follow", false, "follow the run until it finishes")
	skillsStatusCmd.Flags().Bool("json", false, "print the status as JSON")
	skillsCmd.AddCommand(skillsListCmd, skillsShowCmd, skillsRunCmd, skillsStatusCmd, skillsStopCmd)
	rootCmd.AddCommand(skillsCmd)
}
