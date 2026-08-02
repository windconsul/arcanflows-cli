// Package gitctx makes the local git repository visible to the agent
// (PROPOSAL-010 §6.5, D3: on by default, bounded). Read-only: branch,
// short status and diffstat — enough for the agent to reason about state
// without shipping the whole diff unasked.
package gitctx

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func IsRepo() bool {
	_, err := os.Stat(".git")
	return err == nil
}

func run(args ...string) string {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Context returns the bounded git block for chat context, or "".
func Context() string {
	if !IsRepo() {
		return ""
	}
	branch := run("rev-parse", "--abbrev-ref", "HEAD")
	status := boundLines(run("status", "--short"), 30)
	diffstat := boundLines(run("diff", "--stat"), 20)

	var b strings.Builder
	b.WriteString("\n\n# Git state (read-only context)\n")
	fmt.Fprintf(&b, "branch: %s\n", branch)
	if status != "" {
		b.WriteString("status --short:\n" + status + "\n")
	} else {
		b.WriteString("working tree clean\n")
	}
	if diffstat != "" {
		b.WriteString("diff --stat:\n" + diffstat + "\n")
	}
	return b.String()
}

// Diff returns the staged diff, falling back to the full working diff.
// Bounded: an enormous diff is truncated with a note rather than shipped.
func Diff() (string, bool) {
	staged := true
	d := run("diff", "--cached")
	if d == "" {
		staged = false
		d = run("diff")
	}
	const cap = 60000
	if len(d) > cap {
		d = d[:cap] + "\n…(diff truncated)"
	}
	return d, staged
}

func boundLines(s string, n int) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + fmt.Sprintf("\n…(+%d more lines)", len(lines)-n)
}
