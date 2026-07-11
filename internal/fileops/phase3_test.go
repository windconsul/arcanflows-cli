package fileops

import (
	"os"
	"strings"
	"testing"
)

func chdirTemp(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	old, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(old) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	allowOutside = false
}

func TestReplaceInFile(t *testing.T) {
	chdirTemp(t)
	os.WriteFile("f.txt", []byte("line a\nOLD value\nline c\n"), 0644)

	// found → applied
	if err := ReplaceInFile("f.txt", []ReplaceEdit{{Search: "OLD value", Replace: "NEW value"}}, true); err != nil {
		t.Fatalf("replace failed: %v", err)
	}
	got, _ := os.ReadFile("f.txt")
	if string(got) != "line a\nNEW value\nline c\n" {
		t.Errorf("unexpected content: %q", string(got))
	}

	// not found → clear error
	if err := ReplaceInFile("f.txt", []ReplaceEdit{{Search: "ZZZ", Replace: "x"}}, true); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}

	// ambiguous → clear error
	os.WriteFile("amb.txt", []byte("dup\ndup\n"), 0644)
	if err := ReplaceInFile("amb.txt", []ReplaceEdit{{Search: "dup", Replace: "x"}}, true); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Errorf("expected ambiguous error, got %v", err)
	}
}

func TestEditPreservesModeAtomically(t *testing.T) {
	chdirTemp(t)
	os.WriteFile("script.sh", []byte("#!/bin/sh\necho hi\n"), 0755)
	if err := EditFile("script.sh", "#!/bin/sh\necho bye\n", true); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat("script.sh")
	if info.Mode().Perm() != 0755 {
		t.Errorf("expected mode 0755 preserved, got %v", info.Mode().Perm())
	}
}

func TestUndoJournal(t *testing.T) {
	chdirTemp(t)

	// create → undo removes the file
	if err := CreateFile("new.txt", "hello", true); err != nil {
		t.Fatal(err)
	}
	if err := UndoLast(); err != nil {
		t.Fatalf("undo create: %v", err)
	}
	if _, err := os.Stat("new.txt"); !os.IsNotExist(err) {
		t.Errorf("expected new.txt removed after undo")
	}

	// edit → undo restores original content
	os.WriteFile("e.txt", []byte("original\n"), 0644)
	if err := EditFile("e.txt", "changed\n", true); err != nil {
		t.Fatal(err)
	}
	if err := UndoLast(); err != nil {
		t.Fatalf("undo edit: %v", err)
	}
	if got, _ := os.ReadFile("e.txt"); string(got) != "original\n" {
		t.Errorf("expected original restored, got %q", string(got))
	}
}

func TestDiffLinesInsertion(t *testing.T) {
	// A single inserted line must show exactly one '+' and zero '-', not
	// "everything after changed" (the old index-compare bug).
	ops := diffLines([]string{"a", "b", "c"}, []string{"a", "X", "b", "c"})
	var plus, minus int
	for _, o := range ops {
		if o.kind == '+' {
			plus++
			if o.text != "X" {
				t.Errorf("unexpected added line %q", o.text)
			}
		}
		if o.kind == '-' {
			minus++
		}
	}
	if plus != 1 || minus != 0 {
		t.Errorf("expected 1 add / 0 remove, got %d add / %d remove", plus, minus)
	}
}

func TestParseReplaceOperation(t *testing.T) {
	text := strings.Join([]string{
		"Sure:",
		"--- Replace in: src/app.go ---",
		"<<<<<<< SEARCH",
		"foo := 1",
		"=======",
		"foo := 2",
		">>>>>>> REPLACE",
		"--- End replace ---",
	}, "\n")
	ops := ParseOperations(text)
	if len(ops) != 1 || ops[0].Type != "replace" || ops[0].Path != "src/app.go" {
		t.Fatalf("unexpected ops: %+v", ops)
	}
	if len(ops[0].Replacements) != 1 || ops[0].Replacements[0].Search != "foo := 1" || ops[0].Replacements[0].Replace != "foo := 2" {
		t.Errorf("unexpected replacements: %+v", ops[0].Replacements)
	}
}
