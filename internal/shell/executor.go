package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/arcanflows/cli/internal/render"
)

// Default blocked commands that should never execute.
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

	// Confirm
	if !cfg.AutoConfirm {
		fmt.Fprintf(os.Stderr, "\n%s  EXECUTE: %s%s\n", render.Yellow, command, render.Reset)
		fmt.Fprintf(os.Stderr, "%sExecute this command? [y/n]:%s ", render.Bold, render.Reset)
		var response string
		fmt.Scanln(&response)
		response = strings.ToLower(strings.TrimSpace(response))
		if response != "y" && response != "yes" {
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

	fmt.Fprintf(os.Stderr, "%s[Running...]%s\n", render.Dim, render.Reset)

	output, err := cmd.CombinedOutput()
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
func ParseExecuteDirectives(text string) []string {
	var commands []string
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "EXECUTE:") {
			cmd := strings.TrimPrefix(trimmed, "EXECUTE:")
			cmd = strings.TrimSpace(cmd)
			if cmd != "" {
				commands = append(commands, cmd)
			}
		}
		if strings.HasPrefix(trimmed, "EXECUTE_SILENT:") {
			cmd := strings.TrimPrefix(trimmed, "EXECUTE_SILENT:")
			cmd = strings.TrimSpace(cmd)
			if cmd != "" {
				commands = append(commands, cmd)
			}
		}
	}
	return commands
}
