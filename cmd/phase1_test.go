package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/arcanflows/cli/internal/api"
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

// PROPOSAL-121 S1: the done event repeats the whole answer; printing it after
// the deltas doubled every reply from ask and run.
func TestPrintAnswerPrintsOnce(t *testing.T) {
	out := captureStdout(t, func() {
		p := printAnswer()
		p(api.StreamChunk{Delta: "Hel"})
		p(api.StreamChunk{Delta: "lo"})
		p(api.StreamChunk{Done: true, Content: "Hello"})
	})
	if out != "Hello" {
		t.Fatalf("streamed answer printed as %q", out)
	}
	out = captureStdout(t, func() {
		p := printAnswer()
		p(api.StreamChunk{Done: true, Content: "Only final"})
	})
	if out != "Only final" {
		t.Fatalf("non-streamed answer printed as %q", out)
	}
}

func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	return string(b)
}

func TestSplitSkillArgs(t *testing.T) {
	in, text := splitSkillArgs(`topic="EV batteries" depth=2 focus on suppliers`)
	if in["topic"] != "EV batteries" || in["depth"] != "2" || text != "focus on suppliers" {
		t.Fatalf("inputs %v text %q", in, text)
	}
	if in, text := splitSkillArgs("no inputs here"); len(in) != 0 || text != "no inputs here" {
		t.Fatalf("inputs %v text %q", in, text)
	}
}

func TestSkillMatchesAndErrorMessage(t *testing.T) {
	sk := &api.Skill{ID: "11111111-2222-3333-4444-555555555555", Slug: "research-brief-1790865620", Name: "Research Brief"}
	for _, ref := range []string{"research-brief", "Research Brief", "RESEARCH-BRIEF-1790865620", "11111111-2222-3333-4444-555555555555"} {
		if !api.SkillMatches(sk, ref) {
			t.Errorf("%q should match", ref)
		}
	}
	if api.SkillMatches(sk, "research") {
		t.Error("a bare prefix without the dash boundary must not match")
	}
	if got := api.ErrorMessage(fmt.Errorf(`API error 422: {"error":"missing required inputs for \"x\": topic","code":"SKILL_RUN_REFUSED"}`)); got != `missing required inputs for "x": topic` {
		t.Fatalf("got %q", got)
	}
}
