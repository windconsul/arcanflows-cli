package hooks

import "testing"

// A filename the model controls must not be able to break out of its shell
// token in a hook command (PROPOSAL-010 C4).
func TestInterpolateQuotesInjection(t *testing.T) {
	vars := Variables{File: "x.ts; rm -rf .", ProjectRoot: "/tmp/p", Branch: "main", Agent: "a"}
	got := interpolate("git add {{file}}", vars)
	want := `git add 'x.ts; rm -rf .'`
	if got != want {
		t.Errorf("interpolate injection:\n got: %s\nwant: %s", got, want)
	}

	// embedded single quote must be escaped, not terminate the token
	vars2 := Variables{File: "a'b"}
	got2 := interpolate("cat {{file}}", vars2)
	want2 := `cat 'a'\''b'`
	if got2 != want2 {
		t.Errorf("interpolate quote-escape:\n got: %s\nwant: %s", got2, want2)
	}
}
