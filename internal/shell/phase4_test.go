package shell

import "testing"

// Bold/backtick-wrapped EXECUTE directives must still be parsed (C6).
func TestParseExecuteMarkdown(t *testing.T) {
	cases := map[string]string{
		"**EXECUTE: npm test**": "npm test",
		"`EXECUTE: go build`":   "go build",
		"EXECUTE: ls -la":       "ls -la",
	}
	for in, want := range cases {
		got := ParseExecuteDirectives(in)
		if len(got) != 1 || got[0] != want {
			t.Errorf("ParseExecuteDirectives(%q) = %v, want [%q]", in, got, want)
		}
	}
}
