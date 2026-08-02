package rules

import "testing"

func TestSplitDeny(t *testing.T) {
	prose, paths, cmds := splitDeny(`# My rules
Always write tests.

[deny]
path: deploy/**
path: *.pem
command: git push --force

[other]
ignored: yes`)
	if len(paths) != 2 || paths[0] != "deploy/**" || paths[1] != "*.pem" {
		t.Fatalf("paths = %v", paths)
	}
	if len(cmds) != 1 || cmds[0] != "git push --force" {
		t.Fatalf("cmds = %v", cmds)
	}
	if !contains(prose, "Always write tests.") || contains(prose, "deploy/**") {
		t.Fatalf("prose wrong: %q", prose)
	}
}

func TestCheckPath(t *testing.T) {
	r := &Rules{DenyPaths: []string{"deploy/**", "*.pem", "secrets.yaml"}}
	for _, blocked := range []string{"deploy/prod/main.tf", "certs/server.pem", "secrets.yaml", "a/b/secrets.yaml"} {
		if err := r.CheckPath(blocked); err == nil {
			t.Errorf("expected %s blocked", blocked)
		}
	}
	for _, ok := range []string{"src/main.go", "deployment.md", "pemfile.txt"} {
		if err := r.CheckPath(ok); err != nil {
			t.Errorf("expected %s allowed: %v", ok, err)
		}
	}
}

func TestCheckCommand(t *testing.T) {
	r := &Rules{DenyCommands: []string{"git push --force", "terraform destroy"}}
	if err := r.CheckCommand("git push --force origin main"); err == nil {
		t.Error("expected force push blocked")
	}
	if err := r.CheckCommand("git push origin main"); err != nil {
		t.Errorf("expected normal push allowed: %v", err)
	}
}

func TestNilSafe(t *testing.T) {
	var r *Rules
	if r.CheckPath("x") != nil || r.CheckCommand("y") != nil {
		t.Error("nil rules must allow everything")
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (s == sub || len(s) > 0 && (stringsIndex(s, sub) >= 0)) }
func stringsIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
