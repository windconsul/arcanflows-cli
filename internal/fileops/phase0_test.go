package fileops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsProtected(t *testing.T) {
	protected := []string{
		".env", ".env.local", ".env.production", ".envrc",
		"id_rsa", "id_ed25519", "server.key", "cert.pem", "key.p8",
		"config/secrets.yaml", "app/credentials.json", ".npmrc", ".netrc",
		".git/hooks/pre-commit", "sub/.git/config",
	}
	for _, p := range protected {
		if !IsProtected(p) {
			t.Errorf("expected %q to be protected", p)
		}
	}
	allowed := []string{
		"main.go", "src/app.ts", "README.md", "environment.md", "keys.md",
	}
	for _, p := range allowed {
		if IsProtected(p) {
			t.Errorf("expected %q NOT to be protected", p)
		}
	}
}

func TestConfinePath(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	allowOutside = false

	// inside the project — allowed
	for _, p := range []string{"a.txt", "sub/b.txt", "./c.txt"} {
		if _, err := confinedAbs(p); err != nil {
			t.Errorf("expected %q to be allowed, got %v", p, err)
		}
	}
	// escapes — rejected
	for _, p := range []string{"../evil.txt", "../../etc/passwd", "/etc/passwd", "sub/../../out.txt"} {
		if _, err := confinedAbs(p); err == nil {
			t.Errorf("expected %q to be rejected", p)
		}
	}

	// symlink escape: a dir symlink pointing outside must be rejected
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err == nil {
		if _, err := confinedAbs("link/pwn.txt"); err == nil {
			t.Errorf("expected symlink escape 'link/pwn.txt' to be rejected")
		}
	}

	// --allow-outside disables confinement
	allowOutside = true
	if _, err := confinedAbs("../../etc/passwd"); err != nil {
		t.Errorf("expected --allow-outside to permit escape, got %v", err)
	}
	allowOutside = false
}
