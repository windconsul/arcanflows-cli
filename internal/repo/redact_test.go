package repo

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	in := strings.Join([]string{
		`AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE`,
		`api_key = "sk-abcdefghijklmnopqrstuvwxyz123456"`,
		`github_token: ghp_1234567890abcdefghijklmnopqrstuvwx`,
		`password = "hunter2hunter2"`,
		`const port = 8080`, // must NOT be redacted
	}, "\n")

	out, n := Redact(in)
	if n < 4 {
		t.Errorf("expected >=4 redactions, got %d\n%s", n, out)
	}
	for _, leak := range []string{"AKIAIOSFODNN7EXAMPLE", "sk-abcdefghijklmnopqrstuvwxyz123456", "ghp_1234567890abcdefghijklmnopqrstuvwx", "hunter2hunter2"} {
		if strings.Contains(out, leak) {
			t.Errorf("secret leaked through redaction: %s", leak)
		}
	}
	if !strings.Contains(out, "const port = 8080") {
		t.Errorf("non-secret line was altered:\n%s", out)
	}
}

func TestRedactPrivateKey(t *testing.T) {
	in := "-----BEGIN RSA PRIVATE KEY-----\nMIIEabc...\n-----END RSA PRIVATE KEY-----"
	out, n := Redact(in)
	if n != 1 || strings.Contains(out, "MIIEabc") {
		t.Errorf("private key not redacted: n=%d out=%q", n, out)
	}
}
