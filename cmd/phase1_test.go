package cmd

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/arcanflows/cli/internal/hooks"
	"github.com/arcanflows/cli/internal/render"
)

// The agent loop is only useful if the outcome of the actions it requests is
// captured and fed back. This exercises applyAgentActions end-to-end: a file
// gets created and a shell command runs, and the returned summary carries the
// real result (exit code + output) that the next turn sends to the agent.
func TestApplyAgentActionsCapturesResults(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	// Auto-approve every confirmation via the shared reader.
	render.SetInputReader(bufio.NewReader(strings.NewReader("y\ny\ny\ny\ny\n")))

	response := strings.Join([]string{
		"Here you go.",
		"--- Create file: hello.txt ---",
		"hello world",
		"--- End file ---",
		"EXECUTE: printf marker-output",
	}, "\n")

	summary, meaningful, _, _ := applyAgentActions(response, hooks.Load(), &hooks.Variables{})

	if meaningful != 2 {
		t.Fatalf("expected 2 meaningful actions, got %d (summary: %s)", meaningful, summary)
	}
	if data, err := os.ReadFile("hello.txt"); err != nil || strings.TrimSpace(string(data)) != "hello world" {
		t.Errorf("file not created correctly: err=%v content=%q", err, string(data))
	}
	if !strings.Contains(summary, "create hello.txt: ok") {
		t.Errorf("summary missing create result:\n%s", summary)
	}
	if !strings.Contains(summary, "exit 0") || !strings.Contains(summary, "marker-output") {
		t.Errorf("summary missing captured shell output (the loop's whole point):\n%s", summary)
	}
}

func TestBuildToolResultMessageRestatesGoal(t *testing.T) {
	msg := buildToolResultMessage("build the thing", "- $ go build: exit 1")
	if !strings.Contains(msg, "exit 1") {
		t.Errorf("missing summary")
	}
	if !strings.Contains(msg, "Original request, for reference: build the thing") {
		t.Errorf("missing goal recap (needed to survive the rehydration window):\n%s", msg)
	}

	long := strings.Repeat("x", 400)
	msg2 := buildToolResultMessage(long, "ok")
	if !strings.Contains(msg2, "…") {
		t.Errorf("expected long original request to be truncated")
	}
}
