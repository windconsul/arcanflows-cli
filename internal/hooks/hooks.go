package hooks

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/arcanflows/cli/internal/render"
	"gopkg.in/yaml.v3"
)

// HookEvent represents when a hook fires.
type HookEvent string

const (
	PreChat    HookEvent = "pre_chat"
	PostChat   HookEvent = "post_chat"
	PreEdit    HookEvent = "pre_edit"
	PostEdit   HookEvent = "post_edit"
	PreCreate  HookEvent = "pre_create"
	PostCreate HookEvent = "post_create"
	PreDelete  HookEvent = "pre_delete"
	PostDelete HookEvent = "post_delete"
	PreRun     HookEvent = "pre_run"
	PostRun    HookEvent = "post_run"
	PreCommit  HookEvent = "pre_commit"
	PostCommit HookEvent = "post_commit"
)

// Hook represents a single hook command.
type Hook struct {
	Command     string `yaml:"command"`
	Description string `yaml:"description,omitempty"`
	ContinueOn  string `yaml:"continue_on,omitempty"` // "error" = continue even if hook fails
	Silent      bool   `yaml:"silent,omitempty"`       // suppress output
}

// HooksConfig is the top-level hooks.yaml structure.
type HooksConfig struct {
	PreChat    []Hook `yaml:"pre_chat,omitempty"`
	PostChat   []Hook `yaml:"post_chat,omitempty"`
	PreEdit    []Hook `yaml:"pre_edit,omitempty"`
	PostEdit   []Hook `yaml:"post_edit,omitempty"`
	PreCreate  []Hook `yaml:"pre_create,omitempty"`
	PostCreate []Hook `yaml:"post_create,omitempty"`
	PreDelete  []Hook `yaml:"pre_delete,omitempty"`
	PostDelete []Hook `yaml:"post_delete,omitempty"`
	PreRun     []Hook `yaml:"pre_run,omitempty"`
	PostRun    []Hook `yaml:"post_run,omitempty"`
	PreCommit  []Hook `yaml:"pre_commit,omitempty"`
	PostCommit []Hook `yaml:"post_commit,omitempty"`
}

// Variables holds template variables for hook command interpolation.
type Variables struct {
	File        string // path of file being operated on
	ProjectRoot string // project root directory
	Branch      string // current git branch
	Agent       string // current agent name
}

// Manager loads and executes hooks.
type Manager struct {
	config    *HooksConfig
	configDir string
}

// DefaultConfigPath returns the default hooks.yaml path.
func DefaultConfigPath() string {
	return filepath.Join(".arcanflows", "hooks.yaml")
}

// Load reads hooks.yaml from the project directory.
// Returns a Manager even if no hooks file exists (noop manager).
func Load() *Manager {
	m := &Manager{}

	path := DefaultConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return m // no hooks file — all Run() calls are noops
	}

	var cfg HooksConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		render.Warning(fmt.Sprintf("Invalid hooks.yaml: %v", err))
		return m
	}

	m.config = &cfg
	m.configDir = filepath.Dir(path)
	return m
}

// Run executes all hooks for the given event.
// Returns the combined output of pre-hooks (useful for injecting into context).
// Returns error only if a non-continue-on-error hook fails.
func (m *Manager) Run(event HookEvent, vars Variables) (string, error) {
	if m.config == nil {
		return "", nil
	}

	hooks := m.getHooks(event)
	if len(hooks) == 0 {
		return "", nil
	}

	var outputs []string

	for _, hook := range hooks {
		cmd := interpolate(hook.Command, vars)

		if !hook.Silent {
			fmt.Fprintf(os.Stderr, "%s  [hook:%s] %s%s\n", render.Dim, event, cmd, render.Reset)
		}

		output, err := execute(cmd)

		if output != "" && !hook.Silent {
			// Show output for non-silent hooks
			lines := strings.Split(strings.TrimSpace(output), "\n")
			for _, line := range lines {
				fmt.Fprintf(os.Stderr, "%s  │ %s%s\n", render.Dim, line, render.Reset)
			}
		}

		if output != "" {
			outputs = append(outputs, output)
		}

		if err != nil {
			if hook.ContinueOn == "error" {
				if !hook.Silent {
					render.Warning(fmt.Sprintf("Hook failed (continuing): %v", err))
				}
				continue
			}
			return strings.Join(outputs, "\n"), fmt.Errorf("hook '%s' failed: %w", hook.Description, err)
		}
	}

	return strings.Join(outputs, "\n"), nil
}

// HasHooks returns true if any hooks are configured for the given event.
func (m *Manager) HasHooks(event HookEvent) bool {
	if m.config == nil {
		return false
	}
	return len(m.getHooks(event)) > 0
}

// Count returns the total number of hooks configured.
func (m *Manager) Count() int {
	if m.config == nil {
		return 0
	}
	return len(m.config.PreChat) + len(m.config.PostChat) +
		len(m.config.PreEdit) + len(m.config.PostEdit) +
		len(m.config.PreCreate) + len(m.config.PostCreate) +
		len(m.config.PreDelete) + len(m.config.PostDelete) +
		len(m.config.PreRun) + len(m.config.PostRun) +
		len(m.config.PreCommit) + len(m.config.PostCommit)
}

// Summary returns a human-readable summary of configured hooks.
func (m *Manager) Summary() string {
	if m.config == nil || m.Count() == 0 {
		return "No hooks configured"
	}

	var parts []string
	events := []struct {
		name  string
		hooks []Hook
	}{
		{"pre_chat", m.config.PreChat},
		{"post_chat", m.config.PostChat},
		{"pre_edit", m.config.PreEdit},
		{"post_edit", m.config.PostEdit},
		{"pre_create", m.config.PreCreate},
		{"post_create", m.config.PostCreate},
		{"pre_delete", m.config.PreDelete},
		{"post_delete", m.config.PostDelete},
		{"pre_run", m.config.PreRun},
		{"post_run", m.config.PostRun},
		{"pre_commit", m.config.PreCommit},
		{"post_commit", m.config.PostCommit},
	}

	for _, e := range events {
		if len(e.hooks) > 0 {
			parts = append(parts, fmt.Sprintf("%s(%d)", e.name, len(e.hooks)))
		}
	}

	return strings.Join(parts, ", ")
}

// getHooks returns the hooks for a given event.
func (m *Manager) getHooks(event HookEvent) []Hook {
	switch event {
	case PreChat:
		return m.config.PreChat
	case PostChat:
		return m.config.PostChat
	case PreEdit:
		return m.config.PreEdit
	case PostEdit:
		return m.config.PostEdit
	case PreCreate:
		return m.config.PreCreate
	case PostCreate:
		return m.config.PostCreate
	case PreDelete:
		return m.config.PreDelete
	case PostDelete:
		return m.config.PostDelete
	case PreRun:
		return m.config.PreRun
	case PostRun:
		return m.config.PostRun
	case PreCommit:
		return m.config.PreCommit
	case PostCommit:
		return m.config.PostCommit
	default:
		return nil
	}
}

// shellQuote wraps a value in single quotes (escaping embedded single quotes)
// so it becomes a single inert shell token. Hook commands are user-authored
// shell strings that legitimately use pipes/redirects, so we can't drop `sh -c`
// — but the interpolated variables are agent-controlled (a filename the model
// chose), so they MUST be quoted or a name like `x; rm -rf .` injects
// (PROPOSAL-010 C4).
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// interpolate replaces {{variable}} placeholders in a command string, quoting
// each value so it can't break out of its shell token.
func interpolate(command string, vars Variables) string {
	r := strings.NewReplacer(
		"{{file}}", shellQuote(vars.File),
		"{{project_root}}", shellQuote(vars.ProjectRoot),
		"{{branch}}", shellQuote(vars.Branch),
		"{{agent}}", shellQuote(vars.Agent),
	)
	return r.Replace(command)
}

// hookMaxOutputBytes caps hook output buffered in memory (C13).
const hookMaxOutputBytes = 1 << 20 // 1 MiB

// execute runs a shell command and returns its output (output-capped).
func execute(command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = os.Environ()

	var buf bytes.Buffer
	w := &limitedWriter{buf: &buf, cap: hookMaxOutputBytes}
	cmd.Stdout = w
	cmd.Stderr = w

	err := cmd.Run()
	out := buf.String()
	if w.truncated {
		out += fmt.Sprintf("\n... [output truncated at %d bytes]", hookMaxOutputBytes)
	}
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("hook timed out after 30s")
	}
	if err != nil {
		return out, err
	}
	return strings.TrimSpace(out), nil
}

// limitedWriter buffers up to cap and discards the rest, always accepting
// writes so the child never blocks.
type limitedWriter struct {
	buf       *bytes.Buffer
	cap       int
	truncated bool
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if remaining := w.cap - w.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			w.buf.Write(p[:remaining])
			w.truncated = true
		} else {
			w.buf.Write(p)
		}
	} else if len(p) > 0 {
		w.truncated = true
	}
	return len(p), nil
}

// GenerateTemplate returns a starter hooks.yaml template.
func GenerateTemplate() string {
	return `# ArcanFlows Hooks Configuration
# Hooks run automatically before/after CLI operations.
# Available variables: {{file}}, {{project_root}}, {{branch}}, {{agent}}

# Run before each chat session starts
# pre_chat:
#   - command: "git status --short"
#     description: "Include git status in context"

# Run after a file is edited by the agent
# post_edit:
#   - command: "prettier --write {{file}}"
#     description: "Auto-format after edits"
#   - command: "eslint {{file}}"
#     description: "Lint after edits"
#     continue_on: error

# Run after a new file is created
# post_create:
#   - command: "git add {{file}}"
#     description: "Auto-stage new files"

# Run before committing
# pre_commit:
#   - command: "npm test"
#     description: "Run tests before commit"
`
}
