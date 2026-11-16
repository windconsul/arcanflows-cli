// Package commands loads user-defined CLI commands (PROPOSAL-121 S1).
//
// A command is a Markdown file with optional YAML frontmatter:
//
//	---
//	description: Weekly sales report from the CRM table
//	argument-hint: "[week]"
//	agent: sales-analyst   # optional: agent slug, name or id
//	skill: data-report     # optional: run this platform skill instead of a chat turn
//	model:                 # optional model override
//	---
//	Build the weekly sales report for $ARGUMENTS.
//
// Files live in <project>/.arcanflows/commands/ and ~/.arcanflows/commands/;
// a project command shadows a user command of the same name. The frontmatter
// keys match Claude Code / ECC command files, so those mostly drop in. A
// command is text: nothing in it is ever executed locally.
package commands

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	ScopeProject = "project"
	ScopeUser    = "user"
)

// Command is one loaded command file.
type Command struct {
	Name         string
	Description  string
	ArgumentHint string
	Agent        string
	Skill        string
	Model        string
	Body         string
	Path         string
	Scope        string
	// Ignored lists frontmatter keys that have no meaning here.
	Ignored []string
}

type frontmatter struct {
	Description  string `yaml:"description"`
	ArgumentHint string `yaml:"argument-hint"`
	Agent        string `yaml:"agent"`
	Skill        string `yaml:"skill"`
	Model        string `yaml:"model"`
}

var known = map[string]bool{"description": true, "argument-hint": true, "agent": true, "skill": true, "model": true, "name": true}

// validName: what may follow the slash.
var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Builtins are the chat REPL's own commands; a file cannot replace them.
var Builtins = map[string]bool{
	"help": true, "file": true, "create": true, "run": true, "diff": true, "status": true,
	"tree": true, "undo": true, "context": true, "sessions": true, "history": true,
	"hooks": true, "clear": true, "exit": true, "quit": true,
}

// Dirs returns the command directories, project first.
func Dirs(projectDir, configDir string) []struct{ Dir, Scope string } {
	return []struct{ Dir, Scope string }{
		{filepath.Join(projectDir, ".arcanflows", "commands"), ScopeProject},
		{filepath.Join(configDir, "commands"), ScopeUser},
	}
}

// Load reads every command file. Problems with single files are returned as
// warnings; they never stop the others from loading.
func Load(projectDir, configDir string) (map[string]*Command, []string) {
	out := map[string]*Command{}
	var warns []string
	for _, d := range Dirs(projectDir, configDir) {
		entries, err := os.ReadDir(d.Dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
				continue
			}
			name := strings.ToLower(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
			path := filepath.Join(d.Dir, e.Name())
			if _, taken := out[name]; taken {
				continue // the project command already won
			}
			if !validName.MatchString(name) {
				warns = append(warns, fmt.Sprintf("%s: %q is not a valid command name (a-z, 0-9, - and _)", path, name))
				continue
			}
			if Builtins[name] {
				warns = append(warns, fmt.Sprintf("%s: /%s is a built-in command and cannot be replaced", path, name))
				continue
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				warns = append(warns, fmt.Sprintf("%s: %v", path, err))
				continue
			}
			c, err := Parse(name, raw)
			if err != nil {
				warns = append(warns, fmt.Sprintf("%s: %v", path, err))
				continue
			}
			c.Path, c.Scope = path, d.Scope
			out[name] = c
		}
	}
	return out, warns
}

// Parse reads one command file's content.
func Parse(name string, raw []byte) (*Command, error) {
	c := &Command{Name: name}
	body := string(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF}))
	norm := strings.ReplaceAll(body, "\r\n", "\n")
	if strings.HasPrefix(norm, "---\n") {
		end := strings.Index(norm[4:], "\n---")
		if end < 0 {
			return nil, fmt.Errorf("frontmatter is not closed with ---")
		}
		head := norm[4 : 4+end]
		rest := norm[4+end+4:]
		rest = strings.TrimPrefix(rest, "\n")
		var fm frontmatter
		if err := yaml.Unmarshal([]byte(head), &fm); err != nil {
			return nil, fmt.Errorf("frontmatter: %w", err)
		}
		var all map[string]any
		_ = yaml.Unmarshal([]byte(head), &all)
		for k := range all {
			if !known[k] {
				c.Ignored = append(c.Ignored, k)
			}
		}
		sort.Strings(c.Ignored)
		c.Description, c.ArgumentHint = strings.TrimSpace(fm.Description), strings.TrimSpace(fm.ArgumentHint)
		c.Agent, c.Skill, c.Model = strings.TrimSpace(fm.Agent), strings.TrimSpace(fm.Skill), strings.TrimSpace(fm.Model)
		norm = rest
	}
	c.Body = strings.TrimSpace(norm)
	if c.Body == "" && c.Skill == "" {
		return nil, fmt.Errorf("the command has no prompt text")
	}
	return c, nil
}

var placeholder = regexp.MustCompile(`\$\{?(ARGUMENTS|[1-9])\}?`)

// Render substitutes $ARGUMENTS (all arguments as typed) and $1…$9 (words).
// When the body uses neither and arguments were given, they are appended on
// their own line so nothing the user typed is dropped.
func (c *Command) Render(args string) string {
	args = strings.TrimSpace(args)
	words := splitArgs(args)
	used := false
	out := placeholder.ReplaceAllStringFunc(c.Body, func(m string) string {
		used = true
		key := strings.Trim(m, "${}")
		if key == "ARGUMENTS" {
			return args
		}
		i, _ := strconv.Atoi(key)
		if i >= 1 && i <= len(words) {
			return words[i-1]
		}
		return ""
	})
	if !used && args != "" {
		out = strings.TrimRight(out, "\n") + "\n\n" + args
	}
	return out
}

// splitArgs splits on spaces, keeping "double-quoted phrases" together.
func splitArgs(s string) []string {
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

// Sorted returns the commands by name.
func Sorted(m map[string]*Command) []*Command {
	out := make([]*Command, 0, len(m))
	for _, c := range m {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SplitInvocation turns "/name rest of line" into ("name", "rest of line").
func SplitInvocation(input string) (string, string) {
	s := strings.TrimPrefix(strings.TrimSpace(input), "/")
	name, rest, _ := strings.Cut(s, " ")
	return strings.ToLower(name), strings.TrimSpace(rest)
}

// Scaffold is the file `commands new` writes.
func Scaffold(name string) string {
	return fmt.Sprintf(`---
description: What /%s does, in one line
argument-hint: "[what to pass]"
# agent: my-agent        # optional: send to this agent (slug, name or id)
# skill: data-report     # optional: run this platform skill instead
---
Write the prompt here. $ARGUMENTS is replaced by everything after /%s;
$1, $2 … by single words.
`, name, name)
}
