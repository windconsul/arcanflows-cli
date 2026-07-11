package fileops

import (
	"strings"
	"testing"
)

// A model that wraps directives in markdown bold/backticks must still trigger
// the operation (PROPOSAL-010 C6).
func TestParseMarkdownWrappedDirectives(t *testing.T) {
	text := strings.Join([]string{
		"Here you go:",
		"**--- Create file: a.txt ---**",
		"hello",
		"**--- End file ---**",
	}, "\n")
	ops := ParseOperations(text)
	if len(ops) != 1 || ops[0].Type != "create" || ops[0].Path != "a.txt" || ops[0].Content != "hello" {
		t.Fatalf("bold-wrapped create not parsed: %+v", ops)
	}

	// Backtick-wrapped replace directive.
	text2 := strings.Join([]string{
		"`--- Replace in: b.go ---`",
		"<<<<<<< SEARCH",
		"x",
		"=======",
		"y",
		">>>>>>> REPLACE",
		"`--- End replace ---`",
	}, "\n")
	ops2 := ParseOperations(text2)
	if len(ops2) != 1 || ops2[0].Type != "replace" || ops2[0].Path != "b.go" {
		t.Fatalf("backtick-wrapped replace not parsed: %+v", ops2)
	}
}
