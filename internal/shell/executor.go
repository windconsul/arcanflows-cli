package shell

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/arcanflows/cli/internal/render"
)

// maxOutputBytes caps how much command output is buffered in memory, so a
// runaway command (e.g. `yes`, `cat huge.log`) can't OOM the CLI
// (PROPOSAL-010 C13).
const maxOutputBytes = 1 << 20 // 1 MiB

// capWriter buffers up to a cap and discards the rest, but always accepts
// writes so the child process never blocks on a full pipe.
type capWriter struct {
	buf       bytes.Buffer
	cap       int
	truncated bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	if remaining := w.cap - w.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			w.buf.Write(p[:remaining])
			w.truncated = true
		} else {
			w.buf.Write(p)
		}
	} else if len(p) > 0 {
		w.truncated = true
	}
	return len(p), nil
}

// NOTE: BlockedCommands is a best-effort *speed bump*, not a security boundary.
// It's a substring match on a few literal disasters; it does NOT stop a
// determined or creative command (e.g. `rm -rf ~`, `find / -delete`,
// `curl … | sh`). The real safety control is the confirmation prompt below —
// never run this with AutoConfirm on untrusted input (PROPOSAL-010 C12).
var BlockedCommands = []string{
	"rm -rf /",
	"rm -rf /*",
	"sudo rm -rf",
	"mkfs",
	"shutdown",
	"reboot",
	"halt",
	"init 0",
	"init 6",
	"dd if=",
	":(){:|:&};:",
}

// Config holds shell execution settings.
type Config struct {
	Enabled         bool
	TimeoutSeconds  int
	AutoConfirm     bool
	BlockedCommands []string
}

// DefaultConfig returns default shell execution config.
func DefaultConfig() Config {
	return Config{
		Enabled:         true,
		TimeoutSeconds:  60,
		AutoConfirm:     false,
		BlockedCommands: BlockedCommands,
	}
}

// IsBlocked checks if a command matches any blocked pattern.
func IsBlocked(command string, blocked []string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	for _, b := range blocked {
		if strings.Contains(lower, strings.ToLower(b)) {
			return true
		}
	}
	return false
}

// Execute runs a shell command with confirmation and timeout.
func Execute(command string, cfg Config) (string, int, error) {
	if !cfg.Enabled {
		return "", -1, fmt.Errorf("shell execution is disabled")
	}

	if IsBlocked(command, cfg.BlockedCommands) {
		return "", -1, fmt.Errorf("command blocked for safety: %s", command)
	}

	// Confirm (via the shared stdin reader — PROPOSAL-010 C11).
	if !cfg.AutoConfirm {
		fmt.Fprintf(os.Stderr, "\n%s  EXECUTE: %s%s\n", render.Yellow, command, render.Reset)
		if !render.Confirm("Execute this command?") {
			render.Info("Skipped")
			return "", -1, nil
		}
	}

	// Execute with timeout
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = os.Environ()

	// Cap output so a runaway command can't OOM the CLI (C13).
	w := &capWriter{cap: maxOutputBytes}
	cmd.Stdout = w
	cmd.Stderr = w

	fmt.Fprintf(os.Stderr, "%s[Running...]%s\n", render.Dim, render.Reset)

	err := cmd.Run()
	output := w.buf.Bytes()
	exitCode := 0

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			render.Error(fmt.Sprintf("Command timed out after %ds", cfg.TimeoutSeconds))
			return string(output), -1, fmt.Errorf("timeout")
		} else {
			return string(output), -1, err
		}
	}

	outStr := strings.TrimSpace(string(output))
	if w.truncated {
		outStr += fmt.Sprintf("\n%s... [output truncated at %d bytes]%s", render.Dim, maxOutputBytes, render.Reset)
	}

	// Print output
	if outStr != "" {
		fmt.Fprintln(os.Stderr, outStr)
	}

	if exitCode == 0 {
		render.Success(fmt.Sprintf("Command completed (exit code %d)", exitCode))
	} else {
		render.Error(fmt.Sprintf("Command failed (exit code %d)", exitCode))
	}

	return outStr, exitCode, nil
}

// ParseExecuteDirectives extracts EXECUTE: directives from agent response text.
// Leading/trailing markdown emphasis and code ticks are stripped so a model
// that writes `**EXECUTE: npm test**` or `` `EXECUTE: ...` `` still runs
// (PROPOSAL-010 C6).
func ParseExecuteDirectives(text string) []string {
	var commands []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.Trim(strings.TrimSpace(line), "*`")
		trimmed = strings.TrimSpace(trimmed)
		for _, prefix := range []string{"EXECUTE:", "EXECUTE_SILENT:"} {
			if strings.HasPrefix(trimmed, prefix) {
				cmd := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
				// A trailing `**`/backtick may survive if the command itself had
				// no spaces before it; trim those too.
				cmd = strings.TrimSpace(strings.Trim(cmd, "*`"))
				if cmd != "" {
					commands = append(commands, cmd)
				}
				break
			}
		}
	}
	return commands
}
