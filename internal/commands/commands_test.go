package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseFrontmatterAndIgnoredKeys(t *testing.T) {
	c, err := Parse("report", []byte("---\ndescription: Weekly report\nargument-hint: \"[week]\"\nagent: sales\nallowed-tools: Read\n---\nReport for $ARGUMENTS\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Description != "Weekly report" || c.ArgumentHint != "[week]" || c.Agent != "sales" || c.Body != "Report for $ARGUMENTS" {
		t.Fatalf("unexpected %+v", c)
	}
	if strings.Join(c.Ignored, ",") != "allowed-tools" {
		t.Fatalf("ignored = %v", c.Ignored)
	}
	if _, err := Parse("x", []byte("---\ndescription: open\nno close")); err == nil {
		t.Fatal("unclosed frontmatter must fail")
	}
	if _, err := Parse("x", []byte("---\ndescription: empty\n---\n")); err == nil {
		t.Fatal("a command with no prompt and no skill must fail")
	}
	if c, err := Parse("x", []byte("\xEF\xBB\xBFplain prompt, no frontmatter\r\n")); err != nil || c.Body != "plain prompt, no frontmatter" {
		t.Fatalf("plain = %+v %v", c, err)
	}
}

func TestRender(t *testing.T) {
	c := &Command{Body: "Compare $1 with $2. Full: $ARGUMENTS. Missing: [$3]"}
	if got := c.Render(`2026-W46 "last week"`); got != `Compare 2026-W46 with last week. Full: 2026-W46 "last week". Missing: []` {
		t.Fatalf("got %q", got)
	}
	plain := &Command{Body: "Summarise the thread."}
	if got := plain.Render("focus on pricing"); got != "Summarise the thread.\n\nfocus on pricing" {
		t.Fatalf("args must be appended when the body has no placeholder: %q", got)
	}
	if got := plain.Render(""); got != "Summarise the thread." {
		t.Fatalf("got %q", got)
	}
}

func TestLoadScopesAndRules(t *testing.T) {
	proj, home := t.TempDir(), t.TempDir()
	pd, ud := filepath.Join(proj, ".arcanflows", "commands"), filepath.Join(home, "commands")
	write(t, pd, "report.md", "Project report $ARGUMENTS")
	write(t, ud, "report.md", "User report")
	write(t, ud, "triage.md", "---\ndescription: Triage\n---\nTriage the inbox")
	write(t, ud, "help.md", "would shadow a builtin")
	write(t, ud, "Bad Name.md", "x")
	write(t, ud, "notes.txt", "not a command")

	cmds, warns := Load(proj, home)
	if len(cmds) != 2 || cmds["report"].Scope != ScopeProject || cmds["report"].Body != "Project report $ARGUMENTS" {
		t.Fatalf("cmds = %v", cmds)
	}
	if cmds["triage"].Scope != ScopeUser || cmds["triage"].Description != "Triage" {
		t.Fatalf("triage = %+v", cmds["triage"])
	}
	if len(warns) != 2 {
		t.Fatalf("want a warning for the builtin and the bad name, got %v", warns)
	}
	if names := Sorted(cmds); names[0].Name != "report" || names[1].Name != "triage" {
		t.Fatalf("sorted = %v", names)
	}
}

func TestSplitInvocation(t *testing.T) {
	n, a := SplitInvocation("/Weekly-Report  2026-W46 extra ")
	if n != "weekly-report" || a != "2026-W46 extra" {
		t.Fatalf("%q %q", n, a)
	}
}

func TestStarterPackParses(t *testing.T) {
	for name, body := range Starter {
		if _, err := Parse(name, []byte(body)); err != nil {
			t.Errorf("starter %s: %v", name, err)
		}
		if Builtins[name] || !validName.MatchString(name) {
			t.Errorf("starter %s: invalid name", name)
		}
	}
}
